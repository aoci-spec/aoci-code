// aoci hook: the hidden hook runtime subcommand (called by each agent's shell template)
// Index entry: hook.go[CHK8S]
//
// Contract: the shell template holds zero logic and only execs this command; JSON parsing and judgement are all in testable Go.
// pretool has two input channels:
//
//	--path passes the argument explicitly (the generic/test channel);
//	--stdin-json reads the agent's hook JSON from stdin (the Claude Code channel:
//	  {"tool_name":"Edit","tool_input":{"file_path":"..."}}), taking tool_name and file_path.
//
// codex-compact reads no repository state; it only prints the model context carrying a unique refresh_event_id,
// for the synchronous SessionStart(source=compact) to inject before the next model request after compaction.
//
// Exit code mapping:
//
//	pass = 0;
//	block (strict and the entry STALE) = the generic 4; with --agent claude it maps to 2 and the text goes to stderr
//	(the Claude Code contract: exit 2 = block and feed stderr back to the model).
//
// Any environment fault passes with exit 0; the hook must never jam the user's workflow because of its own fault.
package cli

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/spf13/cobra"
)

// ExitHookBlock is the generic block exit code; ExitHookBlockClaude is the Claude Code contract code
const (
	ExitHookBlock       = 4
	ExitHookBlockClaude = 2
)

var codexCompactRefreshSequence atomic.Uint64

// claudeHookInput is the minimal parse structure of the Claude Code hook stdin JSON
type claudeHookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

func init() {
	registerCommand(newHookCmd())
}

// newHookCmd returns the hook command tree. Construction and registration are separate so tests can inject stdin.
func newHookCmd() *cobra.Command {
	var agent, tool, path string
	var stdinJSON bool

	pretool := &cobra.Command{
		Use:   "pretool",
		Short: cliMessage("cli.short.hook"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A failed repository location passes directly (the hook fault-tolerance discipline)
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return nil
			}

			// The stdin-json channel: a parse failure passes (fault tolerance), success overrides tool/path
			if stdinJSON {
				// A read failure or an oversize input always passes; the hook must never jam the workflow because of its own input channel.
				data, oversize, rerr := readLimitedInput(cmd.InOrStdin(), hookInputMaxBytes)
				if rerr == nil && !oversize && len(data) > 0 {
					var in claudeHookInput
					if json.Unmarshal(data, &in) == nil {
						if in.ToolName != "" {
							tool = in.ToolName
						}
						if in.ToolInput.FilePath != "" {
							path = in.ToolInput.FilePath
						}
					}
				}
			}
			if path == "" {
				return nil // no path: nothing to do, pass
			}

			// Claude passing an absolute path is normal (its file_path is absolute); the kernel accepts only relative paths, so convert first
			path = toRepoRel(root, path)

			res := hooks.HandlePreTool(root, tool, path)
			if res.Block {
				if agent == "claude" {
					// The Claude contract: exit 2 + stderr fed back to the model
					return &ExitError{Code: ExitHookBlockClaude, Msg: res.Text}
				}
				if res.Text != "" {
					fmt.Println(res.Text)
				}
				return &ExitError{Code: ExitHookBlock}
			}
			if res.Text != "" {
				fmt.Println(res.Text)
			}
			return nil
		},
	}
	pretool.Flags().StringVar(&agent, "agent", "", cliMessage("cli.flag.hook_agent"))
	pretool.Flags().StringVar(&tool, "tool", "", cliMessage("cli.flag.hook_tool"))
	pretool.Flags().StringVar(&path, "path", "", cliMessage("cli.flag.hook_path"))
	pretool.Flags().BoolVar(&stdinJSON, "stdin-json", false, cliMessage("cli.flag.hook_stdin"))

	cmd := &cobra.Command{
		Use:    "hook",
		Short:  cliMessage("cli.short.hook_runtime"),
		Hidden: true, // not shown in human help; a machine channel
	}
	cmd.AddCommand(pretool, newCodexCompactCmd(cryptorand.Reader))
	return cmd
}

func newCodexCompactCmd(entropy io.Reader) *cobra.Command {
	return &cobra.Command{
		Use:    "codex-compact",
		Hidden: true,
		Args:   cobra.NoArgs,
		// This leaf-level hook deliberately bypasses the repository recovery
		// gate: it only emits context and must not inspect repository state.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		Run: func(cmd *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(
				cmd.OutOrStdout(),
				codexCompactDeveloperContext(
					newCodexCompactRefreshEventID(entropy),
				),
			)
		},
	}
}

func newCodexCompactRefreshEventID(entropy io.Reader) string {
	sequence := codexCompactRefreshSequence.Add(1)
	var random [16]byte
	if entropy != nil {
		if _, err := io.ReadFull(entropy, random[:]); err == nil {
			return fmt.Sprintf("aoci-compact-%x-%016x", random, sequence)
		}
	}
	return fmt.Sprintf(
		"aoci-compact-fallback-%x-%x-%016x",
		time.Now().UnixNano(),
		os.Getpid(),
		sequence,
	)
}

func codexCompactDeveloperContext(refreshEventID string) string {
	return cliMessage(
		"hook.codex_compact_context",
		refreshEventID,
		refreshEventID,
	)
}

// toRepoRel converts an absolute path under the repository root into a relative path (forward slashes); anything else is returned as it is for the kernel to judge.
// The Windows drive letter is normalized in case (C: and c: are one drive; NTFS paths are case-insensitive and the drive letter is
// the most common case drift point; a review correction: before normalization the prefix comparison of c:\repo and C:\repo mismatched,
// silently disabling the injection; only the drive letter is normalized, not the remaining segments, so really case-sensitive POSIX paths are not merged by mistake).
func toRepoRel(root, p string) string {
	if len(p) == 0 || (p[0] != '/' && !(len(p) >= 2 && p[1] == ':')) {
		return p // already a relative path
	}
	norm := func(s string) string {
		out := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' {
				out[i] = '/'
			} else {
				out[i] = s[i]
			}
		}
		// Normalize the drive letter to upper case (only when it starts like X:)
		if len(out) >= 2 && out[1] == ':' && out[0] >= 'a' && out[0] <= 'z' {
			out[0] = out[0] - 'a' + 'A'
		}
		return string(out)
	}
	np, nr := norm(p), norm(root)
	for len(nr) > 0 && nr[len(nr)-1] == '/' {
		nr = nr[:len(nr)-1]
	}
	if np == nr {
		return p
	}
	if len(np) > len(nr)+1 && np[:len(nr)] == nr && np[len(nr)] == '/' {
		return np[len(nr)+1:]
	}
	return p
}
