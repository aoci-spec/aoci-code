// Host-Agent Guide阻断停点的稳定文本资产消费者。
//
// 本文件只负责读取阻断Message和有序Instructions。blocked模式、审批要求、
// Plan阶段判断及允许命令集合仍由index_agent_guide.go控制，文本不得反向决定
// 安全状态。
package cli

import "github.com/aoci-spec/aoci-code/textassets"

// indexReviewBlockedMessage返回正式索引外部漂移停点的稳定结论。
func indexReviewBlockedMessage() string {
	return renderGuideText(
		textassets.ActiveLocale(),
		textassets.ContractGuideIndexReviewBlockedMessage,
		nil,
	)
}

// indexReviewBlockedInstructions返回正式索引外部漂移停点的有序纪律。
func indexReviewBlockedInstructions() []string {
	return renderGuideLines(
		textassets.ActiveLocale(),
		textassets.ContractGuideIndexReviewBlockedInstructions,
		nil,
	)
}

// orphanReviewBlockedMessage返回孤儿条目人工裁决停点的稳定结论。
func orphanReviewBlockedMessage() string {
	return renderGuideText(
		textassets.ActiveLocale(),
		textassets.ContractGuideOrphanReviewBlockedMessage,
		nil,
	)
}

// orphanReviewBlockedInstructions返回孤儿条目人工裁决停点的有序纪律。
func orphanReviewBlockedInstructions() []string {
	return renderGuideLines(
		textassets.ActiveLocale(),
		textassets.ContractGuideOrphanReviewBlockedInstructions,
		nil,
	)
}
