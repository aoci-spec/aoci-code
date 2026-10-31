// Package config owns the configuration reading and writing of aoci-code.
// Index entry: config.go[GCF9M]
//
// The configuration has two layers:
//
//	.aoci/config.json        committed to Git: the team's shared base configuration and governance policy
//	.aoci/config.local.json  not committed: personal overrides such as the AI endpoint, this machine only
//
// Load semantics:
// DefaultConfig → overridden by config.json → overridden by config.local.json.
//
// automation, line_ending_tolerance, and cognition_refresh_threshold are all team governance assets:
// after the local merge the team value must be restored, and the personal layer cannot override them.
//
// Layer leak discipline:
// a caller writing the team layer must take its source from LoadBase and then Save; the merged state of Load is for reading only
// and must never be written back to the team layer, or local's personal endpoint leaks into config.json.
//
// AI configuration iron rule:
// AI is off by default with no default endpoint; APIKeyEnv holds an environment variable name, not a key.
// A key must never enter the config, the ledger, a backup, or a Prompt snapshot.
//
// Technical artifact exclusion extension (R60-F.9-A2, 2026-07-18): in the natural HTTPX live run .ruff_cache
// was judged a false Missing and triggered several rounds of cleanup; deterministic tool artifacts should not enter semantic governance.
// This adds .ruff_cache/.tox/.nox/htmlcov to defaultExcludeDirs and
// .coverage (a file, not a directory) to defaultExcludeFiles. The layering decision for exclusions:
// not hard-coded into the fs layer (like node_modules it is a business/tool artifact, not a .git/.aoci
// structural red line; visible and editable by the user), and no third policy layer (the existing two layers express enough).
// Existing-repository limitation (a known cost): applyFallbacks fills defaults only when the field is nil,
// so an explicit list already on disk freezes the old defaults; this extension only rescues new repositories, and an existing
// repository migrates through the doctor informational hint (the second half of A2), with zero silent configuration changes.
//
// Line-ending tolerance (R60-F.11):
// line_ending_tolerance defaults to true, making governance treat a pure CRLF/LF representation difference
// as equivalent; an explicit false restores the strict raw-byte judgement. The field belongs to the team layer only,
// and config.local.json may not override it, so one repository cannot reach different governance conclusions on different members' machines.
// The field is a bool and relies on DefaultConfig presetting true when absent; it must never be added to applyFallbacks,
// or "field missing" and "team explicitly false" become indistinguishable.
package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	DirName       = ".aoci"
	FileName      = "config.json"
	LocalFileName = "config.local.json"
)

func defaultExcludeDirs() []string {
	return []string{
		"node_modules", "vendor", "dist", "build",
		"target", ".next", "coverage", "tmp", "uploads",
		"backup", "backups", "artifacts", ".output",
		"__pycache__", ".venv", "venv", ".pytest_cache", ".mypy_cache",
		".ruff_cache", ".tox", ".nox", "htmlcov",
	}
}

// These ambiguous names are artifacts by default only at the repository root,
// or where git ignores a directory of that name, in newly initialized
// repositories. Existing exclude_dirs lists stay intact. `cache` is new here:
// it was a built-in exclusion through rc18 and is not in defaultExcludeDirs,
// which older repositories were backfilled with and which must not change.
// `logs` and `run` likewise: built-in runtime names through v0.1.0, never in
// defaultExcludeDirs, so an older repository with a tracked module of that
// name gains candidates on upgrade (#158) and names it here or authors it.
func newProjectRootExcludeDirs() []string {
	return []string{"backup", "backups", "build", "cache", "coverage", "dist", "target", "tmp", "uploads", "artifacts", "vendor", "logs", "run"}
}

// SetNewProjectArtifactExclusions persists the new-project choice before the
// first inventory. The remaining exclude_dirs entries still match at any depth.
func (cfg *Config) SetNewProjectArtifactExclusions() {
	rootDirs := newProjectRootExcludeDirs()
	rootSet := make(map[string]bool, len(rootDirs))
	for _, dir := range rootDirs {
		rootSet[dir] = true
	}
	anywhere := make([]string, 0, len(cfg.ExcludeDirs))
	for _, dir := range cfg.ExcludeDirs {
		if !rootSet[dir] {
			anywhere = append(anywhere, dir)
		}
	}
	cfg.ExcludeDirs = anywhere
	cfg.ExcludeRootDirs = rootDirs
}

func defaultExcludeFiles() []string {
	return []string{
		"*.backup.*",
		"*.bak",
		".coverage",
	}
}

// DefaultTechnicalExcludeDirs exports a copy of the current default technical artifact directory list.
//
// It serves doctor's missing-item hint for existing repositories; the single source of truth is still defaultExcludeDirs.
func DefaultTechnicalExcludeDirs() []string {
	return defaultExcludeDirs()
}

// AIConfig is the AI enhancement layer configuration.
// BaseURL is entirely declared by the user; the CLI sets no default cloud endpoint.
type AIConfig struct {
	Enabled bool `json:"enabled"`

	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`

	// APIKeyEnv is an environment variable name, not a key.
	APIKeyEnv string `json:"api_key_env"`

	TimeoutSeconds int `json:"timeout_seconds"`
	MaxConcurrency int `json:"max_concurrency"`
	MaxInputTokens int `json:"max_input_tokens"`

	// auto/exact/estimated。
	TokenAccounting string `json:"token_accounting"`

	// redacted/full/none。
	PromptSnapshot string `json:"prompt_snapshot"`
}

// OverviewDeliveryConfig controls only MCP transport framing for a complete
// cognition body. It is team-owned and never changes formal cognition identity.
type OverviewDeliveryConfig struct {
	ChunkTokens int `json:"chunk_tokens"`
}

// Config is the complete aoci-code configuration.
type Config struct {
	Version int `json:"version"`

	// Locale is the team-wide language of every formal AOCI asset and contract.
	// It cannot be overridden by config.local.json.
	Locale string `json:"locale"`

	// LocaleMigration records the formal assets that still have to be rewritten
	// after a team changes locale. The target locale becomes active immediately,
	// but Agent Plan cannot report aligned until this receipt is cleared.
	LocaleMigration *LocaleMigration `json:"locale_migration,omitempty"`

	IndexPath string `json:"index_path"`

	ExcludeDirs     []string `json:"exclude_dirs"`
	ExcludeRootDirs []string `json:"exclude_root_dirs,omitempty"`
	ExcludeFiles    []string `json:"exclude_files"`

	// A historical field kept for compatibility; it is not mapped to a file pattern automatically today.
	ExcludeExts []string `json:"exclude_exts"`

	// SafeInventoryHighRiskOptIn is an exact-path, team-owned exception list.
	// It never accepts globs and does not weaken the default for any other path.
	SafeInventoryHighRiskOptIn []string `json:"safe_inventory_high_risk_opt_in,omitempty"`
	// SensitiveNameRules selects the built-in sensitive-name rule set (fs.SensitiveNameRulesV1
	// or V2). Team-owned and absent in old repositories, where absence is the rc22 identity;
	// init writes V2 for a new repository and a change is a governed Scope Change (#148).
	SensitiveNameRules string `json:"sensitive_name_rules,omitempty"`
	// HumanApprovalChannel says how a protected change may reach its person:
	// tty_or_host_dialog (absent) or tty_only. Team-owned, because whether a
	// host's dialog counts as a person's answer is a team decision.
	HumanApprovalChannel string `json:"human_approval_channel,omitempty"`

	// CurationExclude is the team negative space "the tool can see it but the maintainer decided not to record it".
	// The single matching source of truth is Config.CurationExcluded.
	CurationExclude []string `json:"curation_exclude"`

	HookStrict    bool `json:"hook_strict"`
	LedgerEnabled bool `json:"ledger_enabled"`

	// LineEndingTolerance is the team-level line-ending equivalence governance policy.
	//
	// true: governance judgement allows a pure CRLF/LF representation difference;
	// false: strict judgement by the raw byte SHA.
	//
	// Content binding, CAS, and the Stage source_sha256 do not read this field.
	LineEndingTolerance bool `json:"line_ending_tolerance"`

	// CognitionRefreshThreshold is the team-wide number of deduplicated
	// semantic changes that requires cognition refresh at a stable checkpoint.
	CognitionRefreshThreshold int `json:"cognition_refresh_threshold"`

	OverviewDelivery OverviewDeliveryConfig `json:"overview_delivery"`

	// ManagedScope is the team-owned desired policy. A direct edit changes its
	// identity but does not activate role or Baseline changes; Scope Change must
	// validate and apply the resulting transaction first. Nil retains legacy
	// all-managed-candidates-as-index behavior.
	ManagedScope *managedscope.Policy `json:"managed_scope,omitempty"`

	// CognitionBudget governs formal index density. Nil is an old-project
	// compatibility policy in observe mode; new init materializes enforce mode.
	CognitionBudget *cognitionbudget.Policy `json:"cognition_budget,omitempty"`

	InstalledAgents []string `json:"installed_agents"`

	// Automation is the team-level governance mode.
	// nil means the legacy compatibility state of an old repository; omitempty makes sure an old repository is not
	// silently written into a new mode when an unrelated configuration change is written back.
	Automation *AutomationConfig `json:"automation,omitempty"`

	// DatabaseSources contains only non-secret team declarations. CredentialEnv
	// names an environment variable and never contains its value.
	DatabaseSources []dbevidence.SourceConfig `json:"database_sources,omitempty"`

	// Database Cognition batches use machine defaults when these team-owned
	// values are zero. Local configuration cannot override them.
	DatabaseCognitionBatchObjects       int `json:"database_cognition_batch_objects,omitempty"`
	DatabaseCognitionBatchEvidenceBytes int `json:"database_cognition_batch_evidence_bytes,omitempty"`
	// CodeCognitionBatchEntries is how many Code candidates one Maintain asks
	// the model to author in a single call; zero means the machine default.
	// Team-owned: a batch size is a shared authoring contract, not a personal
	// preference, so local configuration cannot override it.
	CodeCognitionBatchEntries int `json:"code_cognition_batch_entries,omitempty"`
	// MaintainTransportBudgetBytes bounds the bytes of one Maintain response;
	// the current batch is cut where the next candidate would exceed it. Zero
	// means the machine default. Team-owned for the same reason as the batch
	// size: the host window is a shared fact of the authoring contract.
	MaintainTransportBudgetBytes int `json:"maintain_transport_budget_bytes,omitempty"`
	// CodeCognitionParallelBatches is how many current Code batches one
	// logical plan may have outstanding at once; zero means one, today's
	// behaviour. Team-owned: it is part of the shared authoring contract and
	// enters no policy, scope, or authorization identity.
	CodeCognitionParallelBatches int `json:"code_cognition_parallel_batches,omitempty"`

	AI AIConfig `json:"ai"`
}

// LocaleMigration is a resumable, deterministic receipt for a full-locale
// migration. Paths are repository-relative and kept in stable sorted order.
type LocaleMigration struct {
	Version                   int      `json:"version"`
	FromLocale                string   `json:"from_locale"`
	ToLocale                  string   `json:"to_locale"`
	HeaderPending             bool     `json:"header_pending"`
	HeaderTotal               int      `json:"header_total"`
	EntryPaths                []string `json:"entry_paths"`
	EntryTotal                int      `json:"entry_total"`
	GovernanceEntryPaths      []string `json:"governance_entry_paths"`
	GovernanceEntryTotal      int      `json:"governance_entry_total"`
	CurationPaths             []string `json:"curation_paths"`
	CurationTotal             int      `json:"curation_total"`
	ManagedIndexTextPending   bool     `json:"managed_index_text_pending"`
	ManagedIndexTextTotal     int      `json:"managed_index_text_total"`
	AgentsManagedBlockPending bool     `json:"agents_managed_block_pending"`
	AgentsManagedBlockTotal   int      `json:"agents_managed_block_total"`
}

// DefaultConfig returns the safe defaults.
//
// Automation=nil is deliberate: an ordinary load cannot tell whether the repository is new, so the default must
// stay legacy; init writes auto explicitly, and Onboarding resolves a missing configuration into the auto policy of the
// current Session only once its Plan has proven a Fresh Bootstrap.
//
// LineEndingTolerance=true is the default governance semantics; an explicit false in JSON overrides it.
func DefaultConfig() *Config {
	return &Config{
		Version:                    2,
		Locale:                     textassets.DefaultLocale,
		IndexPath:                  "aoci.txt",
		ExcludeDirs:                defaultExcludeDirs(),
		ExcludeFiles:               defaultExcludeFiles(),
		ExcludeExts:                []string{},
		SafeInventoryHighRiskOptIn: []string{},
		CurationExclude:            []string{},
		HookStrict:                 false,
		LedgerEnabled:              true,
		LineEndingTolerance:        true,
		CognitionRefreshThreshold:  machinecontract.CognitionRefreshThresholdDefault,
		OverviewDelivery: OverviewDeliveryConfig{
			ChunkTokens: machinecontract.OverviewChunkTokensDefault,
		},
		InstalledAgents: []string{},
		Automation:      nil,
		DatabaseSources: []dbevidence.SourceConfig{},
		AI: AIConfig{
			Enabled:         false,
			Provider:        "openai-compatible",
			BaseURL:         "",
			Model:           "",
			APIKeyEnv:       "",
			TimeoutSeconds:  0,
			MaxConcurrency:  0,
			MaxInputTokens:  0,
			TokenAccounting: "auto",
			PromptSnapshot:  "redacted",
		},
	}
}

func dirPath(repoRoot string) string {
	return filepath.Join(
		repoRoot,
		DirName,
	)
}

// FilePath returns the team configuration file path.
func FilePath(repoRoot string) string {
	return filepath.Join(
		dirPath(repoRoot),
		FileName,
	)
}

// LocalFilePath returns the personal configuration file path.
func LocalFilePath(repoRoot string) string {
	return filepath.Join(
		dirPath(repoRoot),
		LocalFileName,
	)
}

// Load loads the effective configuration.
//
// automation, line_ending_tolerance, cognition_refresh_threshold, and the Database
// Cognition batch cap always take the team-layer value; a same-named key in local must not override them.
func Load(repoRoot string) (*Config, error) {
	return loadEffective(repoRoot, true)
}

// LoadReadOnly loads the same effective configuration without materializing
// the legacy Locale compatibility upgrade. Read-only planners use it so
// observation cannot alter configuration or governance state.
func LoadReadOnly(repoRoot string) (*Config, error) {
	return loadEffective(repoRoot, false)
}

func loadEffective(repoRoot string, materializeLegacyLocale bool) (*Config, error) {
	cfg := DefaultConfig()

	if err := applyTeamJSONFile(repoRoot, cfg, materializeLegacyLocale); err != nil {
		return nil, err
	}

	if err := normalizeAutomationConfig(cfg); err != nil {
		return nil, fmt.Errorf(
			"团队配置 automation 无效 %s: %w",
			FilePath(repoRoot),
			err,
		)
	}
	if err := validateCognitionRefreshThreshold(cfg.CognitionRefreshThreshold); err != nil {
		return nil, fmt.Errorf("team configuration is invalid %s: %w", FilePath(repoRoot), err)
	}
	if err := validateOverviewDelivery(cfg.OverviewDelivery); err != nil {
		return nil, fmt.Errorf("team configuration is invalid %s: %w", FilePath(repoRoot), err)
	}

	teamAutomation := cloneAutomationConfig(
		cfg.Automation,
	)
	teamLineEndingTolerance :=
		cfg.LineEndingTolerance
	teamCognitionRefreshThreshold :=
		cfg.CognitionRefreshThreshold
	teamOverviewDelivery := cfg.OverviewDelivery
	teamManagedScope := cloneManagedScope(cfg.ManagedScope)
	teamCognitionBudget := cloneCognitionBudget(cfg.CognitionBudget)
	teamLocale := cfg.Locale
	teamLocaleMigration := cloneLocaleMigration(cfg.LocaleMigration)
	teamSafeInventoryHighRiskOptIn := append([]string{}, cfg.SafeInventoryHighRiskOptIn...)
	teamExcludeRootDirs := append([]string(nil), cfg.ExcludeRootDirs...)
	teamSensitiveNameRules := cfg.SensitiveNameRules
	teamHumanApprovalChannel := cfg.HumanApprovalChannel
	teamDatabaseSources := append([]dbevidence.SourceConfig{}, cfg.DatabaseSources...)
	teamDatabaseCognitionBatchObjects := cfg.DatabaseCognitionBatchObjects
	teamDatabaseCognitionBatchEvidenceBytes := cfg.DatabaseCognitionBatchEvidenceBytes
	teamCodeCognitionBatchEntries := cfg.CodeCognitionBatchEntries
	teamMaintainTransportBudgetBytes := cfg.MaintainTransportBudgetBytes
	teamCodeCognitionParallelBatches := cfg.CodeCognitionParallelBatches

	if err := applyJSONFileIfExists(
		LocalFilePath(repoRoot),
		cfg,
	); err != nil {
		return nil, err
	}

	// The team governance policy cannot be overridden by the personal layer.
	cfg.Automation = teamAutomation
	cfg.LineEndingTolerance =
		teamLineEndingTolerance
	cfg.CognitionRefreshThreshold =
		teamCognitionRefreshThreshold
	cfg.OverviewDelivery = teamOverviewDelivery
	cfg.ManagedScope = teamManagedScope
	cfg.CognitionBudget = teamCognitionBudget
	cfg.Locale = teamLocale
	cfg.LocaleMigration = teamLocaleMigration
	cfg.SafeInventoryHighRiskOptIn = teamSafeInventoryHighRiskOptIn
	cfg.ExcludeRootDirs = teamExcludeRootDirs
	cfg.SensitiveNameRules = teamSensitiveNameRules
	cfg.HumanApprovalChannel = teamHumanApprovalChannel
	cfg.DatabaseSources = teamDatabaseSources
	cfg.DatabaseCognitionBatchObjects = teamDatabaseCognitionBatchObjects
	cfg.DatabaseCognitionBatchEvidenceBytes = teamDatabaseCognitionBatchEvidenceBytes
	cfg.CodeCognitionBatchEntries = teamCodeCognitionBatchEntries
	cfg.MaintainTransportBudgetBytes = teamMaintainTransportBudgetBytes
	cfg.CodeCognitionParallelBatches = teamCodeCognitionParallelBatches

	applyFallbacks(cfg)

	if err := normalizeAutomationConfig(cfg); err != nil {
		return nil, err
	}
	if err := validateCognitionRefreshThreshold(cfg.CognitionRefreshThreshold); err != nil {
		return nil, err
	}
	if err := validateOverviewDelivery(cfg.OverviewDelivery); err != nil {
		return nil, err
	}
	if err := validateLocale(cfg.Locale); err != nil {
		return nil, err
	}
	if err := validateSensitiveNameRules(cfg.SensitiveNameRules); err != nil {
		return nil, err
	}
	if err := validateHumanApprovalChannel(cfg.HumanApprovalChannel); err != nil {
		return nil, err
	}
	if err := normalizeLocaleMigration(cfg); err != nil {
		return nil, err
	}
	if err := normalizeDatabaseSources(cfg); err != nil {
		return nil, err
	}
	if err := validateDatabaseCognitionBatchLimits(cfg); err != nil {
		return nil, err
	}
	if err := validateCodeCognitionBatchEntries(cfg); err != nil {
		return nil, err
	}
	if err := validateMaintainTransportBudgetBytes(cfg); err != nil {
		return nil, err
	}
	if err := validateCodeCognitionParallelBatches(cfg); err != nil {
		return nil, err
	}
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadBase loads only the raw team layer, for Save after a change.
func LoadBase(repoRoot string) (*Config, error) {
	cfg := DefaultConfig()

	if err := applyTeamJSONFile(repoRoot, cfg, true); err != nil {
		return nil, err
	}

	applyFallbacks(cfg)

	if err := normalizeAutomationConfig(cfg); err != nil {
		return nil, fmt.Errorf(
			"团队配置 automation 无效 %s: %w",
			FilePath(repoRoot),
			err,
		)
	}
	if err := validateCognitionRefreshThreshold(cfg.CognitionRefreshThreshold); err != nil {
		return nil, fmt.Errorf("team configuration is invalid %s: %w", FilePath(repoRoot), err)
	}
	if err := validateOverviewDelivery(cfg.OverviewDelivery); err != nil {
		return nil, fmt.Errorf("team configuration is invalid %s: %w", FilePath(repoRoot), err)
	}
	if err := validateLocale(cfg.Locale); err != nil {
		return nil, err
	}
	if err := validateSensitiveNameRules(cfg.SensitiveNameRules); err != nil {
		return nil, err
	}
	if err := validateHumanApprovalChannel(cfg.HumanApprovalChannel); err != nil {
		return nil, err
	}
	if err := normalizeLocaleMigration(cfg); err != nil {
		return nil, err
	}
	if err := normalizeDatabaseSources(cfg); err != nil {
		return nil, err
	}
	if err := validateDatabaseCognitionBatchLimits(cfg); err != nil {
		return nil, err
	}
	if err := validateCodeCognitionBatchEntries(cfg); err != nil {
		return nil, err
	}
	if err := validateMaintainTransportBudgetBytes(cfg); err != nil {
		return nil, err
	}
	if err := validateCodeCognitionParallelBatches(cfg); err != nil {
		return nil, err
	}
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyFallbacks handles only fields that can tell "missing" from "explicitly empty".
//
// LineEndingTolerance must never be changed here: a bool zero value cannot tell a missing field from an explicit false.
func applyFallbacks(cfg *Config) {
	if cfg.IndexPath == "" {
		cfg.IndexPath = "aoci.txt"
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}

	if cfg.ExcludeDirs == nil {
		cfg.ExcludeDirs = defaultExcludeDirs()
	}

	if cfg.ExcludeFiles == nil {
		cfg.ExcludeFiles = defaultExcludeFiles()
	}

	if cfg.ExcludeExts == nil {
		cfg.ExcludeExts = []string{}
	}

	if cfg.CurationExclude == nil {
		cfg.CurationExclude = []string{}
	}

	if cfg.SafeInventoryHighRiskOptIn == nil {
		cfg.SafeInventoryHighRiskOptIn = []string{}
	}

	if cfg.InstalledAgents == nil {
		cfg.InstalledAgents = []string{}
	}

	if cfg.DatabaseSources == nil {
		cfg.DatabaseSources = []dbevidence.SourceConfig{}
	}

	if cfg.AI.Provider == "" {
		cfg.AI.Provider = "openai-compatible"
	}

	if cfg.AI.TokenAccounting == "" {
		cfg.AI.TokenAccounting = "auto"
	}

	if cfg.AI.PromptSnapshot == "" {
		cfg.AI.PromptSnapshot = "redacted"
	}
}

// applyTeamJSONFile loads the team configuration and performs the one narrow
// rc8 compatibility migration. A pre-locale config deterministically remains
// zh-CN and is atomically materialized as schema version 2. The migration never
// rewrites aoci.txt or any other governance asset.
func applyTeamJSONFile(repoRoot string, cfg *Config, materializeLegacyLocale bool) error {
	configPath := FilePath(repoRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A repository that already has the historical default index but no
			// team config is also a legacy project. Materialize zh-CN without
			// touching the index; a truly new repository has neither file and
			// keeps the en-US default.
			legacyIndex := filepath.Join(repoRoot, filepath.FromSlash(cfg.IndexPath))
			if _, indexErr := os.Stat(legacyIndex); indexErr == nil {
				cfg.Locale = textassets.LegacyLocale
				cfg.Version = 2
				if materializeLegacyLocale {
					if saveErr := saveToPath(configPath, repoRoot, cfg); saveErr != nil {
						return fmt.Errorf("materialize legacy project locale: %w", saveErr)
					}
				}
			} else if !errors.Is(indexErr, os.ErrNotExist) {
				return fmt.Errorf("inspect legacy index %s: %w", legacyIndex, indexErr)
			}
			return nil
		}
		return fmt.Errorf("read configuration file %s: %w", configPath, err)
	}

	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&fields); err != nil {
		return fmt.Errorf(
			"configuration JSON is invalid %s (check the file): %w",
			configPath,
			err,
		)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf(
			"configuration JSON is invalid %s (check the file): %w",
			configPath,
			err,
		)
	}

	if _, exists := fields["locale"]; exists {
		return validateLocale(cfg.Locale)
	}

	cfg.Locale = textassets.LegacyLocale
	cfg.Version = 2
	applyFallbacks(cfg)
	if err := normalizeAutomationConfig(cfg); err != nil {
		return fmt.Errorf("team automation configuration is invalid %s: %w", configPath, err)
	}
	if materializeLegacyLocale {
		if err := saveToPath(configPath, repoRoot, cfg); err != nil {
			return fmt.Errorf("materialize legacy project locale: %w", err)
		}
	}
	return nil
}

// validateSensitiveNameRules accepts an absent value (the rc22 rule set) or a
// named rule set; anything else would silently fall back to V1 while the
// operator believed V2 was active.
func validateSensitiveNameRules(rules string) error {
	if !fs.ValidSensitiveNameRules(rules) {
		return fmt.Errorf("sensitive_name_rules_invalid: %q", rules)
	}
	return nil
}

func validateLocale(locale string) error {
	if !textassets.IsOfficialLocale(locale) {
		return fmt.Errorf(
			"unsupported project locale %q (available: en-US, zh-CN)",
			locale,
		)
	}
	return nil
}

func cloneLocaleMigration(source *LocaleMigration) *LocaleMigration {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.EntryPaths = append([]string{}, source.EntryPaths...)
	cloned.GovernanceEntryPaths = append([]string{}, source.GovernanceEntryPaths...)
	cloned.CurationPaths = append([]string{}, source.CurationPaths...)
	return &cloned
}

func normalizeDatabaseSources(cfg *Config) error {
	normalized, err := dbevidence.NormalizeSources(cfg.DatabaseSources)
	if err != nil {
		return fmt.Errorf("database_sources is invalid: %w", err)
	}
	cfg.DatabaseSources = normalized
	return nil
}

func normalizeLocaleMigration(cfg *Config) error {
	if cfg.LocaleMigration == nil {
		return nil
	}
	migration := cfg.LocaleMigration
	if migration.Version == 0 {
		// rc9 receipts did not distinguish ordinary source Entries from
		// repository-internal governance Entries. Retain every old target and
		// fail closed until a current Plan can classify it deterministically.
		migration.Version = 2
		migration.HeaderTotal = 1
		legacyEntries := append([]string{}, migration.EntryPaths...)
		migration.EntryPaths = removeGovernanceMigrationPaths(legacyEntries)
		for _, relPath := range legacyEntries {
			normalized := strings.TrimPrefix(filepath.ToSlash(relPath), "./")
			if normalized == DirName || strings.HasPrefix(normalized, DirName+"/") {
				migration.GovernanceEntryPaths = append(migration.GovernanceEntryPaths, relPath)
			}
		}
		migration.EntryTotal = len(migration.EntryPaths)
		migration.GovernanceEntryTotal = len(migration.GovernanceEntryPaths)
		migration.CurationTotal = len(migration.CurationPaths)
		migration.ManagedIndexTextPending = true
		migration.AgentsManagedBlockPending = true
		migration.AgentsManagedBlockTotal = 1
	}
	if migration.Version != 2 {
		return fmt.Errorf("unsupported locale migration receipt version %d", migration.Version)
	}
	if err := validateLocale(migration.FromLocale); err != nil {
		return fmt.Errorf("invalid locale migration source: %w", err)
	}
	if err := validateLocale(migration.ToLocale); err != nil {
		return fmt.Errorf("invalid locale migration target: %w", err)
	}
	if migration.FromLocale == migration.ToLocale {
		return fmt.Errorf("locale migration source and target must differ")
	}
	if migration.ToLocale != cfg.Locale {
		return fmt.Errorf(
			"locale migration target %q does not match project locale %q",
			migration.ToLocale,
			cfg.Locale,
		)
	}
	var err error
	migration.EntryPaths, err = normalizeMigrationPaths(migration.EntryPaths)
	if err != nil {
		return fmt.Errorf("invalid locale migration entry paths: %w", err)
	}
	migration.GovernanceEntryPaths, err = normalizeMigrationPaths(migration.GovernanceEntryPaths)
	if err != nil {
		return fmt.Errorf("invalid locale migration governance entry paths: %w", err)
	}
	migration.CurationPaths, err = normalizeMigrationPaths(migration.CurationPaths)
	if err != nil {
		return fmt.Errorf("invalid locale migration curation paths: %w", err)
	}
	if migration.HeaderTotal < 0 || migration.EntryTotal < len(migration.EntryPaths) ||
		migration.GovernanceEntryTotal < len(migration.GovernanceEntryPaths) ||
		migration.CurationTotal < len(migration.CurationPaths) ||
		migration.ManagedIndexTextTotal < 0 || migration.AgentsManagedBlockTotal < 0 {
		return fmt.Errorf("locale migration coverage totals are inconsistent")
	}
	if !migration.HeaderPending && !migration.ManagedIndexTextPending &&
		!migration.AgentsManagedBlockPending && len(migration.EntryPaths) == 0 &&
		len(migration.GovernanceEntryPaths) == 0 && len(migration.CurationPaths) == 0 {
		cfg.LocaleMigration = nil
	}
	return nil
}

func normalizeMigrationPaths(paths []string) ([]string, error) {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, raw := range paths {
		rel, err := fs.NormalizeRelPath(strings.TrimSpace(raw))
		if err != nil {
			return nil, err
		}
		if _, exists := seen[rel]; exists {
			continue
		}
		seen[rel] = struct{}{}
		result = append(result, rel)
	}
	sort.Strings(result)
	return result, nil
}

func removeGovernanceMigrationPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, relPath := range paths {
		normalized := strings.TrimPrefix(filepath.ToSlash(relPath), "./")
		if normalized == DirName || strings.HasPrefix(normalized, DirName+"/") {
			continue
		}
		result = append(result, relPath)
	}
	return result
}

func loadBaseSnapshot(repoRoot string) (*Config, string, error) {
	path := FilePath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read configuration file %s: %w", path, err)
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, "", fmt.Errorf("configuration JSON is invalid %s: %w", path, err)
	}
	applyFallbacks(cfg)
	if err := normalizeAutomationConfig(cfg); err != nil {
		return nil, "", err
	}
	if err := validateCognitionRefreshThreshold(cfg.CognitionRefreshThreshold); err != nil {
		return nil, "", err
	}
	if err := validateOverviewDelivery(cfg.OverviewDelivery); err != nil {
		return nil, "", err
	}
	if err := validateLocale(cfg.Locale); err != nil {
		return nil, "", err
	}
	if err := normalizeLocaleMigration(cfg); err != nil {
		return nil, "", err
	}
	if err := normalizeDatabaseSources(cfg); err != nil {
		return nil, "", err
	}
	if err := validateDatabaseCognitionBatchLimits(cfg); err != nil {
		return nil, "", err
	}
	if err := validateCodeCognitionBatchEntries(cfg); err != nil {
		return nil, "", err
	}
	if err := validateMaintainTransportBudgetBytes(cfg); err != nil {
		return nil, "", err
	}
	if err := validateCodeCognitionParallelBatches(cfg); err != nil {
		return nil, "", err
	}
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	return cfg, hex.EncodeToString(digest[:]), nil
}

func applyJSONFileIfExists(
	path string,
	cfg *Config,
) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf(
			"读取配置文件失败 %s: %w",
			path,
			err,
		)
	}

	if err := json.Unmarshal(
		data,
		cfg,
	); err != nil {
		return fmt.Errorf(
			"配置文件 JSON 解析失败 %s(请检查是否合法): %w",
			path,
			err,
		)
	}

	return nil
}

// Save writes the team-layer config.json.
//
// The given cfg must come from LoadBase or a fresh construction, never from the merged state of Load.
func Save(
	repoRoot string,
	cfg *Config,
) error {
	if err := normalizeAutomationConfig(cfg); err != nil {
		return err
	}
	if err := validateCognitionRefreshThreshold(cfg.CognitionRefreshThreshold); err != nil {
		return err
	}
	if err := validateOverviewDelivery(cfg.OverviewDelivery); err != nil {
		return err
	}
	if err := validateLocale(cfg.Locale); err != nil {
		return err
	}
	if err := normalizeLocaleMigration(cfg); err != nil {
		return err
	}
	if err := normalizeDatabaseSources(cfg); err != nil {
		return err
	}
	if err := validateDatabaseCognitionBatchLimits(cfg); err != nil {
		return err
	}
	if err := validateCodeCognitionBatchEntries(cfg); err != nil {
		return err
	}
	if err := validateMaintainTransportBudgetBytes(cfg); err != nil {
		return err
	}
	if err := validateCodeCognitionParallelBatches(cfg); err != nil {
		return err
	}
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return err
	}
	cfg.Version = 2

	return saveToPath(
		FilePath(repoRoot),
		repoRoot,
		cfg,
	)
}

// SaveLocal writes only the personal override surface.
//
// A team governance field, even if once written into local by hand, is removed on this save.
func SaveLocal(
	repoRoot string,
	cfg *Config,
) error {
	localPath := LocalFilePath(repoRoot)

	existing := map[string]json.RawMessage{}

	if data, err := os.ReadFile(localPath); err == nil {
		if unmarshalErr := json.Unmarshal(
			data,
			&existing,
		); unmarshalErr != nil {
			return fmt.Errorf(
				"现有本地配置 JSON 解析失败 %s"+
					"(请修复或删除后重试,拒绝覆盖): %w",
				localPath,
				unmarshalErr,
			)
		}
	} else if !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return fmt.Errorf(
			"读取本地配置失败 %s: %w",
			localPath,
			err,
		)
	}

	versionRaw, err := json.Marshal(
		cfg.Version,
	)
	if err != nil {
		return fmt.Errorf(
			"配置序列化失败: %w",
			err,
		)
	}

	aiRaw, err := json.Marshal(
		cfg.AI,
	)
	if err != nil {
		return fmt.Errorf(
			"配置序列化失败: %w",
			err,
		)
	}

	existing["version"] = versionRaw
	existing["ai"] = aiRaw

	// The team governance policy must not stay in the personal configuration.
	delete(
		existing,
		"automation",
	)
	delete(
		existing,
		"line_ending_tolerance",
	)
	delete(
		existing,
		"cognition_refresh_threshold",
	)
	delete(existing, "overview_delivery")
	delete(existing, "locale")
	delete(existing, "locale_migration")
	delete(existing, "database_sources")
	delete(existing, "database_cognition_batch_objects")
	delete(existing, "database_cognition_batch_evidence_bytes")
	delete(existing, "code_cognition_batch_entries")
	delete(existing, "maintain_transport_budget_bytes")
	delete(existing, "managed_scope")
	delete(existing, "cognition_budget")
	delete(existing, "exclude_root_dirs")
	delete(existing, "sensitive_name_rules")
	delete(existing, "human_approval_channel")

	if err := os.MkdirAll(
		dirPath(repoRoot),
		0o755,
	); err != nil {
		return fmt.Errorf(
			"创建 .aoci 目录失败: %w",
			err,
		)
	}

	data, err := json.MarshalIndent(
		existing,
		"",
		"  ",
	)
	if err != nil {
		return fmt.Errorf(
			"配置序列化失败: %w",
			err,
		)
	}

	data = append(
		data,
		'\n',
	)

	if err := fs.AtomicWrite(
		localPath,
		data,
	); err != nil {
		return fmt.Errorf(
			"写入配置文件失败: %w",
			err,
		)
	}

	return nil
}

func saveToPath(
	targetPath string,
	repoRoot string,
	cfg *Config,
) error {
	if err := os.MkdirAll(
		dirPath(repoRoot),
		0o755,
	); err != nil {
		return fmt.Errorf(
			"创建 .aoci 目录失败: %w",
			err,
		)
	}

	data, err := json.MarshalIndent(
		cfg,
		"",
		"  ",
	)
	if err != nil {
		return fmt.Errorf(
			"配置序列化失败: %w",
			err,
		)
	}

	data = append(
		data,
		'\n',
	)

	if err := fs.AtomicWrite(
		targetPath,
		data,
	); err != nil {
		return fmt.Errorf(
			"写入配置文件失败: %w",
			err,
		)
	}

	return nil
}

func saveToPathCAS(targetPath string, cfg *Config, expectedSHA256 string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("configuration serialization failed: %w", err)
	}
	data = append(data, '\n')
	if err := fs.AtomicWriteCAS(targetPath, data, expectedSHA256); err != nil {
		return fmt.Errorf("write configuration file with CAS: %w", err)
	}
	return nil
}

// WalkOptions converts the scan exclusion configuration.
// CurationExclude, Automation, and LineEndingTolerance do not enter the walk layer.
func (configValue *Config) WalkOptions() fs.WalkOptions {
	return fs.WalkOptions{
		ExcludeDirs:        configValue.ExcludeDirs,
		ExcludeRootDirs:    configValue.ExcludeRootDirs,
		ExcludeFiles:       configValue.ExcludeFiles,
		HighRiskOptIn:      append([]string{}, configValue.SafeInventoryHighRiskOptIn...),
		SensitiveNameRules: configValue.SensitiveNameRules,
	}
}

// IsAIEnabled reports whether the AI switch and the endpoint are both in effect.
func (configValue *Config) IsAIEnabled() bool {
	return configValue.AI.Enabled &&
		configValue.AI.BaseURL != ""
}
