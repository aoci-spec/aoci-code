package cli

import (
	"os"
	"path/filepath"
	"time"

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
			result, err := scopechange.Apply(root, preview, nil)
			if err != nil {
				return managedScopeExitError(err)
			}
			return writeScopeChangeResult(cmd, result)
		}}
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
		ApproveCommand: hostInteractionCommand("--repo", root, "scope", "approve", "--preview-file", previewFile,
			"--actor", "<id>", "--out-file", approvalFile),
		ApplyCommand: hostInteractionCommand("--repo", root, "scope", "apply", "--preview-file", previewFile,
			"--approval-file", approvalFile),
	}
	return &ExitError{Code: ExitInvalid, MachineCode: "managed_scope_human_approval_required",
		Msg:     cliMessage("scope.activate_approval_required", previewFile, continuation.ApproveCommand, continuation.ApplyCommand),
		Details: continuation}
}
