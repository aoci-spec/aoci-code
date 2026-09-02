package mcptools

import (
	"os"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// next_commands turns a terminal or blocked next_action into commands a host
// can actually run. The prose actions named Verify, Aggregate Check, and Guide
// — names that exist nowhere on the nine-tool MCP surface — and a blocked
// result carried the bare token explicit_orphan_remove_or_resolve_blocker
// while the command that clears it lived only in the CLI Guide. Two hosts on
// two repositories independently stalled on exactly that gap.
//
// Command suffixes come from machinecontract so this surface and the CLI Guide
// compose the identical spelling from one source. The prefix is the running
// server's own executable, absolute and quoted, because the aoci binary is
// whatever path the host's MCP configuration names and is not necessarily on
// PATH. When the executable cannot be resolved the field is omitted: an absent
// advisory beats a wrong command.
func nextCommandPrefix() string {
	executable, err := os.Executable()
	if err != nil || executable == "" ||
		strings.ContainsAny(executable, "\x00\r\n") {
		return ""
	}
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(executable, `"`) {
			return ""
		}
		return `"` + executable + `"`
	}
	if strings.Contains(executable, "'") {
		return ""
	}
	return "'" + executable + "'"
}

func composeNextCommands(prefix string, suffixes ...string) []string {
	if prefix == "" || len(suffixes) == 0 {
		return nil
	}
	commands := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		commands = append(commands, prefix+" "+suffix)
	}
	return commands
}

// finalProofNextCommands names the exact terminal-proof sequence the
// apply-final-proof action has always prescribed in prose.
func finalProofNextCommands() []string {
	return composeNextCommands(nextCommandPrefix(),
		machinecontract.RemediationCommandVerify,
		machinecontract.RemediationCommandAggregateCheck,
		machinecontract.RemediationCommandAgentGuide,
	)
}

// blockedNextCommands mirrors the CLI Guide's blocked-remediation mapping for
// the blocker findings a host can clear itself. Orphan removals need no CLI:
// aoci_remove_entry is already on the MCP surface the caller is holding.
func blockedNextCommands(facts *volumegovernance.Facts) []string {
	if facts == nil {
		return nil
	}
	prefix := nextCommandPrefix()
	if prefix == "" {
		return nil
	}
	suffixes := make([]string, 0, 4)
	seen := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				suffixes = append(suffixes, value)
			}
		}
	}
	for _, finding := range facts.Findings {
		switch finding.Code {
		case "baseline_missing":
			add(machinecontract.RemediationCommandScan)
		case "observed_pending":
			add(machinecontract.RemediationCommandScopeStatus,
				machinecontract.RemediationCommandScopeAcknowledge)
		case "scope_change_required":
			add(machinecontract.RemediationCommandScopeStatus)
		case "cognition_budget_exceeded":
			add(machinecontract.RemediationCommandScopeStatus,
				machinecontract.RemediationCommandScopeBudgetSet)
		}
	}
	return composeNextCommands(prefix, suffixes...)
}

// evidenceNextCommands names the offline-evidence refresh chain for a stopped
// database domain. The MCP server itself never connects: the snapshot and
// acceptance run through the CLI with host-provisioned credentials, and the
// no-argument Maintain that follows is already the tool the caller is holding.
func evidenceNextCommands() []string {
	return composeNextCommands(nextCommandPrefix(),
		machinecontract.RemediationCommandDatabaseSnapshot,
		machinecontract.RemediationCommandDatabaseBaselineAccept,
	)
}
