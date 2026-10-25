// aoci init 基线前移测试: 三条设计裁决的回归防线(v2.6 悬账 P1-3,D51 同族)
// + P-04 防呆三防线(裸目录--here/agent白名单/hooks支持边界明示)。
// 索引条目待补: init_test.go
//
// 锁定行为:
//
//	一、前移: 基线已存在时,init 本轮新写入的文件(如 .codex/config.toml)
//	    其指纹进入基线,不自造 Unbaselined 漂移;
//	二、无基线不动: 全新仓 init 后基线文件不存在 —— 全量承认是 aoci scan
//	    的职权,init 绝不越权建基线;
//	三、防洗白: 外部修改过的文件在 init 幂等跳过时(写前写后指纹一致)
//	    绝不前移 —— 外部漂移必须存活到 verify,init 只承认自己的写入;
//	四、裸目录硬拒(P-04): 定位失败且无 --here 时 ExitConfig 且零写入 ——
//	    clone 失败后误把父目录初始化成仓的事故防线;--here 显式放行;
//	五、agent 白名单(P-04): 拼错的 --agent 值 ExitConfig 且零写入;
//	六、hooks 支持边界(P-04): codex+--hooks 必须真实安装压缩生命周期接入。
//
// 测试工程纪律: 每次 init 都经 runCLI/runCLIBare 走一次完整的进程级调用
// (根 flag 与命令 flag 全部复位、locale 跟随仓库配置),用例之间不共享任何
// 包级状态;--here 在 runInit 路径(--repo 定根,定位恒成功)无效果,仅裸目录
// 辅助 runInitBare 才真正被测。
// stdout 断言(用例六): RunE 的用户输出走 fmt.Println(os.Stdout)而非 cobra
// out writer,须经 runCLIPrinted 截获进程 stdout —— cmd.SetOut 看不见它。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// runInit 执行一次安静的 `aoci init`,返回两路输出与按退出码构造的错误。
// 每次调用都从默认 flag 出发,显式传入的 --agent/--hooks 只是在陈述被测用例。
func runInit(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, code := runCLI(t, root, append([]string{"--quiet", "init"}, args...)...)
	return stdout + stderr, exitError(stderr, code)
}

// runInitBare 在裸目录(不传 --repo + Chdir)执行 init,走真实定位链(P-04 被测面)。
func runInitBare(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	stdout, stderr, code := runCLIBare(t, append([]string{"--quiet", "init"}, args...)...)
	return stdout + stderr, exitError(stderr, code)
}

// seedBaseline 以给定相对路径集的当前磁盘指纹建立基线(测试内的 scan 等价件,
// 不走 scan 命令 —— 隔离被测面,基线内容完全受控)。返回各文件落入基线的指纹。
func seedBaseline(t *testing.T, root string, rels []string) map[string]string {
	t.Helper()
	files := map[string]baseline.Fingerprint{}
	hashes := map[string]string{}
	for _, rel := range rels {
		fp, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("夹具取指纹失败 %s: %v", rel, err)
		}
		files[rel] = fp
		hashes[rel] = fp.SHA256
	}
	if err := baseline.Save(root, baseline.NewBaseline(files)); err != nil {
		t.Fatalf("夹具建基线失败: %v", err)
	}
	return hashes
}

// TestInitNoBaselineNoAdvance 无基线不动: 全新仓 init 后基线文件不存在。
func TestInitNoBaselineNoAdvance(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	if _, exists, err := baseline.Load(root); err != nil || exists {
		t.Fatalf("init 不得越权建基线(全量承认归 scan): exists=%v err=%v", exists, err)
	}
}

func TestInitPersistsRootOnlyArtifactDefaultsOnlyForNewRepositories(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SensitiveNameRules != afs.SensitiveNameRulesV2 || cfg.WalkOptions().SensitiveNameRules != afs.SensitiveNameRulesV2 {
		t.Fatalf("a new repository must persist the v2 sensitive-name rules (#148): %q", cfg.SensitiveNameRules)
	}
	if source, ok := afs.ConfiguredExclusionSource("build/output", cfg.WalkOptions()); !ok || source != "exclude_root_dirs:build" {
		t.Fatalf("root build should be excluded by the persisted policy: %q, %t", source, ok)
	}
	if _, ok := afs.ConfiguredExclusionSource("src/build/tool.go", cfg.WalkOptions()); ok {
		t.Fatal("nested tracked build source should remain a candidate")
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadBase(root)
	if err != nil || len(cfg.ExcludeRootDirs) == 0 {
		t.Fatalf("idempotent init lost the root policy: cfg=%#v err=%v", cfg, err)
	}
	for _, rel := range []string{"src/build/tool.go", "build/output"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("scan failed: code=%d stderr=%s", code, stderr.String())
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("scan baseline: exists=%t err=%v", exists, err)
	}
	if _, ok := state.Files["src/build/tool.go"]; !ok {
		t.Fatal("new-repository scan dropped nested source")
	}
	if _, ok := state.Files["build/output"]; ok {
		t.Fatal("new-repository scan included root build artifact")
	}

	oldRoot := t.TempDir()
	if err := config.Save(oldRoot, config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, oldRoot, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	old, err := config.LoadBase(oldRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.ExcludeRootDirs) != 0 {
		t.Fatalf("existing repository gained new defaults: %v", old.ExcludeRootDirs)
	}
	if source, ok := afs.ConfiguredExclusionSource("src/build/tool.go", old.WalkOptions()); !ok || source != "exclude_dirs:build" {
		t.Fatalf("existing repository must keep nested exclusion: %q, %t", source, ok)
	}
}

func TestFreshVolumeInitAllowsFirstManagedScan(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Volume-first 首次 scan 失败: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.ManagedScope == nil {
		t.Fatalf("首次 scan 未建立带 Managed Scope receipt 的 Baseline: exists=%t err=%v state=%#v", exists, err, state)
	}
}

// TestInitAgentInstallAdvancesBaseline 前移: 基线存在时接入 codex,
// .codex/config.toml 进入基线且指纹与磁盘一致;未触及的 AGENTS.md 保持原值。
func TestInitAgentInstallAdvancesBaseline(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("首次 init 失败: %v", err)
	}
	seeded := seedBaseline(t, root, []string{"aoci.txt", "AGENTS.md"})

	if _, err := runInit(t, root, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatalf("init --agent=codex 失败: %v", err)
	}

	bl, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("基线应存在: exists=%v err=%v", exists, err)
	}
	got, ok := bl.Files[".codex/config.toml"]
	if !ok {
		t.Fatalf("接入写入的 .codex/config.toml 应被前移进基线: %+v", bl.Files)
	}
	disk, err := baseline.HashFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("读磁盘指纹失败: %v", err)
	}
	if got.SHA256 != disk.SHA256 {
		t.Fatalf("基线指纹应与磁盘一致: 基线=%s 磁盘=%s", got.SHA256, disk.SHA256)
	}
	// 幂等跳过的 AGENTS.md 不被触碰: 基线值保持夹具原值
	if bl.Files["AGENTS.md"].SHA256 != seeded["AGENTS.md"] {
		t.Fatalf("未触及文件的基线值不得变动")
	}
}

// TestInitNoWhitewashExternalDrift 防洗白: 外部修改 AGENTS.md(区块外追加)后
// 重跑 init —— 区块已最新致幂等跳过(写前写后指纹一致),基线必须保持旧值,
// 外部漂移(基线值 ≠ 磁盘现值)存活到 verify。
func TestInitNoWhitewashExternalDrift(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("首次 init 失败: %v", err)
	}
	seeded := seedBaseline(t, root, []string{"aoci.txt", "AGENTS.md"})

	// 外部修改: 区块外文末追加一行(EnsureAgentsBlock 只替换区块,
	// 区块已最新时 newText==text 走跳过分支,不产生写入)
	agentsPath := filepath.Join(root, "AGENTS.md")
	old, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentsPath, append(old, []byte("\n# 外部人工追加的说明行\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("重跑 init 失败: %v", err)
	}

	bl, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("基线应存在: exists=%v err=%v", exists, err)
	}
	disk, err := baseline.HashFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bl.Files["AGENTS.md"].SHA256 != seeded["AGENTS.md"] {
		t.Fatalf("防洗白失败: 幂等跳过的文件基线值被改动")
	}
	if bl.Files["AGENTS.md"].SHA256 == disk.SHA256 {
		t.Fatalf("防洗白失败: 外部漂移被 init 洗白(基线值等于磁盘现值)")
	}
}

func TestInitDoesNotBaselineConcurrentChangeAfterOwnWrite(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("首次init失败: %v", err)
	}
	seeded := seedBaseline(t, root, []string{"aoci.txt", "AGENTS.md"})
	oldHook := beforeInitBaselineAdvance
	beforeInitBaselineAdvance = func() {
		path := filepath.Join(root, "AGENTS.md")
		data, _ := os.ReadFile(path)
		_ = os.WriteFile(path, append(data, []byte("\n# concurrent external change\n")...), 0o644)
	}
	t.Cleanup(func() { beforeInitBaselineAdvance = oldHook })

	// 先使托管区块需要更新，从而产生本轮真实postimage；hook随后制造竞态。
	agentsPath := filepath.Join(root, "AGENTS.md")
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agentsPath, []byte(strings.Replace(string(data), "<!-- aoci:end -->", "# stale\n<!-- aoci:end -->", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("并发场景init失败: %v", err)
	}

	state, exists, err := baseline.Load(root)
	current, hashErr := baseline.HashFile(agentsPath)
	if err != nil || hashErr != nil || !exists || state.Files["AGENTS.md"] == current ||
		state.Files["AGENTS.md"].SHA256 != seeded["AGENTS.md"] {
		t.Fatalf("并发外部变化不得被init洗白: exists=%v load=%v hash=%v state=%+v current=%+v", exists, err, hashErr, state, current)
	}
}

// TestInitBareDirRequiresHere 裸目录硬拒(P-04 防呆二): 定位失败且无 --here 时
// ExitConfig 且零写入(clone 失败后误在父目录 init 的事故防线);--here 显式放行。
func TestInitBareDirRequiresHere(t *testing.T) {
	dir := t.TempDir()

	// 无 --here: 硬拒且零写入
	_, err := runInitBare(t, dir, "--agent=", "--hooks=false", "--here=false")
	if err == nil {
		t.Fatal("裸目录无 --here 应硬拒")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitConfig {
		t.Fatalf("应为 ExitConfig 的 ExitError: %v", err)
	}
	if !strings.Contains(ee.Msg, "--here") {
		t.Fatalf("拒绝文案应指引 --here: %q", ee.Msg)
	}
	if _, serr := os.Stat(filepath.Join(dir, ".aoci")); !os.IsNotExist(serr) {
		t.Fatalf("硬拒时不得产生任何写入(.aoci 不应存在)")
	}
	if _, serr := os.Stat(filepath.Join(dir, "aoci.txt")); !os.IsNotExist(serr) {
		t.Fatalf("硬拒时不得产生任何写入(aoci.txt 不应存在)")
	}

	// 显式 --here: 放行,骨架就地生成
	if _, err := runInitBare(t, dir, "--agent=", "--hooks=false", "--here"); err != nil {
		t.Fatalf("--here 应放行就地创建: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "aoci.txt")); serr != nil {
		t.Fatalf("--here 放行后骨架应就地生成: %v", serr)
	}
}

// TestInitInvalidAgentRejected agent 白名单(P-04 防呆一): 拼错值 ExitConfig
// 且零写入(校验置于一切写入之前,不留半初始化状态)。
func TestInitInvalidAgentRejected(t *testing.T) {
	root := t.TempDir()
	// The refusal is asserted in zh-CN wording, so the invocation selects it.
	_, err := runInit(t, root, "--agent=codx", "--hooks=false", "--locale=zh-CN")
	if err == nil {
		t.Fatal("拼错的 --agent 值应硬拒")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitConfig {
		t.Fatalf("应为 ExitConfig 的 ExitError: %v", err)
	}
	if !strings.Contains(ee.Msg, "claude/codex/cursor/opencode/qoder/all") {
		t.Fatalf("拒绝文案应列出合法值: %q", ee.Msg)
	}
	if _, serr := os.Stat(filepath.Join(root, ".aoci")); !os.IsNotExist(serr) {
		t.Fatalf("硬拒时不得产生任何写入(.aoci 不应存在)")
	}
}

// TestInitNewRepositoryOutputOrder保持迁移前的完整生产输出顺序。资源预检必须
// 发生在首次写入前，但不能因此把automation提示移到Git边界提示之前。
func TestInitNewRepositoryOutputOrder(t *testing.T) {
	root := t.TempDir()

	// The order is asserted on zh-CN lines, so the invocation selects them.
	output, _, stderr, code := runCLIPrinted(t, root, "init", "--agent=", "--hooks=false", "--here=false", "--locale=zh-CN")
	if code != ExitOK {
		t.Fatalf("init失败: exit %d\n%s", code, stderr)
	}

	gitBoundary := strings.Index(output, ".aoci/.gitignore 已生成")
	automation := strings.Index(output, "automation.mode 已设为 auto")
	if gitBoundary < 0 || automation < 0 || gitBoundary > automation {
		t.Fatalf("init稳定输出顺序漂移:\n%s", output)
	}
}

// TestInitCodexHooksNotice hooks 支持边界(P-04 防呆三): codex+--hooks 必须
// 真实写入压缩提示与压缩后重载 hook,不得再静默吞参。
// 输出走 fmt.Println(os.Stdout),须经 runCLIPrinted 截获进程 stdout。
func TestInitCodexHooksNotice(t *testing.T) {
	root := t.TempDir()

	// The notice is asserted in zh-CN wording, so the invocation selects it.
	data, _, stderr, code := runCLIPrinted(t, root, "init", "--agent=codex", "--hooks", "--here=false", "--locale=zh-CN")
	if code != ExitOK {
		t.Fatalf("init --agent=codex --hooks 应成功安装压缩接入: exit %d\n%s", code, stderr)
	}
	if strings.Contains(data, "已忽略") {
		t.Fatalf("codex+--hooks 不得再静默忽略: %s", data)
	}
	configData, readErr := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	configText := string(configData)
	if !strings.Contains(configText, "compact_prompt") ||
		!strings.Contains(configText, "[[hooks.SessionStart]]") ||
		!strings.Contains(configText, "hook codex-compact") {
		t.Fatalf("codex+--hooks 未完整安装压缩接入:\n%s", configText)
	}
}

func TestInitCodexHooksConflictIsZeroWrite(t *testing.T) {
	for _, agent := range []string{"codex", "all"} {
		t.Run(agent, func(t *testing.T) {
			root := t.TempDir()
			configDir := filepath.Join(root, ".codex")
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			const existing = "model = \"o4-mini\"\ncompact_prompt = \"keep mine\"\n"
			configPath := filepath.Join(configDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := runInit(t, root, "--agent="+agent, "--hooks=true")
			if err == nil || !strings.Contains(err.Error(), "compact_prompt") {
				t.Fatalf("init --agent=%s --hooks 应在自定义 compact_prompt 上失败: %v", agent, err)
			}

			data, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(data) != existing {
				t.Fatalf("冲突预检改写了用户配置:\n%s", data)
			}
			rootEntries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(rootEntries) != 1 || rootEntries[0].Name() != ".codex" {
				t.Fatalf("冲突预检前发生了仓库写入: %v", rootEntries)
			}
			codexEntries, readErr := os.ReadDir(configDir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(codexEntries) != 1 || codexEntries[0].Name() != "config.toml" {
				t.Fatalf("冲突预检产生了 Codex 备份或其他文件: %v", codexEntries)
			}
		})
	}
}
