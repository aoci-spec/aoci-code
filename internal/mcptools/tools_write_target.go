package mcptools

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

const (
	codeTargetIndexPath    = "aoci.code.target.txt"
	codeTargetReusePrefix  = cognitionplan.CodeTargetReusePrefix
	codeTargetDeletePrefix = cognitionplan.CodeTargetDeletePrefix
)

// CodeTargetIndexOutcome is the CLI-neutral result of finalizing the fixed
// repository target. OutputJSON is the same compact contract returned by MCP.
type CodeTargetIndexOutcome struct {
	OutputJSON     string
	Applied        bool
	RepairRequired bool
}

// ApplyCodeTargetIndex finalizes aoci.code.target.txt without an MCP client.
// It reuses the exact target binding and atomic Apply implementation below;
// only the transport metrics are changed from one MCP call to one shell call.
func ApplyCodeTargetIndex(root, serviceVersion string) (CodeTargetIndexOutcome, error) {
	result := handleMCPApplyCodeTarget(root, serviceVersion, codeTargetIndexPath, nil)
	if result == nil || result.IsError || len(result.Content) != 1 {
		return CodeTargetIndexOutcome{}, fmt.Errorf("code_target_result_invalid")
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return CodeTargetIndexOutcome{}, fmt.Errorf("code_target_result_invalid")
	}
	var parsed autoResult
	if err := json.Unmarshal([]byte(content.Text), &parsed); err != nil {
		return CodeTargetIndexOutcome{}, fmt.Errorf("code_target_result_invalid: %w", err)
	}
	parsed.Metrics.AOCIToolCalls = 0
	parsed.Metrics.ShellAOCICalls = 1
	return CodeTargetIndexOutcome{
		OutputJSON:     renderAutoResult(parsed),
		Applied:        parsed.Status == autoStatusApplied,
		RepairRequired: parsed.Status == autoStatusRepairRequired,
	}, nil
}

func handleMCPApplyCodeTarget(
	root, mcpServiceVersion, targetPath string,
	refreshSession *cognitionRefreshSession,
) *mcp.CallToolResult {
	started := time.Now()
	targetRaw, targetSHA, fail := readCodeTargetIndex(root, targetPath)
	if fail != nil {
		return codeTargetStopped(root, mcpServiceVersion, fail.Code, fail.Msg, started, refreshSession)
	}
	items, fail := bindCodeTargetItems(root, targetRaw)
	if fail != nil {
		return codeTargetStopped(root, mcpServiceVersion, fail.Code, fail.Msg, started, refreshSession)
	}
	if len(items) == 0 {
		if err := syncCodeTargetIndex(root, targetPath, targetSHA, targetRaw); err != nil {
			return codeTargetStopped(root, mcpServiceVersion, "code_target_sync_failed", err.Error(), started, refreshSession)
		}
		aligned, remaining, findings, receipt := inspectAutoAlignment(root, mcpServiceVersion)
		result := autoResult{Version: 1, Status: autoStatusApplied, Aligned: aligned,
			Attempted: 0, Applied: 0, Remaining: remaining, FormalWritesStarted: false,
			Receipt: receipt, Metrics: autoMetrics{DeterministicMs: elapsedMilliseconds(started), AOCIToolCalls: 1},
			Findings: genericMachineFindings(findings), NextAction: map[bool]string{true: "none", false: "call_aoci_maintain"}[aligned]}
		applyAutoRefreshOutcome(&result, refreshSession)
		return textResult(renderAutoResult(result))
	}

	result := handleMCPCodeTargetBatch(root, mcpServiceVersion, items, refreshSession)
	parsed, applied := decodeTargetApplyResult(result)
	if !applied {
		return result
	}
	if err := syncCodeTargetIndex(root, targetPath, targetSHA, targetRaw); err != nil {
		parsed.Status = autoStatusStopped
		parsed.Aligned = false
		parsed.FormalWritesStarted = true
		parsed.Findings = append(parsed.Findings, genericMachineFinding("code_target_sync_failed", err.Error()))
		parsed.NextAction = "retry_aoci_update_entry_target_index"
		return textResult(renderAutoResult(parsed))
	}
	return result
}

func bindCodeTargetItems(root string, targetRaw []byte) ([]updateEntryItemIn, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return nil, fail
	}
	if loaded.set.LayoutMode != cognition.LayoutVolumesV1 || loaded.set.Volumes[cognition.ScopeCode] == nil {
		return nil, &Fail{Code: errBadArgs, Msg: "code_target_apply_requires_code_volume"}
	}
	directives, err := cognitionplan.ParseCodeTargetDirectives(targetRaw)
	if err != nil {
		return nil, &Fail{Code: errCandidateInvalid, Msg: err.Error()}
	}
	diff, err := cognitionplan.CompareCodeTargetIndex(root, loaded.cfg.IndexPath, targetRaw)
	postimageControls := false
	if err != nil && len(directives.DeletePaths) != 0 && strings.Contains(err.Error(), "code_target_delete_marker_extra") {
		diff, err = cognitionplan.CompareCodeTargetIndex(root, loaded.cfg.IndexPath,
			cognitionplan.StripCodeTargetDirectives(targetRaw))
		postimageControls = err == nil && len(diff.Changes) == 0
		if postimageControls {
			diff.Directives = directives
		}
	}
	if err != nil {
		return nil, &Fail{Code: errCandidateInvalid, Msg: err.Error()}
	}
	projected, findings := cognition.ProjectObjectVolume(loaded.set, cognition.ScopeCode, targetRaw)
	if projected == nil || len(findings) != 0 {
		detail := "code_target_projection_invalid"
		if len(findings) != 0 {
			detail = findings[0].Code
		}
		return nil, &Fail{Code: errCandidateInvalid, Msg: detail}
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_scope_unavailable"}
	}
	if facts.ManagedScope.ScopeChangeRequired {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_scope_change_required"}
	}
	if (facts.PendingTransactions != 0 || facts.RecoveryPending) && !postimageControls {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_recovery_required"}
	}
	if postimageControls && facts.PendingTransactions == 0 && !facts.RecoveryPending {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_delete_postimage_unproven"}
	}
	if !facts.StructureValid || facts.ThirdPartyConflict {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_scope_not_ready"}
	}
	base := targetObjectLines(loaded.set.Volumes[cognition.ScopeCode])
	target := targetObjectLines(projected.Volumes[cognition.ScopeCode])
	reuse := make(map[string]bool, len(diff.Directives.ReusePaths))
	for _, path := range diff.Directives.ReusePaths {
		reuse[path] = true
	}
	deleted := make(map[string]bool, len(diff.Directives.DeletePaths))
	for _, path := range diff.Directives.DeletePaths {
		deleted[path] = true
	}
	changes := make(map[string]string, len(diff.Changes))
	for _, change := range diff.Changes {
		changes[strings.TrimPrefix(change.ObjectRef, "code:")] = change.Change
	}
	orphans := make(map[string]bool, len(facts.CodeDrift.Orphan))
	for _, path := range facts.CodeDrift.Orphan {
		orphans[path] = true
	}
	debt := sortedUniqueStrings(append(append(append(append(append([]string{}, facts.CodeDrift.Stale...), facts.CodeDrift.Missing...),
		facts.CodeDrift.Unbaselined...), facts.CodeDrift.Orphan...), diff.Directives.DeletePaths...))
	debtSet := make(map[string]bool, len(debt))
	for _, path := range debt {
		debtSet[path] = true
	}
	postimageOnly := len(debt) == 0 && len(changes) == 0
	for path := range changes {
		if !debtSet[path] {
			return nil, &Fail{Code: errWriteConflict, Msg: "code_target_entry_ahead_of_source: " + path}
		}
	}
	for path := range reuse {
		if (postimageOnly || debtSet[path]) && changes[path] == "" && base[path] != "" && target[path] == base[path] {
			continue
		}
		return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_reuse_invalid: " + path}
	}
	for path := range deleted {
		if !postimageControls && (changes[path] != cognition.ImpactChangeDelete || !orphans[path] || base[path] == "" || target[path] != "") {
			return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_delete_invalid: " + path}
		}
		if info, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(statErr) || info != nil {
			return nil, &Fail{Code: errWriteConflict, Msg: "code_target_delete_source_present: " + path}
		}
	}
	if len(debt) > machinecontract.EntriesBatchMaxItems {
		return nil, &Fail{Code: errBadArgs, Msg: "code_target_batch_too_large"}
	}
	items := make([]updateEntryItemIn, 0, len(debt))
	for _, path := range debt {
		if deleted[path] {
			if !deleted[path] {
				return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_delete_marker_missing: " + path}
			}
			items = append(items, updateEntryItemIn{Change: cognition.ImpactChangeDelete, Path: path})
			continue
		}
		entry := target[path]
		if entry == "" {
			return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_entry_missing: " + path}
		}
		if changes[path] == "" && !reuse[path] && !postimageControls {
			return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_reuse_marker_missing: " + path}
		}
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, &Fail{Code: errWriteConflict, Msg: "code_target_source_unavailable: " + path}
		}
		items = append(items, updateEntryItemIn{Path: path, NewEntry: entry, SourceSHA256: fingerprint.SHA256})
	}
	return items, nil
}

func readCodeTargetIndex(root, requested string) ([]byte, string, *Fail) {
	if requested != codeTargetIndexPath {
		return nil, "", &Fail{Code: errPathUnsafe, Msg: "code_target_path_must_be_" + codeTargetIndexPath}
	}
	path := filepath.Join(root, filepath.FromSlash(requested))
	before, err := os.Lstat(path)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, "", &Fail{Code: errPathUnsafe, Msg: "code_target_not_regular"}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", &Fail{Code: errPathUnsafe, Msg: "code_target_unavailable"}
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, "", &Fail{Code: errPathUnsafe, Msg: "code_target_changed_while_opening"}
	}
	limited := io.LimitReader(file, machinecontract.EntriesRequestMaxBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) == 0 || len(raw) > machinecontract.EntriesRequestMaxBytes {
		return nil, "", &Fail{Code: errBadArgs, Msg: "code_target_size_invalid"}
	}
	after, err := os.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) {
		return nil, "", &Fail{Code: errWriteConflict, Msg: "code_target_changed_while_reading"}
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func targetObjectLines(asset *cognition.Asset) map[string]string {
	result := map[string]string{}
	if asset == nil {
		return result
	}
	for _, object := range asset.Objects {
		result[strings.TrimPrefix(object.CanonicalRef, "code:")] = object.CanonicalLine
	}
	return result
}

func decodeTargetApplyResult(result *mcp.CallToolResult) (autoResult, bool) {
	var parsed autoResult
	if result == nil || result.IsError || len(result.Content) != 1 {
		return parsed, false
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok || json.Unmarshal([]byte(content.Text), &parsed) != nil {
		return autoResult{}, false
	}
	return parsed, parsed.Status == autoStatusApplied
}

func syncCodeTargetIndex(root, targetPath, targetSHA string, targetRaw []byte) error {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return fmt.Errorf("%s", fail.Msg)
	}
	asset := loaded.set.Volumes[cognition.ScopeCode]
	if asset == nil {
		return fmt.Errorf("code_target_formal_volume_missing")
	}
	diff, err := cognitionplan.CompareCodeTargetIndex(root, loaded.cfg.IndexPath,
		cognitionplan.StripCodeTargetDirectives(targetRaw))
	if err != nil || len(diff.Changes) != 0 {
		return fmt.Errorf("code_target_formal_postimage_mismatch")
	}
	if bytes.Equal(targetRaw, asset.Raw) {
		return nil
	}
	return afs.AtomicWriteCAS(filepath.Join(root, filepath.FromSlash(targetPath)), asset.Raw, targetSHA)
}

func codeTargetStopped(
	root, mcpServiceVersion, code, cause string, started time.Time,
	refreshSession *cognitionRefreshSession,
) *mcp.CallToolResult {
	nextAction := "call_aoci_maintain"
	switch cause {
	case "code_target_scope_change_required":
		nextAction = "complete_aoci_scope_change"
	case "code_target_recovery_required":
		nextAction = "resolve_aoci_recovery"
	}
	result := autoResult{Version: 1, Status: autoStatusStopped, Aligned: false,
		Attempted: 0, Applied: 0, Remaining: 1, FormalWritesStarted: false,
		Receipt:  currentWriteCognitionReceipt(root, mcpServiceVersion),
		Metrics:  autoMetrics{DeterministicMs: elapsedMilliseconds(started), AOCIToolCalls: 1},
		Findings: machineFindings{genericMachineFinding(code, cause)}, NextAction: nextAction}
	applyAutoRefreshOutcome(&result, refreshSession)
	return textResult(renderAutoResult(result))
}
