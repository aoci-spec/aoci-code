// config package tests: the field completeness defence (against a repeat of the R21 incident)
// Index entry: config_test.go (to be recorded)
//
// Background (the 2026-07-08 incident): the v2.0 rewrite of config.go lost the four phase-one fields
// (HookStrict/LedgerEnabled/InstalledAgents/ExcludeFiles), breaking the compilation of hooks/mcptools/cli across the
// repository while single-package tests hid it. This file turns "the key fields exist with correct defaults" into test assertions:
// the next whole-file rewrite of config.go that drops a field turns this package's tests red, without waiting for a caller's compilation to blow up.
//
// Coverage: hard default assertions / two-layer load override semantics / an explicit empty array respected / empty-value fallbacks /
//
//	corrupt JSON errors / the Save-Load round trip / the SaveLocal split / IsAIEnabled semantics /
//	the three SaveLocal merge-write defences (against a repeat of the P1-b incident, 2026-07-09).
//
// The P1-b incident background: the old SaveLocal wrote the whole Config snapshot through saveToPath, freezing the then
// exclude_dirs and other team fields into the local layer, after which base-layer evolution was silently shadowed (live: a new
// experiments exclusion in the base layer was overridden by the old local snapshot, yielding 24 ghost Missing).
// The fix: SaveLocal reads the existing local as a RawMessage map and replaces only the version and ai keys.
// The last three cases of this file are the body of that defence: the next change that degrades SaveLocal back to a whole snapshot,
// or lets it silently overwrite a corrupt file, turns the tests red.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultConfig_CriticalFields is the hard assertion of key field defaults (the body of the lost-field defence).
func TestDefaultConfig_CriticalFields(t *testing.T) {
	c := DefaultConfig()

	if c.IndexPath != "aoci.txt" {
		t.Errorf("IndexPath 默认应为 aoci.txt,实得 %q", c.IndexPath)
	}
	if c.LedgerEnabled != true {
		t.Error("LedgerEnabled 默认必须为 true(遥测是研究仪器数据源)")
	}
	if c.HookStrict != false {
		t.Error("HookStrict 默认必须为 false(hook 故障不得卡死工作流)")
	}
	if c.InstalledAgents == nil {
		t.Error("InstalledAgents 必须初始化为空切片而非 nil")
	}
	// ExcludeFiles must hold the backup pattern by default (preventing the platform 1132 false-report incident)
	found := map[string]bool{}
	for _, p := range c.ExcludeFiles {
		found[p] = true
	}
	if !found["*.backup.*"] || !found["*.bak"] {
		t.Errorf("ExcludeFiles 默认必须含 *.backup.* 与 *.bak,实得 %v", c.ExcludeFiles)
	}
	// The AI block's safe defaults
	if c.AI.Enabled {
		t.Error("AI.Enabled 默认必须为 false(纯离线确定性模式)")
	}
	if c.AI.BaseURL != "" {
		t.Error("AI.BaseURL 默认必须为空(绝不设默认端点,负空间禁区)")
	}
	if c.AI.Provider != "openai-compatible" {
		t.Errorf("AI.Provider 默认应为 openai-compatible,实得 %q", c.AI.Provider)
	}
	if c.AI.TokenAccounting != "auto" {
		t.Errorf("AI.TokenAccounting 默认应为 auto,实得 %q", c.AI.TokenAccounting)
	}
	if c.AI.PromptSnapshot != "redacted" {
		t.Errorf("AI.PromptSnapshot 默认应为 redacted,实得 %q", c.AI.PromptSnapshot)
	}
}

// TestLoad_MissingFilesFallsBackToDefault: both layer files missing → the complete default configuration with zero errors.
func TestLoad_MissingFilesFallsBackToDefault(t *testing.T) {
	root := t.TempDir()
	c, err := Load(root)
	if err != nil {
		t.Fatalf("空仓 Load 不应报错: %v", err)
	}
	if c.IndexPath != "aoci.txt" || !c.LedgerEnabled {
		t.Errorf("空仓应回退完整默认,实得 %+v", c)
	}
}

// TestLoad_TwoLayerOverride, the two-layer load: local overrides base, and a field that does not appear keeps the previous layer's value.
func TestLoad_TwoLayerOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// base: change index_path and turn the ledger off
	base := `{"index_path":"custom.txt","ledger_enabled":false,"ai":{"model":"base-model"}}`
	if err := os.WriteFile(FilePath(root), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	// local: override only ai.base_url (no other field appears)
	local := `{"ai":{"base_url":"http://10.0.0.5:8000/v1"}}`
	if err := os.WriteFile(LocalFilePath(root), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.IndexPath != "custom.txt" {
		t.Errorf("base 层 index_path 未生效: %q", c.IndexPath)
	}
	if c.LedgerEnabled {
		t.Error("base 层显式 ledger_enabled=false 未生效")
	}
	if c.AI.BaseURL != "http://10.0.0.5:8000/v1" {
		t.Errorf("local 层 base_url 覆盖未生效: %q", c.AI.BaseURL)
	}
	if c.AI.Model != "base-model" {
		t.Errorf("local 未出现的 ai.model 应保持 base 层值,实得 %q", c.AI.Model)
	}
}

// TestLoad_ExplicitEmptyExcludeFilesRespected: an explicit empty array = the user wants the exclusions cleared, with no default fill.
func TestLoad_ExplicitEmptyExcludeFilesRespected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FilePath(root), []byte(`{"exclude_files":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if len(c.ExcludeFiles) != 0 {
		t.Errorf("显式空数组应受尊重不回填,实得 %v", c.ExcludeFiles)
	}
}

// TestLoad_EmptyValueFallbacks: key fields written empty → fall back to the defaults.
func TestLoad_EmptyValueFallbacks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FilePath(root), []byte(`{"index_path":"","ai":{"provider":"","token_accounting":"","prompt_snapshot":""}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.IndexPath != "aoci.txt" {
		t.Errorf("空 index_path 应兜底 aoci.txt,实得 %q", c.IndexPath)
	}
	if c.AI.Provider != "openai-compatible" || c.AI.TokenAccounting != "auto" || c.AI.PromptSnapshot != "redacted" {
		t.Errorf("AI 空值兜底未生效: %+v", c.AI)
	}
}

// TestLoad_CorruptJSONReturnsActionableError: corrupt JSON → an actionable error carrying the path.
func TestLoad_CorruptJSONReturnsActionableError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FilePath(root), []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if err == nil {
		t.Fatal("损坏 JSON 应报错")
	}
	if !strings.Contains(err.Error(), "config.json") {
		t.Errorf("错误信息应含文件路径便于定位: %v", err)
	}
}

// TestSaveLoadRoundTrip: the Save→Load round trip agrees (including the new recovery fields).
func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	c := DefaultConfig()
	c.HookStrict = true
	c.InstalledAgents = []string{"claude", "codex"}
	c.AI.Enabled = true
	c.AI.BaseURL = "http://10.0.0.5:8000/v1"
	c.AI.APIKeyEnv = "MY_KEY_ENV" // 环境变量名,非密钥

	if err := Save(root, c); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if !got.HookStrict {
		t.Error("HookStrict 往返丢失")
	}
	if len(got.InstalledAgents) != 2 || got.InstalledAgents[0] != "claude" {
		t.Errorf("InstalledAgents 往返不一致: %v", got.InstalledAgents)
	}
	if !got.AI.Enabled || got.AI.BaseURL != "http://10.0.0.5:8000/v1" || got.AI.APIKeyEnv != "MY_KEY_ENV" {
		t.Errorf("AI 块往返不一致: %+v", got.AI)
	}
	// The file on disk must never hold anything shaped like a key value (the R19 spot check: the file should hold only an environment variable name)
	raw, _ := os.ReadFile(FilePath(root))
	if !strings.Contains(string(raw), "MY_KEY_ENV") {
		t.Error("api_key_env 环境变量名应在配置文件中")
	}
}

// TestSaveLocal_WritesToLocalFileOnly: SaveLocal writes only the local file and leaves base alone.
func TestSaveLocal_WritesToLocalFileOnly(t *testing.T) {
	root := t.TempDir()
	c := DefaultConfig()
	c.AI.BaseURL = "http://local-only:8000/v1"
	if err := SaveLocal(root, c); err != nil {
		t.Fatalf("SaveLocal 失败: %v", err)
	}
	if _, err := os.Stat(LocalFilePath(root)); err != nil {
		t.Errorf("config.local.json 应已创建: %v", err)
	}
	if _, err := os.Stat(FilePath(root)); !os.IsNotExist(err) {
		t.Error("SaveLocal 不应创建 config.json")
	}
}

// TestIsAIEnabled: the conjunction semantics of the switch and the endpoint.
func TestIsAIEnabled(t *testing.T) {
	c := DefaultConfig()
	if c.IsAIEnabled() {
		t.Error("默认配置不应判为 AI 已启用")
	}
	c.AI.Enabled = true
	if c.IsAIEnabled() {
		t.Error("开关开但 base_url 空,不应判为已启用")
	}
	c.AI.BaseURL = "http://x/v1"
	if !c.IsAIEnabled() {
		t.Error("开关开且 base_url 非空,应判为已启用")
	}
}

// —— The three cases below are the SaveLocal merge-write defence (against a repeat of the P1-b incident, 2026-07-09) ——

// TestSaveLocal_PreservesExistingKeys: existing non-ai keys (the user's manual overrides) must be kept as they are.
// Incident context: the user may hand-write personal overrides such as hook_strict and ledger_enabled in the local layer;
// SaveLocal (triggered by ai setup --local) should update only version and ai and must not destroy those manual items.
func TestSaveLocal_PreservesExistingKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// Preset: an existing local file holding manual override keys (hook_strict is a real overridable field,
	// my_manual_key simulates a future extension/unknown key; both kinds must be kept intact)
	pre := `{"version":1,"hook_strict":true,"my_manual_key":"keep-me"}`
	if err := os.WriteFile(LocalFilePath(root), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	c.AI.Enabled = true
	c.AI.BaseURL = "http://merge-test:8000/v1"
	if err := SaveLocal(root, c); err != nil {
		t.Fatalf("SaveLocal 失败: %v", err)
	}

	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"my_manual_key"`) || !strings.Contains(s, "keep-me") {
		t.Errorf("既有未知键应原样保留,实得: %s", s)
	}
	if !strings.Contains(s, `"hook_strict"`) {
		t.Errorf("既有真实覆盖键 hook_strict 应原样保留,实得: %s", s)
	}
	if !strings.Contains(s, "http://merge-test:8000/v1") {
		t.Errorf("ai 块应已更新为新值,实得: %s", s)
	}
	// Semantic verification: after the merge, the three-layer Load still applies the hook_strict override
	got, err := Load(root)
	if err != nil {
		t.Fatalf("合并后 Load 失败: %v", err)
	}
	if !got.HookStrict {
		t.Error("合并后 local 层 hook_strict=true 覆盖应仍生效")
	}
}

// TestSaveLocal_DoesNotFreezeTeamFields: team fields (exclude_dirs and the like) never land in local.
// The incident itself: the old whole snapshot froze exclude_dirs into local, and exclusions added later in the base layer were
// silently overridden (24 ghost Missing). Assertion: the SaveLocal write surface holds only the two keys version and ai
// (in the first-write scenario without an existing local), and not one team field.
func TestSaveLocal_DoesNotFreezeTeamFields(t *testing.T) {
	root := t.TempDir()
	c := DefaultConfig()
	c.ExcludeDirs = []string{"experiments", "tmp"} // 模拟内存中携带的团队字段
	c.InstalledAgents = []string{"claude"}
	c.AI.Enabled = true
	c.AI.BaseURL = "http://freeze-test:8000/v1"
	if err := SaveLocal(root, c); err != nil {
		t.Fatalf("SaveLocal 失败: %v", err)
	}

	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, forbidden := range []string{"exclude_dirs", "exclude_files", "installed_agents", "index_path", "ledger_enabled"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("团队字段 %q 绝不应落入 local(整量快照回归,P1-b 复发),实得: %s", forbidden, s)
		}
	}
	if !strings.Contains(s, `"ai"`) || !strings.Contains(s, `"version"`) {
		t.Errorf("local 应含且仅含 version 与 ai 两键,实得: %s", s)
	}
	// Semantic verification: the base layer's exclude_dirs evolution is not shadowed by local
	if err := os.WriteFile(FilePath(root), []byte(`{"exclude_dirs":["experiments"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	foundExp := false
	for _, d := range got.ExcludeDirs {
		if d == "experiments" {
			foundExp = true
		}
	}
	if !foundExp {
		t.Errorf("base 层 exclude_dirs 应生效不被 local 遮蔽,实得 %v", got.ExcludeDirs)
	}
}

// TestSaveLocal_CorruptExistingRefusesOverwrite: an existing corrupt local → an error refusing to overwrite.
// Semantics: silently overwriting a corrupt file loses the user's manual overrides; of the "never guess" family, it reports an actionable error and leaves the decision to a person.
func TestSaveLocal_CorruptExistingRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := `{broken local`
	if err := os.WriteFile(LocalFilePath(root), []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	c.AI.BaseURL = "http://corrupt-test:8000/v1"
	err := SaveLocal(root, c)
	if err == nil {
		t.Fatal("既有 local 损坏时 SaveLocal 应报错拒绝覆盖")
	}
	if !strings.Contains(err.Error(), "config.local.json") {
		t.Errorf("错误信息应含文件路径便于定位: %v", err)
	}
	// The corrupt original must be kept as it is (the proof that the overwrite was refused)
	raw, rerr := os.ReadFile(LocalFilePath(root))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(raw) != corrupt {
		t.Errorf("被拒的 SaveLocal 不得改动损坏原文,实得: %s", raw)
	}
}

// —— The two cases below are the LoadBase layer-leak defence (against a repeat of the P1-c incident, 2026-07-10) ——

// TestLoadBase_ExcludesLocalLayer: LoadBase never merges the local layer (the body of the leak fix).
// Incident context: without --local, the old setup changed the merged state of Load and then Saved, writing local's
// personal endpoint base_url wholesale into the team config.json about to be committed (caught by git diff before the commit).
// Assertion: in the same repository Load sees local's base_url and LoadBase does not; the base-layer fields agree in both.
func TestLoadBase_ExcludesLocalLayer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	base := `{"exclude_dirs":["experiments"],"ai":{"model":"team-model"}}`
	if err := os.WriteFile(FilePath(root), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	local := `{"ai":{"enabled":true,"base_url":"http://personal:8000/v1","api_key_env":"MY_KEY"}}`
	if err := os.WriteFile(LocalFilePath(root), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := Load(root)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if merged.AI.BaseURL != "http://personal:8000/v1" {
		t.Fatalf("Load 合并态应可见 local 的 base_url,实得 %q", merged.AI.BaseURL)
	}

	baseCfg, err := LoadBase(root)
	if err != nil {
		t.Fatalf("LoadBase 失败: %v", err)
	}
	if baseCfg.AI.BaseURL != "" || baseCfg.AI.APIKeyEnv != "" || baseCfg.AI.Enabled {
		t.Fatalf("LoadBase 绝不应含 local 层字段(P1-c 复发),实得 %+v", baseCfg.AI)
	}
	if baseCfg.AI.Model != "team-model" {
		t.Fatalf("LoadBase 应保留 base 层 ai.model,实得 %q", baseCfg.AI.Model)
	}
	if len(baseCfg.ExcludeDirs) != 1 || baseCfg.ExcludeDirs[0] != "experiments" {
		t.Fatalf("LoadBase 应保留 base 层团队字段,实得 %v", baseCfg.ExcludeDirs)
	}
	// The fallback semantics agree with Load (applyFallbacks is the shared single point)
	if baseCfg.IndexPath != "aoci.txt" || baseCfg.AI.Provider != "openai-compatible" {
		t.Fatalf("LoadBase 兜底应与 Load 一致,实得 %+v", baseCfg)
	}
}

// TestLoadBase_SaveRoundTripZeroLeak, the fix end to end: LoadBase→change→Save writes back the team layer,
// with zero leak of local personal fields, and the local file itself not changed by one byte.
func TestLoadBase_SaveRoundTripZeroLeak(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(FilePath(root), []byte(`{"exclude_dirs":["experiments"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	localRaw := `{"version":1,"ai":{"enabled":true,"base_url":"http://leak-test:8000/v1","api_key_env":"K"}}`
	if err := os.WriteFile(LocalFilePath(root), []byte(localRaw), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate the correct chain of setup without --local: LoadBase → change a team field → Save
	cfg, err := LoadBase(root)
	if err != nil {
		t.Fatalf("LoadBase 失败: %v", err)
	}
	cfg.AI.MaxConcurrency = 3
	if err := Save(root, cfg); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	teamRaw, err := os.ReadFile(FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	s := string(teamRaw)
	for _, forbidden := range []string{"leak-test", "http://", `"K"`} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("团队层含 local 泄漏内容 %q(P1-c 复发): %s", forbidden, s)
		}
	}
	if !strings.Contains(s, `"max_concurrency": 3`) {
		t.Fatalf("团队层应含本次写入的字段,实得: %s", s)
	}
	// The local file is not changed by one byte
	gotLocal, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotLocal) != localRaw {
		t.Fatalf("Save 写团队层不得触碰 local 文件,实得: %s", gotLocal)
	}
}
