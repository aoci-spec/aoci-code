// Windows Host-Agent普通JSON原生捕获及自动修复说明合同测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentRuntimeInstructionsForWindowsDeclaresNativeJSONCapture锁定R60-F.12
// 的用户合同：PowerShell 5普通&捕获是默认路径，Stage请求仍走UTF-8文件。
func TestAgentRuntimeInstructionsForWindowsDeclaresNativeJSONCapture(
	t *testing.T,
) {
	runtimeInstructions, err := agentRuntimeInstructionsFor("windows")
	if err != nil {
		t.Fatal(err)
	}
	instructions := strings.Join(
		runtimeInstructions,
		"\n",
	)

	for _, anchor := range []string{
		"绝对路径",
		"不要改回裸aoci",
		"PowerShell",
		"ASCII安全JSON",
		"直接使用&捕获",
		"ConvertFrom-Json",
		"UTF-8 request-file",
		"旧版AOCI",
		"ProcessStartInfo",
	} {
		if !strings.Contains(
			instructions,
			anchor,
		) {
			t.Fatalf(
				"Windows运行时合同缺少%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	for _, obsolete := range []string{
		"机器输出应以ProcessStartInfo",
		"必须使用ProcessStartInfo",
		"不得用文本管道或普通>重定向传递中文JSON",
	} {
		if strings.Contains(
			instructions,
			obsolete,
		) {
			t.Fatalf(
				"Windows运行时合同仍含旧强制说明%q:\n%s",
				obsolete,
				instructions,
			)
		}
	}
}

// TestAgentRuntimeInstructionsForPOSIXDoesNotLeakWindowsAdvice确保非Windows
// Guide不携带PowerShell专属说明。
func TestAgentRuntimeInstructionsForPOSIXDoesNotLeakWindowsAdvice(
	t *testing.T,
) {
	runtimeInstructions, err := agentRuntimeInstructionsFor("linux")
	if err != nil {
		t.Fatal(err)
	}
	instructions := strings.Join(
		runtimeInstructions,
		"\n",
	)

	if !strings.Contains(
		instructions,
		"绝对路径",
	) {
		t.Fatalf(
			"POSIX运行时合同缺少通用绝对路径纪律:\n%s",
			instructions,
		)
	}

	for _, unexpected := range []string{
		"PowerShell",
		"ASCII安全JSON",
		"ConvertFrom-Json",
		"ProcessStartInfo",
	} {
		if strings.Contains(
			instructions,
			unexpected,
		) {
			t.Fatalf(
				"POSIX运行时合同泄漏Windows说明%q:\n%s",
				unexpected,
				instructions,
			)
		}
	}
}

// TestWindowsHostAgentDocumentDeclaresAutoRepair锁定Windows长文档中的
// applied、repair_required、stopped三态及自动修复边界。
func TestWindowsHostAgentDocumentDeclaresAutoRepair(
	t *testing.T,
) {
	path := filepath.Join(
		"..",
		"..",
		"docs",
		"windows-host-agent.md",
	)

	data, err := os.ReadFile(
		path,
	)
	if err != nil {
		t.Fatalf(
			"读取Windows Host-Agent文档失败: %v",
			err,
		)
	}

	text := string(data)

	// “Host Agent不得”是列表的禁止语境，具体禁止行为位于其后列表项。
	// 分别锁定语境与行为，避免测试依赖二者必须写在同一个物理行中。
	for _, anchor := range []string{
		"## 8. Entries Auto三态",
		"auto_finalize.status = applied",
		"auto_finalize.status = repair_required",
		"auto_finalize.status = stopped",
		"Entries Stage进程退出码为0",
		"正式资产零写入",
		"读取auto_finalize.findings",
		"只修正findings中的失败条目",
		"其他候选保持原样",
		"重写当前请求中的同一完整批次",
		"重新执行Guide返回的同一条Entries Stage命令",
		"让新的Stage调用创建新Run",
		"Host Agent不得",
		"要求用户回复“继续”",
		"调用Entries Check、Diff或Apply",
		"普通回复边界不终止完整生成",
		"Generation Plan或源码摘要变化",
		"正式资产写入状态不确定",
		"候选内容错误不得伪装成stopped",
		"对用户停点放松",
		"对正式资产质量不放松",
		"repair_required退出码为0",
	} {
		if !strings.Contains(
			text,
			anchor,
		) {
			t.Fatalf(
				"Windows Host-Agent文档缺少Auto修复合同%q:\n%s",
				anchor,
				text,
			)
		}
	}

	for _, obsolete := range []string{
		"repair_required时立即停止",
		"任何机器硬闸失败都必须立即停止",
		"repair_required后等待用户继续",
	} {
		if strings.Contains(
			text,
			obsolete,
		) {
			t.Fatalf(
				"Windows Host-Agent文档仍含旧停点合同%q:\n%s",
				obsolete,
				text,
			)
		}
	}
}
