// aoci_maintain批次上限与report泄压阀测试。
// 本文件复用tools_maintain_test.go的真实仓库和Baseline辅助，不复制生产判据。
package mcptools

import (
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

// TestMaintainBatchLimit验证普通小批次一次返回，不逐文件往返。
func TestMaintainBatchLimit(
	t *testing.T,
) {
	root := t.TempDir()

	for number := 1; number <= 12; number++ {
		relativePath := "m" +
			string(rune('0'+number/10)) +
			string(rune('0'+number%10)) +
			".go"

		maintainWriteFile(
			t,
			root,
			relativePath,
			"package main\n",
		)
	}

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	maintainSaveBaseline(
		t,
		root,
		map[string]baseline.Fingerprint{
			"aoci.txt": maintainFingerprint(
				t,
				root,
				"aoci.txt",
			),
		},
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if len(result.Candidates) != 12 || result.Metrics.SemanticFiles != 12 {
		t.Fatalf("小批次应一次返回12个语义候选: %+v", result)
	}
}

// TestMaintainReportedNotDispatched验证已登记路径不再派发，其他路径照常派发。
func TestMaintainReportedNotDispatched(
	t *testing.T,
) {
	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"reported.go",
		"package main\n",
	)
	maintainWriteFile(
		t,
		root,
		"fresh.go",
		"package main\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	maintainSaveBaseline(
		t,
		root,
		map[string]baseline.Fingerprint{
			"aoci.txt": maintainFingerprint(
				t,
				root,
				"aoci.txt",
			),
		},
	)

	reportResult := handleReport(
		root,
		reportIn{
			Path: "reported.go",
			Note: "测试登记: 不宜索引",
		},
	)

	if reportResult.IsError {
		t.Fatalf(
			"report登记失败: %s",
			maintainResultText(
				t,
				reportResult,
			),
		)
	}

	result := decodeAutoResult(t, handleMaintain(root))
	paths := candidatePaths(result)
	if paths["reported.go"] || !paths["fresh.go"] ||
		!hasFinding(result, "reported_pending") || result.Status != autoStatusStopped {
		t.Fatalf("登记路径应只作为显式待办停点: %+v", result)
	}
}

// TestMaintainAllReportedTerminates验证全部漂移登记后维护循环可以干净终止。
func TestMaintainAllReportedTerminates(
	t *testing.T,
) {
	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"reported.go",
		"package main\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"gone.go[CRT3T]: F:孤儿靶 | R:- | A:- | S:-\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	maintainSaveBaseline(
		t,
		root,
		map[string]baseline.Fingerprint{
			"aoci.txt": maintainFingerprint(
				t,
				root,
				"aoci.txt",
			),
		},
	)

	for _, relativePath := range []string{
		"reported.go",
		"gone.go",
	} {
		result := handleReport(
			root,
			reportIn{
				Path: relativePath,
				Note: "测试登记",
			},
		)

		if result.IsError {
			t.Fatalf(
				"report登记失败: %s",
				maintainResultText(
					t,
					result,
				),
			)
		}
	}

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusStopped || len(result.Candidates) != 0 ||
		!hasFinding(result, "reported_pending") {
		t.Fatalf("全部登记仍是未解决待办，不得谎报对齐: %+v", result)
	}
}
