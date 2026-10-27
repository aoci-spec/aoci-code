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

// Remediation command suffixes are the single source both surfaces compose
// from: the CLI Guide prepends "aoci ", the MCP next_commands field prepends
// the running server's own absolute executable. Two spellings of one command
// is how the Volumes Guide once told hosts to run commands the MCP surface
// spelled differently; a suffix that exists only here cannot fork.
// {agent}, {tokens}, and {source} are host-filled placeholders, the same
// convention the Guide has always used for --reviewed-by {agent}.
const (
	RemediationCommandScan                   = "scan"
	RemediationCommandScopeStatus            = "scope status --json"
	RemediationCommandScopeActivate          = "scope activate --json"
	RemediationCommandScopeAcknowledge       = "scope acknowledge --reviewed-by {agent} --json"
	RemediationCommandScopeBudgetSet         = "scope budget set --max-tokens {tokens}"
	RemediationCommandVerify                 = "verify --json"
	RemediationCommandAggregateCheck         = "check --json"
	RemediationCommandAgentGuide             = "index agent guide --agent {agent} --json"
	RemediationCommandCognitionOnboardStart  = "cognition onboard start --json"
	RemediationCommandDatabaseSnapshot       = "database snapshot --source {source} --json"
	RemediationCommandDatabaseBaselineAccept = "database baseline accept --source {source} --json"
)

// Onboarding continuation suffixes are what each onboarding result's
// next_commands is spelled from. {completion_file}, {candidate_file}, and
// {mapping_file} are host-filled too: the receipt the Host writes, the
// candidate it authors, and the mapping template it saves and completes.
// {plan_file} and {snapshot_file} name the session's own artifacts, which the
// CLI fills in itself.
const (
	OnboardingCommandNext            = "cognition onboard next --json"
	OnboardingCommandComplete        = "cognition onboard next --completion-file {completion_file} --json"
	OnboardingCommandMappingTemplate = "cognition migration mapping template --plan-file {plan_file} --snapshot-file {snapshot_file} --candidate-file {candidate_file} --out-file {mapping_file} --json"
	OnboardingCommandPreview         = "cognition onboard preview --candidate-file {candidate_file} --json"
	OnboardingCommandPreviewMapping  = "cognition onboard preview --candidate-file {candidate_file} --mapping-file {mapping_file} --json"
	OnboardingCommandPrepare         = "cognition onboard prepare --json"
	OnboardingCommandResume          = "cognition onboard resume --json"
)
