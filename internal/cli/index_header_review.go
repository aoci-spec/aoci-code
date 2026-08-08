// 本文件承载Header审阅阶段的确定性能力:
//   - R52 Run一致性判据;
//   - Header草稿同轮内容快照;
//   - Header P-23内容摘要核对;
//   - Header Diff人读和结构化JSON报告。
//
// 两层一致性:
//   - R52核对审阅与应用是否选择同一个Run;
//   - Header P-23核对同一Run中被审阅的header.txt是否仍是应用时内容。
//
// Diff先完成Manifest核验与Review追加，再输出成功报告。JSON模式不得在审计
// 失败前输出部分Diff，否则根层将无法返回单一有效错误对象。
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/spf13/cobra"
)

// lastReviewedRunID从Ledger取最近一次审阅类工序的DraftRunID。
//
// Header认header_diff；Entries认entries_diff和entries_check。
// 无记录时返回ok=false，调用方按兼容策略警告放行。
func lastReviewedRunID(
	root string,
	operations ...string,
) (string, bool) {
	events, _ := ledger.Recent(
		root,
		200,
	)

	for position := len(events) - 1; position >= 0; position-- {
		for _, operation := range operations {
			if events[position].Op == operation &&
				events[position].DraftRunID != "" {
				return events[position].DraftRunID, true
			}
		}
	}

	return "", false
}

// guardImplicitApply是R52 Run一致性防线。
func guardImplicitApply(
	root,
	runID string,
	explicit bool,
	applyCommand string,
	reviewOperations ...string,
) (string, error) {
	if explicit {
		return "", nil
	}

	reviewedRunID, found := lastReviewedRunID(
		root,
		reviewOperations...,
	)
	if !found {
		return cliMessage("governance.review.none"), nil
	}

	if reviewedRunID != runID {
		return "", fmt.Errorf("%s", cliMessage(
			"governance.review.run_drift",
			reviewedRunID,
			runID,
			applyCommand,
		))
	}

	return "", nil
}

// headerDraftSnapshot是一次Header草稿内存快照。
type headerDraftSnapshot struct {
	Hash             string
	Text             string
	ManagedIndexText string
	Intent           string
}

// loadHeaderDraftSnapshot一次读取header.txt并计算稳定摘要。
func loadHeaderDraftSnapshot(
	root,
	runID string,
) (*headerDraftSnapshot, error) {
	fileNames := []string{draft.HeaderFileName}
	runDir, pathErr := draft.RunDir(root, runID)
	if pathErr != nil {
		return nil, pathErr
	}
	localeIndexPath := filepath.Join(runDir, draft.LocaleIndexFileName)
	if _, statErr := os.Stat(localeIndexPath); statErr == nil {
		fileNames = append(fileNames, draft.LocaleIndexFileName)
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	intentPath := filepath.Join(runDir, draft.HeaderIntentFileName)
	if _, statErr := os.Stat(intentPath); statErr == nil {
		fileNames = append(fileNames, draft.HeaderIntentFileName)
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	files, hash, err := draft.ReadFilesSnapshot(
		root,
		runID,
		fileNames,
	)
	if err != nil {
		return nil, err
	}

	data, ok := files[draft.HeaderFileName]
	if !ok {
		return nil, fmt.Errorf("%s", cliMessage(
			"header.snapshot.missing_file",
			draft.HeaderFileName,
		))
	}
	intent := ""
	if intentData, ok := files[draft.HeaderIntentFileName]; ok {
		intent = strings.TrimSuffix(string(intentData), "\n")
		if intent != agentHeaderStageIntentSemanticRefresh ||
			string(intentData) != intent+"\n" {
			return nil, fmt.Errorf("%s", cliMessage(
				"header.stage.intent_invalid",
				intent,
			))
		}
	}

	return &headerDraftSnapshot{
		Hash:             hash,
		Text:             string(data),
		ManagedIndexText: string(files[draft.LocaleIndexFileName]),
		Intent:           intent,
	}, nil
}

// latestHeaderReview返回最近一次Header Diff内容审阅记录。
func latestHeaderReview(
	manifest *draft.Manifest,
) (draft.ReviewRecord, bool) {
	if manifest == nil {
		return draft.ReviewRecord{}, false
	}

	for position := len(manifest.Reviews) - 1; position >= 0; position-- {
		review := manifest.Reviews[position]
		if review.Action == draft.ReviewActionDiff {
			return review, true
		}
	}

	return draft.ReviewRecord{}, false
}

// guardReviewedHeaderHash核对最近Header Diff与当前草稿是否为同一内容版本。
func guardReviewedHeaderHash(
	manifest *draft.Manifest,
	currentHash string,
) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("%s", cliMessage("header.review.manifest_empty"))
	}
	if manifest.Kind != draft.KindHeader {
		return "", fmt.Errorf("%s", cliMessage(
			"header.review.kind_mismatch",
			manifest.Kind,
			draft.KindHeader,
		))
	}

	review, found := latestHeaderReview(
		manifest,
	)
	if !found {
		return cliMessage("header.review.legacy_missing"), nil
	}

	if review.DraftHash == "" {
		return "", fmt.Errorf("%s", cliMessage("header.review.hash_missing"))
	}
	if currentHash == "" {
		return "", fmt.Errorf("%s", cliMessage("header.review.current_hash_empty"))
	}
	if review.DraftHash != currentHash {
		return "", fmt.Errorf("%s", cliMessage(
			"header.review.hash_drift",
			shortHeaderDraftHash(review.DraftHash),
			shortHeaderDraftHash(currentHash),
		))
	}

	return "", nil
}

// shortHeaderDraftHash缩短摘要，仅用于人读输出。
func shortHeaderDraftHash(
	hash string,
) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16]
}

// buildHeaderDiffReport从同一份Header快照构造人读和JSON共享报告。
func buildHeaderDiffReport(
	runID,
	currentIndex string,
	snapshot *headerDraftSnapshot,
) headerDiffReport {
	currentHeader, _ := index.ExtractHeader(currentIndex)
	change := "update"

	switch {
	case currentHeader == snapshot.Text:
		change = "unchanged"
	case currentHeader == "":
		change = "create"
	}

	report := headerDiffReport{
		Version:       governanceDiffReportVersion,
		OK:            true,
		RunID:         runID,
		DraftHash:     snapshot.Hash,
		Change:        change,
		CurrentHeader: currentHeader,
		DraftHeader:   snapshot.Text,
		DiffText: index.RenderHeaderDiff(
			currentHeader,
			snapshot.Text,
		),
		Warnings:    []string{},
		NextCommand: "aoci index header apply " + runID,
	}
	if snapshot.ManagedIndexText != "" {
		report.ManagedIndexCandidate = true
		report.CurrentIndex = currentIndex
		report.DraftIndex = snapshot.ManagedIndexText
		report.ManagedDiffText = index.RenderHeaderDiff(currentIndex, snapshot.ManagedIndexText)
	}
	return report
}

// renderHeaderDiffHuman保持既有人读输出结构。
func renderHeaderDiffHuman(
	output io.Writer,
	report headerDiffReport,
) {
	fmt.Fprint(output, cliMessage("header.diff.title", report.RunID))
	fmt.Fprint(output, cliMessage(
		"header.diff.hash",
		shortHeaderDraftHash(report.DraftHash),
	))
	fmt.Fprintln(
		output,
		"──────────────────────────────",
	)
	if report.ManagedIndexCandidate {
		fmt.Fprintln(output, cliMessage("locale.diff_managed_index"))
	}
	fmt.Fprint(
		output,
		report.DiffText,
	)
	if report.ManagedIndexCandidate {
		fmt.Fprint(output, report.ManagedDiffText)
	}
	fmt.Fprintln(
		output,
		"──────────────────────────────",
	)

	if report.ReviewRecorded {
		fmt.Fprint(output, cliMessage(
			"header.diff.audit",
			shortHeaderDraftHash(report.DraftHash),
		))
	}
	for _, warning := range report.Warnings {
		fmt.Fprintln(
			output,
			warning,
		)
	}

	fmt.Fprint(output, cliMessage("header.diff.apply_next", report.NextCommand))
}

// newHeaderDiffCmd构造`aoci index header diff [run_id]`。
func newHeaderDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [run_id]",
		Short: cliMessage("cli.short.header_diff"),
		Args:  cobra.MaximumNArgs(1),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			start := time.Now()

			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			cfg, err := config.Load(
				repoRoot,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			runID, err := resolveHeaderRunID(
				repoRoot,
				args,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			snapshot, err := loadHeaderDraftSnapshot(
				repoRoot,
				runID,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"header.snapshot.read_failed",
						runID,
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			doc, _, err := loadIndexForCLI(
				cmd,
				repoRoot,
				cfg,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			report := buildHeaderDiffReport(
				runID,
				doc.RawText,
				snapshot,
			)

			manifest, manifestErr := draft.LoadManifest(
				repoRoot,
				runID,
			)
			switch {
			case manifestErr == nil:
				if manifest.Kind != draft.KindHeader {
					return &ExitError{
						Code: ExitConfig,
						Err: fmt.Errorf("%s", cliMessage(
							"header.manifest.kind_mismatch",
							runID,
							manifest.Kind,
							draft.KindHeader,
						)),
					}
				}

				if err := draft.AppendReview(
					repoRoot,
					runID,
					draft.ReviewRecord{
						Action:     draft.ReviewActionDiff,
						DraftHash:  snapshot.Hash,
						PathsCount: 1,
						Passed:     1,
					},
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"header.diff.audit_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}

				report.ManifestPresent = true
				report.ReviewRecorded = true

			case errors.Is(
				manifestErr,
				os.ErrNotExist,
			):
				report.LegacyCompatibility = true
				report.Warnings = append(report.Warnings, cliMessage("header.diff.legacy"))

			default:
				return &ExitError{
					Code: ExitConfig,
					Err: fmt.Errorf("%s", cliMessage(
						"header.manifest.read_failed",
						runID,
						localeSafeCLIDetail(manifestErr.Error()),
					)),
				}
			}

			ledger.Append(
				repoRoot,
				cfg.LedgerEnabled,
				ledger.Event{
					Op:         "header_diff",
					Source:     ledger.SourceHuman,
					PathsCount: 1,
					DurationMs: time.Since(start).Milliseconds(),
					DraftRunID: runID,
				},
			)

			if flagJSON {
				if err := writeGovernanceDiffJSON(
					cmd.OutOrStdout(),
					report,
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"header.diff.json_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}
				return nil
			}

			renderHeaderDiffHuman(
				cmd.OutOrStdout(),
				report,
			)
			return nil
		},
	}
}
