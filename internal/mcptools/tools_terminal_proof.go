// The verdict the final Volumes Apply carries instead of asking for three
// more calls.
package mcptools

import (
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// terminalProofVersion names the additive field the final Apply carries.
const terminalProofVersion = "terminal-proof/v1"

// TerminalProof is what Verify, Aggregate Check, and Guide would print after
// the final Apply, computed once from the governance fact set all three share
// (volumegovernance.Assess). Until 2026-11-01 the final Apply prescribed those
// three commands as its terminal proof, three calls that re-derived what the
// Apply had just computed; the Agent now reads the verdict here and the task's
// cognition closing is complete. check_passed is Check's verdict (structure
// valid and governance aligned), guide_complete and guide_next_action are the
// Guide's complete and next_action, and findings are the actionable ones,
// informational facts left out as the Guide leaves them out. The CLI commands
// remain for CI and for a person.
type TerminalProof struct {
	Version           string                     `json:"version"`
	GovernanceResult  string                     `json:"governance_result"`
	Aligned           bool                       `json:"aligned"`
	StructureValid    bool                       `json:"structure_valid"`
	CheckPassed       bool                       `json:"check_passed"`
	GuideComplete     bool                       `json:"guide_complete"`
	GuideNextAction   string                     `json:"guide_next_action"`
	CompositeIdentity string                     `json:"composite_identity"`
	Findings          []volumegovernance.Finding `json:"findings"`
}

// BuildTerminalProof assesses the repository after the final Apply exactly as
// verify, check, and Guide do and projects their verdicts. The facts come back
// with it so a proof that disagrees with the Apply's own inspector can name the
// blocker's remediation; a repository that cannot be assessed is reported as
// such, never as aligned.
func BuildTerminalProof(root string) (*TerminalProof, *volumegovernance.Facts) {
	unavailable := func(code string) *TerminalProof {
		return &TerminalProof{Version: terminalProofVersion, GovernanceResult: "unavailable",
			GuideNextAction: machinecontract.ActionCallNoArgumentMaintainForCurrentMachineBatch,
			Findings:        []volumegovernance.Finding{{Code: code, Domain: "code"}}}
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return unavailable(fail.Code), nil
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil {
		return unavailable("governance_assess_failed"), nil
	}
	findings := make([]volumegovernance.Finding, 0, len(facts.Findings))
	for _, finding := range facts.Findings {
		if volumegovernance.Informational(finding.Code) {
			continue
		}
		findings = append(findings, finding)
	}
	return &TerminalProof{
		Version:           terminalProofVersion,
		GovernanceResult:  facts.Result,
		Aligned:           facts.GovernanceAligned,
		StructureValid:    facts.StructureValid,
		CheckPassed:       facts.StructureValid && facts.GovernanceAligned,
		GuideComplete:     facts.GovernanceAligned,
		GuideNextAction:   facts.NextRequiredAction,
		CompositeIdentity: facts.CompositeIdentity,
		Findings:          findings,
	}, facts
}
