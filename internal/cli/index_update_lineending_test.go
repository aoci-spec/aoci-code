// index update换行宽容生产路径测试。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// runUpdateWithoutDryRun执行真实update入口，不提供AI配置。
// 若纯换行差异错误进入目标，它会在构造AI客户端时失败。
func runUpdateWithoutDryRun(
	t *testing.T,
	root string,
) (
	string,
	error,
) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root

	t.Cleanup(
		func() {
			flagRepo = oldRepo
		},
	)

	command := newIndexUpdateCmd()
	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{})

	err := command.Execute()

	return output.String(), err
}

// TestUpdateLineEndingToleranceReturnsBeforeAI验证默认宽容时零目标、零草稿、零外发。
func TestUpdateLineEndingToleranceReturnsBeforeAI(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"f.go",
		),
		[]byte("package f\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err :=
		runUpdateWithoutDryRun(
			t,
			root,
		)

	if err != nil {
		t.Fatalf(
			"纯换行差异应在AI客户端构造前收敛: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"无可执行 changed/new 目标",
	) {
		t.Fatalf(
			"默认宽容应报告无需起草: %s",
			output,
		)
	}

	draftsPath := filepath.Join(
		root,
		".aoci",
		"drafts",
	)

	if _, statErr := os.Stat(
		draftsPath,
	); !os.IsNotExist(statErr) {
		t.Fatalf(
			"纯换行差异不得创建草稿目录: %v",
			statErr,
		)
	}

	assertUpdateLedger(
		t,
		root,
		0,
	)
}

// TestUpdateLineEndingStrictModeDispatchesChanged验证团队严格模式仍派发更新。
func TestUpdateLineEndingStrictModeDispatchesChanged(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	cfg.LineEndingTolerance = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"f.go",
		),
		[]byte("package f\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runUpdateDry(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"严格模式dry-run应成功: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"changed(待更新):",
	) ||
		!strings.Contains(
			output,
			"f.go",
		) ||
		!strings.Contains(
			output,
			"changed 1 / new 0",
		) {
		t.Fatalf(
			"严格模式应把纯换行差异分类为changed: %s",
			output,
		)
	}

	assertUpdateLedger(
		t,
		root,
		1,
	)
}

// TestUpdateLineEndingToleranceDoesNotHideRealChange验证真实内容变化仍派发。
func TestUpdateLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"f.go",
		),
		[]byte(
			"package f\r\n// changed\r\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err := runUpdateDry(
		t,
		root,
	)
	if err != nil {
		t.Fatalf(
			"真实变化dry-run应成功: %v\n%s",
			err,
			output,
		)
	}

	if !strings.Contains(
		output,
		"changed(待更新):",
	) ||
		!strings.Contains(
			output,
			"f.go",
		) ||
		!strings.Contains(
			output,
			"changed 1 / new 0",
		) {
		t.Fatalf(
			"真实内容变化必须进入changed: %s",
			output,
		)
	}

	assertUpdateLedger(
		t,
		root,
		1,
	)
}
