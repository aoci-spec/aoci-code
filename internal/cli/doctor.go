// Index entry: doctor.go[CDR8M]
// Responsibility: aoci doctor, the READ-ONLY diagnosis of the environment and the integrations. Four groups of checks, each item in the three states ✓/✗/–, with a summary at the end.
//
// Design principles:
//   - zero outbound traffic by default: endpoint connectivity is probed only when --net is given explicitly (the same reading as the R19 data sovereignty rule);
//   - lightweight: no whole-repository hashing (drift details point at aoci verify, the same discipline as status's "millisecond pure read");
//   - a single source of truth for criteria: group two's integration criteria always call hooks.IsXxxInstalled, shared with the installer and never duplicated;
//   - key safety: configToLLMOptions stays the single key reader, and the status reports only presence, never the value (R19);
//   - exit code: all passed 0 / failing items 1 / not even the repository root found 3 (aligned with the verify style).
//
// Technical artifact exclusion missing-item hint (the second half of R60-F.9-A2, 2026-07-18):
// the DefaultConfig exclusion extension only rescues new repositories; an explicit exclude_dirs already on disk freezes
// the old defaults (applyFallbacks fills only nil, and keeping an explicit list is existing discipline). An existing repository
// migrates through the doctor informational hint: when the team config.json declares exclude_dirs explicitly and
// lacks current default technical artifact items, the missing items are listed for the maintainer to decide whether to add; zero silent configuration changes
// (isomorphic with the "no silent correction" discipline). The judgement must read the raw team-layer JSON to see whether the key is explicitly
// present; the merged state of Load has already filled the defaults when the field is missing, and a direct comparison would misreport
// a repository that "follows the defaults".
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

// checkState is the three-state result of one check item.
type checkState int

const (
	statePass checkState = iota // passed
	stateFail                   // failed (counts toward a non-zero exit code)
	stateNA                     // not applicable (does not affect the exit code)
	stateInfo                   // pure information (does not affect the exit code, such as the data sovereignty hint line)
)

// stateMark returns the display prefix of a state.
func stateMark(s checkState) string {
	switch s {
	case statePass:
		return "[✓]"
	case stateFail:
		return "[✗]"
	case stateNA:
		return "[–]"
	default:
		return "[·]"
	}
}

// doctorReport accumulates check results and counts failures, for the final summary and the exit code.
type doctorReport struct {
	out      io.Writer
	failures int
}

// line writes one check result line; stateFail increments the failure count.
func (r *doctorReport) line(s checkState, label, detail string) {
	if detail != "" {
		fmt.Fprintf(r.out, "%s %s: %s\n", stateMark(s), label, detail)
	} else {
		fmt.Fprintf(r.out, "%s %s\n", stateMark(s), label)
	}
	if s == stateFail {
		r.failures++
	}
}

// group writes a group title.
func (r *doctorReport) group(title string) {
	fmt.Fprintf(r.out, "\n== %s ==\n", title)
}

// missingTechnicalExcludeDirs judges whether the exclude_dirs declared explicitly in the team config.json
// lacks current default technical artifact items (the single judgement function of the A2 existing-repository migration hint).
//
// It returns the list of missing items; the following cases all return empty (no hint):
//   - config.json is missing or unparseable (the other checks already report that, no duplication);
//   - the exclude_dirs key is not declared explicitly (Load fills the current defaults, so there is nothing missing);
//   - the explicit list already covers every current default item.
//
// The judgement reads the raw team-layer JSON rather than the merged state of Load; the merged state cannot tell "an explicit short list"
// from "missing and filled", and a direct comparison would misreport the latter.
func missingTechnicalExcludeDirs(root string) []string {
	data, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		return nil
	}

	var raw struct {
		ExcludeDirs     *[]string `json:"exclude_dirs"`
		ExcludeRootDirs []string  `json:"exclude_root_dirs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if raw.ExcludeDirs == nil {
		return nil // the key is not declared explicitly, follows the current defaults, nothing missing
	}

	declared := map[string]bool{}
	for _, d := range *raw.ExcludeDirs {
		declared[d] = true
	}
	for _, d := range raw.ExcludeRootDirs {
		declared[d] = true
	}

	var missing []string
	for _, want := range config.DefaultTechnicalExcludeDirs() {
		if !declared[want] {
			missing = append(missing, want)
		}
	}
	return missing
}

// newDoctorCmd builds the `aoci doctor` command.
func newDoctorCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: cliMessage("cli.short.doctor"),
		Long:  doctorLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep := &doctorReport{out: cmd.OutOrStdout()}
			fmt.Fprintln(rep.out, cliMessage("doctor.title"))

			// —— Group one: the repository and the core assets ——
			rep.group(cliMessage("doctor.group.repository"))

			root, err := resolveRepoRoot()
			if err != nil {
				rep.line(stateFail, cliMessage("doctor.label.repo_root"), cliMessage("doctor.repo_missing"))
				fmt.Fprintf(rep.out, "\n%s\n", cliMessage("doctor.aborted"))
				// Not even the repository root found → exit code 3
				return &doctorExitError{code: 3}
			}
			rep.line(statePass, cliMessage("doctor.label.repo_root"), root)

			// Configuration load
			cfg, cfgErr := config.Load(root)
			if cfgErr != nil {
				rep.line(stateFail, cliMessage("doctor.label.config"), cliMessage("doctor.config_failed", cfgErr))
				// With a broken configuration the later AI checks are moot, but the repository/index checks go on, so the defaults stand in
				cfg = config.DefaultConfig()
			} else {
				rep.line(statePass, cliMessage("doctor.label.config"), cliMessage("doctor.config_ok", cfg.IndexPath))
			}

			// Technical artifact exclusion missing items (the A2 existing-repository migration hint; informational, zero silent changes)
			if missing := missingTechnicalExcludeDirs(root); len(missing) > 0 {
				rep.line(stateInfo, cliMessage("doctor.label.exclude_gaps"),
					cliMessage("doctor.exclude_gaps", strings.Join(missing, ", ")))
			}

			// The sensitive filename rule set (#148): an old repository defaults to v1. List the indexed paths v2 would move out of the index,
			// informational, zero silent changes; opting in is one governance Scope Change the maintainer starts.
			if cfgErr == nil && afs.NormalizeSensitiveNameRules(cfg.SensitiveNameRules) != afs.SensitiveNameRulesV2 {
				if candidates := sensitiveNameUpgradeCandidates(root); len(candidates) > 0 {
					rep.line(stateInfo, cliMessage("doctor.label.sensitive_name_rules"),
						cliMessage("doctor.sensitive_name_rules", afs.NormalizeSensitiveNameRules(cfg.SensitiveNameRules),
							len(candidates), previewPaths(candidates, 5)))
				}
			}

			// aoci.txt parsing
			indexPath := cfg.IndexPath
			if indexPath == "" {
				indexPath = "aoci.txt"
			}
			cognitionSet, readErr := cognition.Load(root, indexPath)
			if readErr != nil {
				rep.line(stateFail, cliMessage("doctor.label.index_file"), cliMessage("doctor.index_read_failed", indexPath, readErr))
			} else {
				sectionCount := 0
				entryCount := cognitionSet.Root.ObjectCount
				if cognitionSet.LayoutMode == cognition.LayoutVolumesV1 {
					sectionCount = len(cognitionSet.DeclaredOrder)
					entryCount = volumeObjectCount(cognitionSet)
				} else {
					sectionCount = len(cognitionSet.Root.Document.Sections)
				}
				if sectionCount == 0 {
					rep.line(stateFail, cliMessage("doctor.label.index_parse"), cliMessage("doctor.index_empty", indexPath))
				} else {
					detail := cliMessage("doctor.index_counts", sectionCount, entryCount, len(cognitionSet.Warnings))
					if len(cognitionSet.Warnings) > 0 {
						rep.line(statePass, cliMessage("doctor.label.index_parse"), detail+cliMessage("doctor.index_warning_suffix"))
					} else {
						rep.line(statePass, cliMessage("doctor.label.index_parse"), detail)
					}
				}
			}

			// baseline existence (the second return value bool = whether it was found)
			_, blExists, blErr := baseline.Load(root)
			switch {
			case blErr != nil:
				rep.line(stateFail, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_read_failed", blErr))
			case !blExists:
				rep.line(stateFail, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_missing"))
			default:
				rep.line(statePass, cliMessage("doctor.label.baseline"), cliMessage("doctor.baseline_exists"))
			}

			// Drift: not computed here, pointing at verify (staying lightweight)
			rep.line(stateInfo, cliMessage("doctor.label.drift"), cliMessage("doctor.drift_deferred"))

			// —— Group two: Agent integrations ——
			rep.group(cliMessage("doctor.group.agents"))

			detected := hooks.Detect(root)
			if len(detected) == 0 {
				rep.line(stateInfo, cliMessage("doctor.label.detected_agents"), cliMessage("doctor.no_agents"))
			} else {
				rep.line(stateInfo, cliMessage("doctor.label.detected_agents"), fmt.Sprintf("%v", detected))
			}

			// The criteria always call hooks.IsXxxInstalled (the single source of truth shared with the installer)
			markInstalled(rep, cliMessage("doctor.label.agents_block"), hooks.IsAgentsBlockPresent(root))
			markInstalled(rep, cliMessage("doctor.label.claude_mcp"), hooks.IsClaudeMCPInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.claude_hook"), hooks.IsClaudeHookInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.codex_mcp"), hooks.IsCodexMCPInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.opencode_mcp"), hooks.IsOpenCodeMCPInstalled(root))
			markInstalled(rep, cliMessage("doctor.label.qoder_mcp"), hooks.IsClaudeMCPInstalled(root))

			// —— Group four: platform features ——
			rep.group(cliMessage("doctor.group.platform"))
			if runtime.GOOS == "windows" {
				rep.line(stateInfo, cliMessage("doctor.label.platform"), cliMessage("doctor.platform_windows"))
			} else {
				rep.line(stateInfo, cliMessage("doctor.label.platform"), runtime.GOOS)
			}

			// —— Summary and exit code ——
			fmt.Fprintln(rep.out, "")
			if rep.failures == 0 {
				fmt.Fprintln(rep.out, cliMessage("doctor.complete"))
				return nil
			}
			fmt.Fprintln(rep.out, cliMessage("doctor.failed", rep.failures))
			return &doctorExitError{code: 1}
		},
	}

	return cmd
}

// markInstalled prints "configured/not configured" by the criterion boolean ("not configured" is informational rather than a failure:
// not integrating some agent is a normal choice and should not count as a diagnostic failure).
func markInstalled(rep *doctorReport, label string, installed bool) {
	if installed {
		rep.line(statePass, label, cliMessage("doctor.installed"))
	} else {
		rep.line(stateInfo, label, cliMessage("doctor.not_installed"))
	}
}

// doctorExitError carries doctor's exit code, mapped to the process exit code by Execute in root.go.
type doctorExitError struct{ code int }

func (e *doctorExitError) Error() string {
	return cliMessage("doctor.exit_error", e.code)
}

// ExitCode is read by the exit code mapping.
func (e *doctorExitError) ExitCode() int { return e.code }

// sensitiveNameUpgradeCandidates lists, sorted, the Baseline index-role paths
// the v2 sensitive-name rule set would exclude (#148). It reads only the
// Baseline: no content and no inventory walk, so doctor stays light.
func sensitiveNameUpgradeCandidates(root string) []string {
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		return nil
	}
	paths := make([]string, 0, len(state.Files))
	for path, fingerprint := range state.Files {
		if fingerprint.Role == "" || fingerprint.Role == machinecontract.ScopeRoleIndex {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return afs.SensitiveNameUpgradeCandidates(paths)
}

// previewPaths keeps a doctor line readable on a large repository: the first
// limit paths and an ellipsis, with the count printed beside them.
func previewPaths(paths []string, limit int) string {
	if len(paths) <= limit {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:limit], ", ") + ", ..."
}
