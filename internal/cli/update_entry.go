// aoci update-entry: the human single-entry write command (the CLI counterpart of the MCP aoci_update_entry)
// Index entry: update_entry.go[CUE7T]
//
// It shares the mcptools.ApplyUpdateEntry pipeline with MCP (validate → replace/insert → atomic write → baseline advance),
// so the write path is testable by a person without an MCP client; the ledger records source=human; --preview only renders the diff without writing.
// The exit code routes by Fail.Code (a review correction; it used to be 3 throughout, against the root.go contract):
//
//	index_invalid → 2 (the index is corrupt); internal → 10 (internal error);
//	the rest (bad_args/path_unsafe/write_conflict/not_initialized) → 3 (precondition/argument).
//
// Note: the Fail.Code string values are the stable mcptools contract (they appear on the first line of the error wording) and are compared by literal here.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

type updateEntryRepairReport struct {
	Status                  string                    `json:"status"`
	Attempted               int                       `json:"attempted"`
	Applied                 int                       `json:"applied"`
	Remaining               int                       `json:"remaining"`
	FormalWritesStarted     bool                      `json:"formal_writes_started"`
	FindingCount            int                       `json:"finding_count"`
	Findings                []cognition.RepairFinding `json:"findings"`
	PreserveOtherCandidates bool                      `json:"preserve_other_candidates"`
	RetryScope              []string                  `json:"retry_scope"`
	LocalValidation         string                    `json:"local_validation"`
	ImpactValidation        string                    `json:"impact_validation"`
	NextAction              string                    `json:"next_action"`
}

func updateEntryRepairFindings(findings []cognition.RepairFinding) []cognition.RepairFinding {
	return mcptools.LocalizeRepairFindings(findings)
}

func init() {
	registerCommand(
		newUpdateEntryCmd(),
	)
}

// newUpdateEntryCmd builds the human single-Entry write command.
func newUpdateEntryCmd() *cobra.Command {
	var path, entry, sourceSHA256 string
	var useStdin, preview bool

	cmd := &cobra.Command{
		Use:   "update-entry",
		Short: cliMessage("cli.short.update_entry"),
		Long:  updateEntryLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCLIUpdateEntryMessages(); err != nil {
				return &ExitError{Code: ExitInternal, Err: err}
			}
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if path == "" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.path_required")}
			}
			// The entry source: --entry or --stdin, two strictly exclusive input modes; silently picking one when both are given
			// would let the operator believe the other content was written.
			if useStdin && cmd.Flags().Changed("entry") {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.stdin_conflict")}
			}
			raw := entry
			if useStdin {
				data, oversize, rerr := readLimitedInput(cmd.InOrStdin(), machinecontract.EntriesRequestMaxBytes)
				if rerr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.stdin_failed", rerr)}
				}
				if oversize {
					return &ExitError{Code: ExitConfig,
						Msg: cliMessage("cli.update_entry.stdin_too_large", machinecontract.EntriesRequestMaxBytes)}
				}
				raw = string(data)
			}
			if raw == "" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.entry_empty")}
			}
			sourceSHA256 = strings.ToLower(strings.TrimSpace(sourceSHA256))
			if (!preview || sourceSHA256 != "") && !validEntrySourceSHA256(sourceSHA256) {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("cli.update_entry.source_sha_invalid")}
			}
			out, fail := mcptools.ApplyUpdateEntriesAtomic(
				root,
				[]mcptools.AtomicUpdateItem{{
					Path: path, NewEntry: raw, SourceSHA256: sourceSHA256,
				}},
				ledger.SourceHuman,
				preview,
			)
			if fail != nil {
				if flagJSON && fail.Repairable {
					findings := updateEntryRepairFindings(fail.Findings)
					report := updateEntryRepairReport{
						Status:                  entriesAutoStatusRepairRequired,
						Attempted:               1,
						Applied:                 0,
						Remaining:               1,
						FormalWritesStarted:     fail.FormalWritesStarted,
						FindingCount:            len(findings),
						Findings:                findings,
						PreserveOtherCandidates: true,
						RetryScope:              mcptools.RepairRetryScope(findings),
						LocalValidation:         "passed",
						ImpactValidation:        "failed",
						NextAction:              cliMessage("entries.auto.recovery.repair_findings"),
					}
					if err := writeEntriesJSON(cmd.OutOrStdout(), report); err != nil {
						return &ExitError{Code: ExitInternal, Err: err}
					}
					return &ExitError{Code: ExitInvalid, Msg: ""}
				}
				msg := "[" + fail.Code + "] " + fail.Msg
				if fail.Hint != "" {
					msg += "\n" + cliMessage("cli.update_entry.hint_prefix") + fail.Hint
				}
				return &ExitError{Code: exitCodeForFail(fail.Code), Msg: msg}
			}
			if !flagQuiet {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), mcptools.RenderAtomicBatchOutcome(out))
			}
			if out != nil && !out.BaselineComplete {
				return &ExitError{
					Code: ExitInternal,
					Msg:  cliMessage("cli.update_entry.baseline_incomplete"),
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", cliMessage("cli.flag.entry_path"))
	cmd.Flags().StringVar(&entry, "entry", "", cliMessage("cli.flag.entry_text"))
	cmd.Flags().StringVar(&sourceSHA256, "source-sha256", "", cliMessage("cli.flag.source_sha256"))
	cmd.Flags().BoolVar(&useStdin, "stdin", false, cliMessage("cli.flag.entry_stdin"))
	cmd.Flags().BoolVar(&preview, "preview", false, cliMessage("cli.flag.entry_preview"))
	return cmd
}

var cliUpdateEntryMessageArguments = map[string][]any{
	"cli.update_entry.path_required":        nil,
	"cli.update_entry.stdin_failed":         {fmt.Errorf("read")},
	"cli.update_entry.stdin_too_large":      {machinecontract.EntriesRequestMaxBytes},
	"cli.update_entry.stdin_conflict":       nil,
	"cli.update_entry.entry_empty":          nil,
	"cli.update_entry.source_sha_invalid":   nil,
	"cli.update_entry.hint_prefix":          nil,
	"cli.update_entry.baseline_incomplete":  nil,
	"entries.auto.recovery.repair_findings": nil,
}

func validateCLIUpdateEntryMessages() error {
	for key, arguments := range cliUpdateEntryMessageArguments {
		if _, err := textassets.Message(textassets.ActiveLocale(), key, arguments...); err != nil {
			return fmt.Errorf("update_entry_text_catalog_invalid:%s", key)
		}
	}
	return nil
}

// exitCodeForFail maps a Fail classification code to the CLI exit code contract
func exitCodeForFail(code string) int {
	switch code {
	case "index_invalid":
		return ExitInvalid // 2: the index format is corrupt
	case "internal":
		return ExitInternal // 10: internal error
	default:
		return ExitConfig // 3: precondition problems such as arguments, paths, conflicts, or not initialized
	}
}

// validEntrySourceSHA256 accepts only a complete lower-case raw SHA-256.
func validEntrySourceSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

const entriesAutoStatusRepairRequired = machinecontract.AutoStatusRepairRequired

func writeEntriesJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
