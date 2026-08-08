package cognitionplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
)

const RuntimeLedgerPath = ".aoci/ledger.jsonl"

// PreviewReplayMismatch is a content-level mismatch safe to expose without
// returning candidate, source, Evidence, or cognition bodies.
type PreviewReplayMismatch struct {
	Code           string `json:"code"`
	Field          string `json:"field"`
	ExpectedSHA256 string `json:"expected_sha256"`
	ActualSHA256   string `json:"actual_sha256"`
	ExpectedCount  int    `json:"expected_count"`
	ActualCount    int    `json:"actual_count"`
}

// PreviewReplayMismatches compares only content identity. The Ledger is an
// append-only runtime audit stream: Rules, Overview, Verify, Check, approval,
// or another observation may extend it between Preview and Apply without
// changing the approved source, mapping, candidate, or formal postimages.
func PreviewReplayMismatches(expected, actual *Preview) []PreviewReplayMismatch {
	if expected == nil || actual == nil {
		return []PreviewReplayMismatch{replayMismatch("runtime_metadata_mismatch", "preview", expected, actual, replayCount(expected), replayCount(actual))}
	}
	expectedContent := previewContentIdentity(*expected)
	actualContent := previewContentIdentity(*actual)
	if reflect.DeepEqual(expectedContent, actualContent) {
		return nil
	}

	items := make([]PreviewReplayMismatch, 0, 6)
	if expected.CandidateIdentity != actual.CandidateIdentity {
		items = append(items, replayMismatch("candidate_identity_mismatch", "candidate_identity", expected.CandidateIdentity, actual.CandidateIdentity, 1, 1))
	}
	if expected.PhysicalDiff.PhysicalDiffSHA256 != actual.PhysicalDiff.PhysicalDiffSHA256 {
		items = append(items, replayMismatch("physical_diff_digest_mismatch", "physical_diff_sha256", expected.PhysicalDiff.PhysicalDiffSHA256, actual.PhysicalDiff.PhysicalDiffSHA256, len(expected.PhysicalDiff.Files), len(actual.PhysicalDiff.Files)))
	}
	if expected.LogicalDiff.LogicalDiffSHA256 != actual.LogicalDiff.LogicalDiffSHA256 {
		items = append(items, replayMismatch("semantic_diff_digest_mismatch", "semantic_diff_sha256", expected.LogicalDiff.LogicalDiffSHA256, actual.LogicalDiff.LogicalDiffSHA256, len(expected.LogicalDiff.Changes), len(actual.LogicalDiff.Changes)))
	}
	expectedMapping, actualMapping := "", ""
	if expected.SemanticMapping != nil {
		expectedMapping = expected.SemanticMapping.MappingSHA256
	}
	if actual.SemanticMapping != nil {
		actualMapping = actual.SemanticMapping.MappingSHA256
	}
	if expectedMapping != actualMapping {
		items = append(items, replayMismatch("mapping_identity_mismatch", "mapping_sha256", expectedMapping, actualMapping, replayCount(expected.SemanticMapping), replayCount(actual.SemanticMapping)))
	}
	expectedApproval, actualApproval := "", ""
	if expected.ApprovalDigest != nil {
		expectedApproval = expected.ApprovalDigest.Digest
	}
	if actual.ApprovalDigest != nil {
		actualApproval = actual.ApprovalDigest.Digest
	}
	if expectedApproval != actualApproval {
		items = append(items, replayMismatch("approval_digest_mismatch", "approval_digest", expectedApproval, actualApproval, replayCount(expected.ApprovalDigest), replayCount(actual.ApprovalDigest)))
	}
	if len(items) == 0 {
		items = append(items, replayMismatch("runtime_metadata_mismatch", "preview_content", expectedContent, actualContent, replayCount(expectedContent), replayCount(actualContent)))
	}
	return items
}

func previewContentIdentity(value Preview) Preview {
	value.FormalAssetProof.Before = withoutRuntimeLedger(value.FormalAssetProof.Before)
	value.FormalAssetProof.After = withoutRuntimeLedger(value.FormalAssetProof.After)
	value.FormalAssetProof.LedgerWritten = false
	return value
}

func withoutRuntimeLedger(states []FormalAssetState) []FormalAssetState {
	filtered := make([]FormalAssetState, 0, len(states))
	for _, state := range states {
		if state.Path != RuntimeLedgerPath {
			filtered = append(filtered, state)
		}
	}
	return filtered
}

func replayMismatch(code, field string, expected, actual any, expectedCount, actualCount int) PreviewReplayMismatch {
	return PreviewReplayMismatch{
		Code: code, Field: field,
		ExpectedSHA256: replayDigest(expected), ActualSHA256: replayDigest(actual),
		ExpectedCount: expectedCount, ActualCount: actualCount,
	}
}

func replayDigest(value any) string {
	if text, ok := value.(string); ok && len(text) == 64 {
		return text
	}
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func replayCount(value any) int {
	if value == nil {
		return 0
	}
	return 1
}
