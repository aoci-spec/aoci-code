// A Maintain response must fit an ordinary host's tool result window, whatever the repository size.
//
// Background (real user feedback): the first Maintain of a new repository of about 1400 files returned about 330 KB: 200
// candidates + per-file governance findings + the complete drift list. The host (Claude Code) spilled the oversized result to
// its own project data directory, the model could only write Python to parse, merge, and reassemble a 200-entry call, ran into
// GBK/UTF-8 cross-reading, shell quoting, and temporary paths one after another on Chinese Windows, and could not build the index in an afternoon.
// Two things locked here: the batch defaults to the team configuration (default 20), and the governance lists carry only samples and totals.
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

// buildManyFileVolumesRepo puts count more source files without entries on the Volumes fixture, simulating
// a large repository's first Maintain: every file is missing and becomes a finding and a candidate.
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
	// As in the fixture: retake the Baseline so the added files become ordinary "source without entry" targets.
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
	raw := callMaintain(t, root)
	response := decodeMaintainJSON(t, raw)
	plan := response["code_plan"].(map[string]any)
	// The cap is the machine default; the transport budget decides how many
	// of the 60 creates one response carries, and the whole response stays
	// under that budget.
	included := int(plan["included"].(float64))
	if int(plan["max_entries"].(float64)) != machinecontract.CodeCognitionBatchEntriesDefault ||
		included < 20 || included > machinecontract.CodeCognitionBatchEntriesDefault ||
		int(plan["transport_budget_bytes"].(float64)) != machinecontract.MaintainTransportBudgetBytesDefault {
		t.Fatalf("默认批量必须是机器默认上限 %d 之内、按传输预算裁定: %+v", machinecontract.CodeCognitionBatchEntriesDefault, plan)
	}
	if len(response["candidates"].([]any)) != included {
		t.Fatalf("候选数必须等于批量: %d != %d", len(response["candidates"].([]any)), included)
	}
	if plan["candidates"] != nil {
		t.Fatalf("上线的 code_plan 不得重复候选清单: %+v", plan["candidates"])
	}
	if len(raw) > machinecontract.MaintainTransportBudgetBytesDefault {
		t.Fatalf("Maintain 响应 %d 字节超过传输预算 %d", len(raw), machinecontract.MaintainTransportBudgetBytesDefault)
	}

	// The team configuration changes the batch size: Maintain cuts by the new value at once, and the Guide agrees with it.
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
	// With candidates on the wire the governance lists keep a five-item
	// sample; the complete counts still travel in list_truncation.
	limit := maintainCandidateSampleLimit
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
	// The governance verdict and the candidates are entirely unaffected by the projection.
	if response["status"] != autoStatusRepairRequired || response["result"] != "authoring_required" {
		t.Fatalf("投影不得改变判定: status=%v result=%v", response["status"], response["result"])
	}
	if len(raw) > 64<<10 {
		t.Fatalf("60 文件新仓库的首次 Maintain 必须远小于宿主窗口, 实际 %d 字节", len(raw))
	}
}

// The projection trims only the transport; the complete list is still reported by Verify/Check: verify directly that the pure function keeps counts and scalars.
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

// Deep trees carry their paths in every candidate and in the review and write
// samples; the reservation measures those samples on the candidates, so a
// Java-shaped tree still answers under the team budget and still carries a
// batch worth authoring.
func TestMaintainResponseStaysUnderBudgetWithLongPaths(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	directory := filepath.Join("ruoyi-admin", "src", "main", "java", "com", "example", "web", "controller", "system", "monitor")
	for i := 0; i < 45; i++ {
		rel := filepath.Join(directory, fmt.Sprintf("SysOperationLogQueryController%03d.java", i))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(fmt.Sprintf("package monitor;\n\npublic class C%03d {}\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
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
	raw := callMaintain(t, root)
	response := decodeMaintainJSON(t, raw)
	plan := response["code_plan"].(map[string]any)
	included := int(plan["included"].(float64))
	if len(raw) > machinecontract.MaintainTransportBudgetBytesDefault {
		t.Fatalf("long-path Maintain response %d bytes exceeds the budget %d (included=%d)", len(raw), machinecontract.MaintainTransportBudgetBytesDefault, included)
	}
	if included < 15 {
		t.Fatalf("long paths cut the batch too far: included=%d", included)
	}
	sets := response["sets"].(map[string]any)
	if len(sets["write"].([]any)) != maintainCandidateSampleLimit || int(sets["write_total"].(float64)) != included {
		t.Fatalf("write set must be a five-item sample with the complete count: %v", sets)
	}
}
