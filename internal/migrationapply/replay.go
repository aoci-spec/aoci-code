package migrationapply

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
)

const ReplayDiagnosticV1 = "migration-prepare-replay-diagnostic/v1"

type ReplayMismatchItem = cognitionplan.PreviewReplayMismatch

// ReplayMismatchReport is deliberately digest-only. It must never contain
// source, FRAS, Snapshot, Evidence, credential, or database bodies.
type ReplayMismatchReport struct {
	Version             string               `json:"version"`
	ContractVersion     string               `json:"contract_version"`
	Items               []ReplayMismatchItem `json:"items"`
	FormalWritesStarted bool                 `json:"formal_writes_started"`
}

type ReplayMismatchError struct {
	Code   string
	Report ReplayMismatchReport
}

func (e *ReplayMismatchError) Error() string {
	codes := make([]string, 0, len(e.Report.Items)+1)
	codes = append(codes, e.Code)
	for _, item := range e.Report.Items {
		codes = append(codes, item.Code)
	}
	return strings.Join(codes, " ")
}

func newReplayMismatch(code, contract string, items []ReplayMismatchItem) error {
	return &ReplayMismatchError{Code: code, Report: ReplayMismatchReport{
		Version: ReplayDiagnosticV1, ContractVersion: contract,
		Items: items, FormalWritesStarted: false,
	}}
}

func envelopeReplayMismatches(expected, actual *ApplyEnvelope, approval *Approval) []ReplayMismatchItem {
	items := make([]ReplayMismatchItem, 0, 11)
	appendDigest := func(code, field, left, right string, leftCount, rightCount int) {
		if left != right {
			items = append(items, ReplayMismatchItem{Code: code, Field: field, ExpectedSHA256: digestValue(left), ActualSHA256: digestValue(right), ExpectedCount: leftCount, ActualCount: rightCount})
		}
	}
	appendDigest("physical_diff_digest_mismatch", "physical_diff_sha256", expected.PhysicalDiffSHA256, actual.PhysicalDiffSHA256, len(expected.Preview.PhysicalDiff.Files), len(actual.Preview.PhysicalDiff.Files))
	appendDigest("semantic_diff_digest_mismatch", "semantic_diff_sha256", expected.SemanticDiffSHA256, actual.SemanticDiffSHA256, len(expected.Preview.LogicalDiff.Changes), len(actual.Preview.LogicalDiff.Changes))
	appendDigest("risk_diff_digest_mismatch", "risk_diff_sha256", expected.RiskDiffSHA256, actual.RiskDiffSHA256, len(expected.Preview.Risks), len(actual.Preview.Risks))
	appendDigest("baseline_postimage_mismatch", "baseline_postimage_sha256", expected.Baseline.PostSHA256, actual.Baseline.PostSHA256, 1, 1)
	appendDigest("candidate_identity_mismatch", "candidate_identity", expected.CandidateIdentity, actual.CandidateIdentity, len(expected.Candidate.Assets), len(actual.Candidate.Assets))
	appendDigest("mapping_identity_mismatch", "mapping_sha256", expected.MappingSHA256, actual.MappingSHA256, len(expected.Mapping.Records), len(actual.Mapping.Records))
	appendDigest("source_evidence_identity_mismatch", "source_evidence_identity", expected.SourceEvidenceIdentity, actual.SourceEvidenceIdentity, len(expected.Plan.Evidence), len(actual.Plan.Evidence))
	appendDigest("curation_identity_mismatch", "curation_identity", expected.CurationIdentity, actual.CurationIdentity, 1, 1)
	if expected.RegistryIdentity != actual.RegistryIdentity || expected.ValidatorIdentity != actual.ValidatorIdentity {
		appendDigest("registry_validator_identity_mismatch", "registry_validator_identity", digestJSON([]string{expected.RegistryIdentity, expected.ValidatorIdentity}), digestJSON([]string{actual.RegistryIdentity, actual.ValidatorIdentity}), 2, 2)
	}
	if approval != nil {
		replayedApproval := *approval
		replayedApproval.D2AApprovalDigest = actual.D2AApprovalDigest
		replayedApproval.ApplyEnvelopeDigest = actual.EnvelopeDigest
		replayedApproval.MappingSHA256 = actual.MappingSHA256
		replayedApproval.ApprovedWriteSet = append([]string{}, actual.WriteSet...)
		replayedApproval.ApprovalDigest = ""
		replayedApproval.ApprovalDigest, _ = approvalDigest(&replayedApproval)
		appendDigest("approval_digest_mismatch", "approval_digest", approval.ApprovalDigest, replayedApproval.ApprovalDigest, 1, 1)
	}
	if len(items) == 0 && !reflect.DeepEqual(*expected, *actual) {
		items = append(items, ReplayMismatchItem{Code: "runtime_metadata_mismatch", Field: "envelope_runtime_metadata", ExpectedSHA256: digestJSON(expected), ActualSHA256: digestJSON(actual), ExpectedCount: 1, ActualCount: 1})
	}
	return items
}

func digestValue(value string) string {
	if len(value) == 64 {
		return value
	}
	return sha256Hex([]byte(value))
}

func digestJSON(value any) string {
	data, _ := json.Marshal(value)
	return sha256Hex(data)
}
