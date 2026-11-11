// Human rendering of the Volumes governance verdict, shared by verify, check,
// and status so the three commands a person runs by hand name the same files
// and the same next step.
//
// Until 0.2.0 the Volumes branch of verify printed "candidate structure is
// valid" and three hashes and then exited 1, check printed a result token and
// a finding count, and status printed nothing about alignment at all; the
// paths behind a finding were reachable only through --json. The getting
// started walk-through of 2026-11-01 hit all three on its first drift.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// volumeDriftFindingCodes are the finding codes whose paths the drift lists
// already carry; they are printed as sections, never repeated as findings.
var volumeDriftFindingCodes = map[string]bool{
	"code_missing": true, "code_stale": true, "code_orphan": true, "code_unbaselined": true,
	"observed_pending": true,
}

// volumeActionableFindings counts the findings that decide the verdict:
// informational codes are reported by Verify's JSON but never block.
func volumeActionableFindings(facts *volumegovernance.Facts) int {
	count := 0
	for _, finding := range facts.Findings {
		if !volumegovernance.Informational(finding.Code) {
			count++
		}
	}
	return count
}

// writeVolumeGovernanceVerdict prints the one-line conclusion.
func writeVolumeGovernanceVerdict(out io.Writer, facts *volumegovernance.Facts) {
	if facts.GovernanceAligned {
		if facts.Database.Enabled {
			fmt.Fprintln(out, cliMessage("governance.volumes_aligned_database", facts.CodeEntryCount, facts.CodeSourceCount, facts.DatabaseEntryCount))
			writeVolumeHeldSources(out, facts)
			return
		}
		fmt.Fprintln(out, cliMessage("governance.volumes_aligned", facts.CodeEntryCount, facts.CodeSourceCount))
		writeVolumeHeldSources(out, facts)
		return
	}
	fmt.Fprintln(out, cliMessage("governance.volumes_result", facts.Result, volumeActionableFindings(facts)))
}

// writeVolumeGovernanceFindings prints, for a repository that is not aligned,
// every drift list that is non-empty with one path per line, the remaining
// actionable findings, and the next step. An aligned repository prints
// nothing here.
func writeVolumeGovernanceFindings(out io.Writer, facts *volumegovernance.Facts) {
	if facts.GovernanceAligned {
		return
	}
	// Message keys stay literals: the locale consumer test reads them from
	// the source, so a key passed through a variable is invisible to it.
	section := func(header string, items []string) {
		fmt.Fprintln(out, header)
		for _, item := range items {
			fmt.Fprintf(out, "  %s\n", item)
		}
	}
	drift := facts.CodeDrift
	if len(drift.Missing) > 0 {
		section(cliMessage("governance.volumes_missing", len(drift.Missing)), drift.Missing)
	}
	if len(drift.Stale) > 0 {
		section(cliMessage("governance.volumes_stale", len(drift.Stale)), drift.Stale)
	}
	if len(drift.Orphan) > 0 {
		section(cliMessage("governance.volumes_orphan", len(drift.Orphan)), drift.Orphan)
	}
	if len(drift.Unbaselined) > 0 {
		section(cliMessage("governance.volumes_unbaselined", len(drift.Unbaselined)), drift.Unbaselined)
	}
	if volumeHasFinding(facts, "observed_pending") {
		observed := append([]string{}, drift.ObservedNew...)
		observed = append(observed, drift.ObservedChanged...)
		observed = append(observed, drift.ObservedRemoved...)
		if len(observed) > 0 {
			section(cliMessage("governance.volumes_observed", len(observed)), observed)
		}
	}
	for _, finding := range facts.Findings {
		if volumeDriftFindingCodes[finding.Code] || volumegovernance.Informational(finding.Code) {
			continue
		}
		detail := finding.Target
		if detail == "" {
			detail = finding.AffectedPath
		}
		if finding.Cause != "" {
			detail = strings.TrimSpace(detail + " " + finding.Cause)
		}
		if detail == "" {
			detail = "-"
		}
		fmt.Fprintln(out, cliMessage("governance.volumes_finding", finding.Code, detail))
	}
	fmt.Fprintln(out, cliMessage("governance.volumes_next", volumeGovernanceNextStep(facts)))
}

// writeVolumeGovernanceSummary prints the compact form status uses: the
// verdict, and when not aligned the drift counts and where the paths are.
func writeVolumeGovernanceSummary(out io.Writer, facts *volumegovernance.Facts) {
	if facts.GovernanceAligned {
		writeVolumeGovernanceVerdict(out, facts)
		return
	}
	drift := facts.CodeDrift
	other := 0
	for _, finding := range facts.Findings {
		if !volumeDriftFindingCodes[finding.Code] && !volumegovernance.Informational(finding.Code) {
			other++
		}
	}
	if volumeHasFinding(facts, "observed_pending") {
		other++
	}
	fmt.Fprintln(out, cliMessage("governance.volumes_status", facts.Result,
		len(drift.Missing), len(drift.Stale), len(drift.Orphan), len(drift.Unbaselined), other))
}

func volumeHasFinding(facts *volumegovernance.Facts, code string) bool {
	for _, finding := range facts.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

// volumeGovernanceNextStep names the one command or session action that
// clears the verdict, in the same precedence finalize uses: a hard blocker
// outranks a pending observe review, which outranks missing Evidence, which
// outranks ordinary authoring. The Guide carries the exact bound commands;
// this is the sentence a person reads before opening it.
func volumeGovernanceNextStep(facts *volumegovernance.Facts) string {
	switch {
	case volumeHasFinding(facts, "baseline_missing"):
		return cliMessage("governance.next.baseline_missing")
	case volumeHasFinding(facts, "recovery_pending"):
		return cliMessage("governance.next.recovery_pending")
	case volumeHasFinding(facts, "scope_change_required"):
		return cliMessage("governance.next.scope_change_required")
	case volumeHasFinding(facts, "root_locale_mismatch"):
		return cliMessage("governance.next.root_locale_mismatch")
	case volumeHasFinding(facts, "code_orphan"):
		return cliMessage("governance.next.code_orphan")
	case facts.Result == volumegovernance.ResultBlocked && facts.NextRequiredAction == machinecontract.ActionReviewObservedEvidence:
		return cliMessage("governance.next.review_observed_evidence")
	case facts.Result == volumegovernance.ResultBlocked:
		return cliMessage("governance.next.blocked")
	case facts.Result == volumegovernance.ResultEvidenceRequired && facts.NextRequiredAction == machinecontract.DatabaseCognitionActionBootstrapVolume:
		return cliMessage("governance.next.bootstrap_database_cognition")
	case facts.Result == volumegovernance.ResultEvidenceRequired:
		return cliMessage("governance.next.evidence_required")
	case facts.Result == volumegovernance.ResultAuthoringRequired:
		return cliMessage("governance.next.authoring_required")
	}
	return cliMessage("governance.next.blocked")
}

// writeVolumeHeldSources says why the Entry count is below the source count.
// "369 Entries cover 379 index-role sources" read as ten files left uncovered;
// they were sources no Entry is owed for, and the line now says how many and
// why.
func writeVolumeHeldSources(out io.Writer, facts *volumegovernance.Facts) {
	skipped, excluded := 0, 0
	for _, finding := range facts.Findings {
		switch finding.Code {
		case volumegovernance.FindingCodeSkipped:
			skipped++
		case volumegovernance.FindingCodeCurationExcluded:
			excluded++
		}
	}
	if skipped+excluded > 0 {
		fmt.Fprintln(out, cliMessage("governance.volumes_held", skipped+excluded, skipped, excluded))
	}
}
