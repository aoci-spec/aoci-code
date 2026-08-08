// P-23 entries 三阶段审计与内容版本防线集成测试。
//
// 覆盖:
//   - check 追加机器校验摘要且不覆盖 AI 初始 EntryStatus;
//   - 人工修改草稿后再次 check 产生不同 draft_hash;
//   - diff 追加独立审阅记录;
//   - apply 追加 application 并与最近 review 使用同一摘要;
//   - review 后改稿未重审时 apply 在正式写入前硬拒;
//   - 改稿后重新 check 即可应用;
//   - 旧 manifest 无 reviews 时警告兼容放行。
package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func runEntriesDiffForAudit(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() {
		flagRepo = oldRepo
	})

	cmd := newEntriesDiffCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.RunE(cmd, []string{runID})
	return out.String(), err
}

func runEntriesApplyForAudit(
	t *testing.T,
	root,
	runID string,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() {
		flagRepo = oldRepo
	})

	cmd := newEntriesApplyCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.RunE(cmd, []string{runID})
	return out.String(), err
}

func writeAuditSourceFile(
	t *testing.T,
	root,
	rel string,
) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(root, filepath.FromSlash(rel)),
		[]byte("package demo\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestEntriesCheckAppendsReviewWithoutOverwritingGeneration(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:第一版 | R:- | A:- | S:-",
		},
	)

	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].Status = "warned"
	manifest.Entries[0].Note = "AI 初始警告必须永久保留"
	if err := draft.SaveManifest(root, manifest); err != nil {
		t.Fatal(err)
	}

	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("第一次 check 应成功: %v", err)
	}

	first, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Reviews) != 1 {
		t.Fatalf("应追加一次 review: %+v", first.Reviews)
	}
	if first.Reviews[0].Action != draft.ReviewActionCheck ||
		first.Reviews[0].DraftHash == "" ||
		first.Reviews[0].Passed != 1 ||
		first.Reviews[0].Rejected != 0 {
		t.Fatalf(
			"第一次 review 摘要不符: %+v",
			first.Reviews[0],
		)
	}
	if first.Entries[0].Status != "warned" ||
		first.Entries[0].Note != "AI 初始警告必须永久保留" {
		t.Fatalf(
			"check 不得覆盖 generation state: %+v",
			first.Entries,
		)
	}

	if err := draft.WriteFile(
		root,
		runID,
		entryDraftFileName("g.go"),
		[]byte(
			"g.go[XUT5T]: F:人工修订版 | R:- | A:- | S:-\n",
		),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("第二次 check 应成功: %v", err)
	}

	second, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Reviews) != 2 {
		t.Fatalf(
			"人工修订后应追加第二次 review: %+v",
			second.Reviews,
		)
	}
	if second.Reviews[0].DraftHash ==
		second.Reviews[1].DraftHash {
		t.Fatalf(
			"人工修订后 draft_hash 必须变化: %+v",
			second.Reviews,
		)
	}
}

func TestEntriesDiffAppendsReviewRecord(t *testing.T) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:待审阅 | R:- | A:- | S:-",
		},
	)

	if _, err := runEntriesDiffForAudit(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf("entries diff 应成功: %v", err)
	}

	manifest, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Reviews) != 1 {
		t.Fatalf(
			"diff 应追加 review: %+v",
			manifest.Reviews,
		)
	}
	review := manifest.Reviews[0]
	if review.Action != draft.ReviewActionDiff ||
		review.DraftHash == "" ||
		review.PathsCount != 1 ||
		review.Passed != 1 {
		t.Fatalf("diff review 不符: %+v", review)
	}
}

func TestEntriesApplyAppendsApplicationRecord(t *testing.T) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:可应用 | R:- | A:- | S:-",
		},
	)
	writeAuditSourceFile(t, root, "g.go")

	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("apply 前 check 应成功: %v", err)
	}

	before, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Reviews) != 1 {
		t.Fatalf("apply 前应已有 review: %+v", before)
	}
	reviewHash := before.Reviews[0].DraftHash

	out, err := runEntriesApplyForAudit(t, root, runID)
	if err != nil {
		t.Fatalf("entries apply 应成功: %v\n%s", err, out)
	}
	if !strings.Contains(out, "内容审阅核对: ✓") {
		t.Fatalf("应显示内容摘要核对通过: %s", out)
	}

	after, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Applications) != 1 {
		t.Fatalf(
			"apply 应追加 application: %+v",
			after.Applications,
		)
	}

	application := after.Applications[0]
	if application.Applied != 1 ||
		application.Rejected != 0 ||
		application.DraftHash == "" {
		t.Fatalf(
			"application 摘要不符: %+v",
			application,
		)
	}
	if application.DraftHash != reviewHash {
		t.Fatalf(
			"未修改草稿时 review/apply 摘要应一致: review=%s apply=%s",
			reviewHash,
			application.DraftHash,
		)
	}
	if after.AppliedAt == "" ||
		after.AppliedAt != application.At {
		t.Fatalf(
			"干净应用应同步 applied_at: %+v",
			after,
		)
	}
}

func TestEntriesApplyRejectsDraftChangedAfterReview(t *testing.T) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:已审阅版本 | R:- | A:- | S:-",
		},
	)
	writeAuditSourceFile(t, root, "g.go")

	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("审阅 check 应成功: %v", err)
	}
	indexBefore := readEntriesIndex(t, root)

	if err := draft.WriteFile(
		root,
		runID,
		entryDraftFileName("g.go"),
		[]byte(
			"g.go[XUT5T]: F:审阅后偷偷修改 | R:- | A:- | S:-\n",
		),
	); err != nil {
		t.Fatal(err)
	}

	_, err := runEntriesApplyForAudit(t, root, runID)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"审阅后改稿应 ExitInvalid(2),得到: %v",
			err,
		)
	}
	if !strings.Contains(err.Error(), "P-23 防线") ||
		!strings.Contains(err.Error(), "摘要") {
		t.Fatalf("拒绝错误应点明内容摘要漂移: %v", err)
	}
	if readEntriesIndex(t, root) != indexBefore {
		t.Fatal("P-23 拒绝前不得修改正式索引")
	}

	manifest, loadErr := draft.LoadManifest(root, runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifest.Applications) != 0 {
		t.Fatalf(
			"内容核对失败不得追加 application: %+v",
			manifest.Applications,
		)
	}
	if manifest.AppliedAt != "" {
		t.Fatalf(
			"内容核对失败不得标记 applied_at: %s",
			manifest.AppliedAt,
		)
	}
}

func TestEntriesApplyPassesAfterRecheckOfChangedDraft(t *testing.T) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:第一版 | R:- | A:- | S:-",
		},
	)
	writeAuditSourceFile(t, root, "g.go")

	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("第一次 check 应成功: %v", err)
	}
	if err := draft.WriteFile(
		root,
		runID,
		entryDraftFileName("g.go"),
		[]byte(
			"g.go[XUT5T]: F:重新审阅后的版本 | R:- | A:- | S:-\n",
		),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := runEntriesCheck(t, root, runID); err != nil {
		t.Fatalf("改稿后重新 check 应成功: %v", err)
	}

	before, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	lastReview := before.Reviews[len(before.Reviews)-1]

	if _, err := runEntriesApplyForAudit(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf("重新审阅后 apply 应成功: %v", err)
	}

	after, err := draft.LoadManifest(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Applications) != 1 {
		t.Fatalf(
			"应追加一次 application: %+v",
			after.Applications,
		)
	}
	if after.Applications[0].DraftHash !=
		lastReview.DraftHash {
		t.Fatalf(
			"application 必须对应最后一次 review: review=%s apply=%s",
			lastReview.DraftHash,
			after.Applications[0].DraftHash,
		)
	}

	indexText := readEntriesIndex(t, root)
	if !strings.Contains(indexText, "重新审阅后的版本") {
		t.Fatalf("正式索引应应用重新审阅后的内容: %s", indexText)
	}
}

func TestEntriesApplyLegacyManifestWithoutReviewWarnsAndPasses(
	t *testing.T,
) {
	root, runID := buildEntriesRepo(
		t,
		map[string]string{
			"g.go": "g.go[XUT5T]: F:旧批次兼容 | R:- | A:- | S:-",
		},
	)
	writeAuditSourceFile(t, root, "g.go")

	out, err := runEntriesApplyForAudit(t, root, runID)
	if err != nil {
		t.Fatalf("旧 manifest 无 review 应兼容放行: %v\n%s", err, out)
	}
	if !strings.Contains(
		out,
		"无 P-23 内容审阅记录",
	) {
		t.Fatalf("兼容放行必须明确警告: %s", out)
	}

	manifest, loadErr := draft.LoadManifest(root, runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(manifest.Applications) != 1 ||
		manifest.Applications[0].Applied != 1 {
		t.Fatalf(
			"旧批次兼容应用应进入 application 审计: %+v",
			manifest,
		)
	}
}
