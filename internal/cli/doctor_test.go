// Index entry: doctor_test.go[CDR.Test.5.T]
// Responsibility: verify aoci doctor. A three-layer strategy:
//  1. pure logic unit tests: doctorReport's three-state marks and failure count (independent of any environment, the most stable);
//  2. the failure path: a temporary empty directory (no .git/.aoci) → repository root location fails, exit code 3 (isolation required);
//  3. the healthy path: this project's real root (the test can locate the project root .git upwards from internal/cli/) →
//     assert structural facts (group titles / key check items / exit code), not specific values that change as the project evolves.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runDoctor runs the doctor command in the given working directory and returns the output and the error.
// An empty workDir uses the current working directory (that is, internal/cli/ under the real project path).
func runDoctor(t *testing.T, workDir string, args ...string) (string, error) {
	t.Helper()

	// With a workDir given, switch temporarily (restored after the test); used by the failure path's temporary empty directory test
	if workDir != "" {
		orig, err := os.Getwd()
		if err != nil {
			t.Fatalf("获取当前目录失败: %v", err)
		}
		if err := os.Chdir(workDir); err != nil {
			t.Fatalf("切换到 %s 失败: %v", workDir, err)
		}
		defer func() { _ = os.Chdir(orig) }()
	}

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// —— Layer one: pure logic unit tests (doctorReport's three states and failure count) ——

func TestDoctorReport_FailureCounting(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rep := &doctorReport{out: &buf}

	rep.line(statePass, "通过项", "ok")
	rep.line(stateInfo, "信息项", "info")
	rep.line(stateNA, "不适用项", "na")
	rep.line(stateFail, "失败项一", "boom")
	rep.line(stateFail, "失败项二", "boom")

	// Only stateFail counts toward failures
	if rep.failures != 2 {
		t.Errorf("失败计数应为 2,得到 %d", rep.failures)
	}

	output := buf.String()
	// Every state mark appears correctly
	if !strings.Contains(output, "[✓] 通过项") {
		t.Error("通过项应带 [✓] 标记")
	}
	if !strings.Contains(output, "[✗] 失败项一") {
		t.Error("失败项应带 [✗] 标记")
	}
	if !strings.Contains(output, "[–] 不适用项") {
		t.Error("不适用项应带 [–] 标记")
	}
}

func TestDoctorReport_StateMark(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state checkState
		want  string
	}{
		{statePass, "[✓]"},
		{stateFail, "[✗]"},
		{stateNA, "[–]"},
		{stateInfo, "[·]"},
	}
	for _, c := range cases {
		if got := stateMark(c.state); got != c.want {
			t.Errorf("stateMark(%d) = %q, 期望 %q", c.state, got, c.want)
		}
	}
}

func TestDoctorReport_LineWithoutDetail(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	rep := &doctorReport{out: &buf}
	rep.line(statePass, "无详情项", "")
	out := buf.String()
	if !strings.Contains(out, "[✓] 无详情项") {
		t.Errorf("无详情行格式错误: %q", out)
	}
	// Without details there should be no stray colon separator
	if strings.Contains(out, "无详情项: ") {
		t.Errorf("无详情行不应带冒号分隔: %q", out)
	}
}

// —— Layer two: the failure path (a temporary empty directory → repository root location fails, exit code 3) ——

func TestDoctor_NoRepoRoot(t *testing.T) {
	// A temporary empty directory: no .git, no .aoci; resolveRepoRoot should fail to find one upwards
	emptyDir := t.TempDir()

	out, err := runDoctor(t, emptyDir)

	// It should return an error carrying exit code 3
	if err == nil {
		t.Fatal("空目录下 doctor 应返回错误(找不到仓库根)")
	}
	ec, ok := err.(exitCoder)
	if !ok {
		t.Fatalf("错误应实现 exitCoder 接口,得到 %T", err)
	}
	if ec.ExitCode() != 3 {
		t.Errorf("找不到仓库根应退出码 3,得到 %d", ec.ExitCode())
	}
	// The output should hold the repository root location failure hint
	if !strings.Contains(out, "仓库根") {
		t.Errorf("输出应提示仓库根定位失败: %q", out)
	}
}

// —— Layer three: the healthy path (this project's real root → structural assertions) ——

// findProjectRoot walks up from the current test working directory to the project root holding .git (the same logic as resolveRepoRoot).
// When none is found the healthy-path test is skipped (for example in a build environment without .git).
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".aoci")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "" // 到根仍未找到
		}
		dir = parent
	}
}

func TestDoctor_HealthyProject(t *testing.T) {
	root := findProjectRoot(t)
	if root == "" {
		t.Skip("未找到项目根(.git/.aoci),跳过健康路径测试")
	}

	// Run doctor under the real project root
	out, err := runDoctor(t, root)

	// Assert structural facts (no specific values, so the test does not grow brittle as the project evolves):

	// 1. all three group titles appear (the AI enhancement layer retired with the Legacy authoring surface, 0.2.0)
	for _, group := range []string{"仓库与核心资产", "Agent 接入", "平台特性"} {
		if !strings.Contains(out, group) {
			t.Errorf("输出应含分组标题 %q", group)
		}
	}

	// 2. the key check item labels appear
	for _, label := range []string{"仓库根定位", "配置加载", "基线文件"} {
		if !strings.Contains(out, label) {
			t.Errorf("输出应含检查项 %q", label)
		}
	}

	// 3. repository root location should succeed (this project has .git)
	if !strings.Contains(out, "[✓] 仓库根定位") {
		t.Error("真实项目根下,仓库根定位应为 [✓]")
	}

	// 4. the end should hold the diagnosis completion summary
	if !strings.Contains(out, "诊断完成") {
		t.Error("输出末尾应有诊断完成汇总")
	}

	// 5. exit code semantics: a healthy project should pass (err nil) or fail only on individual optional items.
	//    On failure, print the output for diagnosis (nil is not asserted strictly, since the project environment may hold real pending items).
	if err != nil {
		if ec, ok := err.(exitCoder); ok {
			t.Logf("doctor 退出码 %d(项目存在待处理项,非测试失败):\n%s", ec.ExitCode(), out)
		} else {
			t.Errorf("doctor 返回了非退出码错误: %v", err)
		}
	}
}
