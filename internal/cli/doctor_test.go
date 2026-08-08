// 索引条目: doctor_test.go[CDR.Test.5.T]
// 职责: 验证 aoci doctor。策略三层 ——
//  1. 纯逻辑单元测试: doctorReport 的三态标记与失败计数(不依赖任何环境,最稳);
//  2. 失败路径: 临时空目录(无 .git/.aoci)→ 仓库根定位失败、退出码 3(隔离必需);
//  3. 健康路径: 本项目真实根(测试从 internal/cli/ 向上可定位到项目根 .git)→
//     断言结构性事实(分组标题/关键检查项/退出码),不断言随项目演进变化的具体数值。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runDoctor 在指定工作目录下执行 doctor 命令,返回输出与错误。
// workDir 为空则使用当前工作目录(即真实项目路径下的 internal/cli/)。
func runDoctor(t *testing.T, workDir string, args ...string) (string, error) {
	t.Helper()

	// 若指定 workDir,临时切换(测试后恢复)——用于失败路径的临时空目录测试
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

// —— 第一层: 纯逻辑单元测试(doctorReport 三态与失败计数)——

func TestDoctorReport_FailureCounting(t *testing.T) {
	var buf bytes.Buffer
	rep := &doctorReport{out: &buf}

	rep.line(statePass, "通过项", "ok")
	rep.line(stateInfo, "信息项", "info")
	rep.line(stateNA, "不适用项", "na")
	rep.line(stateFail, "失败项一", "boom")
	rep.line(stateFail, "失败项二", "boom")

	// 仅 stateFail 计入 failures
	if rep.failures != 2 {
		t.Errorf("失败计数应为 2,得到 %d", rep.failures)
	}

	output := buf.String()
	// 各态标记正确出现
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
	var buf bytes.Buffer
	rep := &doctorReport{out: &buf}
	rep.line(statePass, "无详情项", "")
	out := buf.String()
	if !strings.Contains(out, "[✓] 无详情项") {
		t.Errorf("无详情行格式错误: %q", out)
	}
	// 无详情时不应有多余的冒号分隔
	if strings.Contains(out, "无详情项: ") {
		t.Errorf("无详情行不应带冒号分隔: %q", out)
	}
}

// —— 第二层: 失败路径(临时空目录 → 仓库根定位失败,退出码 3)——

func TestDoctor_NoRepoRoot(t *testing.T) {
	// 临时空目录: 无 .git、无 .aoci —— resolveRepoRoot 应向上找不到而失败
	emptyDir := t.TempDir()

	out, err := runDoctor(t, emptyDir)

	// 应返回携带退出码 3 的错误
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
	// 输出应含仓库根定位失败提示
	if !strings.Contains(out, "仓库根") {
		t.Errorf("输出应提示仓库根定位失败: %q", out)
	}
}

// —— 第三层: 健康路径(本项目真实根 → 结构性断言)——

// findProjectRoot 从当前测试工作目录向上找到含 .git 的项目根(与 resolveRepoRoot 同逻辑)。
// 找不到则跳过健康路径测试(例如在无 .git 的构建环境中)。
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

	// 在真实项目根下运行 doctor
	out, err := runDoctor(t, root)

	// 断言结构性事实(不断言具体数值,避免随项目演进脆化):

	// 1. 四个分组标题都出现
	for _, group := range []string{"仓库与核心资产", "Agent 接入", "AI 增强层", "平台特性"} {
		if !strings.Contains(out, group) {
			t.Errorf("输出应含分组标题 %q", group)
		}
	}

	// 2. 关键检查项标签出现
	for _, label := range []string{"仓库根定位", "配置加载", "基线文件", "AI 启用状态", "数据主权"} {
		if !strings.Contains(out, label) {
			t.Errorf("输出应含检查项 %q", label)
		}
	}

	// 3. 仓库根定位应成功(本项目有 .git)
	if !strings.Contains(out, "[✓] 仓库根定位") {
		t.Error("真实项目根下,仓库根定位应为 [✓]")
	}

	// 4. 末尾应有诊断完成汇总
	if !strings.Contains(out, "诊断完成") {
		t.Error("输出末尾应有诊断完成汇总")
	}

	// 5. 退出码语义: 健康项目应通过(err 为 nil)或仅因个别可选项失败。
	//    若失败,打印输出便于诊断(不强制断言 nil —— 项目环境可能有真实待处理项)。
	if err != nil {
		if ec, ok := err.(exitCoder); ok {
			t.Logf("doctor 退出码 %d(项目存在待处理项,非测试失败):\n%s", ec.ExitCode(), out)
		} else {
			t.Errorf("doctor 返回了非退出码错误: %v", err)
		}
	}
}
