// entries check 预检测试: 退出码三态 + 与 apply 判据同源 + 零副作用。
// 索引条目待补: index_entries_test.go
//
// 锁定行为:
//
//	一、全净批次: exit 0,索引/基线零改动(预检是纯读工序);
//	二、字典违规草稿: ExitInvalid(2),且索引零改动 —— "apply 将拒"的预告
//	    必须与 apply 实际行为一致(判据同源的行为级证明);
//	三、配额警告草稿: 只展示不计退出码(warning 放行,与 apply 口径一致)。
//
// 夹具: 自造最小仓(索引含字典头部+一段) + 手工落 entries 草稿批次
// (不走 build —— 隔离 AI 层,草稿内容完全受控)。
// flagRepo 全局覆盖定根,不并行;命令直取 newEntriesCheckCmd().RunE。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

// buildEntriesRepo 造最小真实仓库: 含字典的头部 + 根段一条目 + 指定草稿批次。
// drafts 键=目标相对路径,值=草稿条目行。返回 root 与 runID。
func buildEntriesRepo(t *testing.T, drafts map[string]string) (root, runID string) {
	t.Helper()
	root = t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	idx := "#A层级: X-脚本 C-命令\n" +
		"#B模块: CR核心 UT工具\n" +
		"#E规模: L大 M中 S小 T微\n" +
		"===段" + rootSlash + "/===\n" +
		"f.go[XCR5T]: F:x | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0644); err != nil {
		t.Fatal(err)
	}
	runID = "20260711T090000Z"
	var statuses []draft.EntryStatus
	for rel, line := range drafts {
		absolutePath := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolutePath, []byte("package demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		fingerprint, err := baseline.HashFile(absolutePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := draft.WriteFile(root, runID, entryDraftFileName(rel), []byte(line+"\n")); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, draft.EntryStatus{
			Path: rel, Status: "drafted", SourceSHA256: fingerprint.SHA256,
		})
	}
	if err := draft.SaveManifest(root, &draft.Manifest{
		RunID: runID, Kind: draft.KindEntries, Entries: statuses,
	}); err != nil {
		t.Fatal(err)
	}
	return root, runID
}

// runEntriesCheck 以 flagRepo 覆盖定根执行 entries check,返回输出与错误。
func runEntriesCheck(t *testing.T, root, runID string) (string, error) {
	t.Helper()
	old := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = old })

	cmd := newEntriesCheckCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.RunE(cmd, []string{runID})
	return out.String(), err
}

// readEntriesIndex 读回索引全文(零副作用断言用)。
func readEntriesIndex(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestEntriesCheckCleanBatch 全净批次: exit 0,零副作用。
func TestEntriesCheckCleanBatch(t *testing.T) {
	root, runID := buildEntriesRepo(t, map[string]string{
		"g.go": "g.go[XUT5T]: F:合规新条目 | R:- | A:- | S:-",
	})
	before := readEntriesIndex(t, root)

	out, err := runEntriesCheck(t, root, runID)
	if err != nil {
		t.Fatalf("全净批次应 exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ 预检通过") {
		t.Fatalf("应报预检通过: %s", out)
	}
	if readEntriesIndex(t, root) != before {
		t.Fatal("预检不得改动索引")
	}
	if _, serr := os.Stat(filepath.Join(root, ".aoci", "baseline.json")); serr == nil {
		t.Fatal("预检不得建立/触碰基线")
	}
}

// TestEntriesCheckDictReject 字典违规: ExitInvalid,索引零改动,
// 输出含 [dict] 分类(与 apply 拒绝文案同类别)。
func TestEntriesCheckDictReject(t *testing.T) {
	root, runID := buildEntriesRepo(t, map[string]string{
		"g.go": "g.go[ZQQ5T]: F:臆造标签 | R:- | A:- | S:-", // Z/QQ 均不在字典
	})
	before := readEntriesIndex(t, root)

	out, err := runEntriesCheck(t, root, runID)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitInvalid {
		t.Fatalf("字典违规应 ExitInvalid(2): err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "[dict]") {
		t.Fatalf("应含 [dict] 分类拒绝: %s", out)
	}
	if readEntriesIndex(t, root) != before {
		t.Fatal("预检不得改动索引")
	}
}

// TestEntriesCheckFormatReject 格式硬拒(缺 FRAS 段): ExitInvalid,含 [format] 分类。
func TestEntriesCheckFormatReject(t *testing.T) {
	root, runID := buildEntriesRepo(t, map[string]string{
		"g.go": "g.go[XUT5T]: F:只有F段没有其他", // 缺 R/A/S
	})
	out, err := runEntriesCheck(t, root, runID)
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitInvalid {
		t.Fatalf("格式硬拒应 ExitInvalid(2): err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "[format]") {
		t.Fatalf("应含 [format] 分类拒绝: %s", out)
	}
}

// TestEntriesCheckQuotaWarnPasses 配额警告(C3 条目 S 超 50 字): 展示警告但 exit 0。
func TestEntriesCheckQuotaWarnPasses(t *testing.T) {
	longS := strings.Repeat("超长约束陈述", 20) // 120 字 > C3 配额 50
	root, runID := buildEntriesRepo(t, map[string]string{
		"g.go": "g.go[XUT3T]: F:低重要度 | R:- | A:- | S:" + longS,
	})
	out, err := runEntriesCheck(t, root, runID)
	if err != nil {
		t.Fatalf("仅 Warning 级违规应放行 exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "⚠") {
		t.Fatalf("应展示配额警告: %s", out)
	}
	if !strings.Contains(out, "✓ 预检通过") {
		t.Fatalf("警告放行后应报预检通过: %s", out)
	}
}
