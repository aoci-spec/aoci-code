// Check继承Score换行宽容口径的端到端测试。
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestCheckLineEndingToleranceAndStrictMode(
	t *testing.T,
) {
	root, cfg := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "x.go"),
		[]byte("package x\r\n\r\nvar Value = 1\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runCheck(t, root)
	if err != nil {
		t.Fatalf(
			"默认宽容时check应通过: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(output, "✓ 可提交") {
		t.Fatalf(
			"默认宽容时应报告可提交: %s",
			output,
		)
	}

	if !strings.Contains(
		output,
		"LineEndingOnly 1",
	) {
		t.Fatalf(
			"默认宽容时应明确展示一个LineEndingOnly信息态: %s",
			output,
		)
	}

	cfg.LineEndingTolerance = false

	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	strictOutput, strictErr :=
		runCheck(t, root)

	exitErr, ok := strictErr.(*ExitError)
	if !ok ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"团队严格模式应ExitDrift: %v\n%s",
			strictErr,
			strictOutput,
		)
	}

	if !strings.Contains(
		strictOutput,
		"ActionableMissing 0,Stale 1",
	) {
		t.Fatalf(
			"严格模式应报告一个Stale: %s",
			strictOutput,
		)
	}
}

func TestCheckLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root, _ := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(root, "x.go"),
		[]byte("package x\r\n\r\nvar Value = 2\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, runErr := runCheck(t, root)

	exitErr, ok := runErr.(*ExitError)
	if !ok ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"真实内容变化必须阻断check: %v\n%s",
			runErr,
			output,
		)
	}

	if !strings.Contains(
		output,
		"ActionableMissing 0,Stale 1",
	) {
		t.Fatalf(
			"真实变化应报告一个Stale: %s",
			output,
		)
	}

	if strings.Contains(
		output,
		"LineEndingOnly 1",
	) {
		t.Fatalf(
			"真实内容变化不得被报告为LineEndingOnly: %s",
			output,
		)
	}
}
