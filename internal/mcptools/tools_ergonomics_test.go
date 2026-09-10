package mcptools

// Ergonomics contract additions (0.2.0 line): candidates delivered once with
// source facts, verbose=false, validate_only, reuse_existing, and Chunk
// receipt section anchors. Every test here proves a guard that can go red.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// declareEScaleThresholds gives the fixture Meta numeric E ranges so a
// candidate's scale can be derived; the Baseline is re-snapshotted exactly as
// buildVolumeRepo does.
func declareEScaleThresholds(t *testing.T, root string) {
	t.Helper()
	metaPath := filepath.Join(root, "aoci.meta.txt")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "#E Scale: L M S T\n", "#E Scale: L>400 M200-400 S100-200 T<100\n", 1)
	if text == string(raw) {
		t.Fatal("fixture Meta has no E Scale line to replace")
	}
	if err := os.WriteFile(metaPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
}

func writeFixtureSource(t *testing.T, root, rel string, lines int) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString("package fixture\n")
	for line := 2; line <= lines; line++ {
		fmt.Fprintf(&builder, "// line %d\n", line)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureDigest maps every file under root, formal and hidden alike, to its
// content digest: the oracle for "changed no byte".
func fixtureDigest(t *testing.T, root string) map[string]string {
	t.Helper()
	digests := map[string]string{}
	err := filepath.WalkDir(root, func(current string, entry iofs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(current)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(root, current)
		digests[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return digests
}

func digestDifference(before, after map[string]string) []string {
	changed := []string{}
	for rel, digest := range after {
		if before[rel] != digest {
			changed = append(changed, rel)
		}
	}
	for rel := range before {
		if _, present := after[rel]; !present {
			changed = append(changed, "-"+rel)
		}
	}
	return changed
}

func TestMaintainDeliversCandidatesOnceWithSourceFacts(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	declareEScaleThresholds(t, root)
	writeFixtureSource(t, root, "pkg/medium.go", 250)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	text := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var raw struct {
		Candidates []map[string]any `json:"candidates"`
		CodePlan   map[string]any   `json:"code_plan"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatal(err)
	}
	if _, twice := raw.CodePlan["candidates"]; twice {
		t.Fatalf("code_plan repeats the candidate list: %v", raw.CodePlan)
	}
	for _, key := range []string{"plan_id", "batch_id", "composite_identity", "code_volume_sha256", "next_action"} {
		if value, _ := raw.CodePlan[key].(string); value == "" {
			t.Fatalf("code_plan lost %s: %v", key, raw.CodePlan)
		}
	}
	want := map[string]struct {
		lines int
		scale string
	}{"main.go": {3, "T"}, "pkg/medium.go": {250, "M"}}
	if len(raw.Candidates) != len(want) {
		t.Fatalf("expected %d candidates: %v", len(want), raw.Candidates)
	}
	for _, candidate := range raw.Candidates {
		rel, _ := candidate["path"].(string)
		expect, known := want[rel]
		lines, _ := candidate["source_lines"].(float64)
		counted, err := fs.CountFileLines(filepath.Join(root, filepath.FromSlash(rel)))
		if !known || err != nil || int(lines) != expect.lines || counted != expect.lines || candidate["scale"] != expect.scale {
			t.Fatalf("candidate source facts are wrong for %q: %v (want lines=%d scale=%s)", rel, candidate, expect.lines, expect.scale)
		}
	}
	var decoded volumeMaintainResult
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.CodePlan == nil || decoded.CodePlan.BatchID != raw.CodePlan["batch_id"] || len(decoded.CodePlan.Candidates) != 0 {
		t.Fatalf("decoded plan view diverged from the wire: %+v", decoded.CodePlan)
	}
	for _, candidate := range decoded.Candidates {
		if candidate.BatchID != decoded.CodePlan.BatchID {
			t.Fatalf("candidate batch identity diverged from code_plan: %+v", candidate)
		}
	}
}

func TestMaintainVerboseFalseOmitsOnlyRepeatedBoilerplate(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeFixtureSource(t, root, "pkg/a.go", 5)
	session := connectMCPClient(t, root)
	full := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	brief := callVolumeTool(t, session, "aoci_maintain", map[string]any{"verbose": false})
	explicit := callVolumeTool(t, session, "aoci_maintain", map[string]any{"verbose": true})
	var verbose, compact, again volumeMaintainResult
	for _, pair := range []struct {
		text string
		into *volumeMaintainResult
	}{{full, &verbose}, {brief, &compact}, {explicit, &again}} {
		if err := json.Unmarshal([]byte(pair.text), pair.into); err != nil {
			t.Fatal(err)
		}
	}
	if verbose.Compact || again.Compact || !compact.Compact {
		t.Fatalf("compact marker is wrong: default=%v explicit=%v brief=%v", verbose.Compact, again.Compact, compact.Compact)
	}
	if len(verbose.Instructions) == 0 || verbose.AuthoringMeta == "" || len(again.Instructions) != len(verbose.Instructions) {
		t.Fatalf("verbose responses lost the authoring contract: %d/%d", len(verbose.Instructions), len(again.Instructions))
	}
	var rawBrief map[string]json.RawMessage
	if err := json.Unmarshal([]byte(brief), &rawBrief); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"instructions", "authoring_meta"} {
		if _, present := rawBrief[key]; present {
			t.Fatalf("verbose=false still carries %s", key)
		}
	}
	wantTotal := verbose.Sets.ReviewTotal
	if wantTotal == 0 {
		wantTotal = len(verbose.Sets.Review)
	}
	if len(compact.Sets.Review) != 0 || compact.Sets.ReviewTotal != wantTotal {
		t.Fatalf("review closure count is wrong: sample=%v total=%d want=%d", compact.Sets.Review, compact.Sets.ReviewTotal, wantTotal)
	}
	if !reflect.DeepEqual(verbose.Candidates, compact.Candidates) || !reflect.DeepEqual(verbose.CodePlan, compact.CodePlan) ||
		verbose.Batch != compact.Batch || !reflect.DeepEqual(verbose.Sets.Write, compact.Sets.Write) ||
		!reflect.DeepEqual(verbose.Sets.Guard, compact.Sets.Guard) || verbose.Status != compact.Status ||
		verbose.NextAction != compact.NextAction || verbose.Receipt.ScopeIdentity != compact.Receipt.ScopeIdentity {
		t.Fatalf("verbose=false changed something the model acts on:\n%s\n%s", full, brief)
	}
	if len(brief) >= len(full) {
		t.Fatalf("compact response is not smaller: %d >= %d", len(brief), len(full))
	}
}

func TestUpdateEntryValidateOnlyWritesNothing(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	writeFixtureSource(t, root, "pkg/a.go", 5)
	writeFixtureSource(t, root, "pkg/b.go", 6)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if len(maintain.Candidates) != 2 {
		t.Fatalf("expected two candidates: %+v", maintain.Candidates)
	}
	before := fixtureDigest(t, root)

	valid := codeBatchArguments(maintain)
	valid["validate_only"] = true
	verdict := applyVolumeBatch(t, session, valid)
	if verdict.Status != autoStatusValidated || !verdict.ValidateOnly || verdict.Applied != 0 || verdict.Remaining != 2 ||
		verdict.FormalWritesStarted || verdict.Aligned || verdict.FindingCount != 0 || !verdict.PreserveOtherCandidates ||
		verdict.NextAction != "resubmit_same_batch_without_validate_only" {
		t.Fatalf("validate_only verdict is wrong: %+v", verdict)
	}
	if changed := digestDifference(before, fixtureDigest(t, root)); len(changed) != 0 {
		t.Fatalf("validate_only changed repository bytes: %v", changed)
	}

	rejected := codeBatchArguments(maintain)
	rejected["validate_only"] = true
	entries := rejected["entries"].([]map[string]any)
	entries[1]["new_entry"] = filepath.Base(maintain.Candidates[1].Path) +
		"[CD5S]: F:implement fixture behavior | R:- | A:- | S:" + strings.Repeat("x", 260)
	repair := applyVolumeBatch(t, session, rejected)
	if repair.Status != autoStatusRepairRequired || !repair.ValidateOnly || repair.Applied != 0 || repair.FormalWritesStarted ||
		len(repair.Findings) == 0 || !repair.PreserveOtherCandidates {
		t.Fatalf("validate_only did not report the S quota repair: %+v", repair)
	}
	if finding := repair.Findings[0]; finding.CandidateIndex != 2 || finding.Field != "S" {
		t.Fatalf("repair finding is not the over-quota S of candidate 2: %+v", finding)
	}
	if changed := digestDifference(before, fixtureDigest(t, root)); len(changed) != 0 {
		t.Fatalf("rejected validate_only changed repository bytes: %v", changed)
	}

	applied := applyVolumeBatch(t, session, codeBatchArguments(maintain))
	if applied.Status != autoStatusApplied || applied.Applied != 2 || !applied.Aligned || applied.ValidateOnly {
		t.Fatalf("the validated bytes did not apply for real: %+v", applied)
	}
	if changed := digestDifference(before, fixtureDigest(t, root)); len(changed) == 0 {
		t.Fatal("the real Apply changed no byte")
	}
}

func TestUpdateEntryReuseExistingResubmitsCurrentBytes(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	original, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\n// drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if len(maintain.Candidates) != 1 || maintain.Candidates[0].Change != cognition.ImpactChangeUpdate || maintain.Candidates[0].ExistingEntry == "" {
		t.Fatalf("expected one stale update candidate: %+v", maintain.Candidates)
	}
	candidate := maintain.Candidates[0]
	submission := func(extra map[string]any) map[string]any {
		item := map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256, "candidate_id": candidate.CandidateID}
		for key, value := range extra {
			item[key] = value
		}
		return map[string]any{"code_batch_id": maintain.CodePlan.BatchID, "entries": []map[string]any{item}}
	}

	conflict := applyVolumeBatch(t, session, submission(map[string]any{"reuse_existing": true, "new_entry": candidate.ExistingEntry}))
	if conflict.Status != autoStatusRepairRequired || len(conflict.Findings) != 1 || conflict.FormalWritesStarted ||
		conflict.Findings[0].RuleCode != "reuse_existing_conflicts_with_new_entry" || conflict.Findings[0].CandidateIndex != 1 ||
		conflict.Findings[0].CanonicalObjectIdentity != candidate.ObjectRef {
		t.Fatalf("reuse_existing with new_entry was not a repair finding: %+v", conflict)
	}

	// The planner itself resolves the bytes from its own preimage, so a direct
	// preview through the atomic entry point sees the current line.
	previewed, previewFail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{Path: candidate.Path, SourceSHA256: candidate.SourceSHA256,
		CandidateID: candidate.CandidateID, BatchID: maintain.CodePlan.BatchID, ReuseExisting: true}}, ledger.SourceAgent, true)
	if previewFail != nil || previewed == nil || !previewed.DryRun || len(previewed.Items) != 1 || previewed.Items[0].Rel != candidate.Path {
		t.Fatalf("planner did not resolve reuse_existing: fail=%+v outcome=%+v", previewFail, previewed)
	}
	// The schema only lets reuse_existing stand in for new_entry when it is
	// literally true.
	if rejected, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_update_entry", Arguments: submission(map[string]any{"reuse_existing": false})}); err == nil && !rejected.IsError {
		t.Fatalf("reuse_existing:false without new_entry was accepted: %s", resText(t, rejected))
	}
	reused := applyVolumeBatch(t, session, submission(map[string]any{"reuse_existing": true}))
	if reused.Status != autoStatusApplied || !reused.Aligned || reused.Metrics.DuplicateApplies != 1 || reused.Remaining != 0 {
		t.Fatalf("reuse_existing did not take the duplicate-apply path: %+v", reused)
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("reuse_existing rewrote the Code Volume:\n%s\n%s", original, after)
	}
	if follow := maintainVolumeBatch(t, session); !follow.Aligned || len(follow.Candidates) != 0 {
		t.Fatalf("the Baseline did not advance after reuse_existing: %+v", follow)
	}

	writeFixtureSource(t, root, "pkg/new.go", 4)
	created := maintainVolumeBatch(t, session)
	if len(created.Candidates) != 1 || created.Candidates[0].Change != cognition.ImpactChangeCreate {
		t.Fatalf("expected one create candidate: %+v", created.Candidates)
	}
	fresh := created.Candidates[0]
	missing := applyVolumeBatch(t, session, map[string]any{"path": fresh.Path, "source_sha256": fresh.SourceSHA256, "reuse_existing": true})
	if missing.Status != autoStatusRepairRequired || len(missing.Findings) != 1 || missing.FormalWritesStarted ||
		missing.Findings[0].RuleCode != "reuse_existing_requires_existing_entry" || missing.Findings[0].Path != fresh.Path {
		t.Fatalf("reuse_existing without an Entry was not a repair finding: %+v", missing)
	}
}

func TestOverviewChunkReceiptsCarrySectionAnchors(t *testing.T) {
	var source strings.Builder
	source.WriteString("#AOCI-CLI Complete Index\n#Locale: en-US\n")
	padding := strings.Repeat("x", 150)
	sections := []struct {
		dir   string
		count int
	}{{"src", 40}, {"src/api", 30}, {"docs", 0}, {"internal/core", 50}, {"tools", 20}}
	ordinal := 0
	for _, section := range sections {
		fmt.Fprintf(&source, "===Section /repo/%s/===\n", section.dir)
		for i := 0; i < section.count; i++ {
			ordinal++
			fmt.Fprintf(&source, "file-%03d.go[IM7S]: F:preserve object %03d | R:- | A:- | S:%s\n", ordinal, ordinal, padding)
		}
	}
	document, warnings := index.Parse(source.String())
	if len(warnings) != 0 {
		t.Fatalf("synthetic index warnings: %+v", warnings)
	}
	index.ResolveRelPaths(document, "/repo")

	type anchorReceipt struct {
		FirstOrdinal int                     `json:"first_entry_ordinal"`
		LastOrdinal  int                     `json:"last_entry_ordinal"`
		NextCursor   string                  `json:"next_cursor"`
		Completed    bool                    `json:"completed"`
		ChunkCount   int                     `json:"chunk_count"`
		Anchors      []overviewSectionAnchor `json:"section_anchors"`
	}
	cursor := ""
	seen := []overviewSectionAnchor{}
	var body strings.Builder
	chunkCount := 0
	for calls := 0; calls < 50; calls++ {
		rendered := renderLegacyOverviewForTest(t, "/repo", "test", source.String(), document, overviewIn{Cursor: cursor}, 4000)
		parts := strings.SplitN(rendered.Output, "\n"+overviewChunkBodyMarker+"\n", 2)
		if len(parts) != 2 {
			t.Fatalf("chunk body marker missing: %s", rendered.Output)
		}
		var receipt anchorReceipt
		if err := json.Unmarshal([]byte(parts[0]), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Anchors == nil {
			t.Fatalf("chunk receipt lacks section_anchors: %s", parts[0])
		}
		var rawReceipt struct {
			Anchors []map[string]any `json:"section_anchors"`
		}
		if err := json.Unmarshal([]byte(parts[0]), &rawReceipt); err != nil {
			t.Fatal(err)
		}
		for _, anchor := range rawReceipt.Anchors {
			for _, key := range []string{"section", "directory", "first_entry_ordinal", "entry_count"} {
				if _, present := anchor[key]; !present {
					t.Fatalf("anchor lacks %s: %v", key, anchor)
				}
			}
		}
		for _, anchor := range receipt.Anchors {
			if anchor.EntryCount > 0 && (anchor.FirstEntryOrdinal < receipt.FirstOrdinal || anchor.FirstEntryOrdinal > receipt.LastOrdinal+1) {
				t.Fatalf("anchor ordinal outside its chunk: %+v in %+v", anchor, receipt)
			}
		}
		seen = append(seen, receipt.Anchors...)
		body.WriteString(parts[1])
		chunkCount = receipt.ChunkCount
		if receipt.Completed {
			break
		}
		cursor = receipt.NextCursor
	}
	if chunkCount < 3 {
		t.Fatalf("fixture must span several chunks, got %d", chunkCount)
	}
	markers := []string{}
	for _, line := range strings.Split(body.String(), "\n") {
		if name, ok := index.SectionMarker(line); ok {
			markers = append(markers, name)
		}
	}
	if len(markers) != len(sections) || len(seen) != len(markers) {
		t.Fatalf("anchors do not cover the delivered markers once: markers=%v anchors=%+v", markers, seen)
	}
	entryOrdinal := 0
	for position, section := range document.Sections {
		anchor := seen[position]
		if anchor.Section != markers[position] || anchor.EntryCount != len(section.Entries) {
			t.Fatalf("anchor %d disagrees with the parsed Section: %+v vs %q (%d entries)", position+1, anchor, section.HeaderLine, len(section.Entries))
		}
		if len(section.Entries) == 0 {
			if anchor.FirstEntryOrdinal != 0 || anchor.Directory != "" {
				t.Fatalf("empty Section anchor claims an Entry: %+v", anchor)
			}
			continue
		}
		if anchor.FirstEntryOrdinal != entryOrdinal+1 || anchor.Directory != path.Dir(section.Entries[0].RelPath) {
			t.Fatalf("anchor %d ordinal or directory is wrong: %+v (want ordinal %d, dir %s)", position+1, anchor, entryOrdinal+1, path.Dir(section.Entries[0].RelPath))
		}
		entryOrdinal += len(section.Entries)
	}
}

func TestOverviewSectionAnchorsCrossChunkBoundary(t *testing.T) {
	text := "START\n===A /repo/a/===\na1[X]: F\na2[X]: F\n===B /repo/b/===\nb1[X]: F\n===C /repo/c/===\n"
	contentStart := len("START\n")
	offset := func(needle string) int { return strings.Index(text, needle) }
	sequence := []overviewChallengeTarget{
		{ContentOffset: offset("a1[") - contentStart, ObjectIdentity: "a/a1"},
		{ContentOffset: offset("a2[") - contentStart, ObjectIdentity: "a/a2"},
		{ContentOffset: offset("b1[") - contentStart, ObjectIdentity: "code:b/b1"},
	}
	// Chunk 1 ends right after the B marker line: B's first Entry is in chunk 2.
	first := overviewChunkSpan{Start: contentStart, End: offset("b1["), FirstOrdinal: 1, LastOrdinal: 2}
	second := overviewChunkSpan{Start: offset("b1["), End: len(text), FirstOrdinal: 3, LastOrdinal: 3}
	wantFirst := []overviewSectionAnchor{
		{Section: "A /repo/a/", Directory: "a", FirstEntryOrdinal: 1, EntryCount: 2},
		{Section: "B /repo/b/", Directory: "b", FirstEntryOrdinal: 3, EntryCount: 1},
	}
	wantSecond := []overviewSectionAnchor{{Section: "C /repo/c/", FirstEntryOrdinal: 0, EntryCount: 0}}
	if got := overviewSectionAnchors(text, contentStart, sequence, first); !reflect.DeepEqual(got, wantFirst) {
		t.Fatalf("chunk 1 anchors: got %+v want %+v", got, wantFirst)
	}
	if got := overviewSectionAnchors(text, contentStart, sequence, second); !reflect.DeepEqual(got, wantSecond) {
		t.Fatalf("chunk 2 anchors: got %+v want %+v", got, wantSecond)
	}
}
