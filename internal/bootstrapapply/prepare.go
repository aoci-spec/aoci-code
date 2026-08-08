package bootstrapapply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbaseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

const bootstrapValidatorIdentity = machinecontract.CognitionBootstrapValidatorV1

// Prepare replays D2-A and freezes every formal postimage. It performs no
// filesystem write.
func Prepare(repositoryRoot string, request *ApplyRequest) (*ApplyEnvelope, error) {
	if request == nil || request.Version != machinecontract.CognitionBootstrapApplyRequestV1 {
		return nil, fmt.Errorf("bootstrap_apply_request_version_unknown")
	}
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("repository_root_invalid")
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("bootstrap_automation_policy_invalid")
	}
	automationPolicy := cfg.ResolveOnboardingAutomation(true)
	if request.Plan.Operation != cognitionplan.OperationBootstrap ||
		request.Plan.Layout != machinecontract.CognitionPlannerUninitialized ||
		request.Plan.Status != machinecontract.CognitionPlannerAuthoringRequired {
		return nil, fmt.Errorf("bootstrap_not_eligible")
	}
	if request.Preview.Operation != cognitionplan.OperationBootstrap ||
		request.Preview.Status != machinecontract.CognitionPlannerPreviewReady ||
		request.Preview.ApprovalDigest == nil {
		return nil, fmt.Errorf("bootstrap_preview_not_ready")
	}
	if request.Plan.NetworkAccessed || request.Preview.NetworkAccessed {
		return nil, fmt.Errorf("bootstrap_network_access_forbidden")
	}
	if _, exists, err := baseline.Load(root); err != nil {
		return nil, fmt.Errorf("bootstrap_baseline_invalid: %w", err)
	} else if exists {
		// Current Baseline v1 has no runtime-only provenance marker. Even an
		// empty file is therefore formal governance and cannot be washed away.
		return nil, fmt.Errorf("bootstrap_baseline_present")
	}
	currentPreview, err := cognitionplan.ValidateCandidate(root, &request.Plan, &request.Candidate)
	if err != nil {
		return nil, err
	}
	if currentPreview.Status == machinecontract.CognitionPlannerSuperseded {
		return nil, fmt.Errorf("bootstrap_plan_superseded")
	}
	if len(cognitionplan.PreviewReplayMismatches(&request.Preview, currentPreview)) != 0 {
		return nil, fmt.Errorf("bootstrap_preview_digest_mismatch")
	}
	if err := validateUTC(request.BaselineTimestamp); err != nil {
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
	projected, findings := cognition.BuildProjectedSet(root, []byte(assetByID["root"].Content), volumeRaw)
	if len(findings) > 0 || projected == nil || projected.CompositeIdentity != request.Preview.ProjectedCompositeIdentity {
		return nil, fmt.Errorf("bootstrap_projected_cognition_invalid")
	}
	for _, descriptor := range request.Preview.ProjectedDescriptors {
		if descriptor.State != machinecontract.CognitionVolumeEnabled {
			return nil, fmt.Errorf("bootstrap_candidate_volume_not_enabled: %s", descriptor.ID)
		}
	}

	baselineValue, bindings, err := cognitionbaseline.BuildVolumePostimage(root, &request.Plan, projected, assetByID, request.BaselineTimestamp)
	if err != nil {
		return nil, err
	}
	baselineBytes, err := baseline.MarshalExact(baselineValue)
	if err != nil {
		return nil, err
	}

	orderedIDs := []string{"meta"}
	for _, id := range []string{"code", "database"} {
		if _, exists := assetByID[id]; exists {
			orderedIDs = append(orderedIDs, id)
		}
	}
	orderedIDs = append(orderedIDs, "root")
	targets := make([]FormalPostimage, 0, len(orderedIDs))
	runtimeContent, err := textassets.Load(request.Plan.Locale, textassets.TemplateAOCIGitignore)
	if err != nil {
		return nil, err
	}
	runtimeBoundary := FormalPostimage{
		AssetID: "runtime_boundary", Kind: "runtime", Path: ".aoci/.gitignore",
		ExpectedPreimage: PreimageAbsent, PostSHA256: sha256Hex([]byte(runtimeContent)),
		ByteSize: int64(len([]byte(runtimeContent))), FileMode: "0644", Content: runtimeContent,
	}
	if info, statErr := os.Lstat(filepath.Join(root, ".aoci", ".gitignore")); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("bootstrap_runtime_gitignore_wrong_type")
		}
		data, readErr := os.ReadFile(filepath.Join(root, ".aoci", ".gitignore"))
		if readErr != nil || sha256Hex(data) != runtimeBoundary.PostSHA256 {
			return nil, fmt.Errorf("bootstrap_runtime_gitignore_conflict")
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("bootstrap_runtime_gitignore_unavailable")
	}
	writeSet := make([]string, 0, len(orderedIDs)+2)
	writeSet = append(writeSet, runtimeBoundary.Path)
	writeOrder := make([]string, 0, len(orderedIDs)+1)
	for _, id := range orderedIDs {
		asset, exists := assetByID[id]
		if !exists {
			return nil, fmt.Errorf("bootstrap_candidate_asset_missing: %s", id)
		}
		expectedPreimage := PreimageAbsent
		preimageSHA256 := ""
		preimageContent := ""
		if id == "root" {
			currentRoot, readErr := os.ReadFile(filepath.Join(root, "aoci.txt"))
			if readErr == nil {
				official, matchErr := cognitionplan.OfficialMinimalSkeleton(root, currentRoot)
				if matchErr != nil {
					return nil, matchErr
				}
				if !official {
					return nil, fmt.Errorf("bootstrap_target_exists: %s", asset.Path)
				}
				expectedPreimage = PreimageOfficialMinimal
				preimageSHA256 = sha256Hex(currentRoot)
				preimageContent = string(currentRoot)
			} else if !os.IsNotExist(readErr) {
				return nil, fmt.Errorf("bootstrap_target_inspection_failed: %s", asset.Path)
			}
		} else if err := requireAbsentRegularBoundary(root, asset.Path); err != nil {
			return nil, err
		}
		kind := id
		if id == "root" {
			kind = "root"
		}
		content := []byte(asset.Content)
		targets = append(targets, FormalPostimage{
			AssetID: id, Kind: kind, Path: asset.Path, ExpectedPreimage: expectedPreimage,
			PreimageSHA256: preimageSHA256, PreimageContent: preimageContent,
			PostSHA256: sha256Hex(content), ByteSize: int64(len(content)), FileMode: "0644", Content: asset.Content,
		})
		writeSet = append(writeSet, asset.Path)
		writeOrder = append(writeOrder, asset.Path)
	}
	if err := requireAbsentRegularBoundary(root, ".aoci/baseline.json"); err != nil {
		return nil, err
	}
	writeSet = append(writeSet, ".aoci/baseline.json")
	writeOrder = append(writeOrder, ".aoci/baseline.json")

	envelope := &ApplyEnvelope{
		Version:        machinecontract.CognitionBootstrapApplyEnvelopeV1,
		RequestVersion: request.Version, Operation: OperationBootstrap,
		PlanID: request.Preview.PlanID, CandidateIdentity: request.Preview.CandidateIdentity,
		D2AApprovalDigest:  request.Preview.ApprovalDigest.Digest,
		RepositoryIdentity: request.Plan.RepositoryIdentity, LayoutIdentity: request.Plan.LayoutIdentity,
		BaselineIdentity: request.Plan.BaselineIdentity, InventoryIdentity: request.Plan.InventoryIdentity,
		SourceEvidenceIdentity: request.Plan.SourceEvidenceIdentity, CurationIdentity: request.Plan.CurationIdentity,
		RegistryIdentity: request.Plan.RegistryIdentity, ValidatorIdentity: bootstrapValidatorIdentity,
		ProjectedCompositeIdentity: projected.CompositeIdentity, Locale: request.Plan.Locale,
		AutomationPolicy: automationPolicy,
		Plan:             request.Plan, Candidate: request.Candidate, Preview: request.Preview,
		RuntimeBoundary: runtimeBoundary,
		Targets:         targets,
		Baseline: BaselinePostimage{
			Path: ".aoci/baseline.json", ExpectedPreimage: PreimageAbsent,
			PostSHA256: sha256Hex(baselineBytes), ByteSize: int64(len(baselineBytes)), FileMode: "0644", Content: string(baselineBytes),
		},
		DatabaseBindings: bindings,
		ReviewSet:        append([]string{}, request.Preview.Sets.Review...),
		WriteSet:         writeSet, GuardSet: append([]string{}, request.Preview.Sets.Guard...), WriteOrder: writeOrder,
		RootLast: true, RecoveryDirection: machinecontract.CognitionBootstrapRecoveryDirection,
		NetworkAccessed: false, PreparedAt: request.BaselineTimestamp,
	}
	digest, err := envelopeDigest(envelope)
	if err != nil {
		return nil, err
	}
	envelope.EnvelopeDigest = digest
	return envelope, nil
}

func requireAbsentRegularBoundary(root, relativePath string) error {
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("bootstrap_target_inspection_failed: %s", relativePath)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("bootstrap_target_wrong_type: %s", relativePath)
	}
	return fmt.Errorf("bootstrap_target_exists: %s", relativePath)
}

func envelopeDigest(envelope *ApplyEnvelope) (string, error) {
	copyValue := *envelope
	copyValue.EnvelopeDigest = ""
	copyValue.PreparedAt = ""
	data, err := canonicalJSON(copyValue)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateEnvelope(envelope *ApplyEnvelope) error {
	if envelope == nil || envelope.Version != machinecontract.CognitionBootstrapApplyEnvelopeV1 ||
		envelope.Operation != OperationBootstrap || !envelope.RootLast || envelope.NetworkAccessed ||
		envelope.RecoveryDirection != machinecontract.CognitionBootstrapRecoveryDirection ||
		envelope.ValidatorIdentity != bootstrapValidatorIdentity ||
		envelope.RequestVersion != machinecontract.CognitionBootstrapApplyRequestV1 ||
		envelope.Plan.Version != machinecontract.CognitionBootstrapPlanV1 ||
		envelope.Candidate.Version != machinecontract.CognitionLayoutCandidateV1 ||
		envelope.Preview.Version != machinecontract.CognitionLayoutPreviewV1 ||
		envelope.Preview.ApprovalDigest == nil ||
		envelope.Preview.ApprovalDigest.Version != machinecontract.CognitionApprovalDigestV1 ||
		envelope.Plan.Registry.Version != machinecontract.CognitionVolumeRegistryV1 {
		return fmt.Errorf("bootstrap_apply_envelope_invalid")
	}
	if !validBootstrapAutomationPolicy(envelope.AutomationPolicy) {
		return fmt.Errorf("bootstrap_automation_policy_invalid")
	}
	digest, err := envelopeDigest(envelope)
	if err != nil || digest != envelope.EnvelopeDigest {
		return fmt.Errorf("bootstrap_apply_envelope_digest_mismatch")
	}
	if envelope.Preview.ApprovalDigest.Digest != envelope.D2AApprovalDigest ||
		envelope.PlanID != envelope.Preview.PlanID || envelope.CandidateIdentity != envelope.Preview.CandidateIdentity {
		return fmt.Errorf("bootstrap_apply_envelope_d2a_mismatch")
	}
	if len(envelope.Targets) < 2 || len(envelope.WriteOrder) != len(envelope.Targets)+1 || len(envelope.WriteSet) != len(envelope.WriteOrder)+1 ||
		envelope.WriteSet[0] != envelope.RuntimeBoundary.Path || envelope.RuntimeBoundary.Path != ".aoci/.gitignore" ||
		envelope.RuntimeBoundary.ExpectedPreimage != PreimageAbsent ||
		envelope.RuntimeBoundary.PostSHA256 != sha256Hex([]byte(envelope.RuntimeBoundary.Content)) ||
		envelope.RuntimeBoundary.ByteSize != int64(len([]byte(envelope.RuntimeBoundary.Content))) ||
		envelope.RuntimeBoundary.FileMode != "0644" {
		return fmt.Errorf("bootstrap_apply_write_set_invalid")
	}
	for index, target := range envelope.Targets {
		validPreimage := target.ExpectedPreimage == PreimageAbsent && target.PreimageSHA256 == "" && target.PreimageContent == ""
		if target.AssetID == "root" && target.ExpectedPreimage == PreimageOfficialMinimal &&
			target.PreimageSHA256 == sha256Hex([]byte(target.PreimageContent)) {
			// Prepare and its under-lock replay establish the exact official
			// template match. The immutable envelope binds those preimage bytes.
			validPreimage = true
		}
		if !validPreimage || target.Path != envelope.WriteOrder[index] ||
			target.Path != envelope.WriteSet[index+1] ||
			target.PostSHA256 != sha256Hex([]byte(target.Content)) || target.ByteSize != int64(len([]byte(target.Content))) || target.FileMode != "0644" {
			return fmt.Errorf("bootstrap_apply_target_invalid: %s", target.Path)
		}
	}
	if envelope.Targets[len(envelope.Targets)-1].AssetID != "root" || envelope.Targets[len(envelope.Targets)-1].Path != "aoci.txt" ||
		envelope.Baseline.Path != ".aoci/baseline.json" || envelope.WriteOrder[len(envelope.WriteOrder)-1] != envelope.Baseline.Path ||
		envelope.WriteSet[len(envelope.WriteSet)-1] != envelope.Baseline.Path ||
		envelope.Baseline.ExpectedPreimage != PreimageAbsent || envelope.Baseline.PostSHA256 != sha256Hex([]byte(envelope.Baseline.Content)) ||
		envelope.Baseline.ByteSize != int64(len([]byte(envelope.Baseline.Content))) || envelope.Baseline.FileMode != "0644" {
		return fmt.Errorf("bootstrap_apply_root_last_or_baseline_invalid")
	}
	return validateUTC(envelope.PreparedAt)
}

func validateUTC(value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("bootstrap_baseline_timestamp_invalid")
	}
	_, offset := parsed.Zone()
	if offset != 0 || !strings.HasSuffix(value, "Z") {
		return fmt.Errorf("bootstrap_baseline_timestamp_not_utc")
	}
	return nil
}
