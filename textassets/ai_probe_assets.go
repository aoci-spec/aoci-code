// AI端点最小连通探测使用的稳定Prompt资产ID。
//
// 资产只承载发送给用户配置端点的固定文本；角色、MaxTokens、超时、密钥、
// 调用、错误分类与Ledger均由CLI运行时代码控制。
package textassets

const (
	// PromptAIProbeSystem约束模型仅回复最小确认文本。
	PromptAIProbeSystem ID = "prompts/ai-probe-system"

	// PromptAIProbeUser提供最小用户探测输入。
	PromptAIProbeUser ID = "prompts/ai-probe-user"
)
