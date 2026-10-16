package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

// A guard binds the inputs, not merely the previous governance verdict. Every
// validation enumerates current Safe Inventory and hashes the governed bytes.
// It never re-evaluates policy against every excluded path, trusts mtime, or
// opens excluded business content. Index/observe roles are immutable only while
// config, curation, inventory membership/status and Baseline remain identical.
type overviewSourceGuard struct {
	composite      string
	configIdentity string
	metadata       map[string]string
	inventory      string
	legacy         bool
	sources        map[string]baseline.Fingerprint
}

func overviewConfigIdentity(cfg *config.Config) string {
	encoded, _ := json.Marshal(cfg)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func deliveryMetadata(root string, loaded *cognitionRepoCtx) (map[string]string, error) {
	result := map[string]string{}
	paths := []string{loaded.paths.ConfigPath, config.LocalFilePath(root), loaded.paths.BaselinePath, loaded.paths.CurationPath}
	for _, path := range paths {
		digest, err := deliveryFileHash(path, true)
		if err != nil {
			return nil, err
		}
		result[path] = digest
	}

	return result, nil
}

func deliveryFileHash(path string, allowAbsent bool) (string, error) {
	info, err := os.Lstat(path)
	if allowAbsent && os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("overview_guard_nonregular")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return "", fmt.Errorf("overview_guard_file_replaced")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func overviewInventoryDigest(paths map[string]string, rules string, blockers int, topology string) string {
	keys := make([]string, 0, len(paths))
	for path := range paths {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	digest := sha256.New()
	fmt.Fprintf(digest, "%s\x00%d\x00%s\x00", rules, blockers, topology)
	for _, path := range keys {
		fmt.Fprintf(digest, "%s\x00%s\x00", path, paths[path])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// Repository boundaries are inventory facts, never submodule content or HEAD.
// Runtime exclusions may grow as this delivery writes its own Ledger.
func overviewInventoryTopology(repository bool, boundaries []string) string {
	sort.Strings(boundaries)
	encoded, _ := json.Marshal(struct {
		Repository bool
		Boundaries []string
	}{repository, boundaries})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func overviewSafeTopology(inventory *afs.SafeInventory) string {
	boundaries := []string{}
	for _, item := range inventory.Exclusions {
		if item.Category == afs.SafetyUnsafe {
			boundaries = append(boundaries, fmt.Sprintf("%s\x00%t\x00%s:%s", item.PathSummary, item.GitTracked, item.RuleSource, item.Category))
		}
	}
	return overviewInventoryTopology(inventory.Summary.GitRepository, boundaries)
}

func overviewInventoryPaths(inventory *afs.SafeInventory) map[string]string {
	tracked, ignored := map[string]bool{}, map[string]bool{}
	for _, path := range inventory.TrackedPaths {
		tracked[path] = true
	}
	for _, path := range inventory.IgnoredPaths {
		ignored[path] = true
	}
	paths := map[string]string{}
	for _, path := range inventory.ManagedCandidates {
		status := "untracked_or_non_git"
		if tracked[path] {
			status = "tracked"
		}
		if ignored[path] {
			status = "ignored"
		}
		paths[path] = status
	}
	return paths
}

func newOverviewSourceGuard(loaded *cognitionRepoCtx, metadata map[string]string, state *managedstate.State) *overviewSourceGuard {
	if state == nil || state.ScopeChangeRequired || len(state.Warnings) != 0 {
		return nil
	}
	// Database Evidence can expire without byte drift. Until its time/connector
	// identities have a reusable guard, retain the original live-governance path.
	if asset := loaded.set.Volumes[cognition.ScopeDatabase]; asset != nil && asset.Descriptor.State != machinecontract.CognitionVolumeDisabled {
		return nil
	}
	guard := &overviewSourceGuard{composite: loaded.set.CompositeIdentity, configIdentity: overviewConfigIdentity(loaded.cfg), metadata: metadata, legacy: state.Legacy, sources: map[string]baseline.Fingerprint{}}
	candidates := map[string]string{}
	for path, value := range state.Snapshot {
		guard.sources[path] = value

	}
	rules, blockers, topology := "", 0, ""
	if state.Legacy {
		if state.Inventory == nil {
			return nil
		}
		candidates = overviewInventoryPaths(state.Inventory)
		topology = overviewSafeTopology(state.Inventory)
		rules, blockers = state.Inventory.Summary.RulesIdentity, state.Inventory.Summary.AutoBlockerCount
	} else {
		if state.Evaluation == nil {
			return nil
		}
		rules, blockers = state.Evaluation.SafeInventory.RulesIdentity, state.Evaluation.SafeInventory.AutoBlockerCount
		boundaries := []string{}
		for _, item := range state.Evaluation.Exclude {
			if item.RuleSource == machinecontract.ScopeRuleSafety && item.SafetyStatus == afs.SafetyUnsafe {
				boundaries = append(boundaries, fmt.Sprintf("%s\x00%t\x00%s", item.Path, item.GitStatus == "tracked", item.Reason))
			}
		}
		topology = overviewInventoryTopology(state.Evaluation.SafeInventory.GitRepository, boundaries)
		for _, group := range [][]managedscope.PathEvaluation{state.Evaluation.Index, state.Evaluation.Observe, state.Evaluation.Exclude} {
			for _, item := range group {
				if item.RuleSource != machinecontract.ScopeRuleSafety {
					candidates[item.Path] = item.GitStatus
				}
			}
		}
	}
	guard.inventory = overviewInventoryDigest(candidates, rules, blockers, topology)
	return guard
}

func (guard *overviewSourceGuard) validate(root string, loaded *cognitionRepoCtx) error {
	if loaded.set.CompositeIdentity != guard.composite || overviewConfigIdentity(loaded.cfg) != guard.configIdentity {
		return fmt.Errorf("overview_snapshot_changed")
	}
	metadata, err := deliveryMetadata(root, loaded)
	if err != nil {
		return err
	}
	before, _ := json.Marshal(guard.metadata)
	after, _ := json.Marshal(metadata)
	if string(before) != string(after) {
		return fmt.Errorf("overview_snapshot_changed")
	}
	if fail := confirmCognitionSnapshot(root, loaded.set); fail != nil {
		return fmt.Errorf("overview_snapshot_changed")
	}
	if fail := pendingCognitionDeliveryFail(root, loaded.set); fail != nil {
		return fmt.Errorf("overview_snapshot_changed")
	}
	options := loaded.cfg.WalkOptions()
	options.IncludeIgnoredCandidates = !guard.legacy
	inventory, err := afs.BuildSafeInventory(root, options)
	if err != nil {
		return err
	}
	candidates := overviewInventoryPaths(inventory)
	if overviewInventoryDigest(candidates, inventory.Summary.RulesIdentity, inventory.Summary.AutoBlockerCount, overviewSafeTopology(inventory)) != guard.inventory {
		return fmt.Errorf("overview_snapshot_changed")
	}
	for path, expected := range guard.sources {
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || info.Size() != expected.Size {
			return fmt.Errorf("overview_snapshot_changed")
		}
		actual, err := deliveryFileHash(full, false)
		if err != nil || actual != expected.SHA256 {
			return fmt.Errorf("overview_snapshot_changed")
		}
	}
	return nil
}

func overviewSnapshotFailure() *mcp.CallToolResult {
	return errResult(errCognitionSnapshotUnavailable, "overview_snapshot_changed", mcpMessage("overview.delivery.snapshot_retry"))
}
