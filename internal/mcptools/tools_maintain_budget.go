// Maintain response transport budget: how many candidates one batch carries.
package mcptools

import (
	"encoding/json"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/authoringcontract"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

// maintainBaseOverheadBytes reserves the part of a Maintain response that
// neither the repository's Meta, the locale's instructions, nor the paths
// decide: the bounded governance facts without their path samples (about
// 3 KB), plan and batch identities, the receipt, metrics, and JSON framing.
// Measured at about 6.5 KB on a 120-file v0.1.0-rc16 fixture. The Meta bytes
// and the instruction text are measured on the repository (fixedOverheadBytes)
// and the path-bearing samples on the candidates (sampleBytes), so neither a
// custom Meta nor a deep tree overflows this constant.
const maintainBaseOverheadBytes = 7 << 10

// maintainInstructionsFallbackBytes stands in for the instruction text when
// the authoring contract cannot be built for the estimate.
const maintainInstructionsFallbackBytes = 3 << 10

// maintainCandidateSampleLimit is the per-list sample a Maintain response
// keeps while it carries candidates: the candidates are the actionable set
// and the complete lists stay in Verify and Check.
const maintainCandidateSampleLimit = 5

// optimizationCandidateExtraBytes covers the cost, importance, and selection
// reason an optimization candidate carries beyond an ordinary one.
const optimizationCandidateExtraBytes = 320

func maintainTransportBudget(cfg *config.Config) int {
	return cfg.MaintainTransportBudget()
}

// codeCandidateBudget is the byte budget the current Code batch may occupy in
// one Maintain response: the team transport budget less the reserved fixed
// part and less the path-bearing samples the response will carry, never
// below one KiB so a single candidate always ships. The samples are drawn
// from the head of the ordered plan, so they are measured on the candidates
// themselves: a Java tree with 90-byte paths carries samples three times the
// size of a flat fixture's, which a constant cannot know.
func codeCandidateBudget(cfg *config.Config, set *cognition.Set, candidates []codebatch.Candidate) codebatch.Budget {
	return codebatch.Budget{Bytes: candidateBudgetBytes(cfg, set, candidates), Size: codeCandidateWireSize, Total: maintainTransportBudget(cfg)}
}

func optimizationCandidateBudget(cfg *config.Config, set *cognition.Set, candidates []codebatch.Candidate) codebatch.Budget {
	return codebatch.Budget{Bytes: candidateBudgetBytes(cfg, set, candidates), Total: maintainTransportBudget(cfg), Size: func(candidate codebatch.Candidate) int {
		return codeCandidateWireSize(candidate) + optimizationCandidateExtraBytes
	}}
}

func candidateBudgetBytes(cfg *config.Config, set *cognition.Set, candidates []codebatch.Candidate) int {
	bytes := maintainTransportBudget(cfg) - fixedOverheadBytes(set) - sampleBytes(candidates)
	if bytes < 1<<10 {
		bytes = 1 << 10
	}
	return bytes
}

// fixedOverheadBytes is the base reservation plus the two repository-sized
// parts every candidate-carrying response repeats: the exact Meta bytes as
// authoring_meta and the locale's authoring instructions.
func fixedOverheadBytes(set *cognition.Set) int {
	total := maintainBaseOverheadBytes
	if set == nil {
		return total + maintainInstructionsFallbackBytes
	}
	total += len(set.Meta.Raw)
	contract, err := authoringcontract.Build(set.Meta.Raw, []string{cognition.ScopeCode}, textassets.ActiveLocale())
	if err != nil {
		return total + maintainInstructionsFallbackBytes
	}
	for _, instruction := range contract.Instructions {
		total += len(instruction) + 4
	}
	return total
}

// sampleBytes measures the path-bearing samples the response carries beside
// candidates, all drawn from the head of the ordered plan: the review and
// write samples (one quoted object reference each), the governance findings
// sample (a finding object around the path), and the code_drift missing
// sample. A deep Java tree puts 90-byte paths in every one of them; measuring
// them on the candidates is what keeps the whole response under the budget.
func sampleBytes(candidates []codebatch.Candidate) int {
	ordered, _ := codebatch.Select(candidates, 0)
	total := 0
	for index, candidate := range ordered {
		if index >= maintainCandidateSampleLimit {
			break
		}
		total += 4*(len(candidate.ObjectRef)+3) + governanceFindingOverheadBytes
	}
	return total
}

// governanceFindingOverheadBytes is one governance finding beyond its path:
// code, domain, and the JSON around them.
const governanceFindingOverheadBytes = 64

// codeCandidateWireSize measures one candidate as the response carries it: the
// candidate object with its two 64-hex identities plus its separator; the
// review and write references travel as bounded samples counted separately.
func codeCandidateWireSize(candidate codebatch.Candidate) int {
	data, err := json.Marshal(volumeMaintainCandidate{Domain: cognition.ScopeCode, Change: candidate.Change,
		ObjectRef: candidate.ObjectRef, Path: candidate.Path, ExistingEntry: candidate.ExistingEntry,
		SourceSHA256: candidate.SourceSHA256, CandidateID: strings.Repeat("0", 64), BatchID: strings.Repeat("0", 64),
		ModelAuthoringOnly: true})
	if err != nil {
		return 512
	}
	return len(data) + 1
}

// transportCodePlan is the plan as the wire carries it: every identity and
// count, without the candidate list the response already carries once as the
// top-level candidates. The disk receipt keeps the complete plan.
func transportCodePlan(plan codebatch.Plan) *codebatch.Plan {
	projected := plan
	projected.Candidates = nil
	return &projected
}

func transportCodePlanPtr(plan *codebatch.Plan) *codebatch.Plan {
	if plan == nil {
		return nil
	}
	return transportCodePlan(*plan)
}

// PlanCodeBatchIncluded reports how many of the current Code authoring targets
// the next no-argument Maintain will issue under the team cap and transport
// budget, so the Guide shows the number Maintain will plan with rather than
// the count cap alone.
func PlanCodeBatchIncluded(root string, cfg *config.Config, set *cognition.Set, work volumegovernance.CodeAuthoringWork) int {
	all := codeCandidatesForWork(root, set, work)
	if len(all) == 0 {
		return 0
	}
	_, selected := codebatch.SelectWithBudget(all, codeBatchLimit(cfg), codeCandidateBudget(cfg, set, all))
	return len(selected)
}

// EstimateCodeAuthoringRounds reports how many Maintain rounds the current
// batch rule takes to author the given repository-relative paths as new
// Entries under the team configuration. scan prints it so an operator knows
// what a first build costs before the model starts.
func EstimateCodeAuthoringRounds(cfg *config.Config, set *cognition.Set, paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	candidates := make([]codebatch.Candidate, 0, len(paths))
	for _, path := range paths {
		candidates = append(candidates, codebatch.Candidate{Target: codebatch.Target{Change: cognition.ImpactChangeCreate,
			ObjectRef: "code:" + path, Path: path, SourceSHA256: strings.Repeat("0", 64)}})
	}
	limit := codeBatchLimit(cfg)
	if limit < machinecontract.CodeCognitionBatchEntriesMin {
		limit = machinecontract.CodeCognitionBatchEntriesDefault
	}
	return codebatch.CountBatches(candidates, limit, codeCandidateBudget(cfg, set, candidates))
}
