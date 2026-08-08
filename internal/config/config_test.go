// config 包测试 —— 字段完整性防线(R21 事故防再犯)
// 索引条目: config_test.go(待补录)
//
// 背景(2026-07-08 事故): config.go 在 v2.0 重写时丢失第一期四字段
// (HookStrict/LedgerEnabled/InstalledAgents/ExcludeFiles),致 hooks/mcptools/cli
// 全仓编译失败且被单包测试掩盖。本文件把"关键字段存在且默认值正确"变成测试断言:
// 再有人整文件重写 config.go 丢字段,本包测试即红,不必等调用方编译炸。
//
// 覆盖: 默认值硬断言 / 双层加载覆盖语义 / 显式空数组受尊重 / 空值兜底 /
//
//	损坏 JSON 报错 / Save-Load 往返 / SaveLocal 分流 / IsAIEnabled 语义 /
//	SaveLocal 合并写三防线(P1-b 事故防再犯,2026-07-09)。
//
// P1-b 事故背景: 旧版 SaveLocal 经 saveToPath 落盘整个 Config 快照,把当时的
// exclude_dirs 等团队字段冻结进 local 层,此后 base 层演进被静默遮蔽(实弹:
// base 层新增 experiments 排除被 local 旧快照顶掉,致 24 项幽灵 Missing)。
// 修法: SaveLocal 读现有 local 为 RawMessage 图,只替换 version 与 ai 两键。
// 本文件末三用例是该修法的防线本体 —— 再有人把 SaveLocal 退化回整量快照,
// 或让它静默覆盖损坏文件,测试即红。
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultConfig_CriticalFields 关键字段默认值硬断言(字段丢失防线本体)。
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
	// ExcludeFiles 默认必须含备份模式(平台 1132 误报事故预防)
	found := map[string]bool{}
	for _, p := range c.ExcludeFiles {
		found[p] = true
	}
	if !found["*.backup.*"] || !found["*.bak"] {
		t.Errorf("ExcludeFiles 默认必须含 *.backup.* 与 *.bak,实得 %v", c.ExcludeFiles)
	}
	// AI 块安全默认
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

// TestLoad_MissingFilesFallsBackToDefault 双层文件均缺失 → 完整默认配置,零错误。
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

// TestLoad_TwoLayerOverride 双层加载: local 覆盖 base,未出现字段保持上一层值。
func TestLoad_TwoLayerOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// base: 改 index_path 与关闭 ledger
	base := `{"index_path":"custom.txt","ledger_enabled":false,"ai":{"model":"base-model"}}`
	if err := os.WriteFile(FilePath(root), []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	// local: 只覆盖 ai.base_url(其余字段不出现)
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

// TestLoad_ExplicitEmptyExcludeFilesRespected 显式空数组=用户要清空排除,不回填默认。
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

// TestLoad_EmptyValueFallbacks 关键字段被写空 → 兜底回默认。
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

// TestLoad_CorruptJSONReturnsActionableError 损坏 JSON → 带路径的可操作错误。
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

// TestSaveLoadRoundTrip Save→Load 往返一致(含新恢复字段)。
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
	// 落盘文件绝不含密钥值形态的内容(R19 抽查:文件里只应有环境变量名)
	raw, _ := os.ReadFile(FilePath(root))
	if !strings.Contains(string(raw), "MY_KEY_ENV") {
		t.Error("api_key_env 环境变量名应在配置文件中")
	}
}

// TestSaveLocal_WritesToLocalFileOnly SaveLocal 只写 local 文件不动 base。
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

// TestIsAIEnabled 开关与端点的合取语义。
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

// —— 以下三用例为 SaveLocal 合并写防线(P1-b 事故防再犯,2026-07-09)——

// TestSaveLocal_PreservesExistingKeys 既有非 ai 键(用户手工覆盖项)必须原样保留。
// 事故语境: 用户可能在 local 层手工写 hook_strict、ledger_enabled 等个人覆盖项;
// SaveLocal(由 ai setup --local 触发)只应更新 version 与 ai,不得摧毁这些手工项。
func TestSaveLocal_PreservesExistingKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	// 预置: 含手工覆盖键的既有 local 文件(hook_strict 是真实可覆盖字段,
	// my_manual_key 模拟未来扩展/未知键 —— 两类都必须保真)
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
	// 语义验证: 合并后 Load 三层加载,hook_strict 覆盖仍生效
	got, err := Load(root)
	if err != nil {
		t.Fatalf("合并后 Load 失败: %v", err)
	}
	if !got.HookStrict {
		t.Error("合并后 local 层 hook_strict=true 覆盖应仍生效")
	}
}

// TestSaveLocal_DoesNotFreezeTeamFields 团队字段(exclude_dirs 等)绝不落入 local。
// 事故本体: 旧版整量快照把 exclude_dirs 冻结进 local,base 层后续新增排除项被
// 静默顶掉(24 项幽灵 Missing)。断言: SaveLocal 落盘面只含 version 与 ai 两键
// (在无既有 local 的初次写入场景),团队字段一个不落。
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
	// 语义验证: base 层的 exclude_dirs 演进不被 local 遮蔽
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

// TestSaveLocal_CorruptExistingRefusesOverwrite 既有 local 损坏 → 报错拒绝覆盖。
// 语义: 静默覆盖损坏文件会丢用户手工覆盖项;与"绝不猜"同族,报可操作错误交人裁决。
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
	// 损坏原文必须原样保留(拒绝覆盖的本体证明)
	raw, rerr := os.ReadFile(LocalFilePath(root))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(raw) != corrupt {
		t.Errorf("被拒的 SaveLocal 不得改动损坏原文,实得: %s", raw)
	}
}

// —— 以下两用例为 LoadBase 层泄漏防线(P1-c 事故防再犯,2026-07-10)——

// TestLoadBase_ExcludesLocalLayer LoadBase 绝不合并 local 层(泄漏修法本体)。
// 事故语境: setup 不带 --local 时旧版用 Load 合并态改后 Save,把 local 的
// 个人端点 base_url 整量写入待提交的团队 config.json(git diff 提交前拦截)。
// 断言: 同一仓库 Load 可见 local 的 base_url,LoadBase 不可见;base 层字段两者一致。
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
	// 兜底语义与 Load 一致(applyFallbacks 共享单点)
	if baseCfg.IndexPath != "aoci.txt" || baseCfg.AI.Provider != "openai-compatible" {
		t.Fatalf("LoadBase 兜底应与 Load 一致,实得 %+v", baseCfg)
	}
}

// TestLoadBase_SaveRoundTripZeroLeak 修法端到端: LoadBase→改→Save 写回团队层,
// local 个人字段零泄漏,且 local 文件本身一字节不动。
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

	// 模拟 setup 不带 --local 的正确链路: LoadBase → 改团队字段 → Save
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
	// local 文件一字节不动
	gotLocal, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotLocal) != localRaw {
		t.Fatalf("Save 写团队层不得触碰 local 文件,实得: %s", gotLocal)
	}
}
