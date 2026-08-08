// AI端点最小连通探测的稳定Prompt消费者与统一请求组装。
//
// Doctor --net与aoci ai test必须通过本文件取得同一组消息和MaxTokens，防止
// 两个入口的连通性探测语义漂移。文本来自textassets；动态端点、模型、密钥、
// 超时、调用、错误分类和Ledger继续由各命令控制。
package cli

import (
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/aoci-spec/aoci-code/textassets"
)

// aiTestProbeMessages返回每次调用独立分配的最小探测消息。
//
// 该函数也供失败路径的estimated输入Token估算使用，确保计量文本与真实发送
// 文本完全同源。
func aiTestProbeMessages() ([]llm.Message, error) {
	system, err := textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.PromptAIProbeSystem,
		nil,
	)
	if err != nil {
		return nil, err
	}
	user, err := textassets.RenderScalar(
		textassets.ActiveLocale(),
		textassets.PromptAIProbeUser,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return []llm.Message{
		{
			Role:    "system",
			Content: system,
		},
		{
			Role:    "user",
			Content: user,
		},
	}, nil
}

// aiTestProbeRequest返回Doctor与AI Test共用的完整最小探测请求。
func aiTestProbeRequest() (llm.CompletionRequest, error) {
	messages, err := aiTestProbeMessages()
	if err != nil {
		return llm.CompletionRequest{}, err
	}

	return llm.CompletionRequest{
		Messages:  messages,
		MaxTokens: 16,
	}, nil
}
