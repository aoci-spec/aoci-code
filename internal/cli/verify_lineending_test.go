// Verify换行宽容生产路径测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func buildVerifyLineEndingRepo(
	t *testing.T,
) (
	string,
	*config.Config,
) {
	t.Helper()

	root := t.TempDir()
	rootSlash := strings.TrimRight(
		filepath.ToSlash(root),
		"/",
	)

	indexText := "#测试索引\n" +
		"#A层级: X测试\n" +
		"#B模块: RT根\n" +
		"#C重要度: 9核心\n" +
		"#E规模: T微<100\n" +
		"===配置索引" +
		rootSlash +
		"/===\n" +
		"aoci.txt[XRT9T]: F:索引 | R:- | A:- | S:-\n" +
		"x.go[XRT9T]: F:测试文件 | R:- | A:- | S:-\n"

	if err := os.WriteFile(
		filepath.Join(
			root,
			"aoci.txt",
		),
		[]byte(indexText),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte("package x\n\nvar Value = 1\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg := legacyTestConfig()
	cfg.LedgerEnabled = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	snapshot, warnings, err := baseline.Snapshot(
		root,
		cfg.WalkOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) != 0 {
		t.Fatalf(
			"测试快照不应有警告: %v",
			warnings,
		)
	}

	if err := baseline.Save(
		root,
		baseline.NewBaseline(snapshot),
	); err != nil {
		t.Fatal(err)
	}

	return root, cfg
}

func runVerifyLineEndingJSON(
	t *testing.T,
	root string,
) (
	verifyReport,
	error,
) {
	t.Helper()

	withVerifyFlags(
		t,
		root,
		true,
	)

	command := newVerifyCmd()
	var output bytes.Buffer

	command.SetOut(&output)
	command.SetErr(&output)

	runErr := command.RunE(
		command,
		nil,
	)

	var report verifyReport

	if err := json.Unmarshal(
		output.Bytes(),
		&report,
	); err != nil {
		t.Fatalf(
			"verify JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	return report, runErr
}

// TestVerifyLineEndingToleranceDefaultAndStrict验证默认宽容与团队显式严格。
func TestVerifyLineEndingToleranceDefaultAndStrict(
	t *testing.T,
) {
	root, cfg := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte("package x\r\n\r\nvar Value = 1\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	report, runErr :=
		runVerifyLineEndingJSON(
			t,
			root,
		)

	if runErr != nil {
		t.Fatalf(
			"默认宽容时纯换行差异应exit0: %v",
			runErr,
		)
	}

	if len(report.Result.Stale) != 0 ||
		len(report.Result.LineEndingOnly) != 1 ||
		report.Result.LineEndingOnly[0] != "x.go" {
		t.Fatalf(
			"默认宽容结果不符: %+v",
			report.Result,
		)
	}

	if verifyRawDriftCount(&report) != 0 ||
		verifyUnresolvedDriftCount(&report) != 0 {
		t.Fatalf(
			"LineEndingOnly不得计入四态或治理债务: raw=%d unresolved=%d",
			verifyRawDriftCount(&report),
			verifyUnresolvedDriftCount(&report),
		)
	}

	cfg.LineEndingTolerance = false

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}

	strictReport, strictErr :=
		runVerifyLineEndingJSON(
			t,
			root,
		)

	var exitErr *ExitError

	if !errors.As(
		strictErr,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"团队显式严格时应ExitDrift: %v",
			strictErr,
		)
	}

	if len(strictReport.Result.Stale) != 1 ||
		strictReport.Result.Stale[0] != "x.go" ||
		len(strictReport.Result.LineEndingOnly) != 0 {
		t.Fatalf(
			"严格模式结果不符: %+v",
			strictReport.Result,
		)
	}
}

// TestVerifyLineEndingToleranceDoesNotHideRealChange验证真实内容变化仍阻断。
func TestVerifyLineEndingToleranceDoesNotHideRealChange(
	t *testing.T,
) {
	root, _ := buildVerifyLineEndingRepo(t)

	if err := os.WriteFile(
		filepath.Join(
			root,
			"x.go",
		),
		[]byte("package x\r\n\r\nvar Value = 2\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	report, runErr :=
		runVerifyLineEndingJSON(
			t,
			root,
		)

	var exitErr *ExitError

	if !errors.As(
		runErr,
		&exitErr,
	) ||
		exitErr.Code != ExitDrift {
		t.Fatalf(
			"真实内容变化必须ExitDrift: %v",
			runErr,
		)
	}

	if len(report.Result.Stale) != 1 ||
		report.Result.Stale[0] != "x.go" ||
		len(report.Result.LineEndingOnly) != 0 {
		t.Fatalf(
			"真实变化不得进入LineEndingOnly: %+v",
			report.Result,
		)
	}
}
