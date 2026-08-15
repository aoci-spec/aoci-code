// Maintain 响应必须装进普通宿主的工具结果窗口, 与仓库规模无关。
//
// 背景(真实用户反馈): 一个约 1400 文件的新仓库首次 Maintain 返回约 330 KB —— 200 条
// 候选 + 逐文件的治理 findings + drift 全量清单。宿主(Claude Code)把超限结果落盘到
// 自己的项目数据目录, 模型只能改写 Python 去解析、合并、再拼一次 200 条的调用, 在
// 中文 Windows 上接连撞上 GBK/UTF-8 互读、shell 引号与临时路径, 一下午没能建成索引。
// 这里钉死两条: 批量默认按团队配置(默认 20)切, 治理清单只带样本与总数。
package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// buildManyFileVolumesRepo 在 Volumes 夹具上再放 count 个未建条目的源文件, 模拟
// 大仓库首次 Maintain: 每个文件都是 missing, 都会成为 finding 与候选。
func buildManyFileVolumesRepo(t *testing.T, count int) string {
	t.Helper()
	root := buildSingleCodeWriteRepo(t, false)
	for i := 0; i < count; i++ {
		rel := filepath.Join("pkg", fmt.Sprintf("mod%03d", i/10), fmt.Sprintf("file%03d.go", i))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("package mod%03d\n\n// Item%03d carries fixture behavior.\nfunc Item%03d() int { return %d }\n", i/10, i, i, i)
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 与夹具一致: 重新拍 Baseline, 让新增文件成为普通的"有源无条目"目标。
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func callMaintain(t *testing.T, root string) string {
	t.Helper()
	return callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", map[string]any{})
}

func decodeMaintainJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("maintain response is not JSON: %v\n%s", err, raw[:200])
	}
	return decoded
}

func TestMaintainPlansTheTeamBatchSizeNotTheWireCeiling(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 60)
	response := decodeMaintainJSON(t, callMaintain(t, root))
	plan := response["code_plan"].(map[string]any)
	if int(plan["max_entries"].(float64)) != machinecontract.CodeCognitionBatchEntriesDefault ||
		int(plan["included"].(float64)) != machinecontract.CodeCognitionBatchEntriesDefault {
		t.Fatalf("默认批量必须是机器默认 %d, 不是线上上限: %+v", machinecontract.CodeCognitionBatchEntriesDefault, plan)
	}
	if len(response["candidates"].([]any)) != machinecontract.CodeCognitionBatchEntriesDefault {
		t.Fatalf("候选数必须等于批量: %d", len(response["candidates"].([]any)))
	}

	// 团队配置改批量: Maintain 立即按新值切批, Guide 与它一致。
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionBatchEntries(7); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	response = decodeMaintainJSON(t, callMaintain(t, root))
	plan = response["code_plan"].(map[string]any)
	if int(plan["max_entries"].(float64)) != 7 || int(plan["included"].(float64)) != 7 {
		t.Fatalf("配置批量 7 未生效: %+v", plan)
	}
	if err := cfg.SetCodeCognitionBatchEntries(machinecontract.EntriesBatchMaxItems + 1); err == nil {
		t.Fatal("超过线上上限的批量必须被拒绝")
	}
	if err := cfg.SetCodeCognitionBatchEntries(0); err == nil {
		t.Fatal("零批量必须被拒绝")
	}
}

func TestMaintainBoundsGovernanceListsForTransport(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 60)
	raw := callMaintain(t, root)
	response := decodeMaintainJSON(t, raw)
	governance := response["governance"].(map[string]any)
	limit := machinecontract.MaintainTransportListLimit
	findings := governance["findings"].([]any)
	missing := governance["code_drift"].(map[string]any)["missing"].([]any)
	if len(findings) != limit || len(missing) != limit {
		t.Fatalf("清单必须裁到样本上限 %d: findings=%d missing=%d", limit, len(findings), len(missing))
	}
	truncation, ok := governance["list_truncation"].(map[string]any)
	if !ok {
		t.Fatalf("裁剪必须自报: %s", raw[:300])
	}
	totals := truncation["totals"].(map[string]any)
	if int(truncation["limit"].(float64)) != limit || totals["findings"] == nil || totals["code_drift.missing"] == nil {
		t.Fatalf("总数必须随样本一起返回: %+v", truncation)
	}
	if int(totals["findings"].(float64)) < 60 || int(totals["code_drift.missing"].(float64)) < 60 {
		t.Fatalf("总数必须是完整计数: %+v", totals)
	}
	// 治理判定与候选完全不受投影影响。
	if response["status"] != autoStatusRepairRequired || response["result"] != "authoring_required" {
		t.Fatalf("投影不得改变判定: status=%v result=%v", response["status"], response["result"])
	}
	if len(raw) > 64<<10 {
		t.Fatalf("60 文件新仓库的首次 Maintain 必须远小于宿主窗口, 实际 %d 字节", len(raw))
	}
}

// 投影只裁传输, 完整清单仍由 Verify/Check 报告: 直接验证纯函数保留计数与标量。
func TestBoundListsForTransportKeepsCountsAndScalars(t *testing.T) {
	facts := &volumegovernance.Facts{GovernanceAligned: false, Result: "authoring_required",
		CodeSourceCount: 500, CodeEntryCount: 0}
	for i := 0; i < 45; i++ {
		facts.CodeDrift.Missing = append(facts.CodeDrift.Missing, fmt.Sprintf("src/f%03d.go", i))
		facts.Findings = append(facts.Findings, volumegovernance.Finding{Code: "code_missing", Domain: "code", Target: fmt.Sprintf("src/f%03d.go", i)})
	}
	facts.CodeDrift.Stale = []string{"a.go", "b.go"}
	bounded := volumegovernance.BoundListsForTransport(facts, 10)
	if len(bounded.CodeDrift.Missing) != 10 || len(bounded.Findings) != 10 || len(bounded.CodeDrift.Stale) != 2 {
		t.Fatalf("只有超限清单被裁: %+v", bounded.CodeDrift)
	}
	if bounded.ListTruncation == nil || bounded.ListTruncation.Totals["code_drift.missing"] != 45 ||
		bounded.ListTruncation.Totals["findings"] != 45 || bounded.ListTruncation.Totals["code_drift.stale"] != 0 {
		t.Fatalf("总数记录不对: %+v", bounded.ListTruncation)
	}
	if bounded.CodeSourceCount != 500 || bounded.Result != "authoring_required" || len(facts.Findings) != 45 {
		t.Fatal("标量与原始事实必须原封不动")
	}
	if volumegovernance.BoundListsForTransport(facts, 100).ListTruncation != nil {
		t.Fatal("清单未超限时不得出现裁剪自报")
	}
}
