// header draft 编排测试: httptest 假端点全链路。
// 覆盖成功链(围栏剥离/usage透传/温度0.2/落盘/落账/默认redacted档全文快照与摘要)、
// none档零痕迹、full档与redacted等价(header场景无源码段)、结构违规草稿警告不报错、
// 失败落missing账零草稿、空草稿报错。快照档位断言与config真实枚举redacted/full/none对齐。
// 索引条目: header_test.go[TWF8TM]
package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
)

// buildRepo 造最小真实仓库: 索引(段头绝对路径与 root 一致) + 已索引/未索引各一文件
func buildRepo(t *testing.T) (root string, cfg *config.Config, doc *index.Document) {
	t.Helper()
	root = t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	idx := "#【系统】测试仓 — 演示\n#三分法: 略\n" +
		"===根" + rootSlash + "/===\n" +
		"indexed.go[XC5T]: F:已索引文件 | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(idx), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "indexed.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "newfile.go"), []byte("package x\n// 未索引\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg = config.DefaultConfig()
	cfg.AI.Enabled = true
	cfg.AI.Model = "test-model"
	var warns []index.Warning
	doc, warns = index.Parse(idx)
	if len(warns) > 0 {
		t.Fatalf("测试夹具索引自身有警告: %v", warns)
	}
	index.ResolveRelPaths(doc, root)
	return root, cfg, doc
}

// capturedBody 假端点收到的最后一个请求体(供温度断言)
type capturedBody struct {
	Temperature *float64 `json:"temperature"`
}

// wireReply OpenAI 兼容响应体(经 json.Marshal 保证转义正确,不手拼字符串)
func wireReply(text string, withUsage bool) []byte {
	type msg struct {
		Content string `json:"content"`
	}
	type choice struct {
		Message      msg    `json:"message"`
		FinishReason string `json:"finish_reason"`
	}
	body := map[string]any{
		"choices": []choice{{Message: msg{Content: text}, FinishReason: "stop"}},
	}
	if withUsage {
		body["usage"] = map[string]int{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}
	}
	data, _ := json.Marshal(body)
	return data
}

// fakeEndpoint 起一个 OpenAI 兼容假端点;lastBody 非 nil 时捕获请求体
func fakeEndpoint(t *testing.T, replyText string, withUsage bool, status int, lastBody *capturedBody) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lastBody != nil {
			_ = json.NewDecoder(r.Body).Decode(lastBody)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(wireReply(replyText, withUsage))
	}))
}

func newTestClient(t *testing.T, baseURL string) *llm.Client {
	t.Helper()
	c, err := llm.NewClient(llm.Options{BaseURL: baseURL, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunHeaderDraftSuccess(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	// DefaultConfig 的 prompt_snapshot=redacted: header 场景无源码段,
	// redacted 行为 = 全文快照 + 摘要(脱敏对象为空)
	reply := "```\n#【系统】测试仓 — 由模型完善\n#字典: A层级 X-测试\n```"
	var body capturedBody
	srv := fakeEndpoint(t, reply, true, http.StatusOK, &body)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err != nil {
		t.Fatalf("draft 失败: %v", err)
	}
	// 围栏被剥离,正文多行保留
	if strings.Contains(res.HeaderText, "```") {
		t.Fatalf("围栏未剥离: %q", res.HeaderText)
	}
	if !strings.Contains(res.HeaderText, "#【系统】测试仓 — 由模型完善") ||
		!strings.Contains(res.HeaderText, "#字典: A层级 X-测试") {
		t.Fatalf("草稿正文不符: %q", res.HeaderText)
	}
	// usage=exact 透传
	if res.Usage.Source != llm.TokenSourceExact || res.Usage.InputTokens != 100 {
		t.Fatalf("usage 不符: %+v", res.Usage)
	}
	// 请求体显式携带低温 0.2(遵从度实验自变量)
	if body.Temperature == nil || *body.Temperature != 0.2 {
		t.Fatalf("请求应显式携带 temperature=0.2: %+v", body.Temperature)
	}
	// 草稿与 manifest 落盘验证
	data, err := draft.ReadFile(root, res.RunID, draft.HeaderFileName)
	if err != nil {
		t.Fatalf("读草稿失败: %v", err)
	}
	if !strings.Contains(string(data), "由模型完善") {
		t.Fatalf("落盘草稿不符: %q", data)
	}
	m, err := draft.LoadManifest(root, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != draft.KindHeader || m.TokenSource != ledger.TokenSourceExact || m.InputTokens != 100 {
		t.Fatalf("manifest 不符: %+v", m)
	}
	// manifest 记录温度自变量
	if m.Temperature == nil || *m.Temperature != 0.2 {
		t.Fatalf("manifest 应记录 temperature=0.2: %+v", m.Temperature)
	}
	// 默认 redacted 档: 16 位摘要 + 全文快照文件并入 Files
	if len(m.PromptHash) != 16 {
		t.Fatalf("redacted 档应记16位prompt摘要: %q", m.PromptHash)
	}
	snap, err := draft.ReadFile(root, res.RunID, draft.PromptFileName)
	if err != nil {
		t.Fatalf("redacted 档(无源码段≡full)应有 prompt.txt: %v", err)
	}
	if !strings.Contains(string(snap), "===== system =====") ||
		!strings.Contains(string(snap), "===== user =====") ||
		!strings.Contains(string(snap), "测试仓") {
		t.Fatal("快照应含 system/user 两段原文")
	}
	var hasPromptFile bool
	for _, f := range m.Files {
		if f == draft.PromptFileName {
			hasPromptFile = true
		}
	}
	if !hasPromptFile {
		t.Fatalf("manifest.Files 应含快照文件: %v", m.Files)
	}
	// ledger 落账验证(op/draft_run_id/token_source 三键齐)
	evs, _ := ledger.Recent(root, 10)
	var found bool
	for _, ev := range evs {
		if ev.Op == "header_draft" && ev.DraftRunID == res.RunID && ev.TokenSrc == ledger.TokenSourceExact {
			found = true
		}
	}
	if !found {
		t.Fatalf("ledger 未见 header_draft 落账: %+v", evs)
	}
}

func TestRunHeaderDraftSnapshotFullSameAsRedacted(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "full"
	srv := fakeEndpoint(t, "#合规草稿", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err != nil {
		t.Fatalf("draft 失败: %v", err)
	}
	// header 场景 full ≡ redacted: 全文快照 + 摘要
	if _, err := draft.ReadFile(root, res.RunID, draft.PromptFileName); err != nil {
		t.Fatalf("full 档应有 prompt.txt: %v", err)
	}
	m, _ := draft.LoadManifest(root, res.RunID)
	if len(m.PromptHash) != 16 {
		t.Fatalf("full 档应记摘要: %q", m.PromptHash)
	}
}

func TestRunHeaderDraftSnapshotNone(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "none"
	srv := fakeEndpoint(t, "#合规草稿", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err != nil {
		t.Fatalf("draft 失败: %v", err)
	}
	// none 档零痕迹: 无全文文件、manifest 无摘要、零警告噪音
	if _, err := draft.ReadFile(root, res.RunID, draft.PromptFileName); err == nil {
		t.Fatal("none 档不应产生 prompt.txt")
	}
	m, _ := draft.LoadManifest(root, res.RunID)
	if m.PromptHash != "" {
		t.Fatalf("none 档不应记摘要: %q", m.PromptHash)
	}
	for _, w := range m.Warnings {
		if strings.Contains(w, "取值未知") {
			t.Fatalf("合法档位 none 不应触发未知取值警告: %v", w)
		}
	}
}

func TestRunHeaderDraftSnapshotUnknownValue(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "bogus"
	srv := fakeEndpoint(t, "#合规草稿", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err != nil {
		t.Fatalf("未知档位应降级不报错: %v", err)
	}
	// 未知值按 none 处理且带警告
	if _, err := draft.ReadFile(root, res.RunID, draft.PromptFileName); err == nil {
		t.Fatal("未知档位不应产生 prompt.txt")
	}
	m, _ := draft.LoadManifest(root, res.RunID)
	var hasWarn bool
	for _, w := range m.Warnings {
		if strings.Contains(w, "取值未知") {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Fatalf("未知档位应入警告: %v", m.Warnings)
	}
}

func TestRunHeaderDraftStructuralWarning(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	// 草稿内偷渡段头: 应落草稿并带结构预检警告(apply 阶段才硬拒),不报错
	srv := fakeEndpoint(t, "#合法行\n===偷渡段/opt/evil/===", false, http.StatusOK, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	res, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err != nil {
		t.Fatalf("结构违规草稿应落盘并警告而非报错: %v", err)
	}
	var hasStructWarn bool
	for _, w := range res.Warnings {
		if strings.Contains(w, "结构预检未过") {
			hasStructWarn = true
		}
	}
	if !hasStructWarn {
		t.Fatalf("应携带结构预检警告: %v", res.Warnings)
	}
	// 无 usage 端点: manifest 标 estimated 且警告同步进 manifest
	m, err := draft.LoadManifest(root, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if m.TokenSource != ledger.TokenSourceEstimated {
		t.Fatalf("无 usage 应标 estimated: %+v", m)
	}
	if len(m.Warnings) == 0 {
		t.Fatalf("警告应同步进 manifest: %+v", m)
	}
}

func TestRunHeaderDraftFailureLedger(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	srv := fakeEndpoint(t, "", false, http.StatusInternalServerError, nil)
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	_, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err == nil {
		t.Fatal("5xx 端点应报错")
	}
	// 失败照样落账且 token_source=missing
	evs, _ := ledger.Recent(root, 10)
	var found bool
	for _, ev := range evs {
		if ev.Op == "header_draft" && ev.TokenSrc == ledger.TokenSourceMissing && ev.WarningsCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("失败调用应落 missing 账: %+v", evs)
	}
	// 失败不产生草稿 run
	ids, _ := draft.ListRunIDs(root)
	if len(ids) != 0 {
		t.Fatalf("失败不应产生草稿: %v", ids)
	}
}

func TestRunHeaderDraftEmptyReply(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	srv := fakeEndpoint(t, "```\n\n```", false, http.StatusOK, nil) // 清理围栏后为空
	defer srv.Close()
	cfg.AI.BaseURL = srv.URL

	_, err := RunHeaderDraft(context.Background(), root, cfg, doc, newTestClient(t, srv.URL))
	if err == nil || !strings.Contains(err.Error(), "空草稿") {
		t.Fatalf("空草稿应报错: %v", err)
	}
	// 空草稿照样落账(token 已消耗)
	evs, _ := ledger.Recent(root, 10)
	var found bool
	for _, ev := range evs {
		if ev.Op == "header_draft" && ev.TokenSrc == ledger.TokenSourceEstimated {
			found = true
		}
	}
	if !found {
		t.Fatalf("空草稿应落 estimated 账: %+v", evs)
	}
}
