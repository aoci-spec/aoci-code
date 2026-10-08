// 索引条目: client.go[LL9NS]
// 职责: 组装补全请求、发送、解析响应,返回文本 + token 用量。
//
//	      支持 openai-compatible(/chat/completions)与 anthropic(/v1/messages)两种线协议。
//
//		仅用标准库;如实转达端点返回的 usage(有则 exact,无则标记 missing 交上层决定)。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxResponseBytes 是响应体读取上限(防御性:不可信/故障端点返回超大响应导致 OOM)。
// 补全响应正常远小于此值;超限视为异常。10MB 对任何合理补全都绰绰有余。
const maxResponseBytes = 10 << 20 // 10 MiB

// Message 是一条对话消息(OpenAI 兼容格式)。
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// CompletionRequest 是上层发起补全的输入。
type CompletionRequest struct {
	// Messages 是完整对话(通常 system + user)。必填非空。
	Messages []Message
	// Temperature 采样温度指针。为 nil 时请求体不带该字段(用端点默认)。
	Temperature *float64
	// MaxTokens 输出上限。<=0 时请求体不带该字段(用端点默认)。
	MaxTokens int
}

// TokenSource 标记 token 计量来源(D26)。
type TokenSource string

const (
	// TokenSourceExact 端点返回了 usage,数字可信
	TokenSourceExact TokenSource = "exact"
	// TokenSourceMissing 端点未返回 usage,需上层决定是否本地估算
	TokenSourceMissing TokenSource = "missing"
)

// TokenUsage 是一次调用的 token 用量。
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Source       TokenSource
}

// CompletionResult 是一次补全的输出。
type CompletionResult struct {
	// Text 是模型返回的首个 choice 的文本内容。
	Text string
	// Usage 是本次调用的 token 用量(Source 指明可信度)。
	Usage TokenUsage
	// FinishReason 是首个 choice 的结束原因(stop/length 等,端点提供则透传)。
	FinishReason string
}

// —— 以下为与端点交互的线格式(wire format)结构,仅本包内部使用 ——

type wireChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type wireChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	// 部分端点错误时也返回 200 + error 体,尽量解析以给出更好提示
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete 执行一次补全调用。按 Client 的 Provider 分派到对应线协议实现。
// ctx 控制取消/超时(超时的主控在此 —— 见 options.go 包注释"超时分层")。
func (c *Client) Complete(ctx context.Context, req CompletionRequest) (*CompletionResult, error) {
	if len(req.Messages) == 0 {
		return nil, &Error{Kind: KindConfig, Message: "补全请求 messages 不能为空"}
	}
	if c.provider == ProviderAnthropic {
		return c.completeAnthropic(ctx, req)
	}
	return c.completeOpenAI(ctx, req)
}

// doPost 公共发送层: 构造 JSON POST、限读响应体、非 2xx 归类并尽量带端点错误信息。
// 返回响应体与状态码;调用方负责各自的线格式序列化/解析。
func (c *Client) doPost(ctx context.Context, endpoint string, headers map[string]string, body []byte) ([]byte, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, &Error{Kind: KindConfig, Message: "构造 HTTP 请求失败", Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// 区分超时与其他网络错误
		if errors.Is(err, context.DeadlineExceeded) || isTimeoutErr(err) {
			return nil, 0, &Error{Kind: KindTimeout, Message: "调用端点超时", Err: err}
		}
		return nil, 0, &Error{Kind: KindNetwork, Message: "连接端点失败(请检查 base_url 与网络/内网连通性)", Err: err}
	}
	defer resp.Body.Close()

	// 响应体读取加上限(防御超大响应导致 OOM);LimitReader 至多读 maxResponseBytes+1 字节
	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.StatusCode, &Error{Kind: KindNetwork, Message: "读取端点响应失败", StatusCode: resp.StatusCode, Err: err}
	}
	if len(respBody) > maxResponseBytes {
		return nil, resp.StatusCode, &Error{
			Kind:       KindResponse,
			Message:    fmt.Sprintf("端点响应体超过上限 %d 字节,已中止(疑似异常端点)", maxResponseBytes),
			StatusCode: resp.StatusCode,
		}
	}

	// 非 2xx: 归类并尽量带上端点返回的错误信息
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := classifyHTTPStatus(resp.StatusCode)
		msg := endpointErrorMessage(respBody)
		if msg == "" {
			msg = "端点返回非成功状态"
		}
		return nil, resp.StatusCode, &Error{Kind: kind, Message: msg, StatusCode: resp.StatusCode}
	}
	return respBody, resp.StatusCode, nil
}

// completeOpenAI 是 openai-compatible 线协议实现(原 Complete 主体)。
func (c *Client) completeOpenAI(ctx context.Context, req CompletionRequest) (*CompletionResult, error) {
	endpoint, err := c.completionsURL()
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(wireChatRequest{
		Model:       c.model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return nil, &Error{Kind: KindConfig, Message: "请求体序列化失败", Err: err}
	}

	// APIKey 为空时不带 Authorization —— 支持内网免认证端点
	headers := map[string]string{}
	if c.apiKey != "" {
		headers["Authorization"] = "Bearer " + c.apiKey
	}

	respBody, _, err := c.doPost(ctx, endpoint, headers, body)
	if err != nil {
		return nil, err
	}

	var wire wireChatResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, &Error{Kind: KindResponse, Message: "端点响应 JSON 解析失败", Err: err}
	}

	// 某些端点以 200 + error 体表达错误
	if wire.Error != nil && wire.Error.Message != "" {
		return nil, &Error{Kind: KindHTTP, Message: "端点返回错误: " + wire.Error.Message}
	}

	if len(wire.Choices) == 0 {
		return nil, &Error{Kind: KindResponse, Message: "端点响应不含任何 choices"}
	}

	result := &CompletionResult{
		Text:         wire.Choices[0].Message.Content,
		FinishReason: wire.Choices[0].FinishReason,
	}

	// token 用量: 端点给了 usage 则 exact,否则 missing(交上层按 TokenAccounting 决定是否本地估算)
	if wire.Usage != nil {
		result.Usage = TokenUsage{
			InputTokens:  wire.Usage.PromptTokens,
			OutputTokens: wire.Usage.CompletionTokens,
			TotalTokens:  wire.Usage.TotalTokens,
			Source:       TokenSourceExact,
		}
	} else {
		result.Usage = TokenUsage{Source: TokenSourceMissing}
	}

	return result, nil
}

// —— Anthropic Messages 线格式(/v1/messages) ——

// anthropicDefaultMaxTokens: anthropic 协议 max_tokens 必填,调用方未指定时的默认输出上限。
const anthropicDefaultMaxTokens = 8192

type wireAnthropicRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"` // Message{role,content} 字段名与 anthropic 一致,直接复用
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens"` // 必填,无 omitempty
}

type wireAnthropicResponse struct {
	Content []struct {
		Type string `json:"type"` // text | thinking | tool_use | ...
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	// anthropic 错误体: {"type":"error","error":{"type":..,"message":..}};部分网关 200 也回它
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicStopReasonMap 把 anthropic stop_reason 归一到 OpenAI 惯用的结束原因词。
var anthropicStopReasonMap = map[string]string{
	"end_turn":      "stop",
	"stop_sequence": "stop",
	"max_tokens":    "length",
}

// completeAnthropic 是 Anthropic Messages 线协议实现。
// 差异点: system 消息提为顶层 system 字段;max_tokens 必填;认证走 x-api-key +
// anthropic-version 头;响应 content[] 只拼接 text 块(忽略 thinking 等非文本块)。
func (c *Client) completeAnthropic(ctx context.Context, req CompletionRequest) (*CompletionResult, error) {
	endpoint, err := c.messagesURL()
	if err != nil {
		return nil, err
	}

	// system 提取: OpenAI 惯例放 messages 里,anthropic 要求顶层 system 字段
	var system string
	rest := make([]Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system != "" {
				system += "\n"
			}
			system += m.Content
		} else {
			rest = append(rest, m)
		}
	}
	if len(rest) == 0 { // 防御: 全 system 的退化输入
		return nil, &Error{Kind: KindConfig, Message: "补全请求不含任何非 system 消息"}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}

	body, err := json.Marshal(wireAnthropicRequest{
		Model:       c.model,
		System:      system,
		Messages:    rest,
		Temperature: req.Temperature,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return nil, &Error{Kind: KindConfig, Message: "请求体序列化失败", Err: err}
	}

	headers := map[string]string{"anthropic-version": "2023-06-01"}
	// APIKey 为空时不带 x-api-key —— 支持内网免认证端点
	if c.apiKey != "" {
		headers["x-api-key"] = c.apiKey
	}

	respBody, _, err := c.doPost(ctx, endpoint, headers, body)
	if err != nil {
		return nil, err
	}

	var wire wireAnthropicResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, &Error{Kind: KindResponse, Message: "端点响应 JSON 解析失败", Err: err}
	}

	// 某些网关(如智谱)以 200 + error 体表达错误
	if wire.Error != nil && wire.Error.Message != "" {
		return nil, &Error{Kind: KindHTTP, Message: "端点返回错误: " + wire.Error.Message}
	}

	var text strings.Builder
	for _, block := range wire.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 && len(wire.Content) == 0 {
		return nil, &Error{Kind: KindResponse, Message: "端点响应不含任何 content 块"}
	}

	finish, ok := anthropicStopReasonMap[wire.StopReason]
	if !ok {
		finish = wire.StopReason // 未识别的结束原因原样透传,便于排查
	}

	result := &CompletionResult{
		Text:         text.String(),
		FinishReason: finish,
	}
	if wire.Usage != nil {
		result.Usage = TokenUsage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
			TotalTokens:  wire.Usage.InputTokens + wire.Usage.OutputTokens,
			Source:       TokenSourceExact,
		}
	} else {
		result.Usage = TokenUsage{Source: TokenSourceMissing}
	}

	return result, nil
}

// completionsURL 由 BaseURL 拼接补全路径,容忍 BaseURL 结尾是否带斜杠。
func (c *Client) completionsURL() (string, error) {
	trimmed := strings.TrimRight(c.baseURL, "/")
	full := trimmed + DefaultChatCompletionsPath
	if _, err := url.ParseRequestURI(full); err != nil {
		return "", &Error{Kind: KindConfig, Message: fmt.Sprintf("端点地址非法: %s", full), Err: err}
	}
	return full, nil
}

// messagesURL 由 BaseURL 拼接 Anthropic Messages 路径,容忍 BaseURL 结尾是否带斜杠。
func (c *Client) messagesURL() (string, error) {
	trimmed := strings.TrimRight(c.baseURL, "/")
	full := trimmed + DefaultAnthropicMessagesPath
	if _, err := url.ParseRequestURI(full); err != nil {
		return "", &Error{Kind: KindConfig, Message: fmt.Sprintf("端点地址非法: %s", full), Err: err}
	}
	return full, nil
}

// endpointErrorMessage 尝试从端点错误响应体中提取可读信息(尽力而为,失败返回空)。
func endpointErrorMessage(body []byte) string {
	var probe struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	if probe.Error != nil && probe.Error.Message != "" {
		return "端点错误: " + probe.Error.Message
	}
	if probe.Message != "" {
		return "端点错误: " + probe.Message
	}
	return ""
}

// isTimeoutErr 判断是否为网络超时类错误(net.Error 的 Timeout)。
func isTimeoutErr(err error) bool {
	type timeout interface{ Timeout() bool }
	var t timeout
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}
