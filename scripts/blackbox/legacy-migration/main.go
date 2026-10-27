// Command legacy-migration drills the one way out of the Legacy layout, which
// 0.2.0 makes read-only. A released binary that could still author a Legacy
// index builds one the way its users did and proves it aligned; the binary
// under test then migrates it through onboarding as a host would, to an
// aligned Volumes v1 repository whose Code Entries are the Legacy ones. The
// runner plays the host and its model only: every plan, mapping template,
// preview, approval record, and Apply decision is the binary's, and the
// Entries it authors are transport, not semantics worth keeping.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
)

var passed, failed int

func check(name string, ok bool, detail string) bool {
	status := "PASS"
	if ok {
		passed++
	} else {
		status = "FAIL"
		failed++
	}
	line := status + " " + name
	if detail != "" {
		line += " | " + detail
	}
	fmt.Println(line)
	return ok
}

func main() {
	binary := flag.String("binary", "", "the AOCI binary under test")
	legacyBinary := flag.String("legacy-binary", "", "a released AOCI binary that still authors Legacy indexes (v0.1.0-rc22 or older)")
	keep := flag.Bool("keep", false, "retain the disposable repository")
	flag.Parse()
	if *binary == "" || *legacyBinary == "" {
		fmt.Fprintln(os.Stderr, "usage: legacy-migration --binary <aoci under test> --legacy-binary <released aoci that authors Legacy>")
		os.Exit(2)
	}
	under, err := filepath.Abs(*binary)
	fatalIf(err)
	released, err := filepath.Abs(*legacyBinary)
	fatalIf(err)
	dir, err := os.MkdirTemp("", "aoci-legacy-migration-")
	fatalIf(err)
	if !*keep {
		defer os.RemoveAll(dir)
	}
	root := filepath.Join(dir, "repository")
	fmt.Printf("legacy-migration | under test %s | Legacy author %s | %s\n", version(under), version(released), root)
	if err := run(under, released, root); err != nil {
		check("drill.completed", false, err.Error())
	}
	if *keep {
		fmt.Println("kept " + dir)
	}
	fmt.Printf("\nRESULT: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func run(under, released, root string) error {
	legacy, err := authorLegacyRepository(released, root)
	if err != nil {
		return fmt.Errorf("building the Legacy fixture: %w", err)
	}
	if !checkLegacyStop(under, root) {
		return errors.New("the Legacy layout is not routed to migration")
	}
	session, err := cli(under, root, "cognition", "onboard", "start")
	if err != nil {
		return err
	}
	check("onboard.session_is_a_migration", session["operation"] == "migration" && session["current_layout"] == "legacy",
		fmt.Sprintf("operation=%v layout=%v", session["operation"], session["current_layout"]))
	if err := completeAuthoring(under, root, session); err != nil {
		return err
	}
	plan, snapshotFile, err := loadPlan(root, session)
	if err != nil {
		return err
	}
	candidate, err := authorCandidate(plan, legacy, root)
	if err != nil {
		return err
	}
	candidateFile := filepath.Join(filepath.Dir(root), "candidate.json")
	if err := writeJSON(candidateFile, candidate); err != nil {
		return err
	}
	templateData, err := succeed(under, root, "cognition", "migration", "mapping", "template",
		"--plan-file", filepath.Join(root, filepath.FromSlash(stringField(session, "plan_artifact"))),
		"--snapshot-file", snapshotFile, "--candidate-file", candidateFile)
	if err != nil {
		return fmt.Errorf("mapping template: %w", err)
	}
	template, err := migrationapply.DecodeMapping(templateData)
	if !check("migration.mapping_template_issued", err == nil && template != nil && len(template.Records) > 0, errorText(err)) {
		return errors.New("no mapping template")
	}
	mapping, err := authorMapping(template)
	if err != nil {
		return err
	}
	mappingFile := filepath.Join(filepath.Dir(root), "mapping.json")
	if err := writeJSON(mappingFile, mapping); err != nil {
		return err
	}
	preview, err := cli(under, root, "cognition", "onboard", "preview", "--candidate-file", candidateFile, "--mapping-file", mappingFile)
	if !check("onboard.preview_is_ready", err == nil && preview["approval_digest"] != nil, errorText(err)+compact(preview)) {
		return errors.New("preview not ready")
	}
	prepared, err := cli(under, root, "cognition", "onboard", "prepare")
	if err != nil {
		return err
	}
	interaction, _ := prepared["interaction"].(map[string]any)
	dialog, _ := interaction["host_dialog_request"].(map[string]any)
	arguments, _ := dialog["arguments"].(map[string]any)
	if !check("onboard.prepare_pauses_for_a_person", interaction["model_self_approval"] == false && interaction["formal_writes_started"] == false &&
		dialog["tool"] == "aoci_maintain" && arguments != nil, compact(interaction)) {
		return errors.New("no dialog route to approve the migration")
	}
	approval, shown, err := approveInDialog(under, root, arguments)
	if err != nil {
		return err
	}
	approvalFile, _ := approval["approval_file"].(string)
	if !check("approval.host_dialog_records_the_decision", approval["status"] == machinecontract.HumanApprovalStatusApproved &&
		shown == 1 && approvalFile != "", fmt.Sprintf("status=%v shown=%d file=%s", approval["status"], shown, approvalFile)) {
		return errors.New("the person's approval was not recorded")
	}
	applied, err := cli(under, root, "cognition", "onboard", "apply", "--approval-file", filepath.Join(root, filepath.FromSlash(approvalFile)))
	if !check("onboard.apply_reaches_volumes", err == nil && applied["formal_assets_complete"] == true &&
		applied["active_layout"] == machinecontract.CognitionPlannerVolumes, errorText(err)+compact(applied)) {
		return errors.New("apply did not complete")
	}
	checkAligned(under, root)
	checkEntriesPreserved(root, legacy)
	return nil
}

// selfEntry is the Entry a Legacy index holds for itself, which Verify asks for
// and which the migration must move to the Root rather than into Code.
const selfEntry = "aoci.txt[CG8M]: F:Holds this repository's complete AOCI cognition index | R:- | A:- | S:-"

// legacyIndex is what the released binary wrote: the exact index text and
// the Entry lines of the business files, keyed by repository-relative path.
type legacyIndex struct {
	text    string
	entries map[string]string
}

// authorLegacyRepository builds a Legacy repository with released commands
// only, the way its users did: an existing configuration makes init write
// the Legacy skeleton, the Header gets a real tag dictionary before the first
// scan, Maintain issues the business files, the Legacy write path records
// them, and the CLI records the self-Entry Verify asks for.
func authorLegacyRepository(released, root string) (*legacyIndex, error) {
	files := map[string]string{
		"src/main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello\") }\n",
		"pkg/util.go": "package pkg\n\n// Add returns a+b.\nfunc Add(a, b int) int { return a + b }\n",
	}
	for rel, body := range files {
		if err := writeText(filepath.Join(root, filepath.FromSlash(rel)), body); err != nil {
			return nil, err
		}
	}
	if err := writeText(filepath.Join(root, ".aoci", "config.json"), "{\n  \"locale\": \"en-US\"\n}\n"); err != nil {
		return nil, err
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "drill@example.invalid"}, {"config", "user.name", "drill"},
		{"add", "-A"}, {"commit", "-qm", "fixture"}} {
		if _, err := runIn(root, "git", args...); err != nil {
			return nil, err
		}
	}
	if _, err := succeed(released, root, "init", "--agent=", "--hooks=false"); err != nil {
		return nil, fmt.Errorf("released init: %w", err)
	}
	indexPath := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	text := string(data)
	for pattern, line := range map[string]string{`(?m)^#A Layer: .*$`: "#A Layer: C-Code X-EntryPoint", `(?m)^#B Module: .*$`: "#B Module: G-General",
		`(?m)^#D Trait \(optional\): .*$`: "#D Trait (optional): -"} {
		text = regexp.MustCompile(pattern).ReplaceAllString(text, line)
	}
	if err := writeText(indexPath, text); err != nil {
		return nil, err
	}
	if _, err := succeed(released, root, "scan"); err != nil {
		return nil, fmt.Errorf("released scan: %w", err)
	}
	authored := map[string]string{
		".gitattributes": ".gitattributes[CG3T]: F:Normalizes repository text line endings so content hashes stay deterministic | R:- | A:- | S:-",
		"AGENTS.md":      "AGENTS.md[CG5T]: F:Tells agents how to establish, reuse, and maintain AOCI cognition for this repository | R:- | A:- | S:-",
		"pkg/util.go":    "util.go[CG5T]: F:Adds two integers for the example program | R:- | A:Add | S:-",
		"src/main.go":    "main.go[XG7T]: F:Prints a greeting as the example program entry point | R:- | A:main | S:-",
	}
	maintained, err := callTool(released, root, "aoci_maintain", map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	candidates, _ := maintained["candidates"].([]any)
	entries := []map[string]any{}
	for _, value := range candidates {
		candidate, _ := value.(map[string]any)
		path, _ := candidate["path"].(string)
		line, ok := authored[path]
		if !ok {
			return nil, fmt.Errorf("released Maintain issued an unexpected path %q", path)
		}
		entries = append(entries, map[string]any{"path": path, "source_sha256": candidate["source_sha256"], "new_entry": line})
	}
	if len(entries) != len(authored) {
		return nil, fmt.Errorf("released Maintain issued %d of %d business files", len(entries), len(authored))
	}
	if _, err := callTool(released, root, "aoci_update_entry", map[string]any{"entries": entries}, nil); err != nil {
		return nil, err
	}
	digest, err := fileSHA256(indexPath)
	if err != nil {
		return nil, err
	}
	if _, err := succeed(released, root, "update-entry", "--path", "aoci.txt", "--source-sha256", digest, "--entry", selfEntry); err != nil {
		return nil, fmt.Errorf("released self-Entry: %w", err)
	}
	_, _, verifyCode, _ := cliRaw(released, root, "verify")
	_, _, checkCode, _ := cliRaw(released, root, "check")
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "aoci: Legacy index"}} {
		if _, err := runIn(root, "git", args...); err != nil {
			return nil, err
		}
	}
	final, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}
	legacy := &legacyIndex{text: string(final), entries: authored}
	recorded := 0
	for _, line := range append(mapValues(authored), selfEntry) {
		if strings.Contains(legacy.text, "\n"+line+"\n") {
			recorded++
		}
	}
	_, metaErr := os.Stat(filepath.Join(root, "aoci.meta.txt"))
	isLegacy := !strings.HasPrefix(legacy.text, cognition.RootManifestMarker) && errors.Is(metaErr, os.ErrNotExist)
	check("legacy.authored_and_aligned_by_released_binary", verifyCode == 0 && checkCode == 0 && isLegacy && recorded == len(authored)+1,
		fmt.Sprintf("verify=%d check=%d legacy_layout=%t entries_recorded=%d/%d", verifyCode, checkCode, isLegacy, recorded, len(authored)+1))
	return legacy, nil
}

// checkLegacyStop proves the read-only stop before anything is migrated: the
// Guide routes to onboarding and a write tool refuses.
func checkLegacyStop(under, root string) bool {
	guide, _ := cli(under, root, "index", "agent", "guide", "--agent", "claude")
	commands, _ := guide["commands"].(map[string]any)
	onboard, _ := commands["onboard"].(string)
	routed := check("guide.routes_the_legacy_layout_to_migration", guide["stage"] == machinecontract.StatusLegacyReadOnlyMigrateRequired &&
		strings.Contains(onboard, "cognition onboard start"), fmt.Sprintf("stage=%v onboard=%q", guide["stage"], onboard))
	text, _ := callToolText(under, root, "aoci_update_entry", map[string]any{"entries": []map[string]any{{"path": "src/main.go",
		"source_sha256": strings.Repeat("0", 64), "candidate_id": strings.Repeat("0", 64), "new_entry": "main.go[XG7T]: F:x | R:- | A:- | S:-"}}})
	refused := check("legacy.write_tools_refuse_before_migration", strings.Contains(text, machinecontract.StatusLegacyReadOnlyMigrateRequired), firstLine(text))
	return routed && refused
}

// completeAuthoring submits a completion receipt for every batch onboarding
// issues: the host model reads each batch and authors the candidate below.
// The receipt follows the batch's contract: a v1 batch (every Migration
// session) takes the v1 receipt, which names no batch, and a v2 batch takes
// the v2 receipt with its batch and the Host's authoring-run declaration.
func completeAuthoring(under, root string, session map[string]any) error {
	id := stringField(session, "onboarding_session_id")
	receipt := filepath.Join(filepath.Dir(root), "completion.json")
	args := []string{"cognition", "onboard", "next"}
	for round := 0; round < 20; round++ {
		batch, err := cli(under, root, args...)
		if err != nil {
			return fmt.Errorf("onboard next: %w", err)
		}
		tasks, _ := batch["tasks"].([]any)
		if len(tasks) == 0 {
			pending, _ := batch["pending_count"].(float64)
			check("onboard.authoring_batches_complete", pending == 0, fmt.Sprintf("rounds=%d next_action=%v", round, batch["next_action"]))
			return nil
		}
		completed := []string{}
		for _, value := range tasks {
			task, _ := value.(map[string]any)
			completed = append(completed, stringField(task, "task_id"))
		}
		completion := map[string]any{"version": machinecontract.CognitionOnboardingCompleteV1, "onboarding_session_id": id,
			"completed_task_ids": completed}
		if batch["version"] == machinecontract.CognitionOnboardingBatchV2 {
			requirement, _ := batch["semantic_authoring_requirement"].(map[string]any)
			completion["version"], completion["batch_id"] = machinecontract.CognitionOnboardingCompleteV2, batch["batch_id"]
			completion["semantic_authoring_declaration"] = map[string]any{"version": machinecontract.SemanticAuthoringProvenanceV1,
				"origin": machinecontract.SemanticAuthoringOriginHostModel, "authoring_run_id": "legacy-migration-drill",
				"discovery_plan_id": requirement["discovery_plan_id"], "evidence_binding_sha256": requirement["evidence_binding_sha256"]}
		}
		if err := writeJSON(receipt, completion); err != nil {
			return err
		}
		args = []string{"cognition", "onboard", "next", "--completion-file", receipt}
	}
	check("onboard.authoring_batches_complete", false, "more than 20 batches")
	return errors.New("authoring did not finish")
}

func loadPlan(root string, session map[string]any) (*cognitionplan.Plan, string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stringField(session, "plan_artifact"))))
	if err != nil {
		return nil, "", err
	}
	plan, err := cognitionplan.DecodePlan(data)
	if err != nil {
		return nil, "", err
	}
	if plan.Mapping == nil {
		return nil, "", errors.New("the Migration Plan carries no mapping")
	}
	return plan, filepath.Join(root, filepath.FromSlash(stringField(session, "snapshot_artifact"))), nil
}

// authorCandidate is the host model's Volumes layout: a Root, a Meta whose
// dictionary admits the Legacy tags and which carries one reviewed rule line
// per Legacy Header atom, and a Code Volume holding the Legacy business
// Entries byte for byte in their sections, without the Root-owned self-Entry.
func authorCandidate(plan *cognitionplan.Plan, legacy *legacyIndex, root string) (*cognitionplan.LayoutCandidate, error) {
	rootLines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + plan.Locale,
		"#Project: Legacy migration drill", "#Global-Invariants: Keep the meaning of every migrated Entry",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled"}
	metaLines := []string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing", "#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]", "#A Layer: C-Code X-EntryPoint", "#B Module: G-General",
		"#C Importance: 9-core 8-high-frequency 7-business 5-routine 3-supporting 1-edge", "#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100",
		"#[Tag dictionary: database]", "#A Layer: D-Database", "#B Module: S-Schema",
		"#C Importance: 9-core 8-high-frequency 7-business 5-routine 3-supporting 1-edge", "#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100"}
	headerAtoms := 0
	for _, record := range plan.Mapping.Records {
		if record.UnitKind != "entry" && record.Mode != machinecontract.CognitionMappingStructuralOnly {
			headerAtoms++
		}
	}
	for rule := 0; rule < headerAtoms+4; rule++ {
		metaLines = append(metaLines, fmt.Sprintf("#Reviewed-Rule-%02d: model-reviewed rule carried from the Legacy Header", rule))
	}
	sections, lineToPath := legacySections(legacy.text, root)
	code := []string{cognition.CodeVolumeMarker}
	for _, section := range sections {
		lines := []string{}
		for _, entry := range section.Entries {
			if entry.RelPath != "aoci.txt" {
				lines = append(lines, entry.FullLine)
			}
		}
		if len(lines) > 0 {
			code = append(code, "", section.HeaderLine)
			code = append(code, lines...)
		}
	}
	candidate := &cognitionplan.LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: []cognitionplan.CandidateAsset{
			{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
			{AssetID: "meta", Path: "aoci.meta.txt", Content: strings.Join(metaLines, "\n") + "\n"},
			{AssetID: "code", Path: "aoci.code.txt", Content: strings.Join(code, "\n") + "\n"}}}
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution := cognitionplan.MappingResolution{UnitID: record.UnitID, TargetAsset: record.TargetAsset, TargetRef: record.TargetRef,
			Reviewer: "drill-model", SemanticReviewed: true}
		switch {
		case record.UnitKind == "entry" && record.LegacySelfEntry:
			resolution.TargetAsset, resolution.TargetRef = cognition.OwnerRoot, ""
		case record.UnitKind == "entry":
			path, ok := lineToPath[record.SourceLine]
			if !ok {
				return nil, fmt.Errorf("the Legacy Entry on line %d has no section path", record.SourceLine)
			}
			resolution.TargetAsset, resolution.TargetRef = cognition.OwnerCode, "code:"+path
		default:
			resolution.TargetAsset = "root"
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	return candidate, nil
}

// legacySections reads the Legacy index through the product parser and
// returns its directory sections in index order, and each Entry line number
// (1-based) with the repository-relative path the parser resolves for it.
func legacySections(text, root string) ([]*index.Section, map[int]string) {
	doc, _ := index.Parse(text)
	index.ResolveRelPaths(doc, root)
	sections, paths := []*index.Section{}, map[int]string{}
	for _, section := range doc.Sections {
		if section.AbsPath == "" {
			continue
		}
		sections = append(sections, section)
		for _, entry := range section.Entries {
			if entry.RelPath != "" {
				paths[entry.LineNo] = entry.RelPath
			}
		}
	}
	return sections, paths
}

// authorMapping is the host model's review of the template: an Entry the
// Code Volume carries byte for byte is preserved, the self-Entry and its
// atoms become Root-owned, and each Header atom is regenerated into one
// reviewed Meta or Root line.
func authorMapping(template *migrationapply.MigrationMapping) (*migrationapply.MigrationMapping, error) {
	result := *template
	result.Records = append([]migrationapply.MappingRecord{}, template.Records...)
	used := map[string]bool{}
	targetByKey := map[string][]migrationapply.TargetRange{}
	semanticTargets := []migrationapply.TargetRange{}
	targetEntrySHA := map[string]bool{}
	for _, target := range result.TargetRanges {
		targetByKey[target.Kind+"\x00"+target.SHA256] = append(targetByKey[target.Kind+"\x00"+target.SHA256], target)
		if target.Kind != "entry" && target.Object == "" {
			semanticTargets = append(semanticTargets, target)
		}
		if target.Kind == "entry" {
			targetEntrySHA[target.SHA256] = true
		}
	}
	selfParent := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" && !targetEntrySHA[record.SourceSHA256] {
			selfParent = record.SourceIdentity
			break
		}
	}
	if selfParent == "" {
		return nil, errors.New("the template names no Legacy self-Entry")
	}
	selfSources := map[string]bool{selfParent: true}
	for changed := true; changed; {
		changed = false
		for _, record := range result.Records {
			if selfSources[record.ParentSourceIdentity] && !selfSources[record.SourceIdentity] {
				selfSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	var selfTarget migrationapply.TargetRange
	for _, target := range semanticTargets {
		if target.Asset == cognition.OwnerRoot && target.Kind == "root_semantic" {
			selfTarget = target
			break
		}
	}
	if selfTarget.Identity == "" {
		return nil, errors.New("the template offers no Root semantic target")
	}
	used[selfTarget.Identity] = true
	tasks := map[string]migrationapply.MappingAuthoringTask{}
	for _, task := range template.AuthoringTasks {
		tasks[task.TaskID] = task
	}
	pop := func(values []migrationapply.TargetRange) (migrationapply.TargetRange, bool) {
		for _, value := range values {
			if !used[value.Identity] {
				return value, true
			}
		}
		return migrationapply.TargetRange{}, false
	}
	removedTasks := map[string]bool{}
	selfSourceIDs, selfEvidenceRefs := []string{}, []string{}
	for position := range result.Records {
		record := &result.Records[position]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		if selfSources[record.SourceIdentity] {
			removedTasks[record.AuthoringTaskID] = true
			selfSourceIDs = append(selfSourceIDs, record.SourceIdentity)
			selfEvidenceRefs = append(selfEvidenceRefs, "legacy:"+record.SourceSHA256)
			record.SemanticRole = "model_reviewed_root_ownership"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = cognition.OwnerRoot, "", selfTarget.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			record.AuthoringTaskID, record.MappingGroupID = "author:self-entry-root", "group:self-entry-root"
			record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "drill-reviewer"
			continue
		}
		target, found := pop(targetByKey[record.SourceKind+"\x00"+record.SourceSHA256])
		if found {
			record.SemanticRole = "preserved_model_owned_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode, record.AuthoringTaskID = machinecontract.CognitionMappingPreserved, ""
		} else {
			target, found = pop(semanticTargets)
			if !found {
				return nil, fmt.Errorf("no target left for %s", record.SourceIdentity)
			}
			record.SemanticRole = "model_reviewed_migration_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			task := tasks[record.AuthoringTaskID]
			task.TargetAsset, task.TargetObject = target.Asset, target.Object
			task.CandidateRangeIdentities = []string{target.Identity}
			task.Status, task.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "drill-reviewer"
			tasks[task.TaskID] = task
		}
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "drill-reviewer"
		used[target.Identity] = true
	}
	result.AuthoringTasks = []migrationapply.MappingAuthoringTask{}
	sourceEvidenceIdentity := ""
	for _, task := range tasks {
		if sourceEvidenceIdentity == "" {
			sourceEvidenceIdentity = task.SourceEvidenceIdentity
		}
		if task.Status == machinecontract.CognitionMigrationSemanticReviewed && !removedTasks[task.TaskID] {
			result.AuthoringTasks = append(result.AuthoringTasks, task)
		}
	}
	sort.Strings(selfSourceIDs)
	sort.Strings(selfEvidenceRefs)
	selfEvidenceRefs = uniqueSorted(selfEvidenceRefs)
	result.AuthoringTasks = append(result.AuthoringTasks, migrationapply.MappingAuthoringTask{
		TaskID: "author:self-entry-root", SourceIdentities: selfSourceIDs, SourceEvidenceRefs: selfEvidenceRefs,
		SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: cognition.OwnerRoot,
		CandidateRangeIdentities: []string{selfTarget.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed,
		Reviewer: "drill-reviewer",
	})
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	result.MappingGroups = append(result.MappingGroups, migrationapply.MappingGroup{
		MappingGroupID: "group:self-entry-root", SourceIdentities: selfSourceIDs,
		TargetRangeIdentities: []string{selfTarget.Identity}, AuthoringTaskID: "author:self-entry-root",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "drill-reviewer",
	})
	return &result, nil
}

// approveInDialog makes the call prepare offered from a host whose person
// approves in the dialog, and returns the result and how many dialogs it saw.
func approveInDialog(under, root string, arguments map[string]any) (map[string]any, int, error) {
	shown := 0
	options := &mcp.ClientOptions{ElicitationHandler: func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		shown++
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"decision": machinecontract.HumanApprovalDecisionApprove}}, nil
	}}
	result, err := callTool(under, root, "aoci_maintain", arguments, options)
	return result, shown, err
}

func checkAligned(under, root string) {
	_, _, verifyCode, _ := cliRaw(under, root, "verify")
	_, _, checkCode, _ := cliRaw(under, root, "check")
	guide, _ := cli(under, root, "index", "agent", "guide", "--agent", "claude")
	check("governance.verify_check_and_guide_aligned", verifyCode == 0 && checkCode == 0 && guide["complete"] == true &&
		guide["next_action"] == "none", fmt.Sprintf("verify=%d check=%d guide=%v/%v", verifyCode, checkCode, guide["stage"], guide["next_action"]))
}

// checkEntriesPreserved reads the published assets as plain text, not through
// the product's loader, and compares them with what the released binary wrote:
// the Root is a Volumes Root, every business Entry line is in the Code Volume
// byte for byte, nothing else is, and the self-Entry stayed out of Code.
func checkEntriesPreserved(root string, legacy *legacyIndex) {
	rootData, rootErr := os.ReadFile(filepath.Join(root, "aoci.txt"))
	codeData, codeErr := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	got := map[string]bool{}
	for _, line := range strings.Split(string(codeData), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "===") {
			got[line] = true
		}
	}
	missing := []string{}
	for path, line := range legacy.entries {
		if !got[line] {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	volumes := rootErr == nil && strings.HasPrefix(string(rootData), cognition.RootManifestMarker)
	check("entries.code_volume_carries_the_legacy_entries", codeErr == nil && volumes && len(missing) == 0 &&
		len(got) == len(legacy.entries) && !got[selfEntry], fmt.Sprintf("volumes_root=%t code_entries=%d changed_or_missing=%v self_entry_in_code=%t",
		volumes, len(got), missing, got[selfEntry]))
}

// cli runs one --json command that must succeed and decodes its single JSON
// object.
func cli(binary, root string, args ...string) (map[string]any, error) {
	data, err := succeed(binary, root, args...)
	decoded := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if decodeErr := json.Unmarshal(data, &decoded); decodeErr != nil && err == nil {
			err = fmt.Errorf("%s: output is not one JSON object: %v", strings.Join(args, " "), decodeErr)
		}
	}
	return decoded, err
}

// succeed runs one --json command and turns a nonzero exit into an error that
// carries the machine error message, or else the first stderr line.
func succeed(binary, root string, args ...string) ([]byte, error) {
	stdout, stderr, code, err := cliRaw(binary, root, args...)
	if err == nil && code != 0 {
		var envelope struct {
			Message string `json:"message"`
		}
		reason := firstLine(string(stderr))
		if json.Unmarshal(stdout, &envelope) == nil && envelope.Message != "" {
			reason = envelope.Message
		}
		err = fmt.Errorf("%s exited %d: %s", strings.Join(args, " "), code, reason)
	}
	return stdout, err
}

// cliRaw runs one --json command and reports its exit code without judging it.
func cliRaw(binary, root string, args ...string) ([]byte, []byte, int, error) {
	full := append([]string{"--repo", root, "--json"}, args...)
	command := exec.Command(binary, full...)
	command.Dir = root
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code, err = exit.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, -1, err
	}
	return stdout.Bytes(), stderr.Bytes(), code, nil
}

func callTool(binary, root, name string, arguments map[string]any, options *mcp.ClientOptions) (map[string]any, error) {
	text, err := callToolWith(binary, root, name, arguments, options)
	if err != nil {
		return nil, err
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("%s result is not JSON: %s", name, firstLine(text))
	}
	return decoded, nil
}

func callToolText(binary, root, name string, arguments map[string]any) (string, error) {
	return callToolWith(binary, root, name, arguments, nil)
}

func callToolWith(binary, root, name string, arguments map[string]any, options *mcp.ClientOptions) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "legacy-migration-drill", Version: "1"}, options)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(binary, "--repo", root, "mcp")}, nil)
	if err != nil {
		return "", err
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", err
	}
	text := ""
	for _, content := range result.Content {
		if piece, ok := content.(*mcp.TextContent); ok {
			text += piece.Text
		}
	}
	return text, nil
}

func version(binary string) string {
	out, err := exec.Command(binary, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func runIn(dir, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, firstLine(string(out)))
	}
	return string(out), nil
}

func writeText(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeText(path, string(data)+"\n")
}

func fileSHA256(path string) (string, error) {
	out, err := exec.Command("sha256sum", path).Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", errors.New("sha256sum printed nothing")
	}
	return fields[0], nil
}

func stringField(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func compact(value any) string {
	if value == nil {
		return ""
	}
	data, _ := json.Marshal(value)
	if len(data) > 240 {
		data = data[:240]
	}
	return " " + string(data)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func uniqueSorted(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
