// The planning, validation, CAS, and atomic commit core of single-entry write-back.
// Index entry: tools_write.go[MWR9AL]
//
// Layers:
//   - prepareUpdateEntry: format, R Warning, dictionary, S quota, E band, and index transformation planning;
//   - planUpdateEntry: reads the formal index and forms the CAS plan;
//   - commitPlan: write lock, CAS, atomic write, Baseline advance, and the success Ledger.
//
// Tool registration and result rendering live in tools_write_api.go;
// the report follow-up capability lives in tools_report.go.
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aoci-spec/aoci-code/textassets"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// UpdateOutcome is the planning or application result of one target.
type UpdateOutcome struct {
	Action       string
	Rel          string
	Diff         string
	BaselineNote string
	Warnings     []string
	DryRun       bool
	// OldLine and NewLine are the exact lines the editor replaced and wrote
	// (empty OldLine for an insert); a parallel sibling's applied record
	// replays them.
	OldLine string
	NewLine string
}

func indexTextHash(
	text string,
) string {
	sum := sha256.Sum256(
		[]byte(text),
	)

	return hex.EncodeToString(
		sum[:],
	)
}

// appendWriteFailEvent records a refused or failed write. A repairable
// refusal is the candidate-validation outcome the response calls
// repair_required, not an execution error, and the event says whether formal
// writes had begun (#150). When the configuration fails to load it is skipped silently.
func appendWriteFailEvent(
	root,
	op,
	source string,
	fail *Fail,
	start time.Time,
) {
	cfg, err := config.LoadReadOnly(
		root,
	)
	if err != nil || fail == nil {
		return
	}
	event := ledger.Event{
		Op:                  op,
		DurationMs:          time.Since(start).Milliseconds(),
		Source:              source,
		Result:              resultForFailCode(fail.Code),
		FailCode:            fail.Code,
		FormalWritesStarted: ledgerFlag(fail.FormalWritesStarted),
	}
	if fail.Repairable {
		event.Result, event.GovernanceResult = ledger.ResultRepairRequired, autoStatusRepairRequired
	}
	ledger.Append(root, cfg.LedgerEnabled, event)
}

// prepareUpdateEntry plans one update on the current index context the caller supplies.
//
// Every R relation anomaly is a Warning and never blocks a manual or Auto Apply; the check only queries the few paths the candidate
// lists explicitly, without walking the repository, reading the target body, or calling a model.
func prepareUpdateEntry(
	root,
	rel,
	rawEntry string,
	rc *repoCtx,
) (*UpdateOutcome, string, *Fail) {
	if rc == nil ||
		rc.doc == nil {
		return nil, "", &Fail{
			Code: errInternal,
			Msg:  writeMessage("entry.write.context_empty"),
			Hint: writeMessage("entry.write.hint.reload"),
		}
	}

	line := index.StripFences(
		rawEntry,
	)
	if rc.cfg.CognitionBudget != nil {
		if budgetViolations := cognitionbudget.ValidateEntry(line, rc.cfg.EffectiveCognitionBudget()); len(budgetViolations) > 0 {
			// A per-field overrun is a candidate-repairable problem: a Finding carrying the location and exact token facts,
			// with the caller filling in candidate_index. When the policy itself cannot be parsed, the stop stays unrepairable.
			findings := []cognition.RepairFinding{}
			repairable := true
			for _, violation := range budgetViolations {
				if violation.Code != "entry_field_budget_exceeded" {
					repairable = false
					continue
				}
				findings = append(findings, cognition.RepairFinding{
					Path: rel, CanonicalObjectIdentity: "code:" + rel, ObjectRef: "code:" + rel,
					Domain: cognition.ScopeCode, Field: violation.Field,
					Code: violation.Code, RuleCode: violation.Code,
					Expected: fmt.Sprintf("max_tokens=%d", violation.Maximum),
					Actual:   fmt.Sprintf("actual_tokens=%d", violation.Actual),
				})
			}
			detail, _ := json.Marshal(budgetViolations)
			return nil, "", &Fail{Code: errCandidateInvalid,
				Msg:      writeMessage("entry.write.budget_failed", budgetViolations[0].Code, string(detail)),
				Hint:     writeMessage("entry.write.hint.budget_reauthor"),
				Findings: findings, Repairable: repairable && len(findings) > 0}
		}
	}

	headerText, _ := index.ExtractHeader(
		rc.text,
	)
	dictText := rc.dictionaryText(headerText)

	sQuota := index.ExtractSQuotaThresholds(
		dictText,
	)

	violations := index.ValidateEntryLineWith(
		rel,
		line,
		sQuota,
	)

	if index.HasError(
		violations,
	) {
		messages := []string{}

		for _, violation := range violations {
			messages = append(
				messages,
				"["+violation.Level+"] "+
					localeSafeWriteDetail(violation.Msg),
			)
		}

		return nil, "", &Fail{
			Code: errBadArgs,
			Msg: writeMessage(
				"entry.write.validation_failed",
				strings.Join(
					messages,
					"\n",
				),
			),
			Hint: writeMessage("entry.write.hint.validation"),
		}
	}

	warnings := []string{}

	for _, violation := range violations {
		if violation.Level ==
			index.LevelWarning {
			warnings = append(
				warnings,
				localeSafeWriteDetail(violation.Msg),
			)
		}
	}

	for _, violation := range index.ValidateEntryRelations(
		root,
		rel,
		line,
	) {
		warnings = append(
			warnings,
			localeSafeWriteDetail(violation.Msg),
		)
	}

	if dictionary := index.ExtractTagDict(
		headerText,
	); dictionary != nil {
		if violation := index.CheckTagsAgainstDict(
			line,
			dictionary,
		); violation != nil {
			// A dimension the operator declared in a spelling this parser does
			// not accept is thrown away and silently replaced by the fallback
			// dictionary, and the refusal then reports the operator's own symbol
			// as illegal against a line it discarded. Name the spelling, or the
			// message cannot be acted on at all.
			hint := writeMessage("entry.write.hint.dictionary")
			if misses := dictionary.UnrecognizedDimensionNames(); len(misses) > 0 {
				hint = writeMessage("entry.write.hint.dictionary_name_unrecognized",
					index.DisplayDimensionSpelling(misses[0].Written, textassets.ActiveLocale()),
					strings.Join(index.AcceptedDimensionSpellingsForLocale(
						misses[0].Canonical, textassets.ActiveLocale()), " / "))
			}
			return nil, "", &Fail{
				Code: errBadArgs,
				Msg: writeMessage(
					"entry.write.dictionary_failed",
					localeSafeWriteDetail(violation.Msg),
				),
				Hint: hint,
			}
		}
	}

	// In-section entries use the bare filename; a complete relative path normalizes unambiguously.
	if filenameEnd, _, _, tagFound := index.EntryTagSpan(line); tagFound && filenameEnd > 0 {
		filename := strings.TrimSpace(
			line[:filenameEnd],
		)

		if strings.Contains(
			filename,
			"/",
		) &&
			filename == rel {
			line = path.Base(rel) +
				line[filenameEnd:]

			warnings = append(warnings, writeMessage("entry.write.warning.normalized_filename"))
		}
	}

	if index.ShouldCheckEScalePath(
		rel,
	) &&
		strings.TrimSpace(
			dictText,
		) != "" {
		thresholds := index.ExtractEScaleThresholds(
			dictText,
		)

		if thresholds.HasThresholds() {
			absolutePath := filepath.Join(
				root,
				filepath.FromSlash(rel),
			)

			if stat, statErr := os.Stat(
				absolutePath,
			); statErr == nil &&
				!stat.IsDir() {
				if lines, countErr :=
					afs.CountFileLines(
						absolutePath,
					); countErr == nil {
					if mismatch := index.CheckEScaleDetail(
						line,
						lines,
						thresholds,
					); mismatch != nil {
						warnings = append(
							warnings,
							writeMessage("entry.write.warning.escale_mismatch",
								mismatch.FileLines, strings.Join(mismatch.Expected, "/"), mismatch.Actual),
						)
					}
				}
			}
		}
	}

	oldEntry := index.FindEntry(
		rc.doc,
		rel,
	)

	action := ""
	oldLine := ""
	newText := ""
	var transformErr error

	if oldEntry != nil {
		action = writeMessage("entry.write.action.replace")
		oldLine = oldEntry.FullLine

		newText, transformErr =
			index.ReplaceEntryForPath(
				rc.text,
				root,
				rel,
				oldEntry.FullLine,
				line,
			)

		if transformErr != nil {
			return nil, "", &Fail{
				Code: errWriteConflict,
				Msg:  transformErr.Error(),
				Hint: writeMessage("entry.write.hint.refresh_entry"),
			}
		}
	} else {
		action = writeMessage("entry.write.action.insert")

		newText, transformErr =
			index.InsertEntry(
				rc.text,
				rel,
				line,
				root,
			)

		var unspellable *index.DirectoryUnspellableError
		if errors.As(transformErr, &unspellable) {
			// No edit to the Entry can clear this: the directory itself has no
			// section header that reads back to the same path. Answer stopped with
			// the operator's moves, never repair_required with a retry scope:
			// resubmitting the same batch can only fail the same way.
			return nil, "", directoryUnspellableFail(root, rel, rc.paths.IndexPath, unspellable)
		}
		if transformErr != nil {
			return nil, "", &Fail{
				Code: errBadArgs,
				Msg:  transformErr.Error(),
			}
		}
	}

	return &UpdateOutcome{
		Action: action,
		Rel:    rel,
		Diff: renderEntryWriteDiff(
			oldLine,
			line,
		),
		Warnings: warnings,
		OldLine:  oldLine,
		NewLine:  line,
	}, newText, nil
}

func renderEntryWriteDiff(oldLine, newLine string) string {
	if strings.TrimSpace(oldLine) == "" {
		return "+ " + newLine + "\n" + writeMessage("entry.write.diff.insert_note") + "\n"
	}
	return index.RenderEntryDiff(
		oldLine,
		newLine,
		writeMessage("entry.write.diff_new"),
	)
}

func asManagedIndexFingerprint(state *baseline.Baseline, fingerprint baseline.Fingerprint) baseline.Fingerprint {
	if state != nil && state.ManagedScope != nil {
		fingerprint.Role = machinecontract.ScopeRoleIndex
	}
	return fingerprint
}

// directoryUnspellableFail is the one answer to a directory, or a repository
// root, that no section header can spell: the update path and Maintain's
// pre-authoring check both give it, so the model is told the same thing before
// and after it authors.
func directoryUnspellableFail(root, rel, indexPath string, unspellable *index.DirectoryUnspellableError) *Fail {
	// Message keys stay string literals: the locale contract test reads them
	// statically to prove every consumer is declared.
	ruleCode := "code_directory_unspellable"
	cause := writeMessage("entry.repair.cause.directory_unspellable", unspellable.Directory)
	action := writeMessage("entry.repair.action.directory_unspellable")
	if unspellable.Root {
		ruleCode = "code_root_unspellable"
		cause = writeMessage("entry.repair.cause.root_unspellable", unspellable.Directory)
		action = writeMessage("entry.repair.action.root_unspellable")
	}
	asset := filepath.ToSlash(indexPath)
	if relAsset, err := filepath.Rel(root, indexPath); err == nil {
		asset = filepath.ToSlash(relAsset)
	}
	return &Fail{Code: errDirectoryUnspellable, Msg: cause, Hint: action,
		Findings: []cognition.RepairFinding{{
			Path: rel, CanonicalObjectIdentity: "code:" + rel, ObjectRef: "code:" + rel,
			Domain: cognition.ScopeCode, Field: "path",
			Code: ruleCode, RuleCode: ruleCode,
			Expected: "section_header_round_trip=true", Actual: "directory=" + strconv.Quote(unspellable.Directory),
		}},
		GlobalStop: &GlobalStopFacts{
			AffectedAsset: asset, Field: "section_header", RuleCode: ruleCode,
			Expected: "section_header_round_trip=true", Actual: "directory=" + strconv.Quote(unspellable.Directory),
			Cause: cause, SafeNextAction: action,
		},
		Repairable: false}
}

// resultForFailCode maps Fail.Code to the Ledger terminal-state classification.
func resultForFailCode(
	code string,
) string {
	switch code {
	case errBadArgs,
		errPathUnsafe:
		return ledger.ResultRejected

	case errWriteConflict:
		return ledger.ResultConflict

	default:
		return ledger.ResultError
	}
}
