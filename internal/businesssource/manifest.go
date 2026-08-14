// Package businesssource builds the deterministic, read-only business source
// identity shared by authoring, review, Migration, and Auto workflows.
package businesssource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
)

const (
	Version            = machinecontract.BusinessSourceManifestV1
	AggregateAlgorithm = "sha256-nul-domain-separated-v1"
)

type File struct {
	Path             string `json:"path"`
	SHA256           string `json:"sha256"`
	SizeBytes        int64  `json:"size_bytes"`
	LineEndingPolicy string `json:"line_ending_policy"`
}

type Manifest struct {
	Version                    string                   `json:"version"`
	OrderedPaths               []string                 `json:"ordered_paths"`
	Files                      []File                   `json:"files"`
	InclusionExclusionIdentity string                   `json:"inclusion_exclusion_identity"`
	SafeInventoryRulesIdentity string                   `json:"safe_inventory_rules_identity"`
	CurationIdentity           string                   `json:"curation_identity"`
	GitHead                    string                   `json:"git_head,omitempty"`
	LineEndingPolicy           string                   `json:"line_ending_policy"`
	AggregateAlgorithm         string                   `json:"aggregate_algorithm"`
	AggregateSHA256            string                   `json:"aggregate_sha256"`
	SafeInventory              afs.SafeInventorySummary `json:"safe_inventory"`
	GeneratedAt                string                   `json:"generated_at"`
	NetworkAccessed            bool                     `json:"network_accessed"`
}

func Build(repositoryRoot, generatedAt string) (*Manifest, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("business_source_repository_invalid")
	}
	if generatedAt != "" {
		parsed, parseErr := time.Parse(time.RFC3339, generatedAt)
		if parseErr != nil || parsed.Location() != time.UTC {
			return nil, fmt.Errorf("business_source_generated_at_invalid")
		}
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, fmt.Errorf("business_source_configuration_invalid")
	}
	inventory, err := afs.BuildSafeInventory(root, cfg.WalkOptions())
	if err != nil {
		return nil, fmt.Errorf("business_source_inventory_invalid: %w", err)
	}
	curationDocument, _, curationSHA, err := curation.Load(root)
	if err != nil {
		return nil, fmt.Errorf("business_source_curation_invalid")
	}
	formal := map[string]bool{"aoci.txt": true, "aoci.meta.txt": true, "aoci.code.txt": true, "aoci.database.txt": true}
	if cfg.ManagedScope != nil || cfg.CognitionBudget != nil {
		state, stateErr := managedstate.Load(root, cfg)
		if stateErr != nil {
			return nil, fmt.Errorf("business_source_managed_scope_invalid: %w", stateErr)
		}
		if state.ScopeChangeRequired {
			return nil, fmt.Errorf("business_source_scope_change_required")
		}
		if state.Evaluation == nil {
			return nil, fmt.Errorf("business_source_managed_scope_unavailable")
		}
		paths := make([]string, 0, state.Evaluation.IndexCount+state.Evaluation.ObserveCount)
		for _, item := range state.Evaluation.Index {
			if !formal[item.Path] {
				paths = append(paths, item.Path)
			}
		}
		for _, item := range state.Evaluation.Observe {
			if !formal[item.Path] {
				paths = append(paths, item.Path)
			}
		}
		sort.Strings(paths)
		policy := "strict-bytes"
		if cfg.LineEndingTolerance {
			policy = "raw-sha-authority-with-crlf-lf-equivalence-diagnostic"
		}
		summary := state.Evaluation.SafeInventory
		summary.FinalManagedCandidates = len(paths)
		summary.InclusionExclusionIdentity = afs.ManagedSelectionIdentity(state.Evaluation.PolicyIdentity, paths)
		manifest := &Manifest{Version: Version, OrderedPaths: append([]string{}, paths...), Files: []File{},
			InclusionExclusionIdentity: summary.InclusionExclusionIdentity,
			SafeInventoryRulesIdentity: summary.RulesIdentity, CurationIdentity: curationSelectionIdentity(curationSHA, cfg.CurationExclude),
			GitHead: gitHead(root), LineEndingPolicy: policy, AggregateAlgorithm: AggregateAlgorithm,
			SafeInventory: summary, GeneratedAt: generatedAt, NetworkAccessed: false}
		for _, path := range paths {
			fingerprint, exists := state.Snapshot[path]
			if !exists {
				return nil, fmt.Errorf("business_source_managed_fingerprint_missing: %s", path)
			}
			manifest.Files = append(manifest.Files, File{Path: path, SHA256: fingerprint.SHA256, SizeBytes: fingerprint.Size, LineEndingPolicy: policy})
		}
		manifest.AggregateSHA256 = aggregate(manifest)
		return manifest, nil
	}
	decisions := make(map[string]curation.Decision, len(curationDocument.Decisions))
	for _, decision := range curationDocument.Decisions {
		decisions[decision.Path] = decision
	}
	paths := make([]string, 0, len(inventory.ManagedCandidates))
	for _, path := range inventory.ManagedCandidates {
		if formal[path] {
			continue
		}
		profile, profileErr := curation.ProfilePath(root, path)
		if profileErr != nil {
			return nil, fmt.Errorf("business_source_curation_profile_invalid: %s", path)
		}
		excluded := afs.MatchExcludePattern(path, cfg.CurationExclude)
		if !excluded {
			if decision, exists := decisions[path]; exists && decision.SourceSHA256 == profile.SourceSHA256 {
				excluded = decision.Decision == curation.DecisionExclude
			} else if profile.Reason != "" {
				excluded = true
				inventory.Summary.AddReviewVisible(1)
			}
		}
		if excluded {
			inventory.Summary.CurationExcluded++
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	inventory.Summary.FinalManagedCandidates = len(paths)
	inventory.Summary.InclusionExclusionIdentity = afs.ManagedSelectionIdentity(inventory.Summary.RulesIdentity, paths)
	policy := "strict-bytes"
	if cfg.LineEndingTolerance {
		policy = "raw-sha-authority-with-crlf-lf-equivalence-diagnostic"
	}
	manifest := &Manifest{Version: Version, OrderedPaths: append([]string{}, paths...), Files: []File{},
		InclusionExclusionIdentity: afs.ManagedSelectionIdentity(inventory.Summary.RulesIdentity, paths),
		SafeInventoryRulesIdentity: inventory.Summary.RulesIdentity, CurationIdentity: curationSelectionIdentity(curationSHA, cfg.CurationExclude),
		GitHead: gitHead(root), LineEndingPolicy: policy, AggregateAlgorithm: AggregateAlgorithm,
		SafeInventory: inventory.Summary, GeneratedAt: generatedAt, NetworkAccessed: false}
	for _, path := range paths {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(path)))
		if hashErr != nil {
			return nil, fmt.Errorf("business_source_read_failed: %s", path)
		}
		manifest.Files = append(manifest.Files, File{Path: path, SHA256: fingerprint.SHA256, SizeBytes: fingerprint.Size, LineEndingPolicy: policy})
	}
	manifest.AggregateSHA256 = aggregate(manifest)
	return manifest, nil
}

func curationSelectionIdentity(assetSHA string, patterns []string) string {
	hash := sha256.New()
	for _, value := range append([]string{"business-source-curation/v1", assetSHA}, patterns...) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func aggregate(manifest *Manifest) string {
	hash := sha256.New()
	write := func(value string) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	for _, value := range []string{Version, AggregateAlgorithm, manifest.InclusionExclusionIdentity, manifest.SafeInventoryRulesIdentity,
		manifest.CurationIdentity, manifest.GitHead, manifest.LineEndingPolicy} {
		write(value)
	}
	for _, file := range manifest.Files {
		write(file.Path)
		write(file.SHA256)
		write(fmt.Sprint(file.SizeBytes))
		write(file.LineEndingPolicy)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func gitHead(root string) string {
	command := afs.UntrustedRepositoryGitCommand(root, "rev-parse", "HEAD")
	data, err := command.Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(data))
	if len(value) != 40 && len(value) != 64 {
		return ""
	}
	return value
}

func Encode(manifest *Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
