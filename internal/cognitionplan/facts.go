package cognitionplan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/aoci-spec/aoci-code/textassets"
)

type repositoryFacts struct {
	root                   string
	config                 *config.Config
	layout                 string
	layoutRaw              []byte
	layoutIdentity         string
	baselineIdentity       string
	baselineExists         bool
	curationIdentity       string
	inventoryIdentity      string
	evidenceIdentity       string
	sourceEvidenceIdentity string
	repositoryIdentity     string
	registryIdentity       string
	inventory              []InventoryObject
	safeInventory          afs.SafeInventorySummary
	businessSource         businesssource.Manifest
	evidence               []EvidenceObject
	recommendedKinds       []string
	formalBefore           []FormalAssetState
	formalAfter            []FormalAssetState
	formalProof            FormalAssetProof
}

// ExternalGuardFacts are the D2 facts that remain meaningful after Root-last
// activation. Formal layout and Baseline have their own byte-level recovery
// classifications and therefore are not folded into this structure.
type ExternalGuardFacts struct {
	InventoryIdentity      string
	SourceEvidenceIdentity string
	CurationIdentity       string
	RegistryIdentity       string
	Locale                 string
}

// ValidateExternalGuards replays the same inventory, evidence, curation,
// registry, and Locale authorities used by D2-A without interpreting formal
// Bootstrap targets. Resume can call it after Root activation while recovery
// still owns the formal file states.
func ValidateExternalGuards(repositoryRoot string, plan *Plan) error {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return fmt.Errorf("repository_root_invalid")
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return fmt.Errorf("configuration_invalid: %w", err)
	}
	inventory, safeInventory, curationIdentity, err := buildInventory(root, cfg)
	if err != nil {
		return err
	}
	evidence, err := buildEvidence(root, cfg)
	if err != nil {
		return err
	}
	inventoryIdentity := inventoryDigest(inventory, safeInventory)
	manifest, err := businesssource.Build(root, "")
	if err != nil {
		return err
	}
	evidenceIdentity := evidenceDigest(evidence)
	sourceEvidence := newIdentity("source-evidence")
	sourceEvidence.field("business_source_manifest", manifest.AggregateSHA256)
	sourceEvidence.field("database_evidence_identity", evidenceIdentity)
	registryData, err := canonicalJSON(cognition.VolumeRegistry())
	if err != nil {
		return fmt.Errorf("volume_registry_invalid")
	}
	current := ExternalGuardFacts{
		InventoryIdentity: inventoryIdentity, SourceEvidenceIdentity: sourceEvidence.sum(),
		CurationIdentity: curationIdentity, RegistryIdentity: hashBytes(registryData), Locale: plan.Locale,
	}
	if current.InventoryIdentity != plan.InventoryIdentity {
		return fmt.Errorf("inventory_guard_drift")
	}
	if current.SourceEvidenceIdentity != plan.SourceEvidenceIdentity {
		return fmt.Errorf("source_evidence_guard_drift")
	}
	if current.CurationIdentity != plan.CurationIdentity {
		return fmt.Errorf("curation_guard_drift")
	}
	if current.RegistryIdentity != plan.RegistryIdentity {
		return fmt.Errorf("registry_guard_drift")
	}
	return nil
}

var formalAssetPaths = []string{
	"aoci.txt",
	"aoci.meta.txt",
	"aoci.code.txt",
	"aoci.database.txt",
	".aoci/baseline.json",
	".aoci/database-baseline.json",
	".aoci/config.json",
	".aoci/curation.json",
	".aoci/ledger.jsonl",
}

func collectFacts(options Options) (*repositoryFacts, error) {
	root, err := filepath.Abs(options.RepositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("repository_root_invalid")
	}
	before, err := snapshotFormalAssets(root)
	if err != nil {
		return nil, err
	}
	facts := &repositoryFacts{root: root, formalBefore: before}
	facts.config, err = config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("configuration_invalid: %w", err)
	}
	if strings.TrimSpace(options.Locale) != "" {
		facts.config.Locale = strings.TrimSpace(options.Locale)
	}
	facts.layout, facts.layoutRaw, facts.layoutIdentity, err = inspectLayout(root)
	if err != nil {
		return nil, err
	}
	facts.baselineIdentity, facts.baselineExists, err = inspectBaseline(root)
	if err != nil {
		return nil, err
	}
	if err := rejectPendingRecovery(root); err != nil {
		return nil, err
	}
	facts.inventory, facts.safeInventory, facts.curationIdentity, err = buildInventory(root, facts.config)
	if err != nil {
		return nil, err
	}
	facts.evidence, err = buildEvidence(root, facts.config)
	if err != nil {
		return nil, err
	}
	facts.inventoryIdentity = inventoryDigest(facts.inventory, facts.safeInventory)
	manifest, err := businesssource.Build(root, "")
	if err != nil {
		return nil, err
	}
	facts.businessSource = *manifest
	facts.evidenceIdentity = evidenceDigest(facts.evidence)
	sourceEvidence := newIdentity("source-evidence")
	sourceEvidence.field("business_source_manifest", facts.businessSource.AggregateSHA256)
	sourceEvidence.field("database_evidence_identity", facts.evidenceIdentity)
	facts.sourceEvidenceIdentity = sourceEvidence.sum()
	registry := cognition.VolumeRegistry()
	registryData, marshalErr := canonicalJSON(registry)
	if marshalErr != nil {
		return nil, fmt.Errorf("volume_registry_invalid")
	}
	facts.registryIdentity = hashBytes(registryData)
	facts.recommendedKinds = recommendedKinds(facts.inventory, facts.evidence, facts.config.DatabaseSources)
	repository := newIdentity("repository")
	repository.field("layout_identity", facts.layoutIdentity)
	repository.field("inventory_identity", facts.inventoryIdentity)
	repository.field("source_evidence_identity", facts.sourceEvidenceIdentity)
	repository.field("curation_identity", facts.curationIdentity)
	facts.repositoryIdentity = repository.sum()
	after, err := snapshotFormalAssets(root)
	if err != nil {
		return nil, err
	}
	facts.formalAfter = after
	facts.formalProof = compareFormalAssets(before, after)
	if !facts.formalProof.FormalAssetsUnchanged {
		return nil, fmt.Errorf("formal_assets_changed_during_planning")
	}
	return facts, nil
}

func inspectLayout(root string) (string, []byte, string, error) {
	path := filepath.Join(root, "aoci.txt")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		identity := newIdentity("layout")
		identity.field("state", "absent")
		return "uninitialized", nil, identity.sum(), nil
	}
	if err != nil {
		return "", nil, "", fmt.Errorf("layout_inspection_failed")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, "", fmt.Errorf("layout_path_not_regular")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, "", fmt.Errorf("layout_read_failed")
	}
	mode, err := cognition.DetectLayout(raw)
	if err != nil {
		return "", raw, "", fmt.Errorf("root_marker_invalid")
	}
	layout := "legacy"
	if mode == cognition.LayoutVolumesV1 {
		layout = "volumes"
		if _, loadErr := cognition.Load(root, "aoci.txt"); loadErr != nil {
			return "", raw, "", fmt.Errorf("volumes_layout_invalid")
		}
	} else if official, matchErr := OfficialMinimalSkeleton(root, raw); matchErr != nil {
		return "", raw, "", matchErr
	} else if official {
		layout = "uninitialized"
	}
	identity := newIdentity("layout")
	identity.field("state", layout)
	identity.field("aoci_txt_sha256", hashBytes(raw))
	return layout, raw, identity.sum(), nil
}

// OfficialMinimalSkeleton recognizes only the exact zero-Entry file emitted by
// aoci init for this repository root. A non-empty or user-edited Legacy index
// never enters Bootstrap through this exception.
func OfficialMinimalSkeleton(root string, raw []byte) (bool, error) {
	manifest, err := textassets.ReadManifest()
	if err != nil {
		return false, fmt.Errorf("minimal_skeleton_manifest_invalid")
	}
	for _, locale := range manifest.OfficialLocales {
		templateSource, loadErr := textassets.Load(locale, textassets.TemplateMinimalIndex)
		if loadErr != nil {
			return false, fmt.Errorf("minimal_skeleton_template_invalid")
		}
		rendered, renderErr := hooks.RenderTemplate(
			"minimal-index.txt.tmpl",
			templateSource,
			hooks.NewTplData(root),
		)
		if renderErr != nil {
			return false, fmt.Errorf("minimal_skeleton_render_failed")
		}
		if string(raw) == rendered {
			return true, nil
		}
	}
	return false, nil
}

func inspectBaseline(root string) (string, bool, error) {
	path := filepath.Join(root, ".aoci", "baseline.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		identity := newIdentity("baseline")
		identity.field("state", "absent")
		return identity.sum(), false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("baseline_read_failed")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", true, fmt.Errorf("baseline_path_not_regular")
	}
	if _, _, err := baseline.Load(root); err != nil {
		return "", true, fmt.Errorf("baseline_invalid")
	}
	identity := newIdentity("baseline")
	identity.field("state", "present")
	identity.field("sha256", hashBytes(raw))
	return identity.sum(), true, nil
}

func rejectPendingRecovery(root string) error {
	directory := filepath.Join(root, ".aoci", "transactions")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recovery_state_unavailable")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		return fmt.Errorf("pending_recovery")
	}
	return nil
}

func buildInventory(root string, cfg *config.Config) ([]InventoryObject, afs.SafeInventorySummary, string, error) {
	document, _, curationSHA, err := curation.Load(root)
	if err != nil {
		return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("curation_invalid")
	}
	if cfg.ManagedScope != nil || cfg.CognitionBudget != nil {
		state, stateErr := managedstate.Load(root, cfg)
		if stateErr != nil {
			return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("managed_scope_inventory_invalid: %w", stateErr)
		}
		if state.ScopeChangeRequired {
			return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("scope_change_required")
		}
		if state.Evaluation == nil {
			return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("managed_scope_evaluation_unavailable")
		}
		roles := make(map[string]string, state.Evaluation.IndexCount+state.Evaluation.ObserveCount)
		for _, item := range state.Evaluation.Index {
			roles[item.Path] = machinecontract.ScopeRoleIndex
		}
		for _, item := range state.Evaluation.Observe {
			roles[item.Path] = machinecontract.ScopeRoleObserve
		}
		paths := make([]string, 0, len(roles))
		for path := range roles {
			if _, formal := cognition.FormalAssetOwner(path); !formal {
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		result := make([]InventoryObject, 0, len(paths))
		for _, path := range paths {
			profile, profileErr := curation.ProfilePath(root, path)
			if profileErr != nil {
				return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("inventory_profile_failed: %s", path)
			}
			role := roles[path]
			object := InventoryObject{Path: path, SourceSHA256: profile.SourceSHA256, SizeBytes: profile.SizeBytes,
				Lines: profile.Lines, Extension: profile.Ext, Eligible: role == machinecontract.ScopeRoleIndex, ScopeRole: role}
			if !object.Eligible {
				object.Reason = "managed_scope_observe_evidence"
			}
			result = append(result, object)
		}
		summary := state.Evaluation.SafeInventory
		summary.FinalManagedCandidates = len(paths)
		summary.InclusionExclusionIdentity = afs.ManagedSelectionIdentity(state.Evaluation.PolicyIdentity, paths)
		curationIdentity := newIdentity("curation")
		curationIdentity.field("curation_sha256", curationSHA)
		for _, pattern := range cfg.CurationExclude {
			curationIdentity.field("config_exclusion", pattern)
		}
		return result, summary, curationIdentity.sum(), nil
	}
	safeInventory, err := afs.BuildSafeInventory(root, cfg.WalkOptions())
	if err != nil {
		return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("inventory_walk_failed")
	}
	paths := safeInventory.ManagedCandidates
	decisions := make(map[string]curation.Decision, len(document.Decisions))
	for _, decision := range document.Decisions {
		decisions[decision.Path] = decision
	}
	result := make([]InventoryObject, 0, len(paths))
	selectedPaths := make([]string, 0, len(paths))
	curationExcluded := 0
	for _, path := range paths {
		if _, formal := cognition.FormalAssetOwner(path); formal {
			continue
		}
		profile, profileErr := curation.ProfilePath(root, path)
		if profileErr != nil {
			return nil, afs.SafeInventorySummary{}, "", fmt.Errorf("inventory_profile_failed: %s", path)
		}
		object := InventoryObject{Path: path, SourceSHA256: profile.SourceSHA256, SizeBytes: profile.SizeBytes, Lines: profile.Lines, Extension: profile.Ext, Eligible: true}
		if afs.MatchExcludePattern(path, cfg.CurationExclude) {
			object.Eligible = false
			object.Reason = "curation_excluded"
		} else if decision, exists := decisions[path]; exists && decision.SourceSHA256 == profile.SourceSHA256 {
			object.Eligible = decision.Decision == curation.DecisionInclude
			if !object.Eligible {
				object.Reason = "curation_excluded"
			}
		} else if profile.Reason != "" {
			object.Eligible = false
			object.Reason = profile.Reason
		}
		result = append(result, object)
		if object.Eligible {
			selectedPaths = append(selectedPaths, path)
		} else {
			curationExcluded++
		}
	}
	safeInventory.Summary.CurationExcluded = curationExcluded
	safeInventory.Summary.FinalManagedCandidates = len(selectedPaths)
	safeInventory.Summary.InclusionExclusionIdentity = afs.ManagedSelectionIdentity(safeInventory.Summary.RulesIdentity, selectedPaths)
	curationIdentity := newIdentity("curation")
	curationIdentity.field("curation_sha256", curationSHA)
	for _, pattern := range cfg.CurationExclude {
		curationIdentity.field("config_exclusion", pattern)
	}
	return result, safeInventory.Summary, curationIdentity.sum(), nil
}

func buildEvidence(root string, cfg *config.Config) ([]EvidenceObject, error) {
	result := make([]EvidenceObject, 0)
	for _, source := range cfg.DatabaseSources {
		if !source.Enabled {
			continue
		}
		manifest, snapshot, exists, err := dbevidence.LoadSnapshot(root, source.SourceID)
		if err != nil {
			return nil, fmt.Errorf("database_evidence_invalid: %s", source.SourceID)
		}
		if !exists {
			continue
		}
		if !sourceMatchesManifest(source, manifest) || manifest.SourceID != snapshot.SourceID {
			return nil, fmt.Errorf("database_evidence_selection_mismatch: %s", source.SourceID)
		}
		for _, table := range snapshot.Tables {
			result = append(result, EvidenceObject{SourceID: source.SourceID, ObjectRef: table.ObjectRef, EvidenceVersion: snapshot.EvidenceVersion, TableEvidenceSHA256: table.TableEvidenceSHA256, EvidenceRef: table.EvidenceRef})
		}
		if len(snapshot.Tables) == 0 {
			result = append(result, EvidenceObject{SourceID: source.SourceID, ObjectRef: "database://" + source.SourceID + "/-", EvidenceVersion: snapshot.EvidenceVersion, TableEvidenceSHA256: snapshot.SourceSnapshotSHA256, EvidenceRef: source.SourceID + "/snapshot.json"})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SourceID != result[j].SourceID {
			return result[i].SourceID < result[j].SourceID
		}
		return result[i].ObjectRef < result[j].ObjectRef
	})
	return result, nil
}

func sourceMatchesManifest(source dbevidence.SourceConfig, manifest dbevidence.SourceManifest) bool {
	return source.SourceID == manifest.SourceID && source.Engine == manifest.Engine && source.Database == manifest.Database &&
		reflect.DeepEqual(source.Namespaces, manifest.Namespaces) &&
		reflect.DeepEqual(source.IncludeNamespaces, manifest.IncludeNamespaces) &&
		reflect.DeepEqual(source.ExcludeNamespaces, manifest.ExcludeNamespaces) &&
		reflect.DeepEqual(source.IncludeTables, manifest.IncludeTables) &&
		reflect.DeepEqual(source.ExcludeTables, manifest.ExcludeTables)
}

func inventoryDigest(inventory []InventoryObject, safeInventory afs.SafeInventorySummary) string {
	identity := newIdentity("inventory")
	identity.field("safe_inventory_rules_identity", safeInventory.RulesIdentity)
	identity.field("safe_inventory_selection_identity", safeInventory.InclusionExclusionIdentity)
	for _, object := range inventory {
		identity.field("path", object.Path)
		identity.field("source_sha256", object.SourceSHA256)
		identity.field("eligible", fmt.Sprintf("%t", object.Eligible))
		identity.field("scope_role", object.ScopeRole)
		identity.field("reason", object.Reason)
	}
	return identity.sum()
}

func evidenceDigest(evidence []EvidenceObject) string {
	identity := newIdentity("database-evidence")
	for _, object := range evidence {
		identity.field("source_id", object.SourceID)
		identity.field("object_ref", object.ObjectRef)
		identity.field("evidence_sha256", object.TableEvidenceSHA256)
		identity.field("evidence_ref", object.EvidenceRef)
	}
	return identity.sum()
}

func recommendedKinds(inventory []InventoryObject, evidence []EvidenceObject, sources []dbevidence.SourceConfig) []string {
	kinds := make([]string, 0, 2)
	for _, object := range inventory {
		if object.Eligible {
			kinds = append(kinds, "code")
			break
		}
	}
	hasConfiguredEvidence := false
	for _, source := range sources {
		if source.Enabled {
			for _, object := range evidence {
				if object.SourceID == source.SourceID {
					hasConfiguredEvidence = true
					break
				}
			}
		}
	}
	if hasConfiguredEvidence {
		kinds = append(kinds, "database")
	}
	sort.Strings(kinds)
	return kinds
}

func snapshotFormalAssets(root string) ([]FormalAssetState, error) {
	states := make([]FormalAssetState, 0, len(formalAssetPaths))
	for _, rel := range formalAssetPaths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			states = append(states, FormalAssetState{Path: rel, Exists: false, SHA256: hashBytes(nil)})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("formal_asset_inspection_failed: %s", rel)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("formal_asset_not_regular: %s", rel)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("formal_asset_read_failed: %s", rel)
		}
		states = append(states, FormalAssetState{Path: rel, Exists: true, SHA256: hashBytes(raw), SizeBytes: int64(len(raw))})
	}
	return states, nil
}

func compareFormalAssets(before, after []FormalAssetState) FormalAssetProof {
	proof := FormalAssetProof{Before: before, After: after, FormalAssetsUnchanged: reflect.DeepEqual(before, after), BaselineUnchanged: true, CurationUnchanged: true, LedgerWritten: false}
	for index := range before {
		if (before[index].Path == ".aoci/baseline.json" || before[index].Path == ".aoci/database-baseline.json") && !reflect.DeepEqual(before[index], after[index]) {
			proof.BaselineUnchanged = false
		}
		if before[index].Path == ".aoci/curation.json" && !reflect.DeepEqual(before[index], after[index]) {
			proof.CurationUnchanged = false
		}
		if before[index].Path == ".aoci/ledger.jsonl" && !reflect.DeepEqual(before[index], after[index]) {
			proof.LedgerWritten = true
		}
	}
	return proof
}
