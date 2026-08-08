// Header P-23内容摘要防线测试。
//
// 本文件只验证Run内草稿内容审阅一致性。
// 测试Manifest使用Endpoint生成源，避免把Generation Plan防线混入P-23单元测试；
// Host-Agent Generation Plan漂移由index_agent_apply_guard_test.go独立覆盖。
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

// buildHeaderP23Repo创建带现代Header Manifest的最小仓库。
func buildHeaderP23Repo(
	t *testing.T,
	headerText string,
) (string, string) {
	t.Helper()

	root, runID := buildApplyRepo(
		t,
		headerText,
	)

	manifest := &draft.Manifest{
		RunID:            runID,
		Kind:             draft.KindHeader,
		GenerationSource: draft.GenerationSourceEndpoint,
		AgentName:        "endpoint-test",
		PlanID:           strings.Repeat("a", 64),
		GenerationHash:   strings.Repeat("b", 64),
		Files:            []string{draft.HeaderFileName},
	}
	if err := draft.SaveManifest(
		root,
		manifest,
	); err != nil {
		t.Fatal(err)
	}

	return root, runID
}

// runHeaderDiffForP23执行固定Run的Header Diff并返回输出。
func runHeaderDiffForP23(
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

	cmd := newHeaderDiffCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.RunE(
		cmd,
		[]string{runID},
	)
	return output.String(), err
}

// runHeaderApplyForP23执行固定Run的Header Apply并返回输出。
func runHeaderApplyForP23(
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

	cmd := newHeaderApplyCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err := cmd.RunE(
		cmd,
		[]string{runID},
	)
	return output.String(), err
}

func TestHeaderDiffAppendsContentReview(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#新头部甲\n#新头部乙\n",
	)

	output, err := runHeaderDiffForP23(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"Header Diff应成功: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		output,
		"审计记录: diff draft_hash=",
	) {
		t.Fatalf(
			"Diff输出缺少内容摘要审计: %s",
			output,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 1 {
		t.Fatalf(
			"应追加一次Header Review: %+v",
			manifest.Reviews,
		)
	}

	review := manifest.Reviews[0]
	if review.Action != draft.ReviewActionDiff ||
		review.DraftHash == "" ||
		review.PathsCount != 1 ||
		review.Passed != 1 {
		t.Fatalf(
			"Header Review内容不符: %+v",
			review,
		)
	}

	if manifest.AgentName != "endpoint-test" ||
		manifest.PlanID != strings.Repeat("a", 64) ||
		manifest.GenerationHash != strings.Repeat("b", 64) {
		t.Fatalf(
			"Diff不得覆盖Generation State: %+v",
			manifest,
		)
	}
}

func TestHeaderApplyRejectsDraftChangedAfterDiff(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#已审阅头部\n",
	)

	if _, err := runHeaderDiffForP23(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"初次Diff应成功: %v",
			err,
		)
	}

	indexBefore := readIndex(
		t,
		root,
	)

	if err := draft.WriteFile(
		root,
		runID,
		draft.HeaderFileName,
		[]byte("#审阅后偷偷修改\n"),
	); err != nil {
		t.Fatal(err)
	}

	output, err := runHeaderApplyForP23(
		t,
		root,
		runID,
	)

	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"审阅后改稿应ExitInvalid: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		err.Error(),
		"Header P-23防线",
	) ||
		!strings.Contains(
			err.Error(),
			"摘要",
		) {
		t.Fatalf(
			"拒绝错误应点明Header内容摘要漂移: %v",
			err,
		)
	}

	if readIndex(t, root) != indexBefore {
		t.Fatal(
			"Header P-23拒绝前不得修改正式索引",
		)
	}

	backups, _ := filepath.Glob(
		filepath.Join(
			root,
			"aoci.txt.backup.*",
		),
	)
	if len(backups) != 0 {
		t.Fatalf(
			"内容摘要拒绝不得产生索引备份: %v",
			backups,
		)
	}

	manifest, loadErr := draft.LoadManifest(
		root,
		runID,
	)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if manifest.AppliedAt != "" {
		t.Fatalf(
			"内容摘要拒绝不得标记applied_at: %s",
			manifest.AppliedAt,
		)
	}
}

func TestHeaderApplyPassesAfterRediff(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#第一版头部\n",
	)

	if _, err := runHeaderDiffForP23(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"第一次Diff应成功: %v",
			err,
		)
	}

	if err := draft.WriteFile(
		root,
		runID,
		draft.HeaderFileName,
		[]byte("#重新审阅后的头部\n"),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := runHeaderDiffForP23(
		t,
		root,
		runID,
	); err != nil {
		t.Fatalf(
			"改稿后重新Diff应成功: %v",
			err,
		)
	}

	before, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Reviews) != 2 {
		t.Fatalf(
			"应保留两次Review历史: %+v",
			before.Reviews,
		)
	}
	if before.Reviews[0].DraftHash ==
		before.Reviews[1].DraftHash {
		t.Fatal(
			"改稿后重新Diff的draft_hash必须变化",
		)
	}

	output, err := runHeaderApplyForP23(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"重新Diff后Apply应成功: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		output,
		"内容审阅核对: ✓",
	) {
		t.Fatalf(
			"Apply应显示摘要核对通过: %s",
			output,
		)
	}

	indexText := readIndex(
		t,
		root,
	)
	if !strings.Contains(
		indexText,
		"#重新审阅后的头部",
	) {
		t.Fatalf(
			"正式索引应应用重新审阅版本: %s",
			indexText,
		)
	}

	after, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if after.AppliedAt == "" {
		t.Fatal(
			"干净应用后应标记applied_at",
		)
	}
}

func TestHeaderApplyLegacyManifestWithoutReviewWarns(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#旧Manifest兼容头部\n",
	)

	output, err := runHeaderApplyForP23(
		t,
		root,
		runID,
	)
	if err != nil {
		t.Fatalf(
			"旧Manifest无Reviews应警告兼容放行: %v\n%s",
			err,
			output,
		)
	}
	if !strings.Contains(
		output,
		"无Header P-23内容审阅记录",
	) {
		t.Fatalf(
			"兼容放行必须明确警告: %s",
			output,
		)
	}

	if !strings.Contains(
		readIndex(t, root),
		"#旧Manifest兼容头部",
	) {
		t.Fatal(
			"旧Manifest兼容批次应完成应用",
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AppliedAt == "" {
		t.Fatal(
			"旧Manifest兼容应用后仍应标记applied_at",
		)
	}
}

func TestHeaderP23RejectsCorruptManifestBeforeWrite(
	t *testing.T,
) {
	root, runID := buildHeaderP23Repo(
		t,
		"#不会应用\n",
	)
	indexBefore := readIndex(
		t,
		root,
	)

	runDir, err := draft.RunDir(
		root,
		runID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(
			runDir,
			draft.ManifestFileName,
		),
		[]byte("{not-json"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, err = runHeaderApplyForP23(
		t,
		root,
		runID,
	)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) ||
		exitErr.Code != ExitConfig {
		t.Fatalf(
			"损坏Manifest应ExitConfig而非兼容绕过: %v",
			err,
		)
	}
	if readIndex(t, root) != indexBefore {
		t.Fatal(
			"Manifest损坏时不得修改正式索引",
		)
	}
}
