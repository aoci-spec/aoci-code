// aoci init 基线前移测试: 三条设计裁决的回归防线(v2.6 悬账 P1-3,D51 同族)
// + P-04 防呆三防线(裸目录--here/agent白名单/hooks吞参明示)。
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
//	六、hooks 吞参明示(P-04): codex+--hooks 必须输出"仅支持 claude"提示。
//
// 测试工程纪律: init 的 --agent/--hooks/--here 为包级闭包变量,同进程多次
// Execute 会残留上次取值 —— 每次调用必须显式传 --agent 与 --hooks 取值
// (空值/false 复位),杜绝跨用例污染;--here 在 runInit 路径(flagRepo 覆盖
// 定根,定位恒成功)无效果,仅裸目录辅助 runInitBare 强制显式传值。
// flagRepo/flagQuiet 全局覆盖定根与静音,不并行。
// 命令取用口复用 check_test.go 的 findRegisteredCommand(同包)。
// stdout 断言(用例六): RunE 的用户输出走 fmt.Println(os.Stdout)而非 cobra
// out writer,断言须经 os.Pipe 截获且 flagQuiet=false —— cmd.SetOut 看不见它。
package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

// runInit 以 flagRepo 覆盖定根执行 `aoci init`。
// args 必须显式含 --agent 与 --hooks 取值(见文件头测试工程纪律)。
func runInit(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	for _, want := range []string{"--agent", "--hooks"} {
		found := false
		for _, a := range args {
			if strings.HasPrefix(a, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("用例必须显式传 %s 取值(包级 flag 闭包会跨执行残留)", want)
		}
	}

	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	cmd := findRegisteredCommand(t, "init")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	parent := cmd.Parent()
	if parent != nil {
		parent.RemoveCommand(cmd)
		defer parent.AddCommand(cmd)
	}
	err := cmd.Execute()
	return out.String(), err
}

// runInitBare 在裸目录(flagRepo 置空 + Chdir)执行 init,走真实定位链(P-04 被测面)。
// args 必须显式含 --agent/--hooks/--here 三者取值(闭包残留纪律)。
func runInitBare(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	for _, want := range []string{"--agent", "--hooks", "--here"} {
		found := false
		for _, a := range args {
			if strings.HasPrefix(a, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("裸目录用例必须显式传 %s 取值(包级 flag 闭包会跨执行残留)", want)
		}
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = "", true
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
		flagRepo, flagQuiet = oldRepo, oldQuiet
	})

	cmd := findRegisteredCommand(t, "init")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	parent := cmd.Parent()
	if parent != nil {
		parent.RemoveCommand(cmd)
		defer parent.AddCommand(cmd)
	}
	execErr := cmd.Execute()
	return out.String(), execErr
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
	_, err := runInit(t, root, "--agent=codx", "--hooks=false")
	if err == nil {
		t.Fatal("拼错的 --agent 值应硬拒")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitConfig {
		t.Fatalf("应为 ExitConfig 的 ExitError: %v", err)
	}
	if !strings.Contains(ee.Msg, "claude/codex/cursor/all") {
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

	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, false
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = oldStdout })

	cmd := findRegisteredCommand(t, "init")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--agent=", "--hooks=false", "--here=false"})
	execErr := cmd.Execute()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = oldStdout
	data, readErr := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	if execErr != nil {
		t.Fatalf("init失败: %v", execErr)
	}

	output := string(data)
	gitBoundary := strings.Index(output, ".aoci/.gitignore 已生成")
	automation := strings.Index(output, "automation.mode 已设为 auto")
	if gitBoundary < 0 || automation < 0 || gitBoundary > automation {
		t.Fatalf("init稳定输出顺序漂移:\n%s", output)
	}
}

// TestInitCodexHooksNotice hooks 吞参明示(P-04 防呆三): codex+--hooks 必须
// 输出"仅支持 claude"提示 —— 旧版把 codex 排除在提示条件外,静默吞参。
// 输出走 fmt.Println(os.Stdout),须 os.Pipe 截获且 flagQuiet=false;
// 本用例置于文件末尾: --hooks 残留 true 不再影响后续 init 用例(同文件内无后继)。
func TestInitCodexHooksNotice(t *testing.T) {
	root := t.TempDir()

	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, false
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	cmd := findRegisteredCommand(t, "init")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--agent=codex", "--hooks", "--here=false"})
	execErr := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	data, rerr := io.ReadAll(r)
	r.Close()
	if rerr != nil {
		t.Fatal(rerr)
	}
	if execErr != nil {
		t.Fatalf("init --agent=codex --hooks 应成功(hook 忽略但接入照常): %v", execErr)
	}
	if !strings.Contains(string(data), "仅支持 claude") {
		t.Fatalf("codex+--hooks 应明示 hook 已忽略: %s", data)
	}
}
