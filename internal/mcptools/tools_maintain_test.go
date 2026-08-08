// aoci_maintain核心状态调度工具直测。
// 索引条目: tools_maintain_test.go[TRD7TM]
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func TestMaintainSharesEntriesRecoveryTerminalSemantics(t *testing.T) {
	root := buildFormatOnlyRepo(t)
	runID, err := draft.NewRun(root)
	if err != nil {
		t.Fatal(err)
	}
	transactionID := strings.Repeat("c", 64)
	preIndex := strings.Repeat("a", 64)
	postIndex := strings.Repeat("b", 64)
	manifest := &draft.Manifest{
		RunID: runID, Kind: draft.KindEntries,
		Applications: []draft.ApplicationRecord{{
			DraftHash: strings.Repeat("f", 64), PathsCount: 1, Applied: 1,
			RejectKinds: "baseline_incomplete",
		}},
	}
	if err := draft.SaveManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	pending := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	if pending.Status != autoStatusStopped || pending.Aligned ||
		!strings.Contains(joinedFindingText(pending), runID) {
		t.Fatalf("未终结Entries run必须阻断Maintain: %+v", pending)
	}
	archiveRel := filepath.ToSlash(filepath.Join(
		".aoci", "transactions", "history", "entries-"+transactionID+".json",
	))
	archiveData, _ := json.MarshalIndent(map[string]any{
		"version": 1, "batch_key": transactionID,
		"pre_index_sha256": preIndex, "post_index_sha256": postIndex,
	}, "", "  ")
	archiveData = append(archiveData, '\n')
	maintainWriteFile(t, root, archiveRel, string(archiveData))
	archiveDigest := sha256.Sum256(archiveData)
	if err := draft.AppendRunResolution(root, runID, draft.RunResolutionRecord{
		Status: draft.RunResolutionRecovered, FailureKinds: "baseline_incomplete",
		TransactionID: transactionID, PreIndexSHA256: preIndex,
		PostIndexSHA256: postIndex, CurrentIndexSHA256: postIndex,
		CurrentBaselineSHA256:  strings.Repeat("d", 64),
		RepositorySHA256:       strings.Repeat("e", 64),
		ArchivedRecoveryAsset:  archiveRel,
		ArchivedRecoverySHA256: hex.EncodeToString(archiveDigest[:]),
	}); err != nil {
		t.Fatal(err)
	}
	aligned := decodeAutoResult(t, handleMaintainWithVersion(root, "test-version"))
	if aligned.Status != autoStatusApplied || !aligned.Aligned {
		t.Fatalf("持久恢复终态后Maintain应恢复aligned: %+v", aligned)
	}
}

// maintainWriteFile写入测试文件并自动创建父目录。
func maintainWriteFile(
	t *testing.T,
	root,
	relativePath,
	content string,
) {
	t.Helper()

	path := filepath.Join(
		root,
		filepath.FromSlash(relativePath),
	)

	if err := os.MkdirAll(
		filepath.Dir(path),
		0755,
	); err != nil {
		t.Fatalf(
			"创建目录失败: %v",
			err,
		)
	}

	if err := os.WriteFile(
		path,
		[]byte(content),
		0644,
	); err != nil {
		t.Fatalf(
			"写文件失败: %v",
			err,
		)
	}
}

// maintainHeader构造带或不带标签字典的最小索引头部。
func maintainHeader(
	withDictionary bool,
) string {
	header := "#====测试索引====\n"

	if withDictionary {
		header += "#A层级: C-CLI T测试\n" +
			"#B模块: RT根\n" +
			"#C重要度: 9核心 7业务 3辅助\n" +
			"#E规模: L大>400 M中200-400 S小100-200 T微<100\n"
	}

	return header
}

// maintainResultText提取MCP纯文本结果。
func maintainResultText(
	t *testing.T,
	result *mcp.CallToolResult,
) string {
	t.Helper()

	if result == nil ||
		len(result.Content) == 0 {
		t.Fatal("结果为空")
	}

	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("结果内容非文本类型")
	}

	return content.Text
}

// maintainFingerprint计算仓库相对文件的真实指纹。
func maintainFingerprint(
	t *testing.T,
	root,
	relativePath string,
) baseline.Fingerprint {
	t.Helper()

	fingerprint, err := baseline.HashFile(
		filepath.Join(
			root,
			filepath.FromSlash(relativePath),
		),
	)
	if err != nil {
		t.Fatalf(
			"计算%s指纹失败: %v",
			relativePath,
			err,
		)
	}

	return fingerprint
}

// maintainSaveBaseline按给定指纹建立真实Baseline。
func maintainSaveBaseline(
	t *testing.T,
	root string,
	fingerprints map[string]baseline.Fingerprint,
) {
	t.Helper()

	if err := baseline.Save(
		root,
		baseline.NewBaseline(fingerprints),
	); err != nil {
		t.Fatalf(
			"保存Baseline失败: %v",
			err,
		)
	}
}

// buildMaintainMixedRepo构造Stale、Missing与Orphan并存的仓库。
func buildMaintainMixedRepo(
	t *testing.T,
) string {
	t.Helper()

	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"stale.go",
		"package main // 已变更内容\n",
	)
	maintainWriteFile(
		t,
		root,
		"missing.go",
		"package main\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"stale.go[CRT7T]: F:过期靶文件 | R:- | A:- | S:旧约束甲\n" +
		"orphan.go[CRT3T]: F:孤儿靶 | R:- | A:- | S:-\n"

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
			"stale.go": {
				SHA256: "0000错误指纹",
			},
		},
	)

	return root
}

// TestMaintainMixedStates锁定紧凑候选、安全停点与纯读边界。
func TestMaintainMixedStates(
	t *testing.T,
) {
	root := buildMaintainMixedRepo(t)

	indexPath := filepath.Join(
		root,
		"aoci.txt",
	)
	baselinePath := filepath.Join(
		root,
		".aoci",
		"baseline.json",
	)

	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	baselineBefore, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	result := handleMaintain(root)
	if result.IsError {
		t.Fatalf(
			"不应报错: %s",
			maintainResultText(t, result),
		)
	}

	auto := decodeAutoResult(t, result)
	if auto.Status != autoStatusStopped || auto.Aligned {
		t.Fatalf("孤儿是真实裁决停点: %+v", auto)
	}
	if len(auto.Candidates) != 2 ||
		auto.Candidates[0].Path != "stale.go" ||
		auto.Candidates[0].Kind != "更新" ||
		auto.Candidates[0].SourceSHA256 == "" ||
		!strings.Contains(auto.Candidates[0].ExistingEntry, "旧约束甲") ||
		auto.Candidates[1].Path != "missing.go" ||
		auto.Candidates[1].Kind != "新增" ||
		auto.Candidates[1].SourceSHA256 == "" {
		t.Fatalf("候选顺序或上下文不符: %+v", auto.Candidates)
	}
	if !hasFinding(auto, "orphan: orphan.go") {
		t.Fatalf("孤儿停点缺失: %+v", auto.Findings)
	}

	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	baselineAfter, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}

	if string(indexBefore) != string(indexAfter) {
		t.Error("maintain不得修改正式索引")
	}

	if string(baselineBefore) != string(baselineAfter) {
		t.Error("maintain不得修改Baseline")
	}

	ledgerData, err := os.ReadFile(
		filepath.Join(
			root,
			".aoci",
			"ledger.jsonl",
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(ledgerData),
		"maintain",
	) {
		t.Error("Ledger必须记录maintain事件")
	}
}

// TestMaintainAligned验证完全对齐仓库返回无维护任务。
func TestMaintainAligned(
	t *testing.T,
) {
	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"keep.go",
		"package main\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"keep.go[CRT7T]: F:对齐靶 | R:- | A:- | S:-\n"

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
			"keep.go": maintainFingerprint(
				t,
				root,
				"keep.go",
			),
		},
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusApplied || !result.Aligned ||
		len(result.Candidates) != 0 || len(result.Findings) != 0 {
		t.Fatalf("全对齐仓应返紧凑applied事实: %+v", result)
	}
}

// TestMaintainNoDict验证标签字典缺失时暂停派发。
func TestMaintainNoDict(
	t *testing.T,
) {
	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"missing.go",
		"package main\n",
	)

	indexText := maintainHeader(false) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n"

	maintainWriteFile(
		t,
		root,
		"aoci.txt",
		indexText,
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusStopped || len(result.Candidates) != 0 ||
		len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Cause, "头部字典未建立") {
		t.Fatalf("字典缺失应返回紧凑真停点: %+v", result)
	}
}

// TestMaintainIndexSelfStaleExcluded验证索引自身漂移只提示、不派发。
func TestMaintainIndexSelfStaleExcluded(
	t *testing.T,
) {
	root := t.TempDir()

	maintainWriteFile(
		t,
		root,
		"keep.go",
		"package main\n",
	)

	indexText := maintainHeader(true) +
		"\n===代码索引" + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"keep.go[CRT7T]: F:对齐靶 | R:- | A:- | S:-\n"

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
			"aoci.txt": {
				SHA256: "0000错误指纹",
			},
			"keep.go": maintainFingerprint(
				t,
				root,
				"keep.go",
			),
		},
	)

	result := decodeAutoResult(t, handleMaintain(root))
	if result.Status != autoStatusStopped || len(result.Candidates) != 0 ||
		!hasFinding(result, "index_self_stale") {
		t.Fatalf("索引自身漂移应成为非派发停点: %+v", result)
	}
}
