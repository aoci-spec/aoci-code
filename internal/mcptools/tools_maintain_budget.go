// Maintain response transport budget: how many candidates one batch carries.
package mcptools

import (
	"encoding/json"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/codebatch"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// maintainBaseOverheadBytes reserves the part of a Maintain response that
// neither the repository's Meta, the locale's instructions, nor the paths
// decide: the bounded governance facts without their path samples (about
// 2.3 KB since the per-asset records left the wire), plan and batch
// identities, the receipt, metrics, and JSON framing. Measured at about
// 5.3 KB on the 60-file transport fixture (the transport test asserts the
// reservation covers the fixed part it measures). The Meta bytes and the
// instruction text are measured on the repository (fixedOverheadBytes) and
// the path-bearing samples on the candidates (sampleBytes), so neither a
// custom Meta nor a deep tree overflows this constant.
const maintainBaseOverheadBytes = 6 << 10

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
// size of a flat fixture's, which a constant cannot know. lines carries the
// line count of every candidate the fingerprint cache knows, so the scale
// evidence is reserved at the width it will have on the wire; a candidate
// without one reserves the widest form. contractOnWire says whether this
// response carries the authoring contract or its reference.
func codeCandidateBudget(cfg *config.Config, set *cognition.Set, candidates []codebatch.Candidate, lines map[string]int, contractOnWire bool) codebatch.Budget {
	ordered, _ := codebatch.Select(candidates, 0)
	return reserveCandidateSamples(baseCodeCandidateBudget(cfg, set, lines, contractOnWire), ordered)
}

func baseCodeCandidateBudget(cfg *config.Config, set *cognition.Set, lines map[string]int, contractOnWire bool) codebatch.Budget {
	reserve := scaleEvidenceWireReserve(set)
	thresholds := optimizationEScaleThresholds(set)
	total := maintainTransportBudget(cfg)
	return codebatch.Budget{Bytes: total - fixedOverheadBytes(cfg, set, contractOnWire), Total: total, Size: func(candidate codebatch.Candidate) int {
		return codeCandidateWireSize(candidate) + scaleEvidenceWireWidth(candidate, lines, thresholds, reserve)
	}, Lines: machinecontract.CodeCognitionBatchSourceLines, LineCount: func(candidate codebatch.Candidate) int {
		// A count the caller does not hold never cuts: the batch is then
		// bounded by bytes alone, as it was before the line bound.
		return lines[candidate.Path]
	}}
}

func optimizationCandidateBudget(cfg *config.Config, set *cognition.Set, candidates []codebatch.Candidate, contractOnWire bool) codebatch.Budget {
	reserve := scaleEvidenceWireReserve(set)
	total := maintainTransportBudget(cfg)
	budget := codebatch.Budget{Bytes: total - fixedOverheadBytes(cfg, set, contractOnWire), Total: total, Size: func(candidate codebatch.Candidate) int {
		return codeCandidateWireSize(candidate) + reserve + optimizationCandidateExtraBytes
	}}
	ordered, _ := codebatch.Select(candidates, 0)
	return reserveCandidateSamples(budget, ordered)
}

func reserveCandidateSamples(budget codebatch.Budget, ordered []codebatch.Candidate) codebatch.Budget {
	budget.Bytes = max(1<<10, budget.Bytes-sampleBytes(ordered))
	return budget
}

// fixedOverheadBytes is the base reservation plus the two repository-sized
// parts a response carrying the authoring contract repeats: the exact Meta
// bytes as authoring_meta and the locale's authoring instructions. A response
// carrying the reference instead reserves the reference.
func fixedOverheadBytes(cfg *config.Config, set *cognition.Set, contractOnWire bool) int {
	total := maintainBaseOverheadBytes
	if !contractOnWire {
		return total + maintainInstructionsRefBytes()
	}
	if set == nil {
		return total + maintainInstructionsFallbackBytes
	}
	total += len(set.Meta.Raw)
	contract, err := buildAuthoringContract(cfg, set.Meta.Raw, []string{cognition.ScopeCode})
	if err != nil {
		return total + maintainInstructionsFallbackBytes
	}
	for _, instruction := range contract.Instructions {
		total += len(instruction) + 4
	}
	return total
}

// maintainInstructionsRefBytes is the reference as the wire carries it, with
// its key, a 64-hex digest, and the widest generation an int can print.
func maintainInstructionsRefBytes() int {
	data, err := json.Marshal(struct {
		Ref maintainInstructionsRef `json:"instructions_ref"`
	}{maintainInstructionsRef{SHA256: strings.Repeat("0", 64), DeliveredInGeneration: int(^uint(0) >> 1), RecallInput: maintainRecallInput}})
	if err != nil {
		return 256
	}
	return len(data)
}

// sampleBytes measures the path-bearing samples the response carries beside
// candidates, all drawn from the head of the ordered plan: the review and
// write samples (one quoted object reference each), the governance findings
// sample (a finding object around the path), and the code_drift missing
// sample. A deep Java tree puts 90-byte paths in every one of them; measuring
// them on the candidates is what keeps the whole response under the budget.
func sampleBytes(ordered []codebatch.Candidate) int {
	total := 0
	for _, candidate := range ordered[:min(len(ordered), maintainCandidateSampleLimit)] {
		total += 4*(len(candidate.ObjectRef)+3) + governanceFindingOverheadBytes
	}
	return total
}

// governanceFindingOverheadBytes is one governance finding beyond its path:
// code, domain, and the JSON around them.
const governanceFindingOverheadBytes = 64

// codeCandidateWireSize measures one candidate as the response carries it: the
// candidate object with its source digest and candidate identity plus its
// separator; the review and write references travel as bounded samples
// counted separately, and the scale evidence is added by its own width.
func codeCandidateWireSize(candidate codebatch.Candidate) int {
	data, err := json.Marshal(volumeMaintainCandidate{Domain: cognition.ScopeCode, Change: candidate.Change,
		ObjectRef: candidate.ObjectRef, Path: candidate.Path, ExistingEntry: candidate.ExistingEntry,
		SourceSHA256: candidate.SourceSHA256, CandidateID: strings.Repeat("0", 64),
		ModelAuthoringOnly: true})
	if err != nil {
		return 512
	}
	return len(data) + 1
}

// scaleEvidenceWireWidth is what file_lines and expected_e add to one
// candidate on the wire: nothing for a path the E scale never judges, the
// measured width when the candidate's line count is known (the two fields
// marshaled as an object, whose braces stand in for the separating comma),
// and the widest form otherwise. fillVolumeCandidateScale omits both fields
// when the bytes no longer match the digest, which only narrows the candidate.
func scaleEvidenceWireWidth(candidate codebatch.Candidate, lines map[string]int, thresholds *index.EScaleThresholds, reserve int) int {
	if !index.ShouldCheckEScalePath(candidate.Path) {
		return 0
	}
	count, ok := lines[candidate.Path]
	if !ok {
		return reserve
	}
	data, err := json.Marshal(struct {
		FileLines int      `json:"file_lines"`
		ExpectedE []string `json:"expected_e,omitempty"`
	}{count, index.ExpectedEScaleSymbols(count, thresholds)})
	if err != nil {
		return reserve
	}
	return len(data)
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
// The Guide is a stateless CLI projection, so it shows the shape of a
// process's first round, which carries the authoring contract; a later
// round in the same process carries the reference and may issue more.
func PlanCodeBatchIncluded(root string, cfg *config.Config, set *cognition.Set, work volumegovernance.CodeAuthoringWork, snapshot map[string]baseline.Fingerprint) int {
	all, lines := codeCandidatesForWork(root, set, work, snapshot)
	if len(all) == 0 {
		return 0
	}
	budget := codeCandidateBudget(cfg, set, all, lines, true)
	_, selected := codebatch.SelectWithBudget(all, codeBatchLimit(cfg), budget)
	// Work beyond one batch is issued as siblings, whose first is cut under
	// the budget less the sibling listing: the Guide shows that batch.
	if parallel := cfg.CodeCognitionParallelBatchLimit(); parallel > 1 && len(selected) < len(all) {
		ordered, _ := codebatch.Select(all, 0)
		if cuts := cutParallelSiblings(ordered, codeBatchLimit(cfg), parallel, budget, budget); len(cuts) > 0 {
			return len(cuts[0])
		}
	}
	return len(selected)
}

// EstimateCodeAuthoringRounds projects first-build rounds for one session and
// for sessions applying all siblings in each plan. With parallelism disabled,
// the second value previews enabling the maximum number of siblings.
// Counted lines bound reading. Only trusted cached counts size the scale
// evidence, since Maintain reserves the widest form when the cache is cold.
func EstimateCodeAuthoringRounds(cfg *config.Config, set *cognition.Set, paths []string, lines, cachedLines map[string]int) (rounds, parallelRounds int) {
	if len(paths) == 0 {
		return 0, 0
	}
	candidates := make([]codebatch.Candidate, 0, len(paths))
	for _, path := range paths {
		candidates = append(candidates, codebatch.Candidate{Target: codebatch.Target{Change: cognition.ImpactChangeCreate,
			ObjectRef: "code:" + path, Path: path, SourceSHA256: strings.Repeat("0", 64)}})
	}
	limit := codeBatchLimit(cfg)
	ordered, _ := codebatch.Select(candidates, 0)
	full := baseCodeCandidateBudget(cfg, set, cachedLines, true)
	ref := baseCodeCandidateBudget(cfg, set, cachedLines, false)
	full.LineCount = func(candidate codebatch.Candidate) int { return lines[candidate.Path] }
	ref.LineCount = full.LineCount

	parallel := cfg.CodeCognitionParallelBatchLimit()
	rounds = countCodeAuthoringRounds(ordered, limit, parallel, full, ref, false)
	if rounds <= 1 {
		return rounds, rounds
	}
	if parallel == 1 {
		parallel = machinecontract.CodeCognitionParallelBatchesMax
	}
	return rounds, countCodeAuthoringRounds(ordered, limit, parallel, full, ref, true)
}

func countCodeAuthoringRounds(remaining []codebatch.Candidate, limit, parallel int, full, ref codebatch.Budget, applySiblings bool) int {
	rounds := 0
	for len(remaining) > 0 {
		// Each new plan samples the work left after the previous Apply.
		// The lead session reuses its contract after the first round.
		ahead := reserveCandidateSamples(full, remaining)
		first := ahead
		if rounds > 0 {
			first = reserveCandidateSamples(ref, remaining)
		}
		taken := codebatch.PrefixWithinBudget(remaining, limit, first)
		if parallel > 1 && taken < len(remaining) {
			cuts := cutParallelSiblings(remaining, limit, parallel, first, ahead)
			taken = len(cuts[0])
			if applySiblings {
				for _, cut := range cuts[1:] {
					taken += len(cut)
				}
			} else {
				// One session leaves the other siblings undelivered. Maintain
				// releases them and issues single batches for the rest.
				parallel = 1
			}
		}
		remaining = remaining[taken:]
		rounds++
	}
	return rounds
}

// Reserve the maximum integer width and every declared numeric band: custom
// dictionaries and overlapping boundaries can return more than one symbol.
// These fields are advisory transport facts, not receipt identity inputs.
func scaleEvidenceWireReserve(set *cognition.Set) int {
	maxLines := int(^uint(0) >> 1)
	data, _ := json.Marshal(struct {
		FileLines int      `json:"file_lines"`
		ExpectedE []string `json:"expected_e,omitempty"`
	}{maxLines, optimizationEScaleThresholds(set).Symbols()})
	return len(data)
}
