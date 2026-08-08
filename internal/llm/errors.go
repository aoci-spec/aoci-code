// 索引条目: errors.go[LL7T]
// 职责: 把端点调用失败分类为可被上层区分处理的类型(决定重试/提示用户/放弃)。
package llm

import "fmt"

// Kind 是错误类别。上层可据此决定重试策略与面向用户的提示。
type Kind int

const (
	// KindConfig 配置错误(base_url/model 缺失等)——不可重试,应提示用户修配置
	KindConfig Kind = iota
	// KindNetwork 网络层错误(连接失败/DNS/拒绝)——通常提示检查端点地址与连通性
	KindNetwork
	// KindTimeout 超时——可考虑重试或提示端点过慢
	KindTimeout
	// KindAuth 认证失败(HTTP 401/403)——提示用户检查 api_key_env 对应密钥
	KindAuth
	// KindRateLimit 限流(HTTP 429)——适合退避重试
	KindRateLimit
	// KindServer 端点服务端错误(HTTP 5xx)——适合有限重试
	KindServer
	// KindResponse 响应解析错误(非法 JSON / 缺必要字段)——通常不可重试
	KindResponse
	// KindHTTP 其他非成功 HTTP 状态(4xx 非上述)——一般不可重试
	KindHTTP
)

// kindName 返回类别的可读名称
func kindName(k Kind) string {
	switch k {
	case KindConfig:
		return "配置错误"
	case KindNetwork:
		return "网络错误"
	case KindTimeout:
		return "调用超时"
	case KindAuth:
		return "认证失败"
	case KindRateLimit:
		return "限流"
	case KindServer:
		return "端点服务端错误"
	case KindResponse:
		return "响应解析错误"
	case KindHTTP:
		return "HTTP 错误"
	default:
		return "未知错误"
	}
}

// Error 是本包统一错误类型,携带类别、可读信息、HTTP 状态码(如有)与底层错误。
type Error struct {
	Kind       Kind
	Message    string
	StatusCode int   // HTTP 状态码,0 表示无(如网络层错误)
	Err        error // 底层错误(可选)
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	base := fmt.Sprintf("[%s] %s", kindName(e.Kind), e.Message)
	if e.StatusCode != 0 {
		base = fmt.Sprintf("%s (HTTP %d)", base, e.StatusCode)
	}
	if e.Err != nil {
		base = fmt.Sprintf("%s: %v", base, e.Err)
	}
	return base
}

// Unwrap 支持 errors.Is/As 向下解包底层错误。
func (e *Error) Unwrap() error {
	return e.Err
}

// IsRetryable 表示该错误是否适合重试(限流与服务端 5xx、超时可重试)。
// 上层批量生成可据此实现退避重试(骨架阶段暂不内置退避,仅提供判据)。
func (e *Error) IsRetryable() bool {
	switch e.Kind {
	case KindRateLimit, KindServer, KindTimeout:
		return true
	default:
		return false
	}
}

// classifyHTTPStatus 依据 HTTP 状态码归类(仅用于非 2xx 响应)。
func classifyHTTPStatus(status int) Kind {
	switch {
	case status == 401 || status == 403:
		return KindAuth
	case status == 429:
		return KindRateLimit
	case status >= 500:
		return KindServer
	default:
		return KindHTTP
	}
}
