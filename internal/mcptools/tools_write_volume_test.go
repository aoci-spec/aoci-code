package mcptools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

const volumeUpdateLine = "main.go[CD9S]: F:run the updated fixture | R:- | A:main | S:Keep execution deterministic"

func buildSingleCodeWriteRepo(t *testing.T, includeDatabase bool) string {
	t.Helper()
	root := buildVolumeRepo(t, true, includeDatabase)
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	if includeDatabase {
		writeVolumeTestFile(t, root, "aoci.database.txt",
			cognition.DatabaseMarker+"\n===Primary tables/database://primary/public/===\n"+
				"users[DB9S]: F:store canonical user account state | R:- | A:user_id | S:Hard deletion is forbidden because retained ownership records require the identity\n")
	}
	return root
}

func writeVolumeTestFile(t *testing.T, root, rel, text string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func volumeSourceSHA(t *testing.T, root, rel string) string {
	t.Helper()
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint.SHA256
}

func volumeFileText(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSingleCodeVolumeUpdateUsesExistingGovernancePipeline(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	rootBefore := volumeFileText(t, root, "aoci.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")

	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}})
	if fail != nil {
		t.Fatalf("plan failed: %+v", fail)
	}
	if plan.changeEnvelope == nil || plan.changeEnvelope.ChangeObject != "code:main.go" ||
		plan.changeEnvelope.Volume != "code" || plan.changeEnvelope.Strategy != cognition.ImpactStrategyDependencyClosure ||
		strings.Join(plan.changeEnvelope.WriteSet, ",") != "code" ||
		strings.Join(plan.changeEnvelope.GuardSet, ",") != "root,meta,code" {
		t.Fatalf("unexpected internal Change Envelope: %#v", plan.changeEnvelope)
	}

	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}}, ledger.SourceAgent, false)
	if fail != nil {
		t.Fatalf("apply failed: %+v", fail)
	}
	if outcome == nil || outcome.Volume != "code" || !outcome.BaselineComplete || outcome.AppliedCount != 1 {
		t.Fatalf("unexpected apply outcome: %#v", outcome)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), volumeUpdateLine) {
		t.Fatal("Code Volume did not receive the model-authored Entry")
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore ||
		volumeFileText(t, root, "aoci.meta.txt") != metaBefore ||
		volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("single Code update modified an unapproved cognition asset")
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes["code"].ObjectCount != 1 {
		t.Fatalf("projected CognitionSet is invalid: set=%#v err=%v", set, err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("Baseline was not saved: exists=%v err=%v", exists, err)
	}
	for _, rel := range []string{"main.go", "aoci.code.txt"} {
		current, hashErr := baseline.HashFile(filepath.Join(root, rel))
		stored, ok := state.Files[rel]
		if hashErr != nil || !ok || stored.SHA256 != current.SHA256 {
			t.Fatalf("Baseline is not aligned for %s: stored=%#v current=%#v err=%v", rel, stored, current, hashErr)
		}
	}
}

func TestMCPUpdateEntryKeepsSchemaAndReportsCodeVolumeAlignment(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"path":          "main.go",
		"new_entry":     volumeUpdateLine,
		"source_sha256": volumeSourceSHA(t, root, "main.go"),
	})
	for _, want := range []string{
		`"status":"applied"`, `"aligned":true`, `"applied":1`,
		`"volume":"code"`, `"version":2`, `"layout_mode":"volumes-v1"`,
		`"delivered_volumes":[]`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("Volume update result missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "guard_set") || strings.Contains(output, "change_envelope") {
		t.Fatalf("internal consistency details leaked to the Agent:\n%s", output)
	}
}

func TestFirstVolumeMaintainDeliversMetaTagRelationAndSContract(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	cfg := legacyTestConfig()
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", map[string]any{})
	for _, want := range []string{`"status":"repair_required"`, `"result":"authoring_required"`,
		`"authoring_meta":"#AOCI-META-VOLUME: 1`, "compact A+B+C+[D]+E", "C9-8",
		"code:path/to/file", "database://source/namespace/table"} {
		if !strings.Contains(output, want) {
			t.Fatalf("Maintain did not deliver first-authoring contract %q:\n%s", want, output)
		}
	}
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(output), &maintain); err != nil {
		t.Fatal(err)
	}
	example := deliveredVolumeExample(t, maintain.Instructions, maintain.AuthoringMeta, cognition.ScopeCode)
	if !strings.Contains(output, example) {
		t.Fatalf("Maintain did not deliver its validated complete Code example: %q", example)
	}
}

func TestOldFormalMetaNewRuntimeMaintainAppliesTwoContractDerivedEntriesWithoutMetaWrite(t *testing.T) {
	root, oldMeta := buildOldFormalMetaCodeRepo(t)
	metaBefore, err := baseline.HashFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	maintainText := callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", map[string]any{})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.AuthoringMeta != string(oldMeta) || len(maintain.Candidates) != 2 {
		t.Fatalf("old-Meta Maintain contract or target set is incomplete: %#v", maintain)
	}
	example := deliveredVolumeExample(t, maintain.Instructions, maintain.AuthoringMeta, cognition.ScopeCode)
	entries := make([]map[string]any, 0, len(maintain.Candidates))
	for _, candidate := range maintain.Candidates {
		entry, ok := index.ParseEntryLine(example, 1)
		if !ok {
			t.Fatalf("delivered example is not parseable: %q", example)
		}
		suffix := example[strings.Index(example, "["):]
		derived := filepath.Base(candidate.Path) + suffix
		if parsed, valid := index.ParseEntryLine(derived, 1); !valid || parsed.TagsRaw != entry.TagsRaw {
			t.Fatalf("could not derive target from delivered example: %q", derived)
		}
		entries = append(entries, map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256, "new_entry": derived})
	}
	applyText := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{"entries": entries})
	var applied autoResult
	if err := json.Unmarshal([]byte(applyText), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 2 {
		t.Fatalf("contract-derived old-Meta batch did not apply once: %#v", applied)
	}
	metaAfter, err := baseline.HashFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil || metaAfter.SHA256 != metaBefore.SHA256 || volumeFileText(t, root, "aoci.meta.txt") != string(oldMeta) {
		t.Fatal("old-formal-meta compatibility flow rewrote Meta")
	}
}

func TestVolumeCandidateCompactTagGatePreservesDottedReadCompatibility(t *testing.T) {
	for _, test := range []struct {
		name   string
		path   string
		line   string
		actual string
	}{
		{name: "create", path: "helper.go", line: "helper.go[C.D.7.T]: F:provide a helper | R:- | A:- | S:-", actual: "C.D.7.T"},
		{name: "create_noncanonical_spacing", path: "helper.go", line: "helper.go[ CD7T]: F:provide a helper | R:- | A:- | S:-", actual: " CD7T"},
		{name: "changed_tag", path: "main.go", line: "main.go[C.D.9.S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic", actual: "C.D.9.S"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildSingleCodeWriteRepo(t, false)
			if test.path == "helper.go" {
				writeVolumeTestFile(t, root, test.path, "package main\n\nfunc helper() {}\n")
			}
			before := formalVolumePreimages(t, root)
			output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
				"path": test.path, "new_entry": test.line, "source_sha256": volumeSourceSHA(t, root, test.path),
			})
			var result autoResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != autoStatusRepairRequired || result.Applied != 0 || result.FormalWritesStarted || len(result.Findings) != 1 ||
				result.Findings[0].CandidateIndex != 1 || result.Findings[0].Path != test.path ||
				result.Findings[0].CanonicalObjectIdentity != "code:"+test.path || result.Findings[0].Domain != cognition.ScopeCode ||
				result.Findings[0].Field != "tag" || result.Findings[0].RuleCode != "impact_candidate_tag_not_compact" ||
				result.Findings[0].Expected != "format=ABCDE_compact" || result.Findings[0].Actual != test.actual ||
				result.Findings[0].Cause == "" || result.Findings[0].SafeRepairAction == "" ||
				len(result.RetryScope) != 1 || result.RetryScope[0] != "code:"+test.path {
				t.Fatalf("dotted candidate did not return the complete repair contract: %#v", result)
			}
			assertFormalVolumePreimages(t, root, before)
		})
	}

	root := buildSingleCodeWriteRepo(t, false)
	dotted := "main.go[C.D.9.S]: F:run the persisted fixture | R:- | A:main | S:Keep execution deterministic"
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+dotted+"\n")
	refreshVolumeBaseline(t, root)
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes[cognition.ScopeCode].Objects) != 1 {
		t.Fatalf("persisted dotted Entry is no longer readable: set=%#v err=%v", set, err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	verifyFacts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !verifyFacts.StructureValid || !verifyFacts.GovernanceAligned {
		t.Fatalf("persisted dotted Entry no longer passes Volume Verify facts: facts=%#v err=%v", verifyFacts, err)
	}
	updated := "main.go[C.D.9.S]: F:run the persisted compatibility fixture | R:- | A:main | S:Keep execution deterministic"
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{Path: "main.go", NewEntry: updated, SourceSHA256: volumeSourceSHA(t, root, "main.go")}}, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete {
		t.Fatalf("unchanged persisted dotted tag could not receive a non-tag update: outcome=%#v fail=%+v", outcome, fail)
	}
}

func TestVolumeCandidateCanonicalRelationGateRejectsBareAndAcceptsCanonical(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	writeVolumeTestFile(t, root, "helper.go", "package main\n\nfunc helper() {}\n")
	before := formalVolumePreimages(t, root)
	bare := "helper.go[CD7T]: F:provide a helper | R:main.go | A:- | S:-"
	bareText := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"path": "helper.go", "new_entry": bare, "source_sha256": volumeSourceSHA(t, root, "helper.go"),
	})
	var rejected autoResult
	if err := json.Unmarshal([]byte(bareText), &rejected); err != nil {
		t.Fatal(err)
	}
	if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted || len(rejected.Findings) != 1 ||
		rejected.Findings[0].CandidateIndex != 1 || rejected.Findings[0].Path != "helper.go" ||
		rejected.Findings[0].CanonicalObjectIdentity != "code:helper.go" || rejected.Findings[0].Domain != cognition.ScopeCode ||
		rejected.Findings[0].Field != "R" || rejected.Findings[0].RuleCode != "impact_candidate_relation_not_canonical" ||
		rejected.Findings[0].Expected != "canonical_object_identity" || rejected.Findings[0].Actual != "main.go" ||
		rejected.Findings[0].Cause == "" || rejected.Findings[0].SafeRepairAction == "" ||
		len(rejected.RetryScope) != 1 || rejected.RetryScope[0] != "code:helper.go" {
		t.Fatalf("bare R did not return the complete repair contract: %#v", rejected)
	}
	assertFormalVolumePreimages(t, root, before)
	canonical := "helper.go[CD7T]: F:provide a helper | R:code:main.go | A:- | S:-"
	canonicalText := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"path": "helper.go", "new_entry": canonical, "source_sha256": volumeSourceSHA(t, root, "helper.go"),
	})
	var applied autoResult
	if err := json.Unmarshal([]byte(canonicalText), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 1 || !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), canonical) {
		t.Fatalf("canonical R was not accepted: %#v", applied)
	}
}

func buildOldFormalMetaCodeRepo(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	oldMeta, err := os.ReadFile(filepath.Join("..", "..", "testdata", "volumes", "compat-52bc4af", "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: old Meta compatibility\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta\n"
	writeVolumeTestFile(t, root, "aoci.txt", rootText)
	writeVolumeTestFile(t, root, "aoci.meta.txt", string(oldMeta))
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n")
	writeVolumeTestFile(t, root, "first.go", "package fixture\n\nfunc First() {}\n")
	writeVolumeTestFile(t, root, "second.go", "package fixture\n\nfunc Second() {}\n")
	refreshVolumeBaseline(t, root)
	return root, oldMeta
}

func deliveredVolumeExample(t *testing.T, instructions []string, meta, domain string) string {
	t.Helper()
	dictionary := index.ExtractScopedTagDict(meta, domain)
	for _, instruction := range instructions {
		entry, ok := index.ParseEntryLine(instruction, 1)
		if !ok || entry.FullLine != instruction {
			continue
		}
		if findings := cognition.ValidateVolumeAuthoringExample(domain, instruction, dictionary); len(findings) == 0 {
			return instruction
		}
	}
	t.Fatalf("instructions do not contain a valid %s Entry: %#v", domain, instructions)
	return ""
}

func refreshVolumeBaseline(t *testing.T, root string) {
	t.Helper()
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

func formalVolumePreimages(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", filepath.Join(".aoci", "baseline.json")} {
		result[rel] = volumeFileText(t, root, rel)
	}
	return result
}

func assertFormalVolumePreimages(t *testing.T, root string, before map[string]string) {
	t.Helper()
	for rel, expected := range before {
		if actual := volumeFileText(t, root, rel); actual != expected {
			t.Fatalf("rejected candidate changed formal asset %s", rel)
		}
	}
}

func TestSingleCodeVolumeUpdateUsesCrossVolumeGuardWithoutWritingDatabase(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	line := "main.go[CD9S]: F:run the updated fixture | R:database://primary/public/users | A:main | S:Keep execution deterministic"
	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: line,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}})
	if fail != nil || plan.changeEnvelope == nil ||
		strings.Join(plan.changeEnvelope.WriteSet, ",") != "code" ||
		strings.Join(plan.changeEnvelope.GuardSet, ",") != "root,meta,code,database" {
		t.Fatalf("cross-Volume guard was not derived: plan=%#v fail=%+v", plan, fail)
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: line,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}}, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.Volume != "code" {
		t.Fatalf("guarded Code-only update failed: outcome=%#v fail=%+v", outcome, fail)
	}
	if volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("Code-only update modified the guarded Database Volume")
	}
}

// 只有 Code 卷的仓库里, 模型照样可以在 R 里提到数据库对象 —— 那是它对系统的
// 理解, 不是对机器的断言。写入照常完成, 关系原样保留。
func TestSingleCodeVolumeKeepsDatabaseRelationWhenDatabaseIsAbsent(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	line := "main.go[CD9S]: F:run the updated fixture | R:database://primary/public/users | A:main | S:Keep execution deterministic"
	result, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: line,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}}, ledger.SourceAgent, false)
	if fail != nil || result == nil || result.AppliedCount != 1 {
		t.Fatalf("指向缺席数据库的关系不应阻断写入: fail=%+v result=%+v", fail, result)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "R:database://primary/public/users") {
		t.Fatal("模型写下的关系没有被原样保留")
	}
}

func TestMCPCodeVolumeCrossGuardSuccessStaysSimple(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	databaseBefore := volumeFileText(t, root, "aoci.database.txt")
	line := "main.go[CD9S]: F:run the updated fixture | R:database://primary/public/users | A:main | S:Keep execution deterministic"
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"path":          "main.go",
		"new_entry":     line,
		"source_sha256": volumeSourceSHA(t, root, "main.go"),
	})
	for _, want := range []string{
		`"status":"applied"`, `"aligned":true`, `"applied":1`, `"volume":"code"`,
		`"version":2`, `"layout_mode":"volumes-v1"`, `"delivered_volumes":[]`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("cross-guard success missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "guard_set") || strings.Contains(output, "change_envelope") {
		t.Fatalf("cross-guard success leaked internal structures:\n%s", output)
	}
	if volumeFileText(t, root, "aoci.database.txt") != databaseBefore {
		t.Fatal("MCP Code-only update modified the guarded Database Volume")
	}
}

func TestCodeVolumeBatchUpdatesMultipleObjectsOnce(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	writeVolumeTestFile(t, root, "other.go", "package main\n")
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n"+
			"other.go[CD9S]: F:run another fixture | R:- | A:- | S:-\n")
	items := []AtomicUpdateItem{
		{Path: "main.go", NewEntry: volumeUpdateLine, SourceSHA256: volumeSourceSHA(t, root, "main.go")},
		{Path: "other.go", NewEntry: "other.go[CD9S]: F:run another updated fixture | R:- | A:- | S:-", SourceSHA256: volumeSourceSHA(t, root, "other.go")},
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail != nil || outcome == nil || !outcome.BaselineComplete || outcome.AppliedCount != 2 || outcome.Volume != "code" {
		t.Fatalf("multi-object Volume batch failed: outcome=%#v fail=%+v", outcome, fail)
	}
	text := volumeFileText(t, root, "aoci.code.txt")
	for _, want := range []string{volumeUpdateLine, items[1].NewEntry} {
		if !strings.Contains(text, want) {
			t.Fatalf("multi-object postimage missing %q:\n%s", want, text)
		}
	}
	assertVolumeBaselineAligned(t, root, "main.go", "other.go", "aoci.code.txt")
}

func TestSingleCodeVolumeUpdateRequiresSourceBinding(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	before := volumeFileText(t, root, "aoci.code.txt")
	_, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: volumeUpdateLine,
	}}, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errBadArgs {
		t.Fatalf("unbound Volume candidate was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != before {
		t.Fatal("unbound Volume candidate modified the Code Volume")
	}
}

func TestSingleCodeVolumePathNormalizationKeepsRecoveryIdentity(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	writeVolumeTestFile(t, root, "src/main.go", "package main\n")
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(filepath.Join(root, "src"))+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	item := AtomicUpdateItem{
		Path:         "src/main.go",
		NewEntry:     "src/main.go[CD9S]: F:run the updated fixture | R:- | A:main | S:Keep execution deterministic",
		SourceSHA256: volumeSourceSHA(t, root, "src/main.go"),
	}
	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{item})
	if fail != nil {
		t.Fatalf("path-normalized plan failed: %+v", fail)
	}
	recoveryItems, err := normalizeAtomicRecoveryItems([]AtomicUpdateItem{item})
	if err != nil {
		t.Fatal(err)
	}
	if plan.batchKey != atomicBatchKey(recoveryItems) {
		t.Fatal("identity-only Entry normalization changed the existing recovery key")
	}
}

func TestSingleCodeVolumeEnvelopeRejectsRootAndMetaChanges(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, volumeID := range []string{"root", "meta"} {
		_, fail := resolveSingleVolumeChangeEnvelope(set, cognition.ImpactCandidate{
			Change: cognition.ImpactChangeAsset, VolumeID: volumeID,
		})
		if fail == nil || fail.Code != errVolumeReadOnly {
			t.Fatalf("%s asset change was not rejected: %+v", volumeID, fail)
		}
	}
}

func TestSingleVolumeEnvelopeAcceptsExistingDatabaseUpdate(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, true)
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	envelope, fail := resolveSingleVolumeChangeEnvelope(set, cognition.ImpactCandidate{
		Change:        cognition.ImpactChangeUpdate,
		ObjectRef:     "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store updated user state | R:- | A:user_id | S:Hard deletion is forbidden because retained ownership records require the identity",
	})
	if fail != nil || envelope == nil ||
		strings.Join(envelope.WriteSet, ",") != "database" ||
		strings.Join(envelope.GuardSet, ",") != "root,meta,database" {
		t.Fatalf("existing Database update envelope was not accepted: envelope=%#v fail=%+v", envelope, fail)
	}
}

// 同名多义的关系不再失败: 到底指的是哪个 helper.go, 由读全量索引的模型判断。
func TestSingleCodeVolumeAcceptsAmbiguousRelation(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	writeVolumeTestFile(t, root, "dir1/helper.go", "package helper\n")
	writeVolumeTestFile(t, root, "dir2/helper.go", "package helper\n")
	writeVolumeTestFile(t, root, "aoci.code.txt",
		cognition.CodeVolumeMarker+"\n===Main"+filepath.ToSlash(root)+"/===\n"+
			"main.go[CD9S]: F:run the fixture | R:helper.go | A:main | S:Keep execution deterministic\n"+
			"===First"+filepath.ToSlash(filepath.Join(root, "dir1"))+"/===\n"+
			"helper.go[CD9S]: F:provide the first helper | R:- | A:- | S:-\n"+
			"===Second"+filepath.ToSlash(filepath.Join(root, "dir2"))+"/===\n"+
			"helper.go[CD9S]: F:provide the second helper | R:- | A:- | S:-\n")
	line := "main.go[CD9S]: F:run the updated fixture | R:helper.go | A:main | S:Keep execution deterministic"
	result, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: line,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}}, ledger.SourceAgent, false)
	if fail != nil || result == nil || result.AppliedCount != 1 {
		t.Fatalf("多义关系不应阻断写入: fail=%+v result=%+v", fail, result)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), "F:run the updated fixture | R:helper.go") {
		t.Fatal("模型写下的关系没有被原样保留")
	}
}

func TestSingleCodeVolumeProjectedCognitionMustRemainValid(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	before := volumeFileText(t, root, "aoci.code.txt")
	line := "main.go[ZZ9S]: F:run the updated fixture | R:- | A:main | S:Keep execution deterministic"
	_, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: line,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}}, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errImpactResolutionFailed || !fail.Repairable ||
		len(fail.Findings) != 1 || fail.Findings[0].RuleCode != "object_tag_dictionary_violation" {
		t.Fatalf("invalid projected Code Volume was not rejected: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != before {
		t.Fatal("projected validation failure modified the Code Volume")
	}
}

func TestCodeVolumeTagAxesReturnCandidateRepairFindings(t *testing.T) {
	for _, test := range []struct {
		name string
		tag  string
		axis string
	}{{"invalid_A", "XD9S", "A Layer"}, {"invalid_B", "CX9S", "B Module"},
		{"invalid_C", "CD6S", "C Importance"}, {"invalid_D", "CD9XS", "D Trait"},
		{"invalid_E", "CD9Q", "E Scale"}} {
		t.Run(test.name, func(t *testing.T) {
			root := buildSingleCodeWriteRepo(t, false)
			formalBefore := map[string]string{}
			for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
				formalBefore[rel] = volumeFileText(t, root, rel)
			}
			line := "main.go[" + test.tag + "]: F:run the updated fixture | R:- | A:main | S:Keep execution deterministic"
			_, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
				Path: "main.go", NewEntry: line, SourceSHA256: volumeSourceSHA(t, root, "main.go"),
			}}, ledger.SourceAgent, false)
			if fail == nil || !fail.Repairable || fail.FormalWritesStarted || len(fail.Findings) != 1 {
				t.Fatalf("%s did not return one zero-write repair finding: %+v", test.name, fail)
			}
			finding := fail.Findings[0]
			if finding.CandidateIndex != 1 || finding.Path != "main.go" ||
				finding.CanonicalObjectIdentity != "code:main.go" || finding.Domain != "code" ||
				finding.Field != "tag" || finding.RuleCode != "object_tag_dictionary_violation" ||
				!strings.Contains(finding.Expected, "C=1,3,5,7,8,9") ||
				!strings.Contains(finding.Actual, "tag="+test.tag) || !strings.Contains(finding.Cause, test.axis) ||
				finding.SafeRepairAction == "" {
				t.Fatalf("%s finding is incomplete: %#v", test.name, finding)
			}
			if got := RepairRetryScope(fail.Findings); len(got) != 1 || got[0] != "code:main.go" {
				t.Fatalf("%s retry scope is not canonical: %#v", test.name, got)
			}
			for rel, before := range formalBefore {
				if after := volumeFileText(t, root, rel); after != before {
					t.Fatalf("%s changed formal asset %s", test.name, rel)
				}
			}
		})
	}
}

func TestCodeVolumeTagBatchReturnsAllOriginalCandidatePositions(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	writeVolumeTestFile(t, root, "helper.go", "package main\n")
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n"+
		"helper.go[CD7S]: F:provide the helper | R:- | A:- | S:-\n")
	before := volumeFileText(t, root, "aoci.code.txt")
	items := []AtomicUpdateItem{
		{Path: "main.go", NewEntry: "main.go[CD9Q]: F:run the fixture | R:- | A:main | S:-", SourceSHA256: volumeSourceSHA(t, root, "main.go")},
		{Path: "helper.go", NewEntry: "helper.go[CD6S]: F:provide the helper | R:- | A:- | S:-", SourceSHA256: volumeSourceSHA(t, root, "helper.go")},
	}
	_, fail := ApplyUpdateEntriesAtomic(root, items, ledger.SourceAgent, false)
	if fail == nil || !fail.Repairable || fail.FormalWritesStarted || len(fail.Findings) != 2 {
		t.Fatalf("multi-tag batch did not return every finding: %+v", fail)
	}
	if fail.Findings[0].Path != "helper.go" || fail.Findings[0].CandidateIndex != 2 ||
		fail.Findings[1].Path != "main.go" || fail.Findings[1].CandidateIndex != 1 {
		t.Fatalf("shared deterministic order changed original positions: %#v", fail.Findings)
	}
	if got := RepairRetryScope(fail.Findings); strings.Join(got, ",") != "code:helper.go,code:main.go" {
		t.Fatalf("multi-candidate retry scope is incomplete: %#v", got)
	}
	if volumeFileText(t, root, "aoci.code.txt") != before {
		t.Fatal("multi-tag repair changed the Code Volume")
	}
}

func TestMetaTagDictionaryFailuresAreGlobalStopsWithoutCandidateZeros(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(string) string
		rule   string
	}{
		{"missing", func(meta string) string {
			return strings.Replace(meta, "#[Tag dictionary: code]", "#[Tag dictionary: missing-code]", 1)
		}, "meta_tag_dictionary_missing"},
		{"unparseable", func(meta string) string {
			return strings.Replace(meta, "#C Importance: 9 8 7 5 3 1", "#C Importance: none", 1)
		}, "meta_tag_dictionary_invalid"},
		{"conflict", func(meta string) string {
			return strings.Replace(meta, "#A Layer: C Code", "#A Layer: C-Code C-Config", 1)
		}, "meta_tag_dictionary_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildSingleCodeWriteRepo(t, false)
			session := connectMCPClient(t, root)
			writeVolumeTestFile(t, root, "aoci.meta.txt", test.mutate(volumeFileText(t, root, "aoci.meta.txt")))
			formalBefore := map[string]string{}
			for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
				formalBefore[rel] = volumeFileText(t, root, rel)
			}
			arguments := map[string]any{"path": "main.go", "new_entry": volumeUpdateLine, "source_sha256": volumeSourceSHA(t, root, "main.go")}
			first := callVolumeTool(t, session, "aoci_update_entry", arguments)
			second := callVolumeTool(t, session, "aoci_update_entry", arguments)
			for attempt, output := range []string{first, second} {
				for _, want := range []string{`"status":"stopped"`, `"applied":0`, `"formal_writes_started":false`,
					`"findings":[]`, `"retry_scope":[]`, `"affected_asset":"aoci.meta.txt"`,
					`"field":"tag_dictionary"`, `"rule_code":"` + test.rule + `"`, `"safe_next_action":`, `"next_action":`} {
					if !strings.Contains(output, want) {
						t.Fatalf("attempt %d missing %q:\n%s", attempt+1, want, output)
					}
				}
				if strings.Contains(output, "candidate_index") || strings.Contains(output, "generic replan") {
					t.Fatalf("attempt %d disguised a Meta stop as a candidate/replan:\n%s", attempt+1, output)
				}
			}
			for rel, before := range formalBefore {
				if after := volumeFileText(t, root, rel); after != before {
					t.Fatalf("Meta stop changed formal asset %s", rel)
				}
			}
		})
	}
}

func TestSingleCodeVolumeGuardCASConflictUsesExistingConflict(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	before := volumeFileText(t, root, "aoci.code.txt")
	plan, fail := planUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}})
	if fail != nil {
		t.Fatalf("plan failed: %+v", fail)
	}
	meta := volumeFileText(t, root, "aoci.meta.txt")
	writeVolumeTestFile(t, root, "aoci.meta.txt", meta+"# external guard change\n")
	_, _, fail = commitAtomicBatch(root, ledger.SourceAgent, plan, false)
	if fail == nil || fail.Code != errWriteConflict {
		t.Fatalf("guard CAS did not use existing conflict semantics: %+v", fail)
	}
	if volumeFileText(t, root, "aoci.code.txt") != before {
		t.Fatal("guard CAS conflict modified the Code Volume")
	}
}

func TestSingleCodeVolumeInterruptedAtomicWriteUsesExistingRecovery(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	item := AtomicUpdateItem{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if err := original(target, data, expected); err != nil {
			return err
		}
		return errors.New("simulated interruption after atomic replacement")
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || outcome == nil || outcome.BaselineComplete {
		t.Fatalf("interrupted postimage did not retain recovery state: outcome=%#v fail=%+v", outcome, fail)
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, []AtomicUpdateItem{item})
	if err != nil || !pending {
		t.Fatalf("existing recovery receipt was not retained: pending=%v err=%v", pending, err)
	}

	recovered, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || !recovered.AlreadyApplied || recovered.RecoveredCount != 1 {
		t.Fatalf("existing zero-write recovery did not complete: outcome=%#v fail=%+v", recovered, fail)
	}
	pending, err = UpdateEntriesAtomicRecoveryPending(root, []AtomicUpdateItem{item})
	if err != nil || pending {
		t.Fatalf("completed recovery receipt was not cleaned up: pending=%v err=%v", pending, err)
	}
}

func TestSingleCodeVolumePostwriteMetaDriftHardBlocksRecoveryUntilRestored(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	item := AtomicUpdateItem{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if err := original(target, data, expected); err != nil {
			return err
		}
		meta := volumeFileText(t, root, "aoci.meta.txt")
		writeVolumeTestFile(t, root, "aoci.meta.txt", meta+"# concurrent external change\n")
		return nil
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.BaselineNote == "" {
		t.Fatalf("postwrite guard drift did not stop with recovery evidence: outcome=%#v fail=%+v", outcome, fail)
	}
	pending, err := UpdateEntriesAtomicRecoveryPending(root, []AtomicUpdateItem{item})
	if err != nil || !pending {
		t.Fatalf("postwrite guard drift did not retain the existing receipt: pending=%v err=%v", pending, err)
	}
	recovered, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errWriteConflict || recovered != nil {
		t.Fatalf("third-party Meta drift did not hard-block recovery: outcome=%#v fail=%+v", recovered, fail)
	}
	writeVolumeTestFile(t, root, "aoci.meta.txt", metaBefore)
	recovered, fail = ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || !recovered.AlreadyApplied {
		t.Fatalf("recovery did not converge after exact Meta restoration: outcome=%#v fail=%+v", recovered, fail)
	}
}

func TestSingleCodeVolumePostwriteRootDriftHardBlocksRecoveryUntilRestored(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	rootBefore := volumeFileText(t, root, "aoci.txt")
	item := AtomicUpdateItem{
		Path: "main.go", NewEntry: volumeUpdateLine,
		SourceSHA256: volumeSourceSHA(t, root, "main.go"),
	}
	original := writeAtomicIndex
	t.Cleanup(func() { writeAtomicIndex = original })
	writeAtomicIndex = func(target string, data []byte, expected string) error {
		if err := original(target, data, expected); err != nil {
			return err
		}
		project := volumeFileText(t, root, "aoci.txt")
		writeVolumeTestFile(t, root, "aoci.txt", project+"# concurrent external change\n")
		return nil
	}
	outcome, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	writeAtomicIndex = original
	if fail != nil || outcome == nil || outcome.BaselineComplete || outcome.BaselineNote == "" {
		t.Fatalf("postwrite Root drift did not stop with recovery evidence: outcome=%#v fail=%+v", outcome, fail)
	}
	recovered, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail == nil || fail.Code != errWriteConflict || recovered != nil {
		t.Fatalf("third-party Root drift did not hard-block recovery: outcome=%#v fail=%+v", recovered, fail)
	}
	writeVolumeTestFile(t, root, "aoci.txt", rootBefore)
	recovered, fail = ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{item}, ledger.SourceAgent, false)
	if fail != nil || recovered == nil || !recovered.BaselineComplete || !recovered.AlreadyApplied {
		t.Fatalf("recovery did not converge after exact Root restoration: outcome=%#v fail=%+v", recovered, fail)
	}
}
