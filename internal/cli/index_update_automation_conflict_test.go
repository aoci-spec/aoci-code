// R58 Auto原子应用冲突与失败审计测试。
//
// 先通过真实workflow形成干净草稿，再于Auto Check前制造正式索引重复条目。
// Check和Diff审计均应成功，而原子批量规划必须返回write_conflict；真实Apply
// 尝试须追加rejected application和ledger，但不得设置AppliedAt或覆盖冲突现场。
package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/workflow"
)

func TestUpdateAutomationAutoConflictAudited(
	t *testing.T,
) {
	root := buildUpdateRepo(t)

	writeUpdateAutomationFile(
		t,
		root,
		"f.go",
		"package f\n// conflict漂移\n",
	)

	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:计划应用版 | R:- | A:- | S:-",
		http.StatusOK,
	)

	configureUpdateAutomation(
		t,
		root,
		config.AutomationModeAuto,
		endpoint.server.URL,
	)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	doc := loadEntriesCheckCoreDoc(t, root)

	client, err := buildAIClient(cfg)
	if err != nil {
		t.Fatal(err)
	}

	oldEntry := index.FindEntry(
		doc,
		"f.go",
	)
	if oldEntry == nil {
		t.Fatal(
			"夹具索引缺 f.go 旧条目",
		)
	}

	draftResult, err := workflow.RunEntriesDraft(
		context.Background(),
		root,
		cfg,
		doc,
		client,
		[]string{"f.go"},
		map[string]string{
			"f.go": oldEntry.FullLine,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if draftResult.Drafted != 1 ||
		draftResult.Warned != 0 ||
		draftResult.Failed != 0 ||
		draftResult.Skipped != 0 {
		t.Fatalf(
			"冲突制造前generation应全净: %+v",
			draftResult,
		)
	}

	indexPath := filepath.Join(
		root,
		"aoci.txt",
	)

	indexData, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	tampered := strings.Replace(
		string(indexData),
		oldEntry.FullLine,
		oldEntry.FullLine+"\n"+oldEntry.FullLine,
		1,
	)
	if tampered == string(indexData) {
		t.Fatal(
			"未能构造重复旧条目冲突",
		)
	}

	if err := os.WriteFile(
		indexPath,
		[]byte(tampered),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer

	err = finishUpdateAutoMode(
		root,
		cfg,
		doc,
		draftResult,
		1,
		&output,
	)

	requireUpdateAutomationExit(
		t,
		err,
		exitCodeForFail(
			"write_conflict",
		),
	)

	if !strings.Contains(
		err.Error(),
		"write_conflict",
	) {
		t.Fatalf(
			"冲突错误应保留结构化分类码: %v",
			err,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		draftResult.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 2 {
		t.Fatalf(
			"原子冲突前应形成Check与Diff审计: %+v",
			manifest.Reviews,
		)
	}

	checkReview := manifest.Reviews[0]
	diffReview := manifest.Reviews[1]

	if checkReview.Action !=
		draft.ReviewActionCheck ||
		checkReview.Rejected != 0 ||
		checkReview.Skipped != 0 ||
		checkReview.Passed != 1 ||
		diffReview.Action !=
			draft.ReviewActionDiff ||
		diffReview.Rejected != 0 ||
		diffReview.Skipped != 0 ||
		diffReview.Passed != 1 ||
		checkReview.DraftHash == "" ||
		diffReview.DraftHash !=
			checkReview.DraftHash {
		t.Fatalf(
			"原子冲突前Check与Diff审计不干净: %+v",
			manifest.Reviews,
		)
	}

	if len(manifest.Applications) != 1 {
		t.Fatalf(
			"真实Apply尝试失败须追加application: %+v",
			manifest.Applications,
		)
	}

	application := manifest.Applications[0]

	if application.Applied != 0 ||
		application.Rejected != 1 ||
		application.RejectKinds != "conflict" ||
		application.DraftHash !=
			diffReview.DraftHash {
		t.Fatalf(
			"冲突application审计不符: %+v",
			application,
		)
	}

	if manifest.AppliedAt != "" {
		t.Fatalf(
			"冲突失败不得设置AppliedAt: %s",
			manifest.AppliedAt,
		)
	}

	after, err := os.ReadFile(
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if string(after) != tampered {
		t.Fatal(
			"原子规划冲突后不得覆盖外部修改态",
		)
	}

	events, _ := ledger.Recent(
		root,
		50,
	)

	foundDiff := false
	foundRejectedApply := false

	for _, event := range events {
		if event.Op == "entries_diff" &&
			event.Source == ledger.SourceCLIAI &&
			event.PathsCount == 1 {
			foundDiff = true
		}

		if event.Op == "entries_apply" &&
			event.Source == ledger.SourceCLIAI &&
			event.Result == ledger.ResultConflict &&
			event.AppliedCount == 0 &&
			event.RejectedCount == 1 &&
			event.RejectKinds == "conflict" {
			foundRejectedApply = true
		}
	}

	if !foundDiff ||
		!foundRejectedApply {
		t.Fatalf(
			"冲突审计事件不完整: diff=%v rejected_apply=%v events=%+v",
			foundDiff,
			foundRejectedApply,
			events,
		)
	}
}

func TestUpdateAutomationRejectsSourceDriftAfterEndpointDraft(t *testing.T) {
	root := buildUpdateRepo(t)
	writeUpdateAutomationFile(t, root, "f.go", "package f\n// generation source\n")
	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:绑定生成时源码 | R:- | A:- | S:-",
		http.StatusOK,
	)
	configureUpdateAutomation(t, root, config.AutomationModeAuto, endpoint.server.URL)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := loadEntriesCheckCoreDoc(t, root)
	client, err := buildAIClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldEntry := index.FindEntry(doc, "f.go")
	draftResult, err := workflow.RunEntriesDraft(
		context.Background(), root, cfg, doc, client, []string{"f.go"},
		map[string]string{"f.go": oldEntry.FullLine},
	)
	if err != nil || len(draftResult.Statuses) != 1 || len(draftResult.Statuses[0].SourceSHA256) != 64 {
		t.Fatalf("真实Endpoint生成必须建立源码绑定: err=%v result=%+v", err, draftResult)
	}
	indexBefore, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	writeUpdateAutomationFile(t, root, "f.go", "package f\n// changed after generation\n")
	var output bytes.Buffer
	err = finishUpdateAutoMode(root, cfg, doc, draftResult, 1, &output)
	requireUpdateAutomationExit(t, err, exitCodeForFail("write_conflict"))
	indexAfter, readErr := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if readErr != nil || string(indexAfter) != string(indexBefore) {
		t.Fatalf("源码漂移后旧Endpoint候选必须正式索引零写入: err=%v", readErr)
	}
}

func TestEndpointAutoAcceptsZeroWriteRecoveryReplay(t *testing.T) {
	root := buildUpdateRepo(t)
	writeUpdateAutomationFile(t, root, "f.go", "package f\n")
	endpoint := newUpdateAutomationEndpoint(
		t,
		"f.go[XC5T]: F:端点恢复 | R:- | A:- | S:-",
		http.StatusOK,
	)
	configureUpdateAutomation(t, root, config.AutomationModeAuto, endpoint.server.URL)
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	doc := loadEntriesCheckCoreDoc(t, root)
	client, err := buildAIClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	oldEntry := index.FindEntry(doc, "f.go")
	draftResult, err := workflow.RunEntriesDraft(
		context.Background(), root, cfg, doc, client, []string{"f.go"},
		map[string]string{"f.go": oldEntry.FullLine},
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := finishUpdateAutoMode(root, cfg, doc, draftResult, 1, &output); err != nil {
		t.Fatalf("首次Endpoint Auto应成功: %v\n%s", err, output.String())
	}
	output.Reset()
	if err := finishUpdateAutoMode(root, cfg, doc, draftResult, 1, &output); err != nil {
		t.Fatalf("零写入恢复重放仍应是完整成功: %v\n%s", err, output.String())
	}
}
