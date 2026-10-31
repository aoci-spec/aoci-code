// aoci init baseline advance tests: the regression defences of three design decisions (the v2.6 pending P1-3, the D51 family)
// plus the three P-04 foolproofing defences (bare directory --here / the agent allowlist / the explicit hooks support boundary).
// Index entry pending: init_test.go
//
// Locked behaviour:
//
//	one, advance: with an existing baseline, the files init writes this round (such as .codex/config.toml)
//	    get their fingerprints into the baseline, without manufacturing Unbaselined drift;
//	two, no baseline, no move: after init in a brand-new repository the baseline file does not exist; acknowledging everything is
//	    the authority of aoci scan, and init never oversteps to create a baseline;
//	three, no whitewash: an externally changed file that init skips as idempotent (fingerprints equal before and after the write)
//	    never advances; external drift must survive to verify, and init acknowledges only its own writes;
//	four, bare directory hard refusal (P-04): location fails and no --here means ExitConfig with zero writes,
//	    the defence against the incident of initializing the parent directory as a repository after a failed clone; --here passes explicitly;
//	five, the agent allowlist (P-04): a misspelled --agent value means ExitConfig with zero writes;
//	six, the hooks support boundary (P-04): codex plus --hooks must really install the compaction lifecycle integration.
//
// Test engineering discipline: every init goes through runCLI/runCLIBare as one complete process-level invocation
// (root flags and command flags all reset, locale following the repository configuration), and no package-level state is shared
// between cases; --here has no effect on the runInit path (--repo fixes the root, and location always succeeds), and only the bare-directory
// helper runInitBare really tests it.
// stdout assertions (case six): RunE's user output goes through fmt.Println(os.Stdout) rather than the cobra
// out writer, and must be captured from the process stdout through runCLIPrinted; cmd.SetOut cannot see it.
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

// runInit runs one quiet `aoci init` and returns both outputs and an error built from the exit code.
// Every call starts from the default flags; the --agent/--hooks passed explicitly only state the case under test.
func runInit(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, code := runCLI(t, root, append([]string{"--quiet", "init"}, args...)...)
	return stdout + stderr, exitError(stderr, code)
}

// runInitBare runs init in a bare directory (no --repo, with Chdir), through the real location chain (the P-04 surface under test).
func runInitBare(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	stdout, stderr, code := runCLIBare(t, append([]string{"--quiet", "init"}, args...)...)
	return stdout + stderr, exitError(stderr, code)
}

// seedBaseline establishes a baseline from the current on-disk fingerprints of the given relative path set (the test's equivalent of scan,
// not through the scan command, isolating the surface under test with fully controlled baseline content). It returns each file's fingerprint in the baseline.
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

// TestInitNoBaselineNoAdvance, no baseline, no move: after init in a brand-new repository the baseline file does not exist.
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

// TestInitAgentInstallAdvancesBaseline, advance: with an existing baseline, integrating codex puts
// .codex/config.toml into the baseline with its fingerprint matching the disk; the untouched AGENTS.md keeps its original value.
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
	// The AGENTS.md skipped as idempotent is not touched: the baseline value keeps the fixture's original
	if bl.Files["AGENTS.md"].SHA256 != seeded["AGENTS.md"] {
		t.Fatalf("未触及文件的基线值不得变动")
	}
}

// TestInitNoWhitewashExternalDrift, no whitewash: after AGENTS.md is changed externally (an append outside the block),
// init runs again; the block is already up to date so it is skipped as idempotent (fingerprints equal before and after the write), the baseline must keep the old value,
// and the external drift (baseline value ≠ current disk value) survives to verify.
func TestInitNoWhitewashExternalDrift(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("首次 init 失败: %v", err)
	}
	seeded := seedBaseline(t, root, []string{"aoci.txt", "AGENTS.md"})

	// The external change: one line appended at the end outside the block (EnsureAgentsBlock replaces only the block,
	// and with the block up to date newText==text takes the skip branch and produces no write)
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

	// First make the managed block need an update, producing a real postimage this round; the hook then manufactures the race.
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

// TestInitBareDirRequiresHere, the bare directory hard refusal (P-04 foolproofing two): location fails and no --here means
// ExitConfig with zero writes (the defence against the incident of init in the parent directory after a failed clone); --here passes explicitly.
func TestInitBareDirRequiresHere(t *testing.T) {
	dir := t.TempDir()

	// Without --here: hard refusal with zero writes
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

	// With an explicit --here: pass, the skeleton is generated in place
	if _, err := runInitBare(t, dir, "--agent=", "--hooks=false", "--here"); err != nil {
		t.Fatalf("--here 应放行就地创建: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "aoci.txt")); serr != nil {
		t.Fatalf("--here 放行后骨架应就地生成: %v", serr)
	}
}

// TestInitInvalidAgentRejected, the agent allowlist (P-04 foolproofing one): a misspelled value means ExitConfig
// with zero writes (validation sits before every write, leaving no half-initialized state).
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

// TestInitNewRepositoryOutputOrder keeps the complete production output order from before the migration. The asset precheck must
// happen before the first write, but must not move the automation hint ahead of the Git boundary hint because of that.
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

// TestInitCodexHooksNotice, the hooks support boundary (P-04 foolproofing three): codex plus --hooks must
// really write the compaction prompt and the post-compaction reload hook, never silently swallowing the flag again.
// The output goes through fmt.Println(os.Stdout) and must be captured from the process stdout through runCLIPrinted.
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
