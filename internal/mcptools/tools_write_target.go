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
	codeTargetIndexPath   = "aoci.code.target.txt"
	codeTargetReusePrefix = "#Target-Reuse: "
)

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
	diff, err := cognitionplan.CompareCodeTargetIndex(root, loaded.cfg.IndexPath, targetRaw)
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
	if facts.PendingTransactions != 0 || facts.RecoveryPending {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_recovery_required"}
	}
	if !facts.StructureValid || facts.ThirdPartyConflict {
		return nil, &Fail{Code: errWriteConflict, Msg: "code_target_scope_not_ready"}
	}
	if len(facts.CodeDrift.Orphan) != 0 || diff.Summary.Deleted != 0 {
		return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_delete_requires_ordinary_maintain"}
	}

	base := targetObjectLines(loaded.set.Volumes[cognition.ScopeCode])
	target := targetObjectLines(projected.Volumes[cognition.ScopeCode])
	reuse, err := parseCodeTargetReuse(targetRaw)
	if err != nil {
		return nil, &Fail{Code: errCandidateInvalid, Msg: err.Error()}
	}
	changes := make(map[string]string, len(diff.Changes))
	for _, change := range diff.Changes {
		if change.Change == cognition.ImpactChangeDelete {
			return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_delete_requires_ordinary_maintain"}
		}
		changes[strings.TrimPrefix(change.ObjectRef, "code:")] = change.Change
	}
	debt := sortedUniqueStrings(append(append(append([]string{}, facts.CodeDrift.Stale...), facts.CodeDrift.Missing...), facts.CodeDrift.Unbaselined...))
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
	if len(debt) > machinecontract.EntriesBatchMaxItems {
		return nil, &Fail{Code: errBadArgs, Msg: "code_target_batch_too_large"}
	}
	items := make([]updateEntryItemIn, 0, len(debt))
	for _, path := range debt {
		entry := target[path]
		if entry == "" {
			return nil, &Fail{Code: errCandidateInvalid, Msg: "code_target_entry_missing: " + path}
		}
		if changes[path] == "" && !reuse[path] {
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

func parseCodeTargetReuse(raw []byte) (map[string]bool, error) {
	reuse := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, codeTargetReusePrefix) {
			continue
		}
		objectRef := strings.TrimPrefix(line, codeTargetReusePrefix)
		path := strings.TrimPrefix(objectRef, "code:")
		normalized, err := afs.NormalizeRelPath(path)
		if err != nil || objectRef != "code:"+normalized || reuse[normalized] {
			return nil, fmt.Errorf("code_target_reuse_marker_invalid: %s", objectRef)
		}
		reuse[normalized] = true
	}
	return reuse, nil
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
	diff, err := cognitionplan.CompareCodeTargetIndex(root, loaded.cfg.IndexPath, targetRaw)
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
