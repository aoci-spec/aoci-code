// entries check共用执行内核。
//
// 人工CLI与automation.review/auto必须复用同一份机器校验、草稿快照和
// ReviewRecord写入逻辑，不能复制第二套格式、R关系、字典、S配额或E档位判据。
//
// R关系只执行轻量事实检查并形成Warning：不存在、目录、自引用或格式异常
// 均不得阻断Check或Auto Apply。真正的候选格式和字典错误仍按硬拒处理。
//
// 标签不可解析的兼容分层：
//   - 人工来源保持Warning，允许维护存量非标索引；
//   - agent或cli_ai新生成候选必须可解析，否则转为tagparse拒绝并由Auto修复。
//
// 本内核不依赖Cobra，也不决定命令退出码：
//   - 格式、字典或自动生成标签盲区通过Review.Rejected和Items返回；
//   - R关系、S配额、E档位及其他柔性问题通过Review.Warned返回；
//   - Manifest、快照或审计故障才返回error；
//   - Snapshot是产生Review.DraftHash并参与校验的同一轮内存内容。
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

// entriesCheckResult是一次机器预检的结构化内核结果。
type entriesCheckResult struct {
	RunID    string
	Manifest *draft.Manifest
	Snapshot *entryDraftSnapshot
	Review   draft.ReviewRecord
	Items    []entriesCheckItem
}

// generatedSourceRequiresParseableTags判断当前来源是否属于无存量兼容负担的新生成。
func generatedSourceRequiresParseableTags(
	source string,
) bool {
	return source == ledger.SourceAgent ||
		source == ledger.SourceCLIAI
}

// generatedTagParseErrors复用index.HasTagParseWarning分类既有Violation。
//
// 本函数不重新解析标签，也不复制稳定文案前缀；标签判据继续只有Validator一份。
func generatedTagParseErrors(
	violations []index.Violation,
) []entriesFinding {
	findings := []entriesFinding{}

	for _, violation := range violations {
		if !index.HasTagParseWarning(
			[]index.Violation{
				violation,
			},
		) {
			continue
		}

		findings = append(
			findings,
			entriesFinding{
				Code:    "tagparse",
				Message: localeSafeCLIDetail(violation.Msg),
			},
		)
	}

	return findings
}

// runEntriesCheckCore对固定Entries Run执行机器预检并追加审计。
func runEntriesCheckCore(
	repoRoot string,
	runID string,
	cfg *config.Config,
	doc *index.Document,
	out io.Writer,
	source string,
) (*entriesCheckResult, error) {
	start := time.Now()

	if cfg == nil {
		return nil, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.check.config_empty")),
		}
	}
	if doc == nil {
		return nil, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.check.index_empty")),
		}
	}
	if out == nil {
		out = io.Discard
	}
	if source == "" {
		source = ledger.SourceHuman
	}

	manifest, err := draft.LoadManifest(
		repoRoot,
		runID,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	snapshot, err := loadEntryDraftSnapshot(
		repoRoot,
		runID,
		manifest,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.check.snapshot_read_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	headerText, _ := index.ExtractHeader(
		doc.RawText,
	)
	dict := index.ExtractTagDict(
		headerText,
	)
	eScaleThresholds := index.ExtractEScaleThresholds(
		headerText,
	)
	sQuotaThresholds := index.ExtractSQuotaThresholds(
		headerText,
	)

	passed := 0
	warned := 0
	rejected := 0
	skipped := 0
	items := make(
		[]entriesCheckItem,
		0,
		len(manifest.Entries),
	)

	for _, status := range manifest.Entries {
		item := entriesCheckItem{
			Path:             status.Path,
			GenerationStatus: status.Status,
			Note:             status.Note,
			Errors:           []entriesFinding{},
			Warnings:         []entriesFinding{},
		}

		if status.Status != "drafted" &&
			status.Status != "warned" {
			item.Outcome = "skipped"
			skipped++
			items = append(
				items,
				item,
			)
			continue
		}

		line, lineErr := snapshot.line(
			status.Path,
		)
		if lineErr != nil {
			item.Outcome = "rejected"
			item.Errors = append(
				item.Errors,
				entriesFinding{
					Code:    "draft",
					Message: localeSafeCLIDetail(lineErr.Error()),
				},
			)
			rejected++
			items = append(
				items,
				item,
			)
			continue
		}

		line = index.StripFences(
			line,
		)
		if localeMigrationContainsOrdinaryEntry(cfg, status.Path) {
			oldEntry := index.FindEntry(doc, status.Path)
			if oldEntry == nil {
				item.Outcome = "rejected"
				item.Errors = append(item.Errors, entriesFinding{
					Code: "locale_protected_facts", Message: cliMessage("entries.check.locale_entry_missing"),
				})
				rejected++
				items = append(items, item)
				continue
			}
			sourceEvidence, sourceErr := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(status.Path)))
			if sourceErr != nil {
				item.Outcome = "rejected"
				item.Errors = append(item.Errors, entriesFinding{
					Code: "locale_protected_facts", Message: cliMessage(
						"entries.check.locale_source_read_failed",
						localeSafeCLIDetail(sourceErr.Error()),
					),
				})
				rejected++
				items = append(items, item)
				continue
			}
			if protectedErr := validateLocaleEntryProtectedFactsWithEvidence(oldEntry.FullLine, line, sourceEvidence); protectedErr != nil {
				item.Outcome = "rejected"
				item.Errors = append(item.Errors, entriesFinding{
					Code: "locale_protected_facts", Message: localeSafeCLIDetail(protectedErr.Error()),
				})
				rejected++
				items = append(items, item)
				continue
			}
		}

		violations := index.ValidateEntryLineWith(
			status.Path,
			line,
			sQuotaThresholds,
		)
		if index.HasError(
			violations,
		) {
			item.Outcome = "rejected"
			for _, violation := range violations {
				if violation.Level != index.LevelError {
					continue
				}
				item.Errors = append(
					item.Errors,
					entriesFinding{
						Code:    "format",
						Message: localeSafeCLIDetail(violation.Msg),
					},
				)
			}
			rejected++
			items = append(
				items,
				item,
			)
			continue
		}

		if generatedSourceRequiresParseableTags(
			source,
		) {
			tagParseFindings :=
				generatedTagParseErrors(
					violations,
				)

			if len(tagParseFindings) > 0 {
				item.Outcome = "rejected"
				item.Errors = append(
					item.Errors,
					tagParseFindings...,
				)
				rejected++
				items = append(
					items,
					item,
				)
				continue
			}
		}

		if violation := index.CheckTagsAgainstDict(
			line,
			dict,
		); violation != nil {
			item.Outcome = "rejected"
			item.Errors = append(
				item.Errors,
				entriesFinding{
					Code:    "dict",
					Message: localeSafeCLIDetail(violation.Msg),
				},
			)
			rejected++
			items = append(
				items,
				item,
			)
			continue
		}

		for _, violation := range violations {
			if violation.Level != index.LevelWarning {
				continue
			}
			item.Warnings = append(
				item.Warnings,
				entriesFinding{
					Code:    "warning",
					Message: localeSafeCLIDetail(violation.Msg),
				},
			)
		}

		// R关系检查只查询候选明确列出的少量路径，不遍历仓库。
		// 所有异常均进入relation Warning，绝不改变Auto Apply资格。
		for _, violation := range index.ValidateEntryRelations(
			repoRoot,
			status.Path,
			line,
		) {
			item.Warnings = append(
				item.Warnings,
				entriesFinding{
					Code:    "relation",
					Message: localeSafeCLIDetail(violation.Msg),
				},
			)
		}

		// E档位路径边界必须与Score和prepareUpdateEntry同源。
		if eScaleThresholds.HasThresholds() &&
			index.ShouldCheckEScalePath(
				status.Path,
			) {
			absolutePath := filepath.Join(
				repoRoot,
				filepath.FromSlash(
					status.Path,
				),
			)
			if stat, statErr := os.Stat(
				absolutePath,
			); statErr == nil &&
				!stat.IsDir() {
				if lineCount, countErr :=
					afs.CountFileLines(
						absolutePath,
					); countErr == nil {
					if violation := index.CheckEScale(
						line,
						lineCount,
						eScaleThresholds,
					); violation != nil {
						item.Warnings = append(
							item.Warnings,
							entriesFinding{
								Code:    "escale",
								Message: localeSafeCLIDetail(violation.Msg),
							},
						)
					}
				}
			}
		}

		passed++
		if len(item.Warnings) > 0 {
			item.Outcome = "warned"
			warned++
		} else {
			item.Outcome = "passed"
		}

		items = append(
			items,
			item,
		)
	}

	review := draft.ReviewRecord{
		At:         time.Now().UTC().Format(time.RFC3339),
		Action:     draft.ReviewActionCheck,
		DraftHash:  snapshot.Hash,
		PathsCount: len(manifest.Entries),
		Passed:     passed,
		Warned:     warned,
		Rejected:   rejected,
		Skipped:    skipped,
	}

	result := &entriesCheckResult{
		RunID:    runID,
		Manifest: manifest,
		Snapshot: snapshot,
		Review:   review,
		Items:    items,
	}

	renderEntriesCheckHuman(
		out,
		result,
	)

	if err := draft.AppendReview(
		repoRoot,
		runID,
		review,
	); err != nil {
		return nil, &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.check.audit_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	fmt.Fprint(
		out,
		cliMessage("entries.check.audit_record", shortDraftHash(snapshot.Hash)),
	)

	eventResult := ledger.ResultOK
	if rejected > 0 ||
		skipped > 0 {
		eventResult =
			ledger.ResultRepairRequired
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:            "entries_check",
			Source:        source,
			PathsCount:    len(manifest.Entries),
			DurationMs:    time.Since(start).Milliseconds(),
			DraftRunID:    runID,
			Result:        eventResult,
			WarningsCount: warned,
			PassedCount:   passed,
			WarnedCount:   warned,
			RejectedCount: rejected,
			SkippedCount:  skipped,
		},
	)

	return result, nil
}

// renderEntriesCheckHuman保持既有人读格式。
func renderEntriesCheckHuman(
	out io.Writer,
	result *entriesCheckResult,
) {
	if result == nil ||
		result.Manifest == nil ||
		result.Snapshot == nil {
		return
	}

	fmt.Fprint(
		out,
		cliMessage("entries.check.heading", result.RunID, len(result.Manifest.Entries)),
	)
	fmt.Fprint(
		out,
		cliMessage("entries.check.draft_hash", shortDraftHash(result.Snapshot.Hash)),
	)
	fmt.Fprintln(
		out,
		"──────────────────────────────",
	)

	for _, item := range result.Items {
		switch item.Outcome {
		case "rejected":
			for _, finding := range item.Errors {
				if finding.Code == "draft" {
					fmt.Fprintf(
						out,
						"✗ %s —— %s\n",
						item.Path,
						finding.Message,
					)
					continue
				}
				fmt.Fprintf(
					out,
					"✗ %s —— [%s] %s\n",
					item.Path,
					finding.Code,
					finding.Message,
				)
			}

		case "warned":
			for _, finding := range item.Warnings {
				fmt.Fprintf(
					out,
					"⚠ %s —— [%s] %s\n",
					item.Path,
					finding.Code,
					finding.Message,
				)
			}

		case "passed":
			fmt.Fprintf(
				out,
				"✓ %s\n",
				item.Path,
			)
		}
	}

	fmt.Fprintln(
		out,
		"──────────────────────────────",
	)
	fmt.Fprint(
		out,
		cliMessage(
			"entries.check.summary",
			result.Review.Passed,
			result.Review.Warned,
			result.Review.Rejected,
			result.Review.Skipped,
		),
	)
}
