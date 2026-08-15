// Package config 负责 aoci-code 的配置读写。
// 索引条目: config.go[GCF9M]
//
// 配置分两层:
//
//	.aoci/config.json        进 Git,团队共享基础配置与治理策略
//	.aoci/config.local.json  不进 Git,仅本机 AI 端点等个人覆盖项
//
// 加载语义:
// DefaultConfig → config.json 覆盖 → config.local.json 覆盖。
//
// automation、line_ending_tolerance 与 cognition_refresh_threshold 都属于团队治理资产:
// local 合并后必须恢复团队值，个人层不能覆盖。
//
// 层泄漏纪律:
// 写团队层的调用方必须用 LoadBase 取源再 Save;Load 合并态只供读取,
// 绝不能回写团队层,否则 local 的个人端点会泄漏进 config.json。
//
// AI 配置铁律:
// AI 默认关闭且无默认端点;APIKeyEnv 保存环境变量名而非密钥。
// 密钥不得进入 config、ledger、备份或 Prompt 快照。
//
// 技术产物排除扩充(R60-F.9-A2,2026-07-18): HTTPX 自然实弹中 .ruff_cache
// 被判虚假 Missing 并触发多轮清理 —— 确定性工具产物不应进入语义治理。
// 本次向 defaultExcludeDirs 补 .ruff_cache/.tox/.nox/htmlcov,向
// defaultExcludeFiles 补 .coverage(它是文件不是目录)。排除分层裁决:
// 不硬编码进 fs 层(与 node_modules 同类属业务/工具产物,非 .git/.aoci
// 结构红线;用户可见可改),不新建第三策略层(现有二层已够表达)。
// 存量仓限制(已知代价): applyFallbacks 只在字段为 nil 时回填默认,
// 已落盘的显式清单会冻结旧默认 —— 本次扩充只救新仓;存量仓迁移经
// doctor 信息态提示(A2 下半),零静默修改配置。
//
// 换行宽容(R60-F.11):
// line_ending_tolerance 默认 true，用于治理判定时把纯 CRLF/LF 表示差异
// 识别为等价；显式 false 恢复严格原始字节判定。该字段只属于团队层，
// config.local.json 无权覆盖，防止同一仓库在不同成员机器上产生不同治理结论。
// 本字段是 bool，缺失时依赖 DefaultConfig 预置 true；绝不能加入 applyFallbacks，
// 否则无法区分“字段缺失”与“团队显式 false”。
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

func defaultExcludeFiles() []string {
	return []string{
		"*.backup.*",
		"*.bak",
		".coverage",
	}
}

// DefaultTechnicalExcludeDirs 导出当前默认技术产物目录清单的拷贝。
//
// 供 doctor 对存量仓做缺项提示；唯一事实源仍是 defaultExcludeDirs。
func DefaultTechnicalExcludeDirs() []string {
	return defaultExcludeDirs()
}

// AIConfig 是 AI 增强层配置。
// BaseURL 完全由用户声明;CLI 不设置默认云端。
type AIConfig struct {
	Enabled bool `json:"enabled"`

	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`

	// APIKeyEnv 是环境变量名，不是密钥。
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

// Config 是 aoci-code 完整配置。
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

	ExcludeDirs  []string `json:"exclude_dirs"`
	ExcludeFiles []string `json:"exclude_files"`

	// 历史保留字段，当前不自动映射为文件模式。
	ExcludeExts []string `json:"exclude_exts"`

	// SafeInventoryHighRiskOptIn is an exact-path, team-owned exception list.
	// It never accepts globs and does not weaken the default for any other path.
	SafeInventoryHighRiskOptIn []string `json:"safe_inventory_high_risk_opt_in,omitempty"`

	// CurationExclude 是“工具看得见但维护者裁决不收录”的团队负空间。
	// 唯一匹配事实源为 Config.CurationExcluded。
	CurationExclude []string `json:"curation_exclude"`

	HookStrict    bool `json:"hook_strict"`
	LedgerEnabled bool `json:"ledger_enabled"`

	// LineEndingTolerance 是团队级换行等价治理策略。
	//
	// true：治理判定允许纯 CRLF/LF 表示差异；
	// false：严格按原始字节SHA判断。
	//
	// 内容绑定、CAS和Stage source_sha256不读取本字段。
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

	// Automation 是团队级治理模式。
	// nil 表示旧仓 legacy 兼容态;omitempty 保证旧仓经无关配置写回时不会
	// 被静默写入新模式。
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

// DefaultConfig 返回安全默认值。
//
// Automation=nil 是刻意设计: 普通加载无法判断仓库是否为新仓，因此默认必须
// 保持 legacy; init 会显式写入 auto，而 Onboarding 只在其 Plan 已证明 Fresh
// Bootstrap 时把缺失配置解析为当次 Session 的 auto 策略。
//
// LineEndingTolerance=true 是缺省治理语义；JSON显式false会覆盖该默认值。
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

// FilePath 返回团队配置文件路径。
func FilePath(repoRoot string) string {
	return filepath.Join(
		dirPath(repoRoot),
		FileName,
	)
}

// LocalFilePath 返回个人配置文件路径。
func LocalFilePath(repoRoot string) string {
	return filepath.Join(
		dirPath(repoRoot),
		LocalFileName,
	)
}

// Load 加载生效配置。
//
// automation、line_ending_tolerance、cognition_refresh_threshold 与 Database
// Cognition批次上限始终取团队层值；local 中即使存在同名键也不得覆盖。
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
	teamDatabaseSources := append([]dbevidence.SourceConfig{}, cfg.DatabaseSources...)
	teamDatabaseCognitionBatchObjects := cfg.DatabaseCognitionBatchObjects
	teamDatabaseCognitionBatchEvidenceBytes := cfg.DatabaseCognitionBatchEvidenceBytes
	teamCodeCognitionBatchEntries := cfg.CodeCognitionBatchEntries

	if err := applyJSONFileIfExists(
		LocalFilePath(repoRoot),
		cfg,
	); err != nil {
		return nil, err
	}

	// 团队治理策略不可被个人层覆盖。
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
	cfg.DatabaseSources = teamDatabaseSources
	cfg.DatabaseCognitionBatchObjects = teamDatabaseCognitionBatchObjects
	cfg.DatabaseCognitionBatchEvidenceBytes = teamDatabaseCognitionBatchEvidenceBytes
	cfg.CodeCognitionBatchEntries = teamCodeCognitionBatchEntries

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
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// LoadBase 只加载团队层原始态，供改后 Save。
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
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyFallbacks 只处理能够区分“缺失”和“显式空值”的字段。
//
// 严禁在这里修改 LineEndingTolerance：bool 零值无法区分字段缺失与显式 false。
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

// AdvanceLocaleMigration removes only assets proven to have completed their
// locale rewrite. It is safe to call repeatedly with the same paths.
func AdvanceLocaleMigration(
	repoRoot string,
	headerComplete bool,
	entryPaths []string,
	curationPaths []string,
) error {
	for attempt := 0; attempt < 3; attempt++ {
		cfg, expectedSHA256, err := loadBaseSnapshot(repoRoot)
		if err != nil {
			return err
		}
		if cfg.LocaleMigration == nil {
			return nil
		}
		if headerComplete {
			cfg.LocaleMigration.HeaderPending = false
			cfg.LocaleMigration.ManagedIndexTextPending = false
			cfg.LocaleMigration.AgentsManagedBlockPending = false
			cfg.LocaleMigration.GovernanceEntryPaths = nil
			cfg.LocaleMigration.EntryPaths = removeGovernanceMigrationPaths(
				cfg.LocaleMigration.EntryPaths,
			)
		}
		cfg.LocaleMigration.EntryPaths = removeMigrationPaths(
			cfg.LocaleMigration.EntryPaths,
			entryPaths,
		)
		cfg.LocaleMigration.CurationPaths = removeMigrationPaths(
			cfg.LocaleMigration.CurationPaths,
			curationPaths,
		)
		if !cfg.LocaleMigration.HeaderPending &&
			!cfg.LocaleMigration.ManagedIndexTextPending &&
			!cfg.LocaleMigration.AgentsManagedBlockPending &&
			len(cfg.LocaleMigration.EntryPaths) == 0 &&
			len(cfg.LocaleMigration.GovernanceEntryPaths) == 0 &&
			len(cfg.LocaleMigration.CurationPaths) == 0 {
			cfg.LocaleMigration = nil
		}
		if err := saveToPathCAS(FilePath(repoRoot), cfg, expectedSHA256); err != nil {
			if errors.Is(err, fs.ErrAtomicCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("advance locale migration: %w", fs.ErrAtomicCASConflict)
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
	if err := normalizeManagedScopeAndBudget(cfg); err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	return cfg, hex.EncodeToString(digest[:]), nil
}

func removeMigrationPaths(current, completed []string) []string {
	if len(current) == 0 || len(completed) == 0 {
		return current
	}
	done := make(map[string]struct{}, len(completed))
	for _, path := range completed {
		done[path] = struct{}{}
	}
	result := make([]string, 0, len(current))
	for _, path := range current {
		if _, exists := done[path]; !exists {
			result = append(result, path)
		}
	}
	return result
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

// Save 写入团队层 config.json。
//
// 传入 cfg 必须来自 LoadBase 或全新构造，不得来自 Load 合并态。
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

// SaveLocal 只写个人覆盖面。
//
// 团队治理字段即使曾被人工写入local，也会在本次保存时移除。
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

	// 团队治理策略不得保留在个人配置中。
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
	delete(existing, "managed_scope")
	delete(existing, "cognition_budget")

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

// WalkOptions 转换扫描排除配置。
// CurationExclude、Automation和LineEndingTolerance都不进入遍历层。
func (configValue *Config) WalkOptions() fs.WalkOptions {
	return fs.WalkOptions{
		ExcludeDirs:   configValue.ExcludeDirs,
		ExcludeFiles:  configValue.ExcludeFiles,
		HighRiskOptIn: append([]string{}, configValue.SafeInventoryHighRiskOptIn...),
	}
}

// IsAIEnabled 返回AI开关和端点是否同时生效。
func (configValue *Config) IsAIEnabled() bool {
	return configValue.AI.Enabled &&
		configValue.AI.BaseURL != ""
}
