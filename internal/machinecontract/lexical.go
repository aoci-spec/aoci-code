package machinecontract

// TextMatchMode defines how a stable public-text term is matched.
type TextMatchMode string

const (
	TextMatchSubstringFold TextMatchMode = "substring-fold"
	TextMatchWordExact     TextMatchMode = "word-exact"

	TextTermForbidden = "forbidden"
	TextTermOverclaim = "overclaim"
)

// TextTerm is one canonical public-text safety term and its matching contract.
type TextTerm struct {
	Text string
	Kind string
	Mode TextMatchMode
}

var publicTextTerms = []TextTerm{
	{Text: "Context Pack", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "上下文包", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "Runtime Router", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "运行时路由", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "任务路由", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "K-AOCI", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "任务装配", Kind: TextTermForbidden, Mode: TextMatchSubstringFold},
	{Text: "TRIES", Kind: TextTermForbidden, Mode: TextMatchWordExact},
	{Text: "zero defects", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "零缺陷", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "deterministic retrieval", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "确定性检索", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "O(1)", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "single source of truth for all", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "100% complete", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "完全杜绝", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
	{Text: "彻底解决", Kind: TextTermOverclaim, Mode: TextMatchSubstringFold},
}

var evolutionNarrativeTerms = []string{
	"本次修改",
	"本次新增",
	"本次调整",
	"改为",
	"新增了",
	"修复了",
	"现在改",
	"原来是",
	"此前是",
	"之前是",
}

// PublicTextTerms returns the canonical forbidden and overclaim terms. The
// returned slice is new, so callers cannot mutate package state.
func PublicTextTerms() []TextTerm {
	return append([]TextTerm(nil), publicTextTerms...)
}

// EvolutionNarrativeTerms returns the canonical Entry warning vocabulary.
// The returned slice is new, so callers cannot mutate package state.
func EvolutionNarrativeTerms() []string {
	return append([]string(nil), evolutionNarrativeTerms...)
}
