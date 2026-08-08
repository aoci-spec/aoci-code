// Package llm 是 AOCI-CLI 的 AI 端点适配层。
// 索引条目: options.go[LL9T]
//
// 单一职责: 只负责"如何与一个 OpenAI 兼容端点对话"(组装请求、发送、解析响应、
// 分类错误、如实转达 token 用量),不理解 AOCI 索引/条目/协议 —— 那是 prompt/workflow 层的事。
//
// 依赖纪律(R17/D23):
//
//	本包仅依赖 Go 标准库,不 import config、index、baseline 等任何业务包,
//	也不引入任何第三方 HTTP/LLM SDK；x/sys仅承载跨平台系统原语，不进入AI调用链。
//	因此本包被列入 CI 依赖闸的"AI 编排层",确定性核心包禁止反向依赖它。
//
// 密钥纪律(R19):
//
//	本包接受的 APIKey 是"已由上层从环境变量读出的真实值",作为裸参数传入,用完即弃。
//	本包不接触 os.Getenv、不知道 key 来自何处、不持久化 key。上层(cli 命令层)负责
//	os.Getenv(config.AI.APIKeyEnv) 读出真实 key 再翻译成本包 Options。
//
// 数据主权(边界二):
//
//	本包只向 Options.BaseURL 指定的端点发送内容 —— 该端点完全由用户配置(公有云或内网)。
//
// 超时分层(两层防御,不冲突):
//
//	第一层(精确): 调用方通过 Complete 的 context 控制超时 —— 通常与用户配置 timeout 一致,
//	               是实际期望的超时点。
//	第二层(兜底): 本包内 http.Client.Timeout 作为"调用方忘设 context 超时"时的最后防线,
//	               取值刻意略大于常规 context 超时(见 clientHardTimeout),确保正常情况下
//	               总是 context 先触发、语义由调用方掌控;仅当调用方完全没设 context 超时时,
//	               此兜底才防止无限等待。
package llm

import (
	"net/http"
	"time"
)

// 内置默认值(当 Options 对应字段为零值时由构造函数回退采用)
const (
	// DefaultTimeout 是调用方未指定超时时,建议采用的 context 超时(供上层参考使用)。
	// 300s(P-12 修法,2026-07-12 httpx 实弹): 旧值 120s 按"连通性探测"场景标定,
	// 错配到"条目起草"重载场景 —— 起草注入整文件源码原文(上限 1MiB)+字典+纪律,
	// 慢速自部署端点(内网 vLLM 级)生成一条完整条目 2-4 分钟属正常区间,120s 必然
	// 误伤(实弹: 冷启动 4 分钟耗在超时排障)。用户可经 ai setup --timeout 覆盖;
	// 全批超时=单次×目标数的口径同步放大,该值本就是上限语义非预期耗时。
	DefaultTimeout = 300 * time.Second

	// clientHardTimeout 是 http.Client 层的兜底超时(第二层防御)。
	// 刻意显著大于 DefaultTimeout,保证正常路径下 context 超时先触发、由调用方掌控语义;
	// 此值仅在"调用方完全未设 context 超时"的异常情况下,防止请求无限挂起。
	clientHardTimeout = 10 * time.Minute

	// DefaultChatCompletionsPath 是 OpenAI 兼容的补全端点路径后缀。
	// BaseURL 通常形如 https://host/v1,拼接此路径得到 /v1/chat/completions。
	DefaultChatCompletionsPath = "/chat/completions"
)

// Options 是构造 Client 所需的裸参数(不依赖任何业务结构,便于独立测试)。
type Options struct {
	// BaseURL 是端点根地址(如 https://api.example.com/v1 或内网 http://10.0.0.5:8000/v1)。
	// 必填 —— 为空时构造 Client 返回错误。CLI 绝不为其设默认值。
	BaseURL string

	// Model 是调用所用模型名。必填 —— 为空时构造 Client 返回错误。
	Model string

	// APIKey 是"已从环境变量读出的真实密钥"。允许为空:
	// 空表示端点无需认证(典型内网自部署),请求不带 Authorization 头。
	APIKey string

	// Timeout 曾用于设置 http.Client 超时;现语义调整为:仅作元信息保留,实际超时
	// 由调用方 context 控制(见包注释"超时分层")。为兼容保留字段,<=0 无影响。
	Timeout time.Duration

	// MaxInputTokens 是输入侧软上限,仅作元信息透传给上层参考(本包不据此截断,
	// 截断/分块由 prompt/workflow 层决定)。<=0 表示不限制。
	MaxInputTokens int

	// HTTPClient 允许注入自定义 http.Client(测试时可注入 httptest 的 client)。
	// 为 nil 时构造函数按 clientHardTimeout 新建一个标准 http.Client(兜底超时)。
	HTTPClient *http.Client
}

// Client 是一个可复用的端点客户端。构造后并发安全(内部 http.Client 并发安全,
// 且 Client 自身字段只读)。
type Client struct {
	baseURL        string
	model          string
	apiKey         string
	maxInputTokens int
	httpClient     *http.Client
}

// NewClient 从 Options 构造 Client,并对必填项做校验。
// 校验: BaseURL 与 Model 均不可为空(APIKey 可为空 —— 内网免认证)。
func NewClient(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		return nil, &Error{Kind: KindConfig, Message: "AI 端点 base_url 未配置(不能为空)"}
	}
	if opts.Model == "" {
		return nil, &Error{Kind: KindConfig, Message: "AI 模型 model 未配置(不能为空)"}
	}

	hc := opts.HTTPClient
	if hc == nil {
		// 兜底超时(第二层防御):正常路径由调用方 context 先触发,此处仅防无限挂起。
		hc = &http.Client{Timeout: clientHardTimeout}
	}

	return &Client{
		baseURL:        opts.BaseURL,
		model:          opts.Model,
		apiKey:         opts.APIKey,
		maxInputTokens: opts.MaxInputTokens,
		httpClient:     hc,
	}, nil
}

// Model 返回该客户端所用模型名(供上层记账/日志使用)。
func (c *Client) Model() string {
	return c.model
}
