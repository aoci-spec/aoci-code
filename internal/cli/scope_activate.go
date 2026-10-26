package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/spf13/cobra"
)

// scopeActivationApproval is the continuation in the ordinary CLI error envelope.
// The preview remains the existing versioned Scope Change artifact, so approve
// and apply need no activation-specific transaction or recovery path.
type scopeActivationApproval struct {
	InteractionRequired bool   `json:"interaction_required"`
	FormalWritesStarted bool   `json:"formal_writes_started"`
	PreviewFile         string `json:"preview_file"`
	ApproveCommand      string `json:"approve_command"`
	ApplyCommand        string `json:"apply_command"`
	// HostDialogRequest asks the same person in the MCP host's dialog; the
	// approve command is the terminal route for a host that cannot show one.
	HostDialogRequest *hostDialogRequest `json:"host_dialog_request,omitempty"`
}

func newScopeActivateCmd() *cobra.Command {
	return &cobra.Command{Use: "activate", Short: cliMessage("cli.short.scope_activate"),
		Long: cliMessage("cli.long.scope_activate"), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return managedScopeExitError(err)
			}
			preview, err := scopechange.Build(root, time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
				scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
					Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{}})
			if err != nil {
				return managedScopeExitError(err)
			}
			if preview.Plan.InteractionRequired {
				return pauseScopeActivation(cmd, root, preview)
			}
			aligned, err := scopeActivationAlreadyAligned(root, &preview.Plan)
			if err != nil {
				return managedScopeExitError(err)
			}
			if aligned {
				return writeScopeActivationAligned(cmd, &preview.Plan)
			}
			result, err := scopechange.Apply(root, preview, nil)
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeChangeResult(cmd, result)
		}}
}

// scopeActivationAligned is the activation result when there is nothing to
// activate: no transaction is recorded and no formal write starts.
type scopeActivationAligned struct {
	Status               string `json:"status"`
	FormalWritesStarted  bool   `json:"formal_writes_started"`
	PolicyIdentity       string `json:"policy_identity"`
	BudgetPolicyIdentity string `json:"budget_policy_identity"`
	NetworkAccessed      bool   `json:"network_accessed"`
}

// scopeActivationAlreadyAligned reports whether the desired policy, budget,
// and authorization posture already equal the active receipt and the Plan
// moves no object, Entry, or fingerprint. Apply used to run regardless and
// recorded a transaction whose old and new identities were equal and whose
// change lists were empty, which read as a plain "applied" (#113). An active
// receipt without a recorded posture is left to Apply, which records one.
func scopeActivationAlreadyAligned(root string, plan *scopechange.Plan) (bool, error) {
	state, exists, err := baseline.Load(root)
	if err != nil {
		return false, err
	}
	if !exists || state.ManagedScope == nil {
		return false, nil
	}
	active := state.ManagedScope
	if active.PolicyIdentity != plan.NewPolicyIdentity || plan.OldPolicyIdentity != plan.NewPolicyIdentity {
		return false, nil
	}
	if active.BudgetPolicyIdentity != plan.NewBudgetPolicyIdentity ||
		(plan.OldBudgetPolicyIdentity != "" && plan.OldBudgetPolicyIdentity != plan.NewBudgetPolicyIdentity) {
		return false, nil
	}
	if active.ApplyAuthorizationMode == "" || active.ApplyAuthorizationMode != plan.AuthorizationPolicy.EffectiveMode {
		return false, nil
	}
	moved := len(plan.IndexAdded) + len(plan.IndexRemoved) + len(plan.ObserveAdded) + len(plan.ObserveRemoved) +
		len(plan.ExcludeAdded) + len(plan.ExcludeRemoved) + len(plan.RoleChanges) + len(plan.CoverageReductions) +
		len(plan.EntryCreates) + len(plan.EntryRemoves) + len(plan.EntryUpdates) +
		len(plan.BaselineAdded) + len(plan.BaselineRemoved) +
		len(plan.ObserveFingerprintAdded) + len(plan.ObserveFingerprintRemoved)
	return moved == 0, nil
}

func writeScopeActivationAligned(cmd *cobra.Command, plan *scopechange.Plan) error {
	result := scopeActivationAligned{Status: "already_aligned", PolicyIdentity: plan.NewPolicyIdentity,
		BudgetPolicyIdentity: plan.NewBudgetPolicyIdentity}
	if flagJSON {
		return writePlannerJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("scope.activate_already_aligned", result.PolicyIdentity))
	return nil
}

func pauseScopeActivation(cmd *cobra.Command, root string, preview *scopechange.Preview) error {
	if err := cognitiontxn.EnsureSafeDirectory(root, ".aoci/scope-change"); err != nil {
		return managedScopeExitError(err)
	}
	// Separate attempts never replace a preview that a human may be reviewing.
	directory, err := os.MkdirTemp(filepath.Join(root, ".aoci", "scope-change"), "activate-")
	if err != nil {
		return managedScopeExitError(err)
	}
	previewFile := filepath.Join(directory, "preview.json")
	if err := writePlannerArtifact(cmd, preview, previewFile); err != nil {
		return managedScopeExitError(err)
	}
	approvalFile := filepath.Join(directory, "approval.json")
	continuation := scopeActivationApproval{
		InteractionRequired: true,
		PreviewFile:         previewFile,
		HostDialogRequest:   scopeChangeDialogRequest(root, previewFile),
		ApproveCommand: hostInteractionCommand("--repo", root, "scope", "approve", "--preview-file", previewFile,
			"--actor", "<id>", "--out-file", approvalFile),
		ApplyCommand: hostInteractionCommand("--repo", root, "scope", "apply", "--preview-file", previewFile,
			"--approval-file", approvalFile),
	}
	message := cliMessage("scope.activate_approval_required", previewFile, continuation.ApproveCommand, continuation.ApplyCommand)
	if continuation.HostDialogRequest != nil && scopechange.HostDialogEligible(preview) {
		message = cliMessage("scope.activate_approval_required_dialog", previewFile, continuation.ApproveCommand, continuation.ApplyCommand)
	} else {
		continuation.HostDialogRequest = nil
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "managed_scope_human_approval_required",
		Msg: message, Details: continuation}
}
