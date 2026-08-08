// 索引条目: client_test.go[NLM8TM]
// 职责: 用标准库 httptest 起假端点,验证请求组装、响应解析、usage 提取、错误分类。
//
//	不连真实模型、不走真实网络 —— 契合确定性测试与"-count=1 全绿"纪律。
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient 用假端点 server 的地址构造 Client。
func newTestClient(t *testing.T, ts *httptest.Server, apiKey string) *Client {
	t.Helper()
	c, err := NewClient(Options{
		BaseURL:    ts.URL,
		Model:      "test-model",
		APIKey:     apiKey,
		HTTPClient: ts.Client(),
	})
	if err != nil {
		t.Fatalf("构造 Client 失败: %v", err)
	}
	return c
}

// TestNewClient_RequiredFields 校验必填项。
func TestNewClient_RequiredFields(t *testing.T) {
	if _, err := NewClient(Options{Model: "m"}); err == nil {
		t.Fatal("base_url 为空时应返回错误")
	}
	if _, err := NewClient(Options{BaseURL: "http://x"}); err == nil {
		t.Fatal("model 为空时应返回错误")
	}
	// APIKey 可为空(内网免认证)
	if _, err := NewClient(Options{BaseURL: "http://x", Model: "m"}); err != nil {
		t.Fatalf("APIKey 为空不应报错(内网免认证): %v", err)
	}
}

// TestComplete_Success 验证正常补全:请求组装正确、响应文本与 usage 正确提取。
func TestComplete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 校验请求方法、路径、Content-Type、Authorization
		if r.Method != http.MethodPost {
			t.Errorf("期望 POST,得到 %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("期望路径 /chat/completions,得到 %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("期望 Content-Type application/json,得到 %s", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("期望 Authorization Bearer test-key,得到 %q", auth)
		}
		// 校验请求体含 model 与 messages
		body, _ := io.ReadAll(r.Body)
		var req wireChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("请求体解析失败: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("期望 model test-model,得到 %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Errorf("期望 2 条消息,得到 %d", len(req.Messages))
		}
		// 返回带 usage 的成功响应
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"choices":[{"message":{"content":"生成的条目内容"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150}
		}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "test-key")
	res, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "你是索引生成器"},
			{Role: "user", Content: "为该文件生成条目"},
		},
	})
	if err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}
	if res.Text != "生成的条目内容" {
		t.Errorf("文本不符: %q", res.Text)
	}
	if res.FinishReason != "stop" {
		t.Errorf("finish_reason 不符: %q", res.FinishReason)
	}
	if res.Usage.Source != TokenSourceExact {
		t.Errorf("期望 usage source exact,得到 %s", res.Usage.Source)
	}
	if res.Usage.InputTokens != 120 || res.Usage.OutputTokens != 30 || res.Usage.TotalTokens != 150 {
		t.Errorf("usage 数字不符: %+v", res.Usage)
	}
}

// TestComplete_NoAuthHeaderWhenKeyEmpty 验证内网免认证:APIKey 为空时不带 Authorization。
func TestComplete_NoAuthHeaderWhenKeyEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("APIKey 为空时不应带 Authorization,得到 %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "") // 空 key
	if _, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}
}

// TestComplete_MissingUsage 验证端点不返回 usage 时标记为 missing。
func TestComplete_MissingUsage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"无用量响应"}}]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	res, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}
	if res.Usage.Source != TokenSourceMissing {
		t.Errorf("期望 usage source missing,得到 %s", res.Usage.Source)
	}
}

// TestComplete_AuthError 验证 401 归类为 KindAuth。
func TestComplete_AuthError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "wrong")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T: %v", err, err)
	}
	if e.Kind != KindAuth {
		t.Errorf("期望 KindAuth,得到 %s", kindName(e.Kind))
	}
	if e.StatusCode != http.StatusUnauthorized {
		t.Errorf("期望状态码 401,得到 %d", e.StatusCode)
	}
}

// TestComplete_RateLimitRetryable 验证 429 归类为 KindRateLimit 且可重试。
func TestComplete_RateLimitRetryable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T", err)
	}
	if e.Kind != KindRateLimit {
		t.Errorf("期望 KindRateLimit,得到 %s", kindName(e.Kind))
	}
	if !e.IsRetryable() {
		t.Error("限流错误应可重试")
	}
}

// TestComplete_ServerErrorRetryable 验证 5xx 归类为 KindServer 且可重试。
func TestComplete_ServerErrorRetryable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T", err)
	}
	if e.Kind != KindServer || !e.IsRetryable() {
		t.Errorf("期望 KindServer 且可重试,得到 %s retryable=%v", kindName(e.Kind), e.IsRetryable())
	}
}

// TestComplete_BadJSON 验证非法 JSON 响应归类为 KindResponse。
func TestComplete_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{not valid json`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T", err)
	}
	if e.Kind != KindResponse {
		t.Errorf("期望 KindResponse,得到 %s", kindName(e.Kind))
	}
}

// TestComplete_EmptyMessages 验证空 messages 被拒。
func TestComplete_EmptyMessages(t *testing.T) {
	c, _ := NewClient(Options{BaseURL: "http://x", Model: "m"})
	_, err := c.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatal("空 messages 应返回错误")
	}
}

// TestComplete_OversizeResponseRejected 验证超过 maxResponseBytes 的响应体被拒收
// 且归类 KindResponse(防御 OOM 路径;2026-07-09 条目重审发现该防御代码零覆盖,补齐)。
func TestComplete_OversizeResponseRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 写出超过上限 1 字节的响应体(内容无需是合法 JSON:超限判定先于解析)
		big := strings.Repeat("x", maxResponseBytes+1)
		_, _ = io.WriteString(w, big)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T: %v", err, err)
	}
	if e.Kind != KindResponse {
		t.Errorf("超大响应体应归 KindResponse,得到 %s", kindName(e.Kind))
	}
	if !strings.Contains(e.Message, "超过上限") {
		t.Errorf("错误信息应指明超限,得到 %q", e.Message)
	}
}

// TestComplete_EmptyChoices 验证 200 但 choices 为空的响应归类 KindResponse
// (2026-07-09 条目重审发现该防御代码零覆盖,补齐)。
func TestComplete_EmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer ts.Close()

	c := newTestClient(t, ts, "k")
	_, err := c.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *Error,得到 %T: %v", err, err)
	}
	if e.Kind != KindResponse {
		t.Errorf("空 choices 应归 KindResponse,得到 %s", kindName(e.Kind))
	}
}
