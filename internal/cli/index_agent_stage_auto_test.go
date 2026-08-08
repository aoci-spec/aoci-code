// R65/R65-03 Host-Agent Entries Stage Auto接入测试。
//
// 锁定：
//   - auto一次Stage内部完成Check、Diff和原子Apply；
//   - 候选内容拒绝返回repair_required、唯一JSON和进程成功码；
//   - review仍只Stage，不自动形成Review或Application；
//   - Host-Agent治理事件全部使用agent来源。
//
// 测试隔离纪律：
// 本文件不得通过executeCLI复用包级全局Cobra命令树。Stage请求文件Flag绑定在
// 命令实例闭包中，复用全局命令树会把已解析Flag带入后续测试。单对象协议通过
// 新建Stage命令执行，再调用finishBufferedExecution验证根层收口语义。
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// r65ConfigureHostAgentMode写入测试仓团队automation模式。
func r65ConfigureHostAgentMode(
	t *testing.T,
	root,
	mode string,
) {
	t.Helper()

	cfg, err := config.LoadBase(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.SetAutomationMode(
		mode,
	); err != nil {
		t.Fatal(err)
	}

	if err := config.Save(
		root,
		cfg,
	); err != nil {
		t.Fatal(err)
	}
}

// r65HostAgentRequest构造当前Plan中一个目标的合法Stage请求。
func r65HostAgentRequest(
	t *testing.T,
	root,
	path,
	entry string,
) agentStageRequest {
	t.Helper()

	cfg, doc, indexPath :=
		agentPlanLoadDocument(
			t,
			root,
		)

	plan, err := buildAgentPlan(
		root,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	target := agentStageFindTarget(
		t,
		plan,
		path,
	)

	return agentStageRequest{
		Version: agentStageVersion,
		PlanID:  plan.PlanID,
		Agent:   "codex",
		Model:   "test-model",
		Entries: []agentStageEntry{
			{
				Path:         path,
				SourceSHA256: target.SourceSHA256,
				Entry:        entry,
			},
		},
	}
}

// r65RunAgentStageDirect使用全新Stage命令执行JSON请求。
//
// 全局根Flag必须在本函数返回前立即恢复，不能延迟到整个测试结束，避免同一测试
// 后续断言或包内其他直接命令调用观察到flagJSON=true。
func r65RunAgentStageDirect(
	t *testing.T,
	root string,
	request agentStageRequest,
) (string, error) {
	t.Helper()

	oldRepo := flagRepo
	oldJSON := flagJSON
	oldQuiet := flagQuiet

	flagRepo = root
	flagJSON = true
	flagQuiet = false

	defer func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
		flagQuiet = oldQuiet
	}()

	data, err := json.Marshal(
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	command := newIndexAgentStageCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs(
		[]string{
			"--stdin-json",
		},
	)
	command.SetIn(
		bytes.NewReader(data),
	)

	var output bytes.Buffer
	command.SetOut(
		&output,
	)
	command.SetErr(
		&output,
	)

	runErr := command.Execute()

	return output.String(), runErr
}

func TestAgentStageCommandAutoFinalizesAndApplies(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	r65ConfigureHostAgentMode(
		t,
		root,
		config.AutomationModeAuto,
	)

	request := r65HostAgentRequest(
		t,
		root,
		"new.go",
		"new.go[XAP7T]: F:自动收口 | R:- | A:- | S:-",
	)

	output, err := r65RunAgentStageDirect(
		t,
		root,
		request,
	)
	if err != nil {
		t.Fatalf(
			"Host-Agent Auto Stage应成功: %v\n%s",
			err,
			output,
		)
	}

	var result agentStageResult
	if err := json.Unmarshal(
		[]byte(output),
		&result,
	); err != nil {
		t.Fatalf(
			"Host-Agent Auto Stage JSON不可解析: %v\n%s",
			err,
			output,
		)
	}

	if result.AutoFinalize == nil ||
		result.AutoFinalize.Status !=
			entriesAutoStatusApplied ||
		result.AutoFinalize.FailedStep != "" ||
		result.AutoFinalize.Checked != 1 ||
		result.AutoFinalize.DiffReviewed != 1 ||
		result.AutoFinalize.Applied != 1 ||
		!result.AutoFinalize.AssetWritten ||
		!result.AutoFinalize.AuditRecorded ||
		result.AutoFinalize.Error != nil ||
		!strings.Contains(
			result.NextCommand,
			"verify",
		) {
		t.Fatalf(
			"Host-Agent Auto Stage结果不符: %+v",
			result,
		)
	}

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	indexText, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(indexText),
		"F:自动收口",
	) {
		t.Fatalf(
			"Host-Agent Auto未写入正式索引:\n%s",
			indexText,
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 2 ||
		len(manifest.Applications) != 1 ||
		manifest.AppliedAt == "" {
		t.Fatalf(
			"Host-Agent Auto审计不完整: %+v",
			manifest,
		)
	}

	requiredOps := map[string]bool{
		"agent_stage":          false,
		"entries_check":        false,
		"entries_diff":         false,
		"update_entries_batch": false,
		"entries_apply":        false,
	}

	events, _ := ledger.Recent(
		root,
		100,
	)

	for _, event := range events {
		if _, tracked := requiredOps[event.Op]; !tracked {
			continue
		}

		if event.Source != ledger.SourceAgent {
			t.Fatalf(
				"Host-Agent治理事件来源错误: %+v",
				event,
			)
		}

		requiredOps[event.Op] = true
	}

	for operation, found := range requiredOps {
		if !found {
			t.Fatalf(
				"Host-Agent Auto缺少Ledger事件: %s events=%+v",
				operation,
				events,
			)
		}
	}
}

func TestAgentStageCommandAutoCheckRepairIsSingleJSON(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	r65ConfigureHostAgentMode(
		t,
		root,
		config.AutomationModeAuto,
	)

	request := r65HostAgentRequest(
		t,
		root,
		"new.go",
		"wrong.go[XAP7T]: F:错误文件名 | R:- | A:- | S:-",
	)

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	indexBefore, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	commandOutput, commandErr :=
		r65RunAgentStageDirect(
			t,
			root,
			request,
		)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := finishBufferedExecution(
		commandErr,
		true,
		[]byte(commandOutput),
		nil,
		&stdout,
		&stderr,
	)

	if code != 0 {
		t.Fatalf(
			"可修复Check拒绝应返回成功码0，得到%d\nstdout=%s\nstderr=%s",
			code,
			stdout.String(),
			stderr.String(),
		)
	}

	if stderr.Len() != 0 {
		t.Fatalf(
			"repair_required JSON不得追加stderr: %s",
			stderr.String(),
		)
	}

	var result agentStageResult
	if err := json.Unmarshal(
		stdout.Bytes(),
		&result,
	); err != nil {
		t.Fatalf(
			"根层输出必须是单一JSON对象: %v\n%s",
			err,
			stdout.String(),
		)
	}

	if result.AutoFinalize == nil ||
		result.AutoFinalize.Status !=
			entriesAutoStatusRepairRequired ||
		result.AutoFinalize.FailedStep !=
			entriesAutoStepCheck ||
		result.AutoFinalize.AssetWritten ||
		result.AutoFinalize.AuditRecorded ||
		result.AutoFinalize.Rejected == 0 ||
		len(result.AutoFinalize.Findings) == 0 ||
		result.AutoFinalize.Error != nil ||
		result.NextCommand != "" ||
		!strings.Contains(
			result.AutoFinalize.Recovery,
			"再次提交同一完整批次",
		) {
		t.Fatalf(
			"Host-Agent repair_required报告不符: %+v",
			result,
		)
	}

	indexAfter, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal(
			"repair_required后正式索引必须零写入",
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 1 ||
		manifest.Reviews[0].Action !=
			draft.ReviewActionCheck ||
		manifest.Reviews[0].Rejected == 0 ||
		len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"repair_required草稿审计不符: %+v",
			manifest,
		)
	}

	pendingRun, err := draft.LatestPendingRun(root, draft.KindEntries)
	if err != nil || pendingRun != "" {
		t.Fatalf(
			"零写入Check拒绝不得阻断重启后的Guide与修复批次: run=%q err=%v",
			pendingRun,
			err,
		)
	}
	if err := guardPendingEntriesForAgent(root); err != nil {
		t.Fatalf("零写入repair_required之后Guide应可恢复: %v", err)
	}
}

func TestAgentStageCommandReviewDoesNotAutoFinalize(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	r65ConfigureHostAgentMode(
		t,
		root,
		config.AutomationModeReview,
	)

	request := r65HostAgentRequest(
		t,
		root,
		"new.go",
		"new.go[XAP7T]: F:等待审阅 | R:- | A:- | S:-",
	)

	cfg, err := config.Load(
		root,
	)
	if err != nil {
		t.Fatal(err)
	}

	paths := config.AOCIPaths(
		root,
		cfg.IndexPath,
	)

	indexBefore, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	output, runErr := r65RunAgentStageDirect(
		t,
		root,
		request,
	)
	if runErr != nil {
		t.Fatalf(
			"review Stage应成功: %v\n%s",
			runErr,
			output,
		)
	}

	var result agentStageResult
	if err := json.Unmarshal(
		[]byte(output),
		&result,
	); err != nil {
		t.Fatal(err)
	}

	if result.AutoFinalize != nil ||
		!result.ApprovalRequired ||
		!result.StopBeforeApply ||
		!strings.Contains(
			result.NextCommand,
			"entries check",
		) {
		t.Fatalf(
			"review Stage停点变化: %+v",
			result,
		)
	}

	indexAfter, err := os.ReadFile(
		paths.IndexPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(indexAfter) != string(indexBefore) {
		t.Fatal(
			"review Stage不得修改正式索引",
		)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Reviews) != 0 ||
		len(manifest.Applications) != 0 ||
		manifest.AppliedAt != "" {
		t.Fatalf(
			"review Stage不得自动审阅或应用: %+v",
			manifest,
		)
	}
}
