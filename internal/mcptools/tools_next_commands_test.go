package mcptools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The commands must be runnable as returned: absolute quoted executable, the
// repository named by --repo so the Host's shell need not stand in it, plus a
// suffix that is byte-identical to the shared machinecontract spelling, so the
// MCP surface and the CLI Guide can never drift apart on one command.
func TestNextCommandsCarryTheSharedSpellings(t *testing.T) {
	root := t.TempDir()
	prefix := nextCommandPrefixFor(root)
	if prefix == "" {
		t.Fatal("the test binary has a resolvable executable; prefix must not be empty")
	}
	if !strings.HasPrefix(prefix, "'") && !strings.HasPrefix(prefix, `"`) {
		t.Fatalf("prefix must be quoted for the shell: %q", prefix)
	}
	quotedRoot, ok := quoteCommandArgument(root)
	if !ok || !strings.HasSuffix(prefix, " --repo "+quotedRoot) {
		t.Fatalf("prefix must name the repository the server serves: %q", prefix)
	}
	bound := func(commands []string) {
		t.Helper()
		for _, command := range commands {
			if !strings.HasPrefix(command, prefix+" ") {
				t.Fatalf("command does not carry the bound prefix %q: %q", prefix, command)
			}
		}
	}

	facts := &volumegovernance.Facts{}
	facts.Findings = []volumegovernance.Finding{
		{Code: "observed_pending"},
		{Code: "observed_pending"}, // duplicates must not duplicate commands
		{Code: "cognition_budget_exceeded"},
	}
	blocked := blockedNextCommands(root, facts)
	joined := strings.Join(blocked, "\n")
	if len(blocked) != 2 ||
		strings.Contains(joined, machinecontract.RemediationCommandScopeAcknowledge) ||
		!strings.Contains(joined, machinecontract.RemediationCommandScopeBudgetSet) ||
		strings.Count(joined, machinecontract.RemediationCommandScopeStatus+"\n")+strings.Count(joined, machinecontract.RemediationCommandScopeStatus) < 1 {
		t.Fatalf("blocked commands wrong: %v", blocked)
	}
	bound(blocked)
	review := blockedNextCommands(root, &volumegovernance.Facts{
		NextRequiredAction: machinecontract.ActionReviewObservedEvidence,
		Findings:           []volumegovernance.Finding{{Code: "observed_pending"}, {Code: "observed_pending"}},
	})
	if len(review) != 3 || !strings.HasSuffix(review[0], machinecontract.RemediationCommandScopeStatus) ||
		!strings.HasSuffix(review[1], machinecontract.RemediationCommandScopeAcknowledge) ||
		!strings.HasSuffix(review[2], machinecontract.RemediationCommandAgentGuide) {
		t.Fatalf("observe review must lead through status, reviewed acknowledgement, and a fresh Guide: %v", review)
	}
	bound(review)

	scopeCommands := blockedNextCommands(root, &volumegovernance.Facts{Findings: []volumegovernance.Finding{
		{Code: "scope_change_required"}, {Code: "scope_change_required"},
	}})
	if len(scopeCommands) != 2 ||
		!strings.HasSuffix(scopeCommands[0], " "+machinecontract.RemediationCommandScopeStatus) ||
		!strings.HasSuffix(scopeCommands[1], " "+machinecontract.RemediationCommandScopeActivate) {
		t.Fatalf("scope blocker must name status then activation once: %v", scopeCommands)
	}
	bound(scopeCommands)

	if commands := blockedNextCommands(root, &volumegovernance.Facts{}); commands != nil {
		t.Fatalf("no clearable blocker must yield no commands, got %v", commands)
	}

	evidence := evidenceNextCommands(root)
	if len(evidence) != 2 ||
		!strings.HasSuffix(evidence[0], " "+machinecontract.RemediationCommandDatabaseSnapshot) ||
		!strings.HasSuffix(evidence[1], " "+machinecontract.RemediationCommandDatabaseBaselineAccept) {
		t.Fatalf("evidence chain wrong: %v", evidence)
	}
	bound(evidence)
	bound(legacyMigrationNextCommands(root))

	// A root the shell cannot be handed safely yields no command at all: an
	// absent advisory beats one that would govern the wrong tree.
	for _, unusable := range []string{"", "/tmp/one\ntwo"} {
		if commands := blockedNextCommands(unusable, facts); commands != nil {
			t.Fatalf("root %q must yield no commands, got %v", unusable, commands)
		}
		if commands := evidenceNextCommands(unusable); commands != nil {
			t.Fatalf("root %q must yield no evidence commands, got %v", unusable, commands)
		}
	}
}

// A blocked Maintain names its actual blocker. The orphan token is kept for the
// one case it was right about; a pending Observe review alone must not send a
// Host hunting for orphans that do not exist (#147).
func TestBlockedNextActionNamesTheActualBlocker(t *testing.T) {
	observed := &volumegovernance.Facts{NextRequiredAction: machinecontract.ActionReviewObservedEvidence, Findings: []volumegovernance.Finding{{Code: "observed_pending"}}}
	if got := blockedNextAction(observed, nil); got != machinecontract.ActionReviewObservedEvidence {
		t.Fatalf("pending Observe review named %q", got)
	}
	if got := blockedNextAction(observed, []string{"code:gone.go"}); got != machinecontract.MaintainActionExplicitOrphanRemove {
		t.Fatalf("an orphan must keep the orphan token, got %q", got)
	}
	for _, step := range []struct {
		codes []string
		want  string
	}{
		{[]string{"observed_pending", "recovery_pending"}, machinecontract.MaintainActionClosePendingRecovery},
		{[]string{"cognition_budget_exceeded", "baseline_missing"}, machinecontract.MaintainActionScanToEstablishBaseline},
		{[]string{"observed_pending", "scope_change_required"}, machinecontract.MaintainActionActivateScopeChange},
		{[]string{"cognition_budget_exceeded"}, machinecontract.MaintainActionResolveCognitionBudget},
		{[]string{"observed_pending", "cognition_budget_exceeded"}, machinecontract.MaintainActionResolveCognitionBudget},
		{[]string{"observed_pending", "future_unknown_blocker"}, machinecontract.MaintainActionResolveBlocker},
		{[]string{"code_skipped"}, machinecontract.MaintainActionResolveBlocker},
	} {
		facts := &volumegovernance.Facts{}
		for _, code := range step.codes {
			facts.Findings = append(facts.Findings, volumegovernance.Finding{Code: code})
		}
		if got := blockedNextAction(facts, nil); got != step.want {
			t.Fatalf("%v named %q, want %q", step.codes, got, step.want)
		}
	}
	if got := blockedNextAction(nil, nil); got != machinecontract.MaintainActionResolveBlocker {
		t.Fatalf("no facts must still answer a non-orphan token, got %q", got)
	}
}

// A Volume a merge left unlike its Baseline record is the blocker, whatever
// authoring findings precede it, and its repair is one git command over the
// formal assets the Root enables.
func TestMergedVolumeStopNamesTheRestore(t *testing.T) {
	root := t.TempDir()
	facts := &volumegovernance.Facts{}
	facts.Root.Path, facts.Meta.Path = "aoci.txt", "aoci.meta.txt"
	facts.Code = volumegovernance.AssetState{Enabled: true, Path: "aoci.code.txt"}
	facts.Database = volumegovernance.AssetState{Path: "aoci.database.txt"}
	facts.Findings = []volumegovernance.Finding{
		{Code: "code_missing", Target: "pkg/a.go"},
		{Code: "code_volume_unbaselined", Target: "aoci.code.txt", Cause: volumegovernance.CauseVolumeChangedAfterBaseline},
	}
	if code, action := blockingFinding(facts, nil); code != "code_volume_unbaselined" || action != machinecontract.MaintainActionRestoreFormalAssets {
		t.Fatalf("the merged Volume must be the named blocker: %s %s", code, action)
	}
	commands := blockedNextCommands(root, facts)
	if len(commands) != 1 || !strings.HasPrefix(commands[0], "git -C ") ||
		!strings.Contains(commands[0], " "+machinecontract.RemediationGitRestoreFormalAssets+" ") ||
		strings.Contains(commands[0], "aoci.database.txt") {
		t.Fatalf("restore command wrong: %v", commands)
	}
	for _, asset := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
		if !strings.Contains(commands[0], asset) {
			t.Fatalf("the restore must name %s: %s", asset, commands[0])
		}
	}
	// An enabled Database Volume is restored with the rest.
	facts.Database.Enabled = true
	if commands = blockedNextCommands(root, facts); len(commands) != 1 || !strings.Contains(commands[0], "aoci.database.txt") {
		t.Fatalf("an enabled Database Volume belongs to the set: %v", commands)
	}
	// A Volume Git hides needs scan, not a restore: nothing is offered.
	facts.Findings[1].Cause = "hidden_from_git"
	if code, action := blockingFinding(facts, nil); action != machinecontract.MaintainActionResolveBlocker || code != "code_missing" {
		t.Fatalf("a hidden Volume is not the restore stop: %s %s", code, action)
	}
	if commands = blockedNextCommands(root, facts); commands != nil {
		t.Fatalf("no restore for a hidden Volume: %v", commands)
	}
}

// The restore is offered only while it is the blocker the result names: a
// pending Recovery comes first and must not sit beside a command that
// overwrites the formal assets, while a Scope Change waits for the restore.
func TestRestoreIsOfferedOnlyAsTheNamedBlocker(t *testing.T) {
	root := t.TempDir()
	merged := volumegovernance.Finding{Code: "code_volume_unbaselined", Target: "aoci.code.txt", Cause: volumegovernance.CauseVolumeChangedAfterBaseline}
	build := func(extra string) *volumegovernance.Facts {
		facts := &volumegovernance.Facts{}
		facts.Root.Path, facts.Meta.Path = "aoci.txt", "aoci.meta.txt"
		facts.Code = volumegovernance.AssetState{Enabled: true, Path: "aoci.code.txt"}
		facts.Findings = []volumegovernance.Finding{{Code: extra, Cause: "entries"}, merged}
		return facts
	}
	pending := build("recovery_pending")
	if _, action := blockingFinding(pending, nil); action != machinecontract.MaintainActionClosePendingRecovery {
		t.Fatalf("a pending Recovery comes first: %s", action)
	}
	if commands := strings.Join(blockedNextCommands(root, pending), "\n"); strings.Contains(commands, "git ") {
		t.Fatalf("no restore beside a pending Recovery: %s", commands)
	}
	for _, later := range []string{"scope_change_required", "cognition_budget_exceeded"} {
		facts := build(later)
		if _, action := blockingFinding(facts, nil); action != machinecontract.MaintainActionRestoreFormalAssets {
			t.Fatalf("the restore comes before %s: %s", later, action)
		}
		if commands := blockedNextCommands(root, facts); len(commands) == 0 || !strings.HasPrefix(commands[0], "git -C ") {
			t.Fatalf("the restore command leads before %s: %v", later, commands)
		}
	}
}

// A Volumes v1 repository whose assets do not load is damaged, not Legacy:
// Maintain answers the load failure and never the migration.
func TestMaintainOnDamagedVolumesIsNotTheLegacyStop(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	session := connectMCPClient(t, root)
	if err := os.Remove(filepath.Join(root, "aoci.meta.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_maintain", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := resText(t, result)
	if !result.IsError || strings.Contains(text, machinecontract.StatusLegacyReadOnlyMigrateRequired) || strings.Contains(text, "migrate_legacy_repository") {
		t.Fatalf("a damaged Volumes repository was answered as Legacy:\n%s", text)
	}
}
