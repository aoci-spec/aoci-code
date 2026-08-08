package cli

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

const hostInteractionVersion = machinecontract.HostInteractionV1

type hostInteractionRequired struct {
	Version             string `json:"version"`
	InteractionRequired bool   `json:"interaction_required"`
	InteractionKind     string `json:"interaction_kind"`
	ExactCommand        string `json:"exact_command"`
	Digest              string `json:"digest"`
	SafeSummary         string `json:"safe_summary"`
	ConfirmationPhrase  string `json:"confirmation_phrase"`
	ResumeAction        string `json:"resume_action"`
	TTYRequired         bool   `json:"tty_required"`
	ModelSelfApproval   bool   `json:"model_self_approval"`
	FormalWritesStarted bool   `json:"formal_writes_started"`
}

type hostInteractionError struct {
	State hostInteractionRequired
}

func (e *hostInteractionError) Error() string { return "human_tty_digest_confirmation_required" }

func requireHumanPhrase(
	cmd *cobra.Command,
	phrase,
	prompt,
	exactCommand,
	digest,
	safeSummary,
	resumeAction string,
) error {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return &hostInteractionError{State: hostInteractionRequired{
			Version: hostInteractionVersion, InteractionRequired: true,
			InteractionKind: "human_tty_digest_confirmation", ExactCommand: exactCommand,
			Digest: digest, SafeSummary: safeSummary, ConfirmationPhrase: phrase,
			ResumeAction: resumeAction, TTYRequired: true, ModelSelfApproval: false,
			FormalWritesStarted: false,
		}}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), prompt)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != phrase {
		return fmt.Errorf("human_tty_digest_confirmation_not_confirmed")
	}
	return nil
}

func hostInteractionCommand(args ...string) string {
	executable, err := os.Executable()
	if err != nil {
		executable = "aoci"
	}
	parts := make([]string, 0, len(args)+1)
	if runtime.GOOS == "windows" {
		parts = append(parts, "& "+powerShellQuote(executable))
		for _, arg := range args {
			parts = append(parts, powerShellQuote(arg))
		}
		return strings.Join(parts, " ")
	}
	parts = append(parts, posixQuote(executable))
	for _, arg := range args {
		parts = append(parts, posixQuote(arg))
	}
	return strings.Join(parts, " ")
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
