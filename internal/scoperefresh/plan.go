package scoperefresh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func Build(repositoryRoot, timestamp string) (*Preview, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_repository_invalid")
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || parsed.Location() != time.UTC {
		return nil, fmt.Errorf("baseline_scope_timestamp_invalid")
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_configuration_invalid")
	}
	old, exists, err := baseline.Load(root)
	if err != nil || !exists {
		return nil, fmt.Errorf("baseline_scope_preimage_unavailable")
	}
	// Managed Scope owns policy, Entry, Baseline, and observe-fingerprint
	// transitions as one transaction. The legacy Baseline-only lifecycle must
	// not mutate a Baseline after that authority has been established.
	if old.ManagedScope != nil {
		return nil, fmt.Errorf("baseline_scope_managed_scope_unsupported")
	}
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	baselineBytes, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_preimage_unavailable")
	}
	rawCurrent, warnings, inventory, err := baseline.SnapshotWithInventory(root, cfg.WalkOptions())
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_inventory_failed: %w", err)
	}
	if len(warnings) != 0 {
		return nil, fmt.Errorf("baseline_scope_source_unreadable")
	}
	sourceManifest, err := businesssource.Build(root, "")
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_business_source_invalid")
	}
	selectedBusinessPaths := make(map[string]bool, len(sourceManifest.OrderedPaths))
	for _, path := range sourceManifest.OrderedPaths {
		selectedBusinessPaths[path] = true
	}
	current := make(map[string]baseline.Fingerprint, len(rawCurrent))
	for path, fingerprint := range rawCurrent {
		if selectedBusinessPaths[path] || formalCognitionAsset(path) {
			current[path] = fingerprint
		}
	}
	inventory.Summary.CurationExcluded = sourceManifest.SafeInventory.CurationExcluded
	inventory.Summary.ReviewVisibleCount = sourceManifest.SafeInventory.ReviewVisibleCount
	inventory.Summary.AutoBlockerCount = sourceManifest.SafeInventory.AutoBlockerCount
	inventory.Summary.RequiredHumanReview = sourceManifest.SafeInventory.RequiredHumanReview
	inventory.Summary.FinalManagedCandidates = len(current)
	inventory.Summary.InclusionExclusionIdentity = afs.ManagedSelectionIdentity(inventory.Summary.RulesIdentity, sortedFingerprintPaths(current))

	plan := Plan{
		Version: machinecontract.BaselineScopePlanV1, RepositoryRootIdentity: repositoryIdentity(root, old.Files),
		ExpectedBaselineSHA256: digestBytes(baselineBytes), OldManagedSetIdentity: fingerprintIdentity(old.Files),
		NewManagedSetIdentity: fingerprintIdentity(current), RulesIdentity: inventory.Summary.RulesIdentity,
		CurationIdentity: sourceManifest.CurationIdentity, SourceIdentity: sourceManifest.AggregateSHA256, SafeInventory: inventory.Summary,
		Added: []ScopeObject{}, Removed: []ScopeObject{}, Preserved: []ScopeObject{}, SourceDrift: []SourceDrift{},
		BaselineTimestamp: timestamp, NetworkAccessed: false,
	}
	exclusions := make(map[string]afs.SafeInventoryExclusion, len(inventory.Exclusions))
	for _, exclusion := range inventory.Exclusions {
		exclusions[exclusion.PathSummary] = exclusion
	}
	for _, path := range sortedFingerprintPaths(old.Files) {
		oldFingerprint := old.Files[path]
		if now, ok := current[path]; ok {
			plan.Preserved = append(plan.Preserved, scopeObject(path, now, "preserved", ""))
			if oldFingerprint.SHA256 != now.SHA256 {
				plan.SourceDrift = append(plan.SourceDrift, SourceDrift{Path: path, Code: "source_bytes_changed", ExpectedSHA256: oldFingerprint.SHA256, ActualSHA256: now.SHA256})
			}
			continue
		}
		reason, source, tracked, safe := removedReason(root, path, cfg.WalkOptions(), exclusions, rawCurrent, selectedBusinessPaths)
		if reason == "source_missing" {
			plan.SourceDrift = append(plan.SourceDrift, SourceDrift{Path: path, Code: reason, ExpectedSHA256: oldFingerprint.SHA256})
			continue
		}
		plan.Removed = append(plan.Removed, ScopeObject{Path: path, SHA256: oldFingerprint.SHA256, SizeBytes: oldFingerprint.Size, Reason: reason, RuleSource: source, GitTracked: tracked})
		if safe {
			plan.SafeRemovalCount++
		} else {
			plan.OrdinaryRemovalCount++
		}
	}
	for _, path := range sortedFingerprintPaths(current) {
		if _, exists := old.Files[path]; !exists {
			plan.Added = append(plan.Added, scopeObject(path, current[path], "scope_added", "safe_inventory"))
		}
	}
	plan.ScopeOnlyDelta = len(plan.Added) + len(plan.Removed)
	threshold := 25
	if proportional := len(old.Files) / 4; proportional > threshold {
		threshold = proportional
	}
	plan.HighRiskReduction = plan.OrdinaryRemovalCount >= threshold
	plan.InteractionRequired = plan.OrdinaryRemovalCount > 0
	if plan.InteractionRequired {
		plan.InteractionKind = "human_tty_digest_confirmation"
	}

	postimage := baseline.Baseline{Version: old.Version, CreatedAt: old.CreatedAt, UpdatedAt: timestamp, Files: cloneFingerprints(current),
		ManagedScope: cloneManagedScopeState(old.ManagedScope), DatabaseCognition: cloneDatabaseBindings(old.DatabaseCognition)}
	postimageBytes, err := baseline.MarshalExact(&postimage)
	if err != nil {
		return nil, fmt.Errorf("baseline_scope_postimage_invalid")
	}
	plan.BaselinePostimageSHA256 = digestBytes(postimageBytes)
	plan.PlanID, err = planIdentity(plan)
	if err != nil {
		return nil, err
	}
	if plan.InteractionRequired {
		plan.ConfirmationPhrase = "APPLY BASELINE SCOPE " + plan.PlanID
	}
	preview := &Preview{Version: machinecontract.BaselineScopePreviewV1, Plan: plan, BaselinePostimage: postimage,
		BaselinePostimageSHA256: plan.BaselinePostimageSHA256, WriteSet: []string{".aoci/baseline.json", ".aoci/ledger.jsonl"},
		GuardSet: []string{".aoci/baseline.json", "safe_inventory", "curation", "business_source"}, RecoveryDirection: "baseline_preimage_to_scope_postimage", NetworkAccessed: false}
	preview.PreviewID, err = previewIdentity(*preview)
	return preview, err
}

func removedReason(root, path string, options afs.WalkOptions, exclusions map[string]afs.SafeInventoryExclusion, rawCurrent map[string]baseline.Fingerprint, selectedBusinessPaths map[string]bool) (string, string, bool, bool) {
	if category, source := afs.BuiltInSafetyCategory(path); category != "" {
		return category, source, false, true
	}
	if exclusion, exists := exclusions[path]; exists {
		safe := exclusion.Category == afs.SafetySensitive || exclusion.Category == afs.SafetyRuntime || exclusion.Category == afs.SafetyGenerated || exclusion.Category == afs.SafetyUnsafe
		return exclusion.Category, exclusion.RuleSource, exclusion.GitTracked, safe
	}
	if afs.PathExcludedByConfig(path, options) {
		return afs.SafetyConfigured, "project_config", false, false
	}
	if _, exists := rawCurrent[path]; exists && !formalCognitionAsset(path) && !selectedBusinessPaths[path] {
		return "curation_excluded", "curation", false, false
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); errors.Is(err, os.ErrNotExist) {
		return "source_missing", "filesystem", false, false
	}
	return "scope_rule_excluded", "inventory", false, false
}

func formalCognitionAsset(path string) bool {
	switch path {
	case "aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt":
		return true
	default:
		return false
	}
}

func scopeObject(path string, fingerprint baseline.Fingerprint, reason, source string) ScopeObject {
	return ScopeObject{Path: path, SHA256: fingerprint.SHA256, SizeBytes: fingerprint.Size, Reason: reason, RuleSource: source}
}

func sortedFingerprintPaths(files map[string]baseline.Fingerprint) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func fingerprintIdentity(files map[string]baseline.Fingerprint) string {
	values := []string{"managed-set/v1"}
	for _, path := range sortedFingerprintPaths(files) {
		fingerprint := files[path]
		values = append(values, path, fingerprint.SHA256, fmt.Sprint(fingerprint.Size), fingerprint.NormalizedSHA256, fingerprint.FormatSHA256, fingerprint.FormatKind)
	}
	return digestStrings(values)
}

func cloneFingerprints(source map[string]baseline.Fingerprint) map[string]baseline.Fingerprint {
	result := make(map[string]baseline.Fingerprint, len(source))
	for path, fingerprint := range source {
		result[path] = fingerprint
	}
	return result
}

func cloneDatabaseBindings(source *baseline.DatabaseCognitionBindings) *baseline.DatabaseCognitionBindings {
	if source == nil {
		return nil
	}
	result := *source
	result.Entries = append([]baseline.DatabaseCognitionBinding{}, source.Entries...)
	return &result
}

func cloneManagedScopeState(source *baseline.ManagedScopeState) *baseline.ManagedScopeState {
	if source == nil {
		return nil
	}
	result := *source
	if source.BudgetPolicy != nil {
		budget := *source.BudgetPolicy
		budget.R = append([]cognitionbudget.FieldBand{}, source.BudgetPolicy.R...)
		budget.S = append([]cognitionbudget.FieldBand{}, source.BudgetPolicy.S...)
		result.BudgetPolicy = &budget
	}
	return &result
}

func repositoryIdentity(root string, fallback map[string]baseline.Fingerprint) string {
	gitHead, err := os.ReadFile(filepath.Join(root, ".git", "HEAD"))
	if err == nil {
		value := strings.TrimSpace(string(gitHead))
		if strings.HasPrefix(value, "ref: ") {
			if resolved, readErr := os.ReadFile(filepath.Join(root, ".git", filepath.FromSlash(strings.TrimPrefix(value, "ref: ")))); readErr == nil {
				value = strings.TrimSpace(string(resolved))
			}
		}
		if value != "" {
			return digestStrings([]string{"git-head", value})
		}
	}
	return digestStrings([]string{"non-git", fingerprintIdentity(fallback)})
}

func planIdentity(plan Plan) (string, error) {
	plan.PlanID = ""
	plan.ConfirmationPhrase = ""
	plan.BaselineTimestamp = ""
	return digestJSON(plan)
}

func previewIdentity(preview Preview) (string, error) {
	preview.PreviewID = ""
	preview.Plan.BaselineTimestamp = ""
	return digestJSON(preview)
}

func digestJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("baseline_scope_identity_failed")
	}
	return digestBytes(data), nil
}

func digestStrings(values []string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func DecodePreview(data []byte) (*Preview, error) {
	var preview Preview
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&preview); err != nil {
		return nil, fmt.Errorf("baseline_scope_preview_invalid")
	}
	if preview.Version != machinecontract.BaselineScopePreviewV1 || preview.Plan.Version != machinecontract.BaselineScopePlanV1 {
		return nil, fmt.Errorf("baseline_scope_preview_version_unsupported")
	}
	want, err := previewIdentity(preview)
	if err != nil || want != preview.PreviewID || preview.BaselinePostimageSHA256 != preview.Plan.BaselinePostimageSHA256 {
		return nil, fmt.Errorf("baseline_scope_preview_identity_invalid")
	}
	postimage, err := baseline.MarshalExact(&preview.BaselinePostimage)
	if err != nil || digestBytes(postimage) != preview.BaselinePostimageSHA256 {
		return nil, fmt.Errorf("baseline_scope_postimage_identity_invalid")
	}
	return &preview, nil
}

func Encode(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
