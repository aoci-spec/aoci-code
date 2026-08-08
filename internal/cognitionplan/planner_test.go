package cognitionplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestBootstrapMatrixAndCompletePreview(t *testing.T) {
	tests := []struct {
		name        string
		code        bool
		database    bool
		kinds       []string
		wantTasks   int
		wantVolumes int
	}{
		{name: "root_meta_only", kinds: nil, wantTasks: 2, wantVolumes: 1},
		{name: "code_only", code: true, kinds: []string{"code"}, wantTasks: 3, wantVolumes: 2},
		{name: "database_only", database: true, kinds: []string{"database"}, wantTasks: 3, wantVolumes: 2},
		{name: "code_database", code: true, database: true, kinds: []string{"code", "database"}, wantTasks: 4, wantVolumes: 3},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			root := t.TempDir()
			if current.code {
				writeFile(t, root, "src/main.go", "package main\n")
			}
			if current.database {
				installDatabaseEvidence(t, root)
			}
			plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: current.kinds})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Status != machinecontract.CognitionPlannerAuthoringRequired || plan.Layout != "uninitialized" || len(plan.AuthoringTasks) != current.wantTasks || !plan.FormalAssetProof.FormalAssetsUnchanged || plan.NetworkAccessed {
				t.Fatalf("unexpected Bootstrap Plan: %#v", plan)
			}
			for _, framework := range plan.CandidateFrameworks {
				if strings.Contains(framework.Framework, "F:") || strings.Contains(framework.Framework, "R:") || strings.Contains(framework.Framework, "A:") || strings.Contains(framework.Framework, "S:") {
					t.Fatalf("program generated FRAS in %s", framework.AssetID)
				}
			}
			candidate := validCandidate(t, root, plan)
			preview, err := ValidateCandidate(root, plan, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil || len(preview.ProjectedDescriptors) != current.wantVolumes || !preview.FormalAssetProof.FormalAssetsUnchanged || preview.NetworkAccessed {
				t.Fatalf("unexpected complete Preview: %#v", preview)
			}
		})
	}
}

func TestBootstrapRejectsRootOwnedAssetInCodeVolume(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range plan.AuthoringTasks {
		if task.ObjectRef == "code:aoci.txt" {
			t.Fatal("Bootstrap classified the Root manifest as Code-owned")
		}
	}
	candidate := validCandidate(t, root, plan)
	for index := range candidate.Assets {
		if candidate.Assets[index].AssetID == "code" {
			candidate.Assets[index].Content += "aoci.txt[CD9S]: F:Invalid duplicate ownership | R:- | A:- | S:Must remain Root-owned\n"
		}
	}
	attachTestHostModelProvenance(plan, candidate)
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status == machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest != nil || !hasRisk(preview.Risks, "volume_ownership_conflict") {
		t.Fatalf("Bootstrap accepted a Root-owned object in Code: %#v", preview)
	}
}

func TestBootstrapLayoutSelectionFailsClosed(t *testing.T) {
	t.Run("official_zero_entry_skeleton_is_uninitialized", func(t *testing.T) {
		root := t.TempDir()
		templateSource, err := textassets.Load("en-US", textassets.TemplateMinimalIndex)
		if err != nil {
			t.Fatal(err)
		}
		minimal, err := hooks.RenderTemplate("minimal-index.txt.tmpl", templateSource, hooks.NewTplData(root))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, "aoci.txt", minimal)
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Layout != machinecontract.CognitionPlannerUninitialized ||
			plan.Status != machinecontract.CognitionPlannerAuthoringRequired {
			t.Fatalf("official zero-Entry skeleton was not Bootstrap-eligible: %#v", plan)
		}
	})
	t.Run("edited_zero_entry_skeleton_remains_legacy", func(t *testing.T) {
		root := t.TempDir()
		templateSource, err := textassets.Load("en-US", textassets.TemplateMinimalIndex)
		if err != nil {
			t.Fatal(err)
		}
		minimal, err := hooks.RenderTemplate("minimal-index.txt.tmpl", templateSource, hooks.NewTplData(root))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, "aoci.txt", minimal+"# user byte\n")
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Layout != machinecontract.CognitionPlannerLegacy ||
			plan.Status != machinecontract.CognitionPlannerMigrationRequired {
			t.Fatalf("edited Legacy bytes were treated as Bootstrap: %#v", plan)
		}
	})
	t.Run("legacy_requires_migration", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "aoci.txt", "# legacy\n")
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "zh-CN"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Status != machinecontract.CognitionPlannerMigrationRequired || len(plan.AuthoringTasks) != 0 {
			t.Fatalf("Legacy was treated as Bootstrap: %#v", plan)
		}
	})
	t.Run("volumes_not_replanned", func(t *testing.T) {
		root := t.TempDir()
		writeVolumeLayout(t, root, nil)
		for _, operation := range []string{OperationBootstrap, OperationMigration} {
			var plan *Plan
			var err error
			if operation == OperationBootstrap {
				plan, err = BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
			} else {
				plan, err = MigrationPlan(Options{RepositoryRoot: root, Locale: "en-US"})
			}
			if err != nil || plan.Status != machinecontract.CognitionPlannerAlreadyVolumes {
				t.Fatalf("%s did not stop on Volumes: plan=%#v err=%v", operation, plan, err)
			}
		}
	})
	t.Run("damaged_root_marker", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "aoci.txt", "#AOCI-ROOT-MANIFEST: 2\n")
		if _, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"}); err == nil || !strings.Contains(err.Error(), "root_marker_invalid") {
			t.Fatalf("damaged Root marker fell back to Legacy: %v", err)
		}
	})
}

func TestMigrationMappingPreservationAndSemanticBoundary(t *testing.T) {
	root := legacyFixture(t, false)
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mapping == nil || plan.Mapping.Coverage.LegacyEntryCoverage != "100.00%" || plan.Mapping.Coverage.LegacySemanticAtomCoverage != "100.00%" || !plan.Mapping.Coverage.ByteReversible {
		t.Fatalf("mapping coverage incomplete: %#v", plan.Mapping)
	}
	if plan.Mapping.Coverage.SemanticReviewStatus != machinecontract.CognitionSemanticEquivalenceUnverified {
		t.Fatalf("structural coverage was overstated as equivalence: %#v", plan.Mapping.Coverage)
	}
	candidate := validCandidate(t, root, plan)
	reviewMigrationMappings(plan, candidate)
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.SemanticMapping == nil || preview.ApprovalDigest == nil {
		t.Fatalf("migration Preview incomplete: %#v", preview)
	}
	preserved := 0
	for _, record := range preview.SemanticMapping.Records {
		if record.UnitKind == "entry" && record.Mode == machinecontract.CognitionMappingPreserved {
			preserved++
		}
	}
	if preserved != 1 || preview.SemanticMapping.Coverage.SemanticReviewStatus != machinecontract.CognitionSemanticEquivalenceUnverified {
		t.Fatalf("byte preservation/equivalence boundary wrong: preserved=%d mapping=%#v", preserved, preview.SemanticMapping)
	}
}

func TestMigrationPlanDoesNotClassifySemanticTargets(t *testing.T) {
	root := legacyFixture(t, false)
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		if record.TargetAsset != "" || record.TargetRef != "" {
			t.Fatalf("Planner classified model-owned semantic destination: %#v", record)
		}
	}
	for _, task := range plan.AuthoringTasks {
		if strings.HasPrefix(task.TaskID, "mapping:") && (task.AssetID != "" || task.ObjectRef != "") {
			t.Fatalf("Planner authoring task classified model-owned destination: %#v", task)
		}
	}
}

func TestMigrationCandidateV1PreservedCodeEntryRemainsBackwardCompatible(t *testing.T) {
	root := legacyFixture(t, false)
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	candidate := validCandidate(t, root, plan)
	for _, record := range plan.Mapping.Records {
		if record.Mode != machinecontract.CognitionMappingModelRegenerationRequired {
			continue
		}
		resolution := MappingResolution{UnitID: record.UnitID, TargetAsset: "root", Reviewer: "model", SemanticReviewed: true}
		if record.UnitKind == "entry" {
			resolution.TargetAsset = "code"
			resolution.TargetRef = "code:" + firstCodePath(plan)
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("frozen D2-A Candidate v1 behavior regressed: %#v err=%v", preview, err)
	}
	for _, record := range preview.SemanticMapping.Records {
		if record.UnitKind == "entry" && record.Mode != machinecontract.CognitionMappingPreserved {
			t.Fatalf("exact preserved Code Entry lost compatibility: %#v", record)
		}
	}
}

func TestDamagedLegacyDoesNotProduceApproval(t *testing.T) {
	root := legacyFixture(t, true)
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mapping.Coverage.DuplicateTargetCount == 0 || len(plan.Warnings) == 0 {
		t.Fatalf("damaged Legacy facts were hidden: %#v", plan.Mapping.Coverage)
	}
	candidate := validCandidate(t, root, plan)
	reviewMigrationMappings(plan, candidate)
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ApprovalDigest != nil || preview.Status == machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("damaged Legacy received approval: %#v", preview)
	}
}

func TestMigrationProjectedRelationClosureFailsClosed(t *testing.T) {
	root := legacyFixture(t, false)
	path := filepath.Join(root, "aoci.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "R:-", "R:code:src/missing.go", 1))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := validCandidate(t, root, plan)
	reviewMigrationMappings(plan, candidate)
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ApprovalDigest != nil || !hasRisk(preview.Risks, "impact_relation_unresolved") {
		t.Fatalf("dangling relation was accepted: %#v", preview)
	}
}

func TestPlanDeterminismSupersessionAndConcurrency(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	first, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil || first.PlanID != second.PlanID {
		t.Fatalf("same inputs were not deterministic: %s %s err=%v", first.PlanID, second.PlanID, err)
	}
	const workers = 8
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			plan, planErr := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
			if planErr != nil {
				errs <- planErr
				return
			}
			ids <- plan.PlanID
		}()
	}
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for id := range ids {
		if id != first.PlanID {
			t.Fatalf("concurrent Plan identity drifted: %s != %s", id, first.PlanID)
		}
	}
	candidate := validCandidate(t, root, first)
	writeFile(t, root, "main.go", "package main\n// changed\n")
	preview, err := ValidateCandidate(root, first, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != machinecontract.CognitionPlannerSuperseded || preview.ApprovalDigest != nil {
		t.Fatalf("source drift did not supersede Plan: %#v", preview)
	}
	localized, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "zh-CN", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	if localized.PlanID == first.PlanID {
		t.Fatal("Locale drift did not invalidate Plan identity")
	}
}

func TestCandidateStrictJSONAndAuthoringGate(t *testing.T) {
	if _, err := DecodeCandidate([]byte(`{"version":"cognition-layout-candidate/v1","plan_id":"a","plan_id":"b","assets":[],"mapping_resolutions":[]}`)); err == nil {
		t.Fatal("duplicate JSON field was accepted")
	}
	if _, err := DecodeCandidate([]byte(`{"version":"cognition-layout-candidate/v1","plan_id":"a","assets":[],"mapping_resolutions":[],"unknown":true}`)); err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
	root := t.TempDir()
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := &LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID, Assets: []CandidateAsset{
		{AssetID: "root", Path: "aoci.txt", Content: plan.CandidateFrameworks[0].Framework},
		{AssetID: "meta", Path: "aoci.meta.txt", Content: plan.CandidateFrameworks[1].Framework},
	}, MappingResolutions: []MappingResolution{}}
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status == machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest != nil || !hasRisk(preview.Risks, "candidate_authoring_incomplete") {
		t.Fatalf("program framework passed as model semantics: %#v", preview)
	}
}

func TestFormalAssetsRemainByteExact(t *testing.T) {
	root := legacyFixture(t, false)
	paths := []string{"aoci.txt", ".aoci/baseline.json"}
	before := readFiles(t, root, paths)
	plan, err := MigrationPlan(Options{RepositoryRoot: root, Locale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := validCandidate(t, root, plan)
	_, _ = ValidateCandidate(root, plan, candidate)
	after := readFiles(t, root, paths)
	for path := range before {
		if before[path] != after[path] {
			t.Fatalf("formal asset changed during planning: %s", path)
		}
	}
	for _, path := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("Planner created formal Volume %s", path)
		}
	}
}

func TestPlannerScaleIdentity(t *testing.T) {
	for _, count := range []int{1, 100, 1000, 10000} {
		inventory := make([]InventoryObject, count)
		for index := range inventory {
			inventory[index] = InventoryObject{Path: fmt.Sprintf("src/%05d.go", index), SourceSHA256: strings.Repeat("a", 64), Eligible: true}
		}
		started := time.Now()
		first := inventoryDigest(inventory, afs.SafeInventorySummary{})
		second := inventoryDigest(inventory, afs.SafeInventorySummary{})
		if first != second {
			t.Fatalf("identity changed at %d objects", count)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("identity planning too slow at %d objects: %s", count, elapsed)
		}
		if testing.Verbose() {
			t.Logf("objects=%d elapsed=%s", count, time.Since(started))
		}
	}
}

func validCandidate(t *testing.T, root string, plan *Plan) *LayoutCandidate {
	t.Helper()
	kinds := plan.TargetKinds
	rootText := validRoot(kinds, plan.Locale)
	assets := []CandidateAsset{{AssetID: "root", Path: "aoci.txt", Content: rootText}, {AssetID: "meta", Path: "aoci.meta.txt", Content: validMeta()}}
	for _, kind := range kinds {
		switch kind {
		case "code":
			lines := make([]string, 0)
			mappedTargets := map[string]bool{}
			if plan.Mapping != nil {
				for _, record := range plan.Mapping.Records {
					if record.UnitKind == "entry" && !containsStringValue(lines, record.SourceText) {
						lines = append(lines, record.SourceText)
						mappedTargets["code:"+firstCodePath(plan)] = true
					}
				}
			}
			for _, object := range plan.Inventory {
				if object.Eligible {
					if mappedTargets["code:"+object.Path] {
						continue
					}
					line := filepath.Base(object.Path) + "[CD9S]: F:Model-authored fixture responsibility | R:- | A:- | S:Keep fixture planning deterministic"
					if !containsStringValue(lines, line) {
						lines = append(lines, line)
					}
				}
			}
			sort.Strings(lines)
			content := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Dir(filepath.Join(root, firstCodePath(plan)))) + "/===\n" + strings.Join(lines, "\n") + "\n"
			assets = append(assets, CandidateAsset{AssetID: "code", Path: "aoci.code.txt", Content: content})
		case "database":
			sections := make([]string, 0)
			for _, evidence := range plan.Evidence {
				if strings.HasSuffix(evidence.ObjectRef, "/-") {
					continue
				}
				parts := strings.Split(strings.TrimPrefix(evidence.ObjectRef, "database://"), "/")
				section := "===Database/database://" + parts[0] + "/" + parts[1] + "/===\n" + parts[2] + "[DB9S]: F:Model-authored table responsibility | R:- | A:- | S:Preserve evidence binding"
				sections = append(sections, section)
			}
			sort.Strings(sections)
			content := cognition.DatabaseMarker + "\n"
			if len(sections) > 0 {
				content += strings.Join(sections, "\n") + "\n"
			}
			assets = append(assets, CandidateAsset{AssetID: "database", Path: "aoci.database.txt", Content: content})
		}
	}
	candidate := &LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID, Assets: assets, MappingResolutions: []MappingResolution{}}
	if plan.Operation == OperationBootstrap {
		attachTestHostModelProvenance(plan, candidate)
	}
	return candidate
}

func attachTestHostModelProvenance(plan *Plan, candidate *LayoutCandidate) {
	candidate.SemanticAuthoringProvenance = &SemanticAuthoringProvenance{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "test-host-model-run", PlanID: plan.PlanID,
		EvidenceBindingSHA256: SemanticAuthoringEvidenceBindingSHA256(plan),
	}
	candidate.SemanticAuthoringProvenance.CandidatePayloadSHA256 = CandidatePayloadSHA256(candidate)
}

func reviewMigrationMappings(plan *Plan, candidate *LayoutCandidate) {
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution := MappingResolution{UnitID: record.UnitID, TargetAsset: record.TargetAsset, TargetRef: record.TargetRef,
			Reviewer: "model", SemanticReviewed: true}
		if record.UnitKind == "entry" {
			resolution.TargetAsset = "code"
			resolution.TargetRef = "code:" + firstCodePath(plan)
		} else {
			resolution.TargetAsset = "root"
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
}

func firstCodePath(plan *Plan) string {
	for _, object := range plan.Inventory {
		if object.Eligible {
			return object.Path
		}
	}
	for _, record := range plan.Mapping.Records {
		if strings.HasPrefix(record.TargetRef, "code:") {
			return strings.TrimPrefix(record.TargetRef, "code:")
		}
	}
	return "main.go"
}

func validRoot(kinds []string, locale string) string {
	lines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + locale, "#Project: Model-authored fixture project", "#Global-Invariants: Model-authored fixture boundary", "#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled"}
	for _, kind := range kinds {
		if kind == "code" {
			lines = append(lines, "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled")
		} else {
			lines = append(lines, "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func validMeta() string {
	return strings.Join([]string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2", "#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing", "#Object-Kinds: code=file database=table", "#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T", "#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T"}, "\n") + "\n"
}

func writeVolumeLayout(t *testing.T, root string, kinds []string) {
	t.Helper()
	writeFile(t, root, "aoci.txt", strings.ReplaceAll(validRoot(kinds, "en-US"), " state=enabled", ""))
	writeFile(t, root, "aoci.meta.txt", validMeta())
	for _, kind := range kinds {
		if kind == "code" {
			writeFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n")
		} else {
			writeFile(t, root, "aoci.database.txt", cognition.DatabaseMarker+"\n")
		}
	}
}

func legacyFixture(t *testing.T, damaged bool) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	entry := "main.go[CD9S]: F:Model-authored legacy responsibility | R:- | A:- | S:Preserve the stable boundary"
	legacy := "#AOCI-CLI Complete Index\n#Project: Fixture\n#[Tag dictionary]\n#A Layer: C Code\n===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + entry + "\n"
	if damaged {
		legacy += entry + "\n"
	}
	writeFile(t, root, "aoci.txt", legacy)
	writeBaseline(t, root)
	return root
}

func writeBaseline(t *testing.T, root string) {
	t.Helper()
	value := &baseline.Baseline{Version: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z", Files: map[string]baseline.Fingerprint{}}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".aoci/baseline.json", string(data)+"\n")
}

func installDatabaseEvidence(t *testing.T, root string) {
	t.Helper()
	zero := 0
	source := dbevidence.SourceConfig{SourceID: "primary", Engine: dbevidence.EngineMySQL, Database: "fixture", Namespaces: []string{"fixture"}, CredentialEnv: "FIXTURE_DB_PASSWORD", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}
	cfg := config.DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{source}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: source.SourceID, Engine: source.Engine, Database: source.Database, Namespaces: source.Namespaces, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{}, CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve", LowerCaseTableNames: &zero}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/fixture/items", Engine: source.Engine, SourceID: source.SourceID, Database: source.Database, Namespace: "fixture", Name: "items", Kind: "base_table", Columns: []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}}, UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
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

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFiles(t *testing.T, root string, paths []string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		result[path] = string(data)
	}
	return result
}

func hasRisk(risks []Risk, code string) bool {
	for _, risk := range risks {
		if risk.Code == code {
			return true
		}
	}
	return false
}

func containsStringValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
