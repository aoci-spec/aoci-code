package cli

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

// Onboarding results name their next step as a token (authoring_next,
// preview, prepare), and a Host that had only those tokens could not tell
// which command reads a batch, what receipt completes it, or that a Migration
// needs a mapping template before Preview. next_commands spells each step,
// from the machinecontract suffixes the Guide and MCP compose from, bound to
// the running binary the way the Guide's commands are. A binary that cannot be
// bound gets no next_commands: an absent advisory beats a wrong command.

// onboardingNextCommands answers what continues a session from its state.
func onboardingNextCommands(root string, session *onboarding.Session) []string {
	if session == nil {
		return nil
	}
	switch session.NextAction {
	case "authoring_next":
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandNext)
	case "preview":
		return onboardingPreviewCommands(root, session)
	case "prepare":
		// Under policy-bound auto, resume prepares and applies in one step.
		if session.Operation == cognitionplan.OperationBootstrap &&
			onboarding.EffectiveAutomationPolicy(session).Mode == config.AutomationModeAuto {
			return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandResume)
		}
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandPrepare)
	case "human_tty_digest_confirmation":
		// Prepare answers again with the person's approval routes.
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandPrepare)
	case "auto_apply":
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandResume)
	case "none":
		if session.Status == "completed" {
			return bindOnboardingCommands(root, session, machinecontract.RemediationCommandVerify,
				machinecontract.RemediationCommandAggregateCheck, machinecontract.RemediationCommandAgentGuide)
		}
	}
	if session.Status == "governance_pending" {
		return bindOnboardingCommands(root, session, machinecontract.RemediationCommandAgentGuide)
	}
	return nil
}

// onboardingBatchNextCommands answers what follows a batch: the receipt that
// completes it, or Preview once nothing is pending.
func onboardingBatchNextCommands(root string, session *onboarding.Session, batch *onboarding.AuthoringBatch) []string {
	if batch == nil || session == nil {
		return nil
	}
	if len(batch.Tasks) != 0 {
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandComplete)
	}
	return onboardingPreviewCommands(root, session)
}

// onboardingPreviewCommands is the step after authoring: a Migration first
// saves the mapping template over its candidate, then previews both.
func onboardingPreviewCommands(root string, session *onboarding.Session) []string {
	if session.Operation == cognitionplan.OperationMigration {
		return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandMappingTemplate,
			machinecontract.OnboardingCommandPreviewMapping)
	}
	return bindOnboardingCommands(root, session, machinecontract.OnboardingCommandPreview)
}

// bindOnboardingCommands prefixes each suffix with the running binary and
// fills the session's own artifact paths, quoted for the target shell.
func bindOnboardingCommands(root string, session *onboarding.Session, suffixes ...string) []string {
	executable, err := currentAgentExecutablePath()
	if err != nil {
		return nil
	}
	prefix, err := agentCommandPrefixFor(runtime.GOOS, executable)
	if err != nil {
		return nil
	}
	artifacts := strings.NewReplacer(
		"{plan_file}", onboardingShellPath(root, session.PlanArtifact),
		"{snapshot_file}", onboardingShellPath(root, session.SnapshotArtifact))
	commands := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		commands = append(commands, prefix+" "+artifacts.Replace(suffix))
	}
	return commands
}

func onboardingShellPath(root, rel string) string {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if runtime.GOOS == "windows" {
		return powerShellQuote(path)
	}
	return posixQuote(path)
}
