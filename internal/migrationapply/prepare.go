package migrationapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbaseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Prepare replays the read-only planner, reviewed mapping, current guards, and
// projected CognitionSet, then freezes every exact migration postimage. It does
// not write formal or runtime repository state.
func Prepare(repositoryRoot string, request *ApplyRequest) (*ApplyEnvelope, error) {
	if request == nil || request.Version != machinecontract.CognitionMigrationApplyRequestV2 {
		return nil, fmt.Errorf("migration_apply_request_version_unknown")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("migration_repository_root_invalid")
	}
	if err := validateUTC(request.BaselineTimestamp); err != nil {
		return nil, err
	}
	if err := validateSnapshot(&request.Snapshot); err != nil {
		return nil, err
	}
	if request.Snapshot.Eligibility != machinecontract.CognitionMigrationEligibilityEligible || len(request.Snapshot.Findings) != 0 {
		return nil, fmt.Errorf("migration_snapshot_not_eligible")
	}
	if request.Plan.Version != machinecontract.CognitionMigrationPlanV2 || request.Plan.Operation != cognitionplan.OperationMigration ||
		request.Plan.Layout != machinecontract.CognitionPlannerLegacy || request.Plan.Status != machinecontract.CognitionPlannerAuthoringRequired || request.Plan.Mapping == nil {
		return nil, fmt.Errorf("migration_plan_not_eligible")
	}
	if request.Preview.Operation != cognitionplan.OperationMigration || request.Preview.Status != machinecontract.CognitionPlannerPreviewReady || request.Preview.ApprovalDigest == nil {
		return nil, fmt.Errorf("migration_preview_not_ready")
	}
	if request.Plan.NetworkAccessed || request.Preview.NetworkAccessed || request.Snapshot.NetworkAccessed {
		return nil, fmt.Errorf("migration_network_access_forbidden")
	}
	if err := validateSnapshotPlanBinding(&request.Snapshot, &request.Plan); err != nil {
		return nil, err
	}
	if pending, err := cognitiontxn.Pending(root); err != nil {
		return nil, fmt.Errorf("migration_recovery_state_unavailable")
	} else if len(pending) != 0 {
		return nil, fmt.Errorf("migration_pending_recovery")
	}
	if err := validateLiveSnapshotPreimages(root, &request.Snapshot); err != nil {
		return nil, err
	}
	if err := cognitionplan.ValidateExternalGuards(root, &request.Plan); err != nil {
		return nil, fmt.Errorf("migration_guard_drift: %w", err)
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil || cfg == nil || !cfg.LedgerEnabled {
		return nil, fmt.Errorf("migration_ledger_required")
	}

	currentPreview, err := cognitionplan.ValidateCandidate(root, &request.Plan, &request.Candidate)
	if err != nil {
		return nil, err
	}
	if currentPreview.Status == machinecontract.CognitionPlannerSuperseded {
		return nil, fmt.Errorf("migration_plan_superseded")
	}
	if mismatches := cognitionplan.PreviewReplayMismatches(&request.Preview, currentPreview); len(mismatches) != 0 {
		return nil, newReplayMismatch("migration_preview_digest_mismatch", request.Version, mismatches)
	}
	if err := VerifyMapping(root, &request.Snapshot, &request.Plan, &request.Candidate, &request.Mapping); err != nil {
		return nil, err
	}

	assetByID := make(map[string]cognitionplan.CandidateAsset, len(request.Candidate.Assets))
	volumeRaw := map[string][]byte{}
	for _, asset := range request.Candidate.Assets {
		assetByID[asset.AssetID] = asset
		if asset.AssetID != "root" {
			volumeRaw[asset.AssetID] = []byte(asset.Content)
		}
	}
	rootAsset, exists := assetByID["root"]
	if !exists {
		return nil, fmt.Errorf("migration_root_candidate_missing")
	}
	projected, findings := cognition.BuildProjectedSet(root, []byte(rootAsset.Content), volumeRaw)
	if len(findings) != 0 || projected == nil || projected.CompositeIdentity != request.Preview.ProjectedCompositeIdentity {
		return nil, fmt.Errorf("migration_projected_cognition_invalid")
	}
	for _, descriptor := range request.Preview.ProjectedDescriptors {
		if descriptor.State != machinecontract.CognitionVolumeEnabled {
			return nil, fmt.Errorf("migration_candidate_volume_not_enabled: %s", descriptor.ID)
		}
	}

	baselinePlan := request.Plan
	baselinePlan.Mapping = request.Preview.SemanticMapping
	baselineValue, bindings, err := cognitionbaseline.BuildVolumePostimage(root, &baselinePlan, projected, assetByID, request.BaselineTimestamp)
	if err != nil {
		return nil, err
	}
	baselineBytes, err := baseline.MarshalExact(baselineValue)
	if err != nil {
		return nil, err
	}
	runtimeContent, err := textassets.Load(request.Plan.Locale, textassets.TemplateAOCIGitignore)
	if err != nil {
		return nil, err
	}
	runtimeBoundary := FormalPostimage{
		AssetID: "runtime_boundary", Kind: "runtime", Path: ".aoci/.gitignore", PreimageState: "exact_or_absent",
		PreimageSHA256: sha256Hex(nil), PostSHA256: sha256Hex([]byte(runtimeContent)), ByteSize: int64(len([]byte(runtimeContent))), FileMode: "0644", Content: runtimeContent,
	}
	if err := validateRuntimeBoundary(root, &runtimeBoundary); err != nil {
		return nil, err
	}

	orderedIDs := []string{"meta"}
	for _, id := range []string{"code", "database"} {
		if _, exists := assetByID[id]; exists {
			orderedIDs = append(orderedIDs, id)
		}
	}
	volumes := make([]FormalPostimage, 0, len(orderedIDs))
	writeOrder := []string{}
	writeSet := []string{runtimeBoundary.Path}
	for _, id := range orderedIDs {
		asset, exists := assetByID[id]
		if !exists {
			return nil, fmt.Errorf("migration_candidate_asset_missing: %s", id)
		}
		if err := requireAbsent(root, asset.Path); err != nil {
			return nil, err
		}
		content := []byte(asset.Content)
		volumes = append(volumes, FormalPostimage{
			AssetID: id, Kind: id, Path: asset.Path, PreimageState: "absent", PreimageSHA256: sha256Hex(nil),
			PostSHA256: sha256Hex(content), ByteSize: int64(len(content)), FileMode: "0644", Content: asset.Content,
		})
		writeOrder = append(writeOrder, asset.Path)
		writeSet = append(writeSet, asset.Path)
	}
	rootPostimage := FormalPostimage{
		AssetID: "root", Kind: "root", Path: "aoci.txt", PreimageState: "legacy",
		PreimageSHA256: request.Snapshot.LegacySHA256, PostSHA256: sha256Hex([]byte(rootAsset.Content)),
		ByteSize: int64(len([]byte(rootAsset.Content))), FileMode: request.Snapshot.LegacyFileMode, Content: rootAsset.Content,
	}
	baselinePostimage := BaselinePostimage{
		Path: ".aoci/baseline.json", PreimageSHA256: request.Snapshot.BaselineSHA256,
		PostSHA256: sha256Hex(baselineBytes), ByteSize: int64(len(baselineBytes)), FileMode: request.Snapshot.BaselineFileMode, Content: string(baselineBytes),
	}
	writeOrder = append(writeOrder, rootPostimage.Path, baselinePostimage.Path)
	writeSet = append(writeSet, rootPostimage.Path, baselinePostimage.Path)
	riskDigest := migrationRiskDiffDigest(&request.Snapshot, &request.Mapping, &request.Preview)
	semanticDiff := buildMigrationSemanticDiff(&request.Mapping)
	semanticDigest := sha256Hex([]byte(request.Preview.LogicalDiff.LogicalDiffSHA256 + "\n" + semanticDiff.DiffSHA256 + "\n"))
	envelope := &ApplyEnvelope{
		Version: machinecontract.CognitionMigrationApplyEnvelopeV2, RequestVersion: request.Version, Operation: OperationMigration,
		PlanID: request.Preview.PlanID, CandidateIdentity: request.Preview.CandidateIdentity, D2AApprovalDigest: request.Preview.ApprovalDigest.Digest,
		Snapshot: request.Snapshot, Mapping: request.Mapping, MappingSHA256: request.Mapping.MappingSHA256,
		RepositoryIdentity: request.Plan.RepositoryIdentity, LayoutIdentity: request.Plan.LayoutIdentity, BaselineIdentity: request.Plan.BaselineIdentity,
		InventoryIdentity: request.Plan.InventoryIdentity, SourceEvidenceIdentity: request.Plan.SourceEvidenceIdentity,
		CurationIdentity: request.Plan.CurationIdentity, RegistryIdentity: request.Plan.RegistryIdentity,
		ValidatorIdentity: machinecontract.CognitionMigrationValidatorV2, ProjectedCompositeIdentity: projected.CompositeIdentity,
		Locale: request.Plan.Locale, Plan: request.Plan, Candidate: request.Candidate, Preview: request.Preview,
		RuntimeBoundary: runtimeBoundary, VolumeTargets: volumes, Root: rootPostimage, Baseline: baselinePostimage,
		DatabaseBindings: bindings, PhysicalDiffSHA256: request.Preview.PhysicalDiff.PhysicalDiffSHA256,
		SemanticDiffSHA256: semanticDigest, SemanticDiff: semanticDiff, RiskDiffSHA256: riskDigest,
		ReviewSet: append([]string{}, request.Preview.Sets.Review...), WriteSet: writeSet, GuardSet: append([]string{}, request.Preview.Sets.Guard...),
		WriteOrder: writeOrder, RootLast: true, RecoveryDirection: machinecontract.CognitionMigrationRecoveryDirection,
		NetworkAccessed: false, PreparedAt: request.BaselineTimestamp,
	}
	envelope.EnvelopeDigest, err = envelopeDigest(envelope)
	if err != nil {
		return nil, err
	}
	return envelope, validateEnvelope(envelope)
}

func validateSnapshotPlanBinding(snapshot *LegacySnapshot, plan *cognitionplan.Plan) error {
	if snapshot.RepositoryIdentity != plan.RepositoryIdentity || snapshot.LayoutIdentity != plan.LayoutIdentity ||
		snapshot.BaselineIdentity != plan.BaselineIdentity || snapshot.InventoryIdentity != plan.InventoryIdentity ||
		snapshot.SourceEvidenceIdentity != plan.SourceEvidenceIdentity || snapshot.CurationIdentity != plan.CurationIdentity ||
		snapshot.RegistryIdentity != plan.RegistryIdentity {
		return fmt.Errorf("migration_snapshot_plan_binding_mismatch")
	}
	return nil
}

func validateLiveSnapshotPreimages(root string, snapshot *LegacySnapshot) error {
	for _, item := range []struct{ relative, digest string }{{snapshot.LegacyPath, snapshot.LegacySHA256}, {snapshot.BaselinePath, snapshot.BaselineSHA256}} {
		path := filepath.Join(root, filepath.FromSlash(item.relative))
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("migration_preimage_wrong_type: %s", item.relative)
		}
		data, err := os.ReadFile(path)
		if err != nil || sha256Hex(data) != item.digest {
			return fmt.Errorf("migration_preimage_drift: %s", item.relative)
		}
	}
	for _, relative := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
		if err := requireAbsent(root, relative); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeBoundary(root string, boundary *FormalPostimage) error {
	path := filepath.Join(root, filepath.FromSlash(boundary.Path))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("migration_runtime_boundary_wrong_type")
	}
	data, err := os.ReadFile(path)
	if err != nil || sha256Hex(data) != boundary.PostSHA256 {
		return fmt.Errorf("migration_runtime_boundary_conflict")
	}
	return nil
}

func requireAbsent(root, relative string) error {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migration_target_inspection_failed: %s", relative)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("migration_target_wrong_type: %s", relative)
	}
	return fmt.Errorf("migration_target_exists: %s", relative)
}

func migrationRiskDiffDigest(snapshot *LegacySnapshot, mapping *MigrationMapping, preview *cognitionplan.Preview) string {
	value := struct {
		SnapshotFindings []SnapshotFinding    `json:"snapshot_findings"`
		Coverage         MappingCoverage      `json:"mapping_coverage"`
		PreviewRisks     []cognitionplan.Risk `json:"preview_risks"`
	}{snapshot.Findings, mapping.Coverage, preview.Risks}
	data, _ := canonicalJSON(value)
	return sha256Hex(data)
}

func envelopeDigest(envelope *ApplyEnvelope) (string, error) {
	copyValue := *envelope
	copyValue.EnvelopeDigest = ""
	copyValue.PreparedAt = ""
	copyValue.Snapshot.CapturedAt = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateEnvelope(envelope *ApplyEnvelope) error {
	if envelope == nil || envelope.Version != machinecontract.CognitionMigrationApplyEnvelopeV2 || envelope.RequestVersion != machinecontract.CognitionMigrationApplyRequestV2 ||
		envelope.Operation != OperationMigration || !envelope.RootLast || envelope.NetworkAccessed ||
		envelope.RecoveryDirection != machinecontract.CognitionMigrationRecoveryDirection || envelope.ValidatorIdentity != machinecontract.CognitionMigrationValidatorV2 ||
		envelope.Plan.Version != machinecontract.CognitionMigrationPlanV2 || envelope.Candidate.Version != machinecontract.CognitionLayoutCandidateV1 ||
		envelope.Preview.Version != machinecontract.CognitionLayoutPreviewV1 || envelope.Preview.ApprovalDigest == nil ||
		envelope.Mapping.Version != machinecontract.CognitionMigrationMappingV2 || envelope.Snapshot.Version != machinecontract.CognitionLegacySnapshotV1 {
		return fmt.Errorf("migration_apply_envelope_invalid")
	}
	digest, err := envelopeDigest(envelope)
	if err != nil || digest != envelope.EnvelopeDigest {
		return fmt.Errorf("migration_apply_envelope_digest_mismatch")
	}
	if envelope.D2AApprovalDigest != envelope.Preview.ApprovalDigest.Digest || envelope.MappingSHA256 != envelope.Mapping.MappingSHA256 ||
		envelope.PlanID != envelope.Preview.PlanID || envelope.CandidateIdentity != envelope.Preview.CandidateIdentity ||
		envelope.Snapshot.SnapshotIdentity != envelope.Mapping.SnapshotIdentity {
		return fmt.Errorf("migration_apply_envelope_binding_mismatch")
	}
	if len(envelope.VolumeTargets) < 1 || len(envelope.WriteOrder) != len(envelope.VolumeTargets)+2 || len(envelope.WriteSet) != len(envelope.WriteOrder)+1 ||
		envelope.WriteSet[0] != envelope.RuntimeBoundary.Path || envelope.Root.Path != "aoci.txt" || envelope.Baseline.Path != ".aoci/baseline.json" ||
		envelope.WriteOrder[len(envelope.WriteOrder)-2] != envelope.Root.Path || envelope.WriteOrder[len(envelope.WriteOrder)-1] != envelope.Baseline.Path {
		return fmt.Errorf("migration_apply_write_set_invalid")
	}
	for indexValue, target := range envelope.VolumeTargets {
		if target.PreimageState != "absent" || target.PreimageSHA256 != sha256Hex(nil) || target.Path != envelope.WriteOrder[indexValue] ||
			target.Path != envelope.WriteSet[indexValue+1] || target.PostSHA256 != sha256Hex([]byte(target.Content)) ||
			target.ByteSize != int64(len([]byte(target.Content))) || target.FileMode != "0644" {
			return fmt.Errorf("migration_volume_target_invalid: %s", target.Path)
		}
	}
	if envelope.VolumeTargets[0].AssetID != "meta" || envelope.Root.PreimageSHA256 != envelope.Snapshot.LegacySHA256 ||
		envelope.Root.PostSHA256 != sha256Hex([]byte(envelope.Root.Content)) || envelope.Root.ByteSize != int64(len([]byte(envelope.Root.Content))) ||
		envelope.Baseline.PreimageSHA256 != envelope.Snapshot.BaselineSHA256 || envelope.Baseline.PostSHA256 != sha256Hex([]byte(envelope.Baseline.Content)) ||
		envelope.Baseline.ByteSize != int64(len([]byte(envelope.Baseline.Content))) {
		return fmt.Errorf("migration_root_or_baseline_invalid")
	}
	if envelope.PhysicalDiffSHA256 != envelope.Preview.PhysicalDiff.PhysicalDiffSHA256 ||
		envelope.SemanticDiffSHA256 != sha256Hex([]byte(envelope.Preview.LogicalDiff.LogicalDiffSHA256+"\n"+envelope.SemanticDiff.DiffSHA256+"\n")) ||
		envelope.SemanticDiff.DiffSHA256 != buildMigrationSemanticDiff(&envelope.Mapping).DiffSHA256 ||
		envelope.RiskDiffSHA256 != migrationRiskDiffDigest(&envelope.Snapshot, &envelope.Mapping, &envelope.Preview) ||
		!reflect.DeepEqual(envelope.ReviewSet, envelope.Preview.Sets.Review) || !reflect.DeepEqual(envelope.GuardSet, envelope.Preview.Sets.Guard) {
		return fmt.Errorf("migration_diff_or_review_set_mismatch")
	}
	if strings.TrimSpace(envelope.MappingSHA256) == "" {
		return fmt.Errorf("migration_mapping_digest_missing")
	}
	return validateUTC(envelope.PreparedAt)
}

func buildMigrationSemanticDiff(mapping *MigrationMapping) MigrationSemanticDiff {
	diff := MigrationSemanticDiff{Version: "migration-semantic-diff/v2", Entries: []FieldPreservationDiff{}}
	if mapping != nil {
		for _, record := range mapping.Records {
			if record.SourceKind != "entry" {
				continue
			}
			entry := FieldPreservationDiff{SourceIdentity: record.SourceIdentity, TargetObject: record.TargetObject, Mode: record.MappingMode,
				PreservedFields: []string{}, RegeneratedFields: []string{}}
			switch record.MappingMode {
			case machinecontract.CognitionMappingPreserved:
				entry.PreservedFields = []string{"tags", "F", "R", "A", "S"}
			case machinecontract.CognitionMappingFieldPreserved:
				if record.EntryPreservation != nil {
					entry.PreservedFields = append([]string{}, record.EntryPreservation.PreservedFields...)
					entry.RegeneratedFields = append([]string{}, record.EntryPreservation.RegeneratedFields...)
					entry.IdentityCanonicalization = record.EntryPreservation.IdentityCanonicalizationProposal != nil
				}
			default:
				entry.RegeneratedFields = []string{"tags", "F", "R", "A", "S"}
				diff.FullRegenerated++
			}
			diff.PreservedFields += len(entry.PreservedFields)
			diff.RegeneratedFields += len(entry.RegeneratedFields)
			diff.Entries = append(diff.Entries, entry)
		}
	}
	copyValue := diff
	copyValue.DiffSHA256 = ""
	data, _ := canonicalJSON(copyValue)
	diff.DiffSHA256 = sha256Hex(data)
	return diff
}
