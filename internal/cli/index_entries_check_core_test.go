// R57 entries check 共用执行内核测试。
//
// 核心判据:
//   - 不经 Cobra 也能返回参与审阅的 Manifest、Snapshot 和 ReviewRecord；
//   - source 可由未来自动编排传 cli_ai；
//   - 草稿硬拒作为结构化结果返回，不由 core 擅自映射命令退出码；
//   - 快照读取失败发生在 ReviewRecord 形成之前。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

func loadEntriesCheckCoreDoc(
	t *testing.T,
	root string,
) *index.Document {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(root, "aoci.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}

	doc, _ := index.Parse(string(data))
	if doc == nil || len(doc.Sections) == 0 {
		t.Fatal("测试索引未解析出目录段")
	}
	index.ResolveRelPaths(doc, root)
	return doc
}

func TestEntriesCheckCoreReturnsReviewedSnapshot(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:合规条目 | R:- | A:- | S:-",
		},
	)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := loadEntriesCheckCoreDoc(t, root)
	indexBefore := readEntriesIndex(t, root)

	var out bytes.Buffer
	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		doc,
		&out,
		ledger.SourceCLIAI,
	)
	if err != nil {
		t.Fatalf("共用内核应成功: %v\n%s", err, out.String())
	}
	if result == nil ||
		result.Manifest == nil ||
		result.Snapshot == nil {
		t.Fatalf("结构化结果不完整: %+v", result)
	}
	if result.RunID != runID {
		t.Fatalf("run_id 不符: %+v", result)
	}
	if result.Snapshot.Hash == "" ||
		result.Review.DraftHash != result.Snapshot.Hash {
		t.Fatalf(
			"Review 与 Snapshot 摘要必须同源: %+v",
			result,
		)
	}
	if result.Review.Passed != 1 ||
		result.Review.Warned != 0 ||
		result.Review.Rejected != 0 ||
		result.Review.Skipped != 0 {
		t.Fatalf("校验摘要不符: %+v", result.Review)
	}
	if !strings.Contains(
		out.String(),
		"审计记录: check draft_hash=",
	) {
		t.Fatalf("输出缺审计摘要: %s", out.String())
	}

	loaded, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Reviews) != 1 ||
		loaded.Reviews[0].DraftHash != result.Snapshot.Hash {
		t.Fatalf(
			"manifest 未追加同一摘要的 review: %+v",
			loaded.Reviews,
		)
	}
	if len(loaded.Entries) != 1 ||
		loaded.Entries[0].Status != "drafted" {
		t.Fatalf(
			"core 不得覆盖 generation state: %+v",
			loaded.Entries,
		)
	}

	events, _ := ledger.Recent(root, 20)
	found := false
	for _, event := range events {
		if event.Op == "entries_check" &&
			event.Source == ledger.SourceCLIAI &&
			event.DraftRunID == runID {
			found = true
		}
	}
	if !found {
		t.Fatalf(
			"自动编排来源未进入 ledger: %+v",
			events,
		)
	}

	if readEntriesIndex(t, root) != indexBefore {
		t.Fatal("共用 check 内核不得修改正式索引")
	}
	if _, statErr := os.Stat(
		filepath.Join(root, ".aoci", "baseline.json"),
	); statErr == nil {
		t.Fatal("共用 check 内核不得建立基线")
	}
}

func TestEntriesCheckCoreReturnsRejectedResult(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[ZQQ5T]: F:臆造标签 | R:- | A:- | S:-",
		},
	)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(t, root),
		&out,
		ledger.SourceCLIAI,
	)
	if err != nil {
		t.Fatalf(
			"正常校验拒绝应由结果表达而非 core error: %v",
			err,
		)
	}
	if result == nil ||
		result.Review.Rejected != 1 ||
		result.Review.Passed != 0 {
		t.Fatalf("拒绝结果不符: %+v", result)
	}
	if !strings.Contains(out.String(), "[dict]") {
		t.Fatalf("拒绝输出缺 dict 分类: %s", out.String())
	}

	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Rejected != 1 {
		t.Fatalf(
			"被拒批次仍须追加机器审阅记录: %+v",
			manifest.Reviews,
		)
	}
}

func TestEntriesCheckCoreSnapshotFailurePrecedesReview(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:将被删除的草稿 | R:- | A:- | S:-",
		},
	)

	draftPath := filepath.Join(
		root,
		".aoci",
		"drafts",
		runID,
		entryDraftFileName("g.go"),
	)
	if err := os.Remove(draftPath); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runEntriesCheckCore(
		root,
		runID,
		cfg,
		loadEntriesCheckCoreDoc(t, root),
		&bytes.Buffer{},
		ledger.SourceCLIAI,
	)
	if result != nil {
		t.Fatalf("快照失败不得返回可用结果: %+v", result)
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"快照失败应映射 ExitInvalid: %v",
			err,
		)
	}

	manifest, loadErr := draft.LoadManifest(root, runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifest.Reviews) != 0 {
		t.Fatalf(
			"快照读取失败前不得形成审阅授权: %+v",
			manifest.Reviews,
		)
	}
}
