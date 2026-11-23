// Shared fixtures and helpers for the migrationapply tests. The tests themselves
// live in migration_fault_matrix_test.go, migration_rollback_recovery_test.go,
// and migration_replay_reversal_test.go.
package migrationapply

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// migrationApplyRequest builds the complete request Prepare takes.
func migrationApplyRequest(t testing.TB, root string, kinds []string) *ApplyRequest {
	t.Helper()
	snapshot, err := CaptureSnapshot(root, "en-US", kinds, "2026-07-30T00:00:00Z")
	if err != nil || snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityEligible {
		t.Fatalf("Snapshot invalid: %#v err=%v", snapshot, err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: kinds})
	if err != nil {
		t.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, kinds)
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("D2-A Preview invalid: %#v err=%v", preview, err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorApplyGradeMapping(t, template)
	mapping, err := ValidateMapping(root, snapshot, plan, candidate, authored)
	if err != nil {
		t.Fatal(err)
	}
	return &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2,
		Snapshot: *snapshot, Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview,
		BaselineTimestamp: "2026-07-30T00:00:00Z"}
}

func preparedMigrationFixture(t testing.TB, root string, kinds []string) (*ApplyEnvelope, *Approval) {
	t.Helper()
	envelope, err := Prepare(root, migrationApplyRequest(t, root, kinds))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, approval
}

func migrationFixture(t testing.TB, withDatabase bool) string {
	t.Helper()
	root := t.TempDir()
	writeMigrationFile(t, root, "src/main.go", "package main\n\nfunc main() {}\n")
	if withDatabase {
		installMigrationDatabaseEvidence(t, root)
	} else {
		cfg := config.DefaultConfig()
		cfg.LedgerEnabled = true
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{
		"#AOCI-CLI Complete Index",
		"#Project: Model-authored legacy fixture",
		"#[Tag dictionary]",
		"#A Layer: C Code",
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===",
		"main.go[CD9S]: F:Preserve the legacy code responsibility | R:- | A:- | S:Keep source behavior byte-stable during cognition migration",
	}
	if withDatabase {
		lines = append(lines,
			"===Database/database://primary/fixture/===",
			"items[DB9S]: F:Preserve durable item identity | R:code:src/main.go | A:id | S:Writes and their audit record commit together",
		)
	}
	writeMigrationFile(t, root, "aoci.txt", strings.Join(lines, "\n")+"\n")
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, root, ".aoci/baseline.json", string(data))
	return root
}

func migrationCandidate(root string, plan *cognitionplan.Plan, kinds []string) *cognitionplan.LayoutCandidate {
	rootLines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + plan.Locale,
		"#Project: Reviewed migrated fixture", "#Global-Invariants: Preserve approved semantic ownership",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled"}
	for _, kind := range kinds {
		format := "object-fras-v2"
		path := "aoci.code.txt"
		if kind == "database" {
			format, path = "table-fras-v2", "aoci.database.txt"
		}
		rootLines = append(rootLines, fmt.Sprintf("#Volume: id=%s kind=%s path=%s format=%s depends=meta state=enabled", kind, kind, path, format))
	}
	metaLines := []string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table", "#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain F Frontend A API S Service", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
		"#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T"}
	for index := 0; index < 20; index++ {
		metaLines = append(metaLines, fmt.Sprintf("#Reviewed-Rule-%02d: model-owned semantic calibration", index))
	}
	assets := []cognitionplan.CandidateAsset{{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
		{AssetID: "meta", Path: "aoci.meta.txt", Content: strings.Join(metaLines, "\n") + "\n"}}
	legacy, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	for _, kind := range kinds {
		if kind == "code" {
			line := findLine(string(legacy), "main.go[")
			content := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + line + "\n"
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "code", Path: "aoci.code.txt",
				Content: content})
		} else {
			line := findLine(string(legacy), "items[")
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "database", Path: "aoci.database.txt",
				Content: cognition.DatabaseMarker + "\n===Database/database://primary/fixture/===\n" + line + "\n"})
		}
	}
	candidate := &cognitionplan.LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID, Assets: assets}
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution := cognitionplan.MappingResolution{UnitID: record.UnitID, TargetAsset: record.TargetAsset, TargetRef: record.TargetRef,
			Reviewer: "fixture-model", SemanticReviewed: true}
		if record.UnitKind == "entry" {
			if record.LegacySelfEntry {
				resolution.TargetAsset, resolution.TargetRef = cognition.OwnerRoot, ""
			} else if strings.HasPrefix(record.SourceText, "items[") {
				resolution.TargetAsset, resolution.TargetRef = "database", "database://primary/fixture/items"
			} else {
				resolution.TargetAsset, resolution.TargetRef = "code", "code:src/main.go"
			}
		} else if record.UnitKind == "header_semantic_atom" {
			resolution.TargetAsset = "root"
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	return candidate
}

func cognitionObjectExists(asset *cognition.Asset, objectRef string) bool {
	if asset == nil {
		return false
	}
	for _, object := range asset.Objects {
		if object.CanonicalRef == objectRef {
			return true
		}
	}
	return false
}

func pilotMigrationCandidate(root string, plan *cognitionplan.Plan, legacy string) *cognitionplan.LayoutCandidate {
	candidate := migrationCandidate(root, plan, []string{"code", "database"})
	entries := map[string]string{"App.jsx": findLine(legacy, "App.jsx["), "api.js": findLine(legacy, "api.js["),
		"service.js": findLine(legacy, "service.js["), "items": findLine(legacy, "items[")}
	for index := range candidate.Assets {
		switch candidate.Assets[index].AssetID {
		case "code":
			candidate.Assets[index].Content = cognition.CodeVolumeMarker + "\n" +
				"===Code " + filepath.ToSlash(filepath.Join(root, "frontend", "src")) + "/===\n" + entries["App.jsx"] + "\n" +
				"===Code " + filepath.ToSlash(filepath.Join(root, "backend", "src")) + "/===\n" + entries["api.js"] + "\n" + entries["service.js"] + "\n"
		case "database":
			candidate.Assets[index].Content = cognition.DatabaseMarker + "\n===Database/database://primary/fixture/===\n" + entries["items"] + "\n"
		}
	}
	refs := map[string]struct{ asset, ref string }{
		"App.jsx[": {"code", "code:frontend/src/App.jsx"}, "api.js[": {"code", "code:backend/src/api.js"},
		"service.js[": {"code", "code:backend/src/service.js"}, "items[": {"database", "database://primary/fixture/items"},
	}
	for index := range candidate.MappingResolutions {
		for prefix, target := range refs {
			for _, record := range plan.Mapping.Records {
				if record.UnitID == candidate.MappingResolutions[index].UnitID && strings.HasPrefix(record.SourceText, prefix) {
					candidate.MappingResolutions[index].TargetAsset = target.asset
					candidate.MappingResolutions[index].TargetRef = target.ref
				}
			}
		}
	}
	return candidate
}

func authorApplyGradeMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	used := map[string]bool{}
	targetByKindSHA := map[string][]TargetRange{}
	targetByKindSHAObject := map[string][]TargetRange{}
	semanticTargets := []TargetRange{}
	for _, target := range result.TargetRanges {
		key := target.Kind + "\x00" + target.SHA256
		targetByKindSHA[key] = append(targetByKindSHA[key], target)
		targetByKindSHAObject[key+"\x00"+target.Object] = append(targetByKindSHAObject[key+"\x00"+target.Object], target)
		if target.Kind != "entry" && target.Object == "" {
			semanticTargets = append(semanticTargets, target)
		}
	}
	popUnused := func(values []TargetRange) (TargetRange, bool) {
		for _, value := range values {
			if !used[value.Identity] {
				return value, true
			}
		}
		return TargetRange{}, false
	}
	taskByID := map[string]MappingAuthoringTask{}
	for _, task := range template.AuthoringTasks {
		taskByID[task.TaskID] = task
	}
	recordObject := map[string]string{}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		var target TargetRange
		found := false
		parentObject := ""
		if record.ParentSourceIdentity != "" {
			parentObject = recordObject[record.ParentSourceIdentity]
		}
		if record.SourceKind == "entry" || record.ParentSourceIdentity != "" {
			key := record.SourceKind + "\x00" + record.SourceSHA256
			if parentObject != "" {
				target, found = popUnused(targetByKindSHAObject[key+"\x00"+parentObject])
			} else {
				target, found = popUnused(targetByKindSHA[key])
			}
		}
		if found {
			record.SemanticRole = "preserved_model_owned_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMappingPreserved
			record.AuthoringTaskID = ""
		} else {
			target, found = popUnused(semanticTargets)
			if !found {
				t.Fatalf("no target range for source %s kind=%s", record.SourceIdentity, record.SourceKind)
			}
			record.SemanticRole = "model_reviewed_migration_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			task := taskByID[record.AuthoringTaskID]
			task.TargetAsset, task.TargetObject = target.Asset, target.Object
			task.CandidateRangeIdentities = []string{target.Identity}
			task.Status, task.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			taskByID[task.TaskID] = task
		}
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		used[target.Identity] = true
		recordObject[record.SourceIdentity] = record.TargetObject
	}
	result.AuthoringTasks = []MappingAuthoringTask{}
	for _, task := range taskByID {
		if task.Status == machinecontract.CognitionMigrationSemanticReviewed {
			result.AuthoringTasks = append(result.AuthoringTasks, task)
		}
	}
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	return &result
}

func authorSelfEntryRootMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	result := authorApplyGradeMapping(t, template)
	targetEntrySHA := map[string]bool{}
	for _, target := range result.TargetRanges {
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
		t.Fatal("Legacy self-entry source missing")
	}
	groupSources := map[string]bool{selfParent: true}
	changed := true
	for changed {
		changed = false
		for _, record := range result.Records {
			if groupSources[record.ParentSourceIdentity] && !groupSources[record.SourceIdentity] {
				groupSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	usedByOther := map[string]bool{}
	for _, record := range result.Records {
		if !groupSources[record.SourceIdentity] {
			usedByOther[record.TargetSemanticRangeIdentity] = true
		}
	}
	var rootTarget TargetRange
	for _, target := range result.TargetRanges {
		if target.Asset == cognition.OwnerRoot && target.Kind == "root_semantic" {
			rootTarget = target
			break
		}
	}
	if rootTarget.Identity == "" {
		t.Fatal("Root semantic target missing")
	}
	if usedByOther[rootTarget.Identity] {
		targetByID := map[string]TargetRange{}
		for _, target := range result.TargetRanges {
			targetByID[target.Identity] = target
		}
		var replacement TargetRange
		for _, record := range result.Records {
			if groupSources[record.SourceIdentity] && record.TargetSemanticRangeIdentity != rootTarget.Identity {
				replacement = targetByID[record.TargetSemanticRangeIdentity]
				break
			}
		}
		if replacement.Identity == "" {
			t.Fatal("self-entry replacement target missing")
		}
		for index := range result.Records {
			record := &result.Records[index]
			if groupSources[record.SourceIdentity] || record.TargetSemanticRangeIdentity != rootTarget.Identity {
				continue
			}
			record.TargetAsset, record.TargetObject = replacement.Asset, replacement.Object
			record.TargetSemanticRangeIdentity = replacement.Identity
			for taskIndex := range result.AuthoringTasks {
				task := &result.AuthoringTasks[taskIndex]
				if task.TaskID == record.AuthoringTaskID {
					task.TargetAsset, task.TargetObject = replacement.Asset, replacement.Object
					task.CandidateRangeIdentities = []string{replacement.Identity}
				}
			}
			break
		}
	}
	removedTasks := map[string]bool{}
	sourceIDs, evidenceRefs := []string{}, []string{}
	for index := range result.Records {
		record := &result.Records[index]
		if !groupSources[record.SourceIdentity] {
			continue
		}
		removedTasks[record.AuthoringTaskID] = true
		sourceIDs = append(sourceIDs, record.SourceIdentity)
		evidenceRefs = append(evidenceRefs, "legacy:"+record.SourceSHA256)
		record.SemanticRole = "model_reviewed_root_ownership"
		record.TargetAsset, record.TargetObject = cognition.OwnerRoot, ""
		record.TargetSemanticRangeIdentity = rootTarget.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.AuthoringTaskID, record.MappingGroupID = "author:self-entry-root", "group:self-entry-root"
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
	}
	sort.Strings(sourceIDs)
	sort.Strings(evidenceRefs)
	evidenceRefs = uniqueStrings(evidenceRefs)
	tasks := result.AuthoringTasks[:0]
	sourceEvidenceIdentity := ""
	for _, task := range result.AuthoringTasks {
		if sourceEvidenceIdentity == "" {
			sourceEvidenceIdentity = task.SourceEvidenceIdentity
		}
		if !removedTasks[task.TaskID] {
			tasks = append(tasks, task)
		}
	}
	tasks = append(tasks, MappingAuthoringTask{TaskID: "author:self-entry-root", SourceIdentities: sourceIDs,
		SourceEvidenceRefs: evidenceRefs, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: cognition.OwnerRoot,
		CandidateRangeIdentities: []string{rootTarget.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed,
		Reviewer: "fixture-reviewer"})
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	result.AuthoringTasks = tasks
	result.MappingGroups = append(result.MappingGroups, MappingGroup{MappingGroupID: "group:self-entry-root",
		SourceIdentities: sourceIDs, TargetRangeIdentities: []string{rootTarget.Identity}, AuthoringTaskID: "author:self-entry-root",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"})
	sort.Slice(result.MappingGroups, func(i, j int) bool {
		return result.MappingGroups[i].MappingGroupID < result.MappingGroups[j].MappingGroupID
	})
	return result
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func authorRegeneratedEntryMapping(t testing.TB, template *MigrationMapping, object string) *MigrationMapping {
	t.Helper()
	sourceEvidenceIdentity := template.AuthoringTasks[0].SourceEvidenceIdentity
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	entrySource := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" {
			entrySource = record.SourceIdentity
			break
		}
	}
	groupSources := map[string]bool{entrySource: true}
	changed := true
	for changed {
		changed = false
		for _, record := range result.Records {
			if groupSources[record.ParentSourceIdentity] && !groupSources[record.SourceIdentity] {
				groupSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	used := map[string]bool{}
	groupSourceIDs, groupTargetIDs := []string{}, []string{}
	for index := range result.Records {
		record := &result.Records[index]
		if !groupSources[record.SourceIdentity] {
			continue
		}
		var target TargetRange
		found := false
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Object == object && candidate.Kind == record.SourceKind {
				target, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("regenerated group target missing for %s", record.SourceKind)
		}
		record.SemanticRole = "model_regenerated_object_semantics"
		record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.AuthoringTaskID = "author:group-code-main"
		record.MappingGroupID = "group:code-main"
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		groupSourceIDs = append(groupSourceIDs, record.SourceIdentity)
		groupTargetIDs = append(groupTargetIDs, target.Identity)
		used[target.Identity] = true
	}
	sort.Strings(groupSourceIDs)
	sort.Strings(groupTargetIDs)
	tasks := []MappingAuthoringTask{{TaskID: "author:group-code-main", SourceIdentities: groupSourceIDs,
		SourceEvidenceRefs: []string{"source:code:src/main.go"}, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: "code", TargetObject: object,
		CandidateRangeIdentities: groupTargetIDs, Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly || groupSources[record.SourceIdentity] {
			continue
		}
		var target TargetRange
		found := false
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Kind != "entry" && candidate.Object == "" {
				target, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("header target missing")
		}
		record.SemanticRole = "model_reviewed_migration_semantics"
		record.TargetAsset, record.TargetSemanticRangeIdentity = target.Asset, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		task := MappingAuthoringTask{TaskID: record.AuthoringTaskID, SourceIdentities: []string{record.SourceIdentity},
			SourceEvidenceRefs: []string{"legacy:" + record.SourceSHA256}, SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: target.Asset,
			CandidateRangeIdentities: []string{target.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}
		tasks = append(tasks, task)
		used[target.Identity] = true
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TaskID < tasks[j].TaskID })
	result.AuthoringTasks = tasks
	result.MappingGroups = []MappingGroup{{MappingGroupID: "group:code-main", SourceIdentities: groupSourceIDs,
		TargetRangeIdentities: groupTargetIDs, AuthoringTaskID: "author:group-code-main",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	return &result
}

func authorSplitEntryMapping(t testing.TB, template *MigrationMapping) *MigrationMapping {
	t.Helper()
	sourceEvidenceIdentity := template.AuthoringTasks[0].SourceEvidenceIdentity
	result := *template
	result.Records = append([]MappingRecord{}, template.Records...)
	entrySources := map[string]bool{}
	entryParent := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" {
			entryParent = record.SourceIdentity
			entrySources[record.SourceIdentity] = true
		}
	}
	for _, record := range result.Records {
		if record.ParentSourceIdentity == entryParent {
			entrySources[record.SourceIdentity] = true
		}
	}
	codeTargets, databaseTargets := map[string]TargetRange{}, map[string]TargetRange{}
	groupTargetIDs := []string{}
	for _, target := range result.TargetRanges {
		if target.Object == "code:src/main.go" {
			codeTargets[target.Kind] = target
			groupTargetIDs = append(groupTargetIDs, target.Identity)
		}
		if target.Object == "database://primary/fixture/items" {
			databaseTargets[target.Kind] = target
			groupTargetIDs = append(groupTargetIDs, target.Identity)
		}
	}
	sort.Strings(groupTargetIDs)
	groupSourceIDs := []string{}
	used := map[string]bool{}
	headerTasks := []MappingAuthoringTask{}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		if entrySources[record.SourceIdentity] {
			target := codeTargets[record.SourceKind]
			if record.SourceKind == "R" || record.SourceKind == "S" {
				target = databaseTargets[record.SourceKind]
			}
			if target.Identity == "" {
				t.Fatalf("split target missing for %s", record.SourceKind)
			}
			record.SemanticRole = "model_owned_split_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			record.AuthoringTaskID, record.MappingGroupID = "author:mixed-split", "group:mixed-split"
			record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			groupSourceIDs = append(groupSourceIDs, record.SourceIdentity)
			used[target.Identity] = true
			continue
		}
		var target TargetRange
		for _, candidate := range result.TargetRanges {
			if !used[candidate.Identity] && candidate.Kind != "entry" && candidate.Object == "" {
				target = candidate
				break
			}
		}
		if target.Identity == "" {
			t.Fatalf("header target missing for %s", record.SourceIdentity)
		}
		record.SemanticRole = "model_reviewed_migration_semantics"
		record.TargetAsset, record.TargetSemanticRangeIdentity = target.Asset, target.Identity
		record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		headerTasks = append(headerTasks, MappingAuthoringTask{TaskID: record.AuthoringTaskID,
			SourceIdentities: []string{record.SourceIdentity}, SourceEvidenceRefs: []string{"legacy:" + record.SourceSHA256},
			SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: target.Asset, CandidateRangeIdentities: []string{target.Identity},
			Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"})
		used[target.Identity] = true
	}
	sort.Strings(groupSourceIDs)
	splitTask := MappingAuthoringTask{TaskID: "author:mixed-split", SourceIdentities: groupSourceIDs,
		SourceEvidenceRefs:     []string{"evidence:database://primary/fixture/items", "source:code:src/main.go"},
		SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: "multiple", CandidateRangeIdentities: groupTargetIDs,
		Status: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}
	result.AuthoringTasks = append(headerTasks, splitTask)
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	result.MappingGroups = []MappingGroup{{MappingGroupID: "group:mixed-split", SourceIdentities: groupSourceIDs,
		TargetRangeIdentities: groupTargetIDs, AuthoringTaskID: splitTask.TaskID,
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer"}}
	return &result
}

func installMigrationDatabaseEvidence(t testing.TB, root string) {
	t.Helper()
	zero := 0
	source := dbevidence.SourceConfig{SourceID: "primary", Engine: dbevidence.EngineMySQL, Database: "fixture", Namespaces: []string{"fixture"},
		CredentialEnv: "FIXTURE_DB_PASSWORD", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}
	cfg := config.DefaultConfig()
	cfg.LedgerEnabled = true
	cfg.DatabaseSources = []dbevidence.SourceConfig{source}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: source.SourceID, Engine: source.Engine,
		Database: source.Database, Namespaces: source.Namespaces, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{},
		IncludeTables: []string{}, ExcludeTables: []string{}, CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve", LowerCaseTableNames: &zero}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/fixture/items", Engine: source.Engine,
		SourceID: source.SourceID, Database: source.Database, Namespace: "fixture", Name: "items", Kind: "base_table",
		Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
}

func refreshLegacyBaseline(t testing.TB, root string) {
	t.Helper()
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func refreshPilotBaseline(t testing.TB, root string, paths []string) {
	t.Helper()
	files := map[string]baseline.Fingerprint{}
	for _, relative := range paths {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, root, ".aoci/baseline.json", string(data))
}

func failMigrationOnce(want string) func(string) error {
	fired := false
	return func(step string) error {
		if !fired && step == want {
			fired = true
			return os.ErrPermission
		}
		return nil
	}
}

func stateFor(status *TransactionStatus, path string) string {
	for _, target := range status.Targets {
		if target.Path == path {
			return target.DiskState
		}
	}
	return "missing"
}

func writeMigrationFile(t testing.TB, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustDecodeLegacy(t testing.TB, envelope *ApplyEnvelope) []byte {
	t.Helper()
	data, err := decodeSnapshotContent(envelope.Snapshot.LegacyContentBase64, envelope.Snapshot.LegacySHA256)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func findLine(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func migrationLedgerCount(root, transactionID string) int {
	events, _ := ledger.Recent(root, 0)
	count := 0
	for _, event := range events {
		if event.Op == "cognition_migration_apply" && event.RecoveryTransactionID == transactionID {
			count++
		}
	}
	return count
}

func cognitionPendingReversal(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".aoci", "transactions"))
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "reversal-") && strings.HasSuffix(entry.Name(), ".json") {
			result = append(result, entry.Name())
		}
	}
	return result, nil
}
