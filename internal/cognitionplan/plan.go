package cognitionplan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// BootstrapPlan discovers an uninitialized repository and returns only a
// deterministic authoring request. It never writes candidates or formal assets.
func BootstrapPlan(options Options) (*Plan, error) {
	facts, err := collectFacts(options)
	if err != nil {
		return nil, err
	}
	kinds, err := normalizeTargetKinds(options.TargetKinds)
	if err != nil {
		return nil, err
	}
	status := machinecontract.CognitionPlannerAuthoringRequired
	nextAction := machinecontract.CognitionPlannerAuthoringRequired
	if facts.layout == machinecontract.CognitionPlannerLegacy {
		status = machinecontract.CognitionPlannerMigrationRequired
		nextAction = machinecontract.CognitionPlannerMigrationRequired
	} else if facts.layout == machinecontract.CognitionPlannerVolumes {
		status = machinecontract.CognitionPlannerAlreadyVolumes
		nextAction = machinecontract.CognitionPlannerAlreadyVolumes
	}
	if facts.layout == machinecontract.CognitionPlannerUninitialized {
		if err := validateSelectedKinds(kinds, facts); err != nil {
			return nil, err
		}
	}
	plan := basePlan(machinecontract.CognitionBootstrapPlanV1, OperationBootstrap, status, nextAction, kinds, facts)
	if status == machinecontract.CognitionPlannerAuthoringRequired {
		plan.AuthoringTasks = bootstrapAuthoringTasks(kinds, facts)
		plan.CandidateFrameworks = candidateFrameworks(kinds, facts.config.Locale)
	}
	plan.PlanID = planIdentity(plan)
	if status == machinecontract.CognitionPlannerAuthoringRequired {
		plan.SemanticAuthoringRequirement = SemanticAuthoringRequirementForPlan(plan, nil)
	}
	return plan, nil
}

// MigrationPlan inventories every Legacy information unit and requests a
// complete candidate. It is separate from Bootstrap and has no Apply path.
func MigrationPlan(options Options) (*Plan, error) {
	facts, err := collectFacts(options)
	if err != nil {
		return nil, err
	}
	kinds, err := normalizeTargetKinds(options.TargetKinds)
	if err != nil {
		return nil, err
	}
	status := machinecontract.CognitionPlannerAuthoringRequired
	nextAction := machinecontract.CognitionPlannerAuthoringRequired
	if facts.layout == machinecontract.CognitionPlannerUninitialized {
		status = machinecontract.CognitionPlannerBootstrapRequired
		nextAction = machinecontract.CognitionPlannerBootstrapRequired
	} else if facts.layout == machinecontract.CognitionPlannerVolumes {
		status = machinecontract.CognitionPlannerAlreadyVolumes
		nextAction = machinecontract.CognitionPlannerAlreadyVolumes
	}
	if facts.layout == machinecontract.CognitionPlannerLegacy {
		if !facts.baselineExists {
			return nil, fmt.Errorf("legacy_baseline_required")
		}
		if len(kinds) == 0 {
			kinds = []string{"code"}
		}
		if err := validateSelectedKinds(kinds, facts); err != nil {
			return nil, err
		}
	}
	plan := basePlan(machinecontract.CognitionMigrationPlanV2, OperationMigration, status, nextAction, kinds, facts)
	if status == machinecontract.CognitionPlannerAuthoringRequired {
		mapping, warnings, mappingErr := buildLegacyMapping(facts.root, facts.layoutRaw, facts.config.IndexPath, kinds)
		if mappingErr != nil {
			return nil, mappingErr
		}
		plan.Mapping = mapping
		plan.Warnings = append(plan.Warnings, warnings...)
		plan.AuthoringTasks = migrationAuthoringTasks(kinds, facts, mapping)
		plan.CandidateFrameworks = candidateFrameworks(kinds, facts.config.Locale)
	}
	plan.PlanID = planIdentity(plan)
	return plan, nil
}

func basePlan(version, operation, status, nextAction string, kinds []string, facts *repositoryFacts) *Plan {
	return &Plan{
		Version: version, Operation: operation, Status: status, Layout: facts.layout,
		RepositoryIdentity: facts.repositoryIdentity, LayoutIdentity: facts.layoutIdentity,
		BaselineIdentity: facts.baselineIdentity, InventoryIdentity: facts.inventoryIdentity,
		SourceEvidenceIdentity: facts.sourceEvidenceIdentity, CurationIdentity: facts.curationIdentity,
		Locale: facts.config.Locale, Registry: cognition.VolumeRegistry(), RegistryIdentity: facts.registryIdentity,
		TargetKinds: append([]string{}, kinds...), RecommendedKinds: append([]string{}, facts.recommendedKinds...),
		Inventory: append([]InventoryObject{}, facts.inventory...), Evidence: append([]EvidenceObject{}, facts.evidence...),
		SafeInventory: facts.safeInventory, BusinessSourceManifest: facts.businessSource,
		AuthoringTasks: []AuthoringTask{}, CandidateFrameworks: []CandidateFramework{}, Warnings: []cognition.Finding{},
		FormalAssetProof: facts.formalProof, NetworkAccessed: false, NextAction: nextAction,
	}
}

func normalizeTargetKinds(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		for _, value := range strings.Split(raw, ",") {
			kind := strings.ToLower(strings.TrimSpace(value))
			if kind == "" {
				continue
			}
			if kind != "code" && kind != "database" {
				return nil, fmt.Errorf("target_kind_unknown: %s", kind)
			}
			if !seen[kind] {
				seen[kind] = true
				result = append(result, kind)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func validateSelectedKinds(kinds []string, facts *repositoryFacts) error {
	for _, kind := range kinds {
		switch kind {
		case "code":
			eligible := false
			for _, object := range facts.inventory {
				eligible = eligible || object.Eligible
			}
			if !eligible {
				return fmt.Errorf("code_inventory_not_applicable")
			}
		case "database":
			if len(facts.evidence) == 0 {
				return fmt.Errorf("database_evidence_required")
			}
		}
	}
	return nil
}

func bootstrapAuthoringTasks(kinds []string, facts *repositoryFacts) []AuthoringTask {
	tasks := rootAndMetaTasks()
	for _, kind := range kinds {
		switch kind {
		case "code":
			for _, object := range facts.inventory {
				if !object.Eligible {
					continue
				}
				tasks = append(tasks, AuthoringTask{TaskID: "code:" + object.Path, AssetID: "code", ObjectKind: "file", ObjectRef: "code:" + object.Path, EvidenceRefs: []string{"source:" + object.Path + "@" + object.SourceSHA256}, RequiredSemantic: []string{"tags", "F", "R", "A", "S"}, Reason: "model_semantics_required"})
			}
		case "database":
			for _, object := range facts.evidence {
				if strings.HasSuffix(object.ObjectRef, "/-") {
					continue
				}
				tasks = append(tasks, AuthoringTask{TaskID: "database:" + object.ObjectRef, AssetID: "database", ObjectKind: "table", ObjectRef: object.ObjectRef, EvidenceRefs: []string{"evidence:" + object.EvidenceRef + "@" + object.TableEvidenceSHA256}, RequiredSemantic: []string{"tags", "F", "R", "A", "S"}, Reason: "model_semantics_required"})
			}
		}
	}
	return tasks
}

func rootAndMetaTasks() []AuthoringTask {
	return []AuthoringTask{
		{TaskID: "root:project", AssetID: "root", ObjectKind: "project", EvidenceRefs: []string{}, RequiredSemantic: []string{"project_identity", "project_overview", "global_invariants"}, Reason: "model_semantics_required"},
		{TaskID: "meta:governance", AssetID: "meta", ObjectKind: "meta", EvidenceRefs: []string{}, RequiredSemantic: []string{"rules", "tag_dictionary_code", "tag_dictionary_database"}, Reason: "model_semantics_required"},
	}
}

func migrationAuthoringTasks(kinds []string, facts *repositoryFacts, mapping *SemanticMapping) []AuthoringTask {
	tasks := rootAndMetaTasks()
	for _, record := range mapping.Records {
		if record.Mode != machinecontract.CognitionMappingModelRegenerationRequired {
			continue
		}
		tasks = append(tasks, AuthoringTask{TaskID: "mapping:" + record.UnitID, AssetID: record.TargetAsset, ObjectKind: record.UnitKind, ObjectRef: record.TargetRef, EvidenceRefs: []string{"legacy:" + record.SourceSHA256}, RequiredSemantic: []string{"semantic_mapping_review"}, Reason: record.ReasonCode})
	}
	bootstrapTasks := bootstrapAuthoringTasks(kinds, facts)
	for _, task := range bootstrapTasks[2:] {
		found := false
		for _, existing := range tasks {
			if existing.AssetID == task.AssetID && existing.ObjectRef == task.ObjectRef {
				found = true
				break
			}
		}
		if !found {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func candidateFrameworks(kinds []string, locale string) []CandidateFramework {
	descriptors := []string{"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled"}
	for _, kind := range kinds {
		switch kind {
		case "code":
			descriptors = append(descriptors, "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled")
		case "database":
			descriptors = append(descriptors, "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled")
		}
	}
	root := strings.Join([]string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + locale, "#Project: MODEL_AUTHORING_REQUIRED", "#Global-Invariants: MODEL_AUTHORING_REQUIRED", strings.Join(descriptors, "\n")}, "\n") + "\n"
	meta := strings.Join([]string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2", "#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing", "#Object-Kinds: code=file database=table", "#[Tag dictionary: code]", "#MODEL_AUTHORING_REQUIRED", "#[Tag dictionary: database]", "#MODEL_AUTHORING_REQUIRED"}, "\n") + "\n"
	frameworks := []CandidateFramework{{AssetID: "root", Path: "aoci.txt", Framework: root}, {AssetID: "meta", Path: "aoci.meta.txt", Framework: meta}}
	for _, kind := range kinds {
		registration := map[string]struct{ path, marker string }{"code": {"aoci.code.txt", cognition.CodeVolumeMarker}, "database": {"aoci.database.txt", cognition.DatabaseMarker}}[kind]
		frameworks = append(frameworks, CandidateFramework{AssetID: kind, Path: registration.path, Framework: registration.marker + "\n"})
	}
	return frameworks
}

func planIdentity(plan *Plan) string {
	identity := newIdentity("cognition-plan")
	for _, pair := range [][2]string{
		{"version", plan.Version}, {"operation", plan.Operation}, {"status", plan.Status}, {"layout", plan.Layout},
		{"repository_identity", plan.RepositoryIdentity}, {"layout_identity", plan.LayoutIdentity}, {"baseline_identity", plan.BaselineIdentity},
		{"inventory_identity", plan.InventoryIdentity}, {"source_evidence_identity", plan.SourceEvidenceIdentity},
		{"business_source_manifest", plan.BusinessSourceManifest.AggregateSHA256},
		{"safe_inventory_rules_identity", plan.SafeInventory.RulesIdentity}, {"safe_inventory_selection_identity", plan.SafeInventory.InclusionExclusionIdentity},
		{"curation_identity", plan.CurationIdentity}, {"locale", plan.Locale}, {"registry_identity", plan.RegistryIdentity},
	} {
		identity.field(pair[0], pair[1])
	}
	for _, kind := range plan.TargetKinds {
		identity.field("target_kind", kind)
	}
	for _, task := range plan.AuthoringTasks {
		identity.field("authoring_task", task.TaskID)
	}
	if plan.Mapping != nil {
		identity.field("mapping_sha256", plan.Mapping.MappingSHA256)
	}
	return identity.sum()
}

// ValidateLocale reports whether a requested planner Locale is production
// selectable without changing the process-global catalog.
func ValidateLocale(locale string) error {
	if !textassets.IsOfficialLocale(strings.TrimSpace(locale)) {
		return fmt.Errorf("planner_locale_invalid")
	}
	return nil
}

func legacyDocument(root string, raw []byte) (*index.Document, []index.Warning) {
	document, warnings := index.Parse(string(raw))
	index.ResolveRelPaths(document, root)
	return document, warnings
}
