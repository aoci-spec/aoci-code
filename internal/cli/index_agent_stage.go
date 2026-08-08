// `aoci index agent stage`把Codex、Claude Code等宿主Agent生成的条目候选
// 接入标准Draft/Review/Application治理链。
//
// Stage阶段只负责：
//   - 读取UTF-8请求；
//   - automation权限硬闸；
//   - 当前Plan、目标路径与源码摘要核对；
//   - 创建标准Entries Draft、Manifest和Stage Ledger。
//
// R65新增行为：
// automation.mode=auto时，Stage草稿安全提交后，由外层命令继续执行
// Generation Plan复核、Check、Diff审计和原子Apply。任何失败均保留草稿Run。
//
// review和legacy仍只Stage并返回Check命令；off仍在创建Run前拒绝。
// Stage本身不调用模型或访问网络。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

func newIndexAgentStageCmd() *cobra.Command {
	var stdinJSON bool
	var requestFile string
	var agentName string

	command := &cobra.Command{
		Use:   "stage",
		Short: cliMessage("cli.short.agent_stage"),
		Args:  cobra.NoArgs,
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			reader, source, err := loadAgentRequestInput(
				stdinJSON,
				requestFile,
				cmd.InOrStdin(),
				machinecontract.EntriesRequestMaxBytes,
				"Entries Stage",
			)
			if err != nil {
				var inputErr *agentRequestInputError
				if errors.As(
					err,
					&inputErr,
				) {
					return &ExitError{
						Code: inputErr.Code,
						Err:  inputErr.Err,
					}
				}

				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			request, err := readAgentStageRequest(
				reader,
			)
			if err != nil {
				return &ExitError{
					Code: ExitInvalid,
					Err: fmt.Errorf("%s", cliMessage(
						"agent.input.invalid",
						source,
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}
			if agentName != "" && agentName != request.Agent {
				return &ExitError{Code: ExitInvalid, MachineCode: "transport_schema_invalid", Err: fmt.Errorf("agent_flag_request_mismatch")}
			}

			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Err:  err,
				}
			}

			doc, indexPath, err := loadIndexForCLI(
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

			result, err := stageAgentEntries(
				repoRoot,
				cfg,
				doc,
				indexPath,
				request,
			)
			if err != nil {
				var stageErr *agentStageError
				if errors.As(
					err,
					&stageErr,
				) {
					return &ExitError{
						Code: stageErr.Code,
						Err:  stageErr.Err,
					}
				}

				return &ExitError{
					Code: ExitInternal,
					Err:  err,
				}
			}

			autoErr := finalizeHostAgentEntriesStageAuto(
				cmd,
				repoRoot,
				cfg,
				doc,
				result,
			)
			if autoErr != nil &&
				result.AutoFinalize != nil {
				setEntriesAutoFinalizeError(
					result.AutoFinalize,
					autoErr,
				)
			}

			if flagJSON {
				encoder := json.NewEncoder(
					cmd.OutOrStdout(),
				)
				encoder.SetIndent(
					"",
					"  ",
				)

				if err := encoder.Encode(
					result,
				); err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err: fmt.Errorf("%s", cliMessage(
							"entries.stage.json_failed",
							localeSafeCLIDetail(err.Error()),
						)),
					}
				}

				if autoErr != nil {
					return &ExitError{
						Code: executionExitCode(
							autoErr,
						),
					}
				}

				return nil
			}

			renderAgentStageHuman(
				cmd.OutOrStdout(),
				result,
			)

			if autoErr != nil {
				return autoErr
			}

			return nil
		},
	}

	command.Flags().BoolVar(
		&stdinJSON,
		"stdin-json",
		false,
		cliMessage("cli.flag.stdin_entries"),
	)
	command.Flags().StringVar(
		&requestFile,
		"request-file",
		"",
		cliMessage("cli.flag.file_entries"),
	)
	command.Flags().StringVar(&agentName, "agent", "", cliMessage("agent.flag.agent"))

	return command
}

// stageAgentEntries执行纯Stage事务，不承担后续Auto收口。
func stageAgentEntries(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	indexPath string,
	request agentStageRequest,
) (*agentStageResult, error) {
	start := time.Now()

	if cfg == nil ||
		doc == nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("entries.stage.state_incomplete")),
		}
	}

	if err := normalizeAndValidateAgentStageRequest(
		&request,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	policy, err := guardHostAgentStageAutomation(
		cfg,
		"Entries Stage",
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	currentPlan, err := buildAgentPlan(
		repoRoot,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		var planErr *agentPlanBuildError
		if errors.As(
			err,
			&planErr,
		) {
			return nil, &agentStageError{
				Code: planErr.Code,
				Err:  planErr.Err,
			}
		}

		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	if currentPlan.Stage !=
		agentPlanStageEntriesRequired {
		return nil, &agentStageError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.wrong_stage",
				currentPlan.Stage,
				currentPlan.NextAction,
			)),
		}
	}

	if request.PlanID != currentPlan.PlanID {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.plan_stale",
				shortAgentStageHash(
					request.PlanID,
				),
				shortAgentStageHash(
					currentPlan.PlanID,
				),
			)),
		}
	}

	prepared, err := prepareAgentStageEntries(
		request,
		currentPlan,
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	runID, err := draft.NewRun(repoRoot)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	runDir, err := draft.RunDir(
		repoRoot,
		runID,
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(runDir)
		}
	}()

	statuses := make(
		[]draft.EntryStatus,
		0,
		len(prepared),
	)
	files := make(
		[]string,
		0,
		len(prepared),
	)
	draftedCount := 0
	warnedCount := 0

	for _, item := range prepared {
		if err := draft.WriteFile(
			repoRoot,
			runID,
			item.DraftName,
			[]byte(item.Line+"\n"),
		); err != nil {
			return nil, &agentStageError{
				Code: ExitInternal,
				Err: fmt.Errorf("%s", cliMessage(
					"entries.stage.write_failed",
					item.Path,
					localeSafeCLIDetail(err.Error()),
				)),
			}
		}

		statuses = append(
			statuses,
			item.Status,
		)
		files = append(
			files,
			item.DraftName,
		)

		switch item.Status.Status {
		case "drafted":
			draftedCount++

		case "warned":
			warnedCount++
		}
	}

	_, generationHash, err := draft.ReadFilesSnapshot(
		repoRoot,
		runID,
		files,
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.hash_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	manifest := &draft.Manifest{
		RunID:            runID,
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceHostAgent,
		AgentName:        request.Agent,
		PlanID:           currentPlan.PlanID,
		IndexSHA256:      currentPlan.IndexSHA256,
		HeaderSHA256:     currentPlan.HeaderSHA256,
		GenerationHash:   generationHash,
		Model:            request.Model,
		Provider:         agentStageProvider,
		TokenSource:      ledger.TokenSourceMissing,
		Entries:          statuses,
		Files:            files,
	}
	if err := draft.SaveManifest(
		repoRoot,
		manifest,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"entries.stage.manifest_save_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:               "agent_stage",
			Source:           ledger.SourceAgent,
			PathsCount:       len(statuses),
			DurationMs:       time.Since(start).Milliseconds(),
			GenerationSource: draft.GenerationSourceHostAgent,
			AgentName:        request.Agent,
			Model:            request.Model,
			Provider:         agentStageProvider,
			TokenSrc:         ledger.TokenSourceMissing,
			DraftRunID:       runID,
			WarningsCount:    warnedCount,
		},
	)

	committed = true

	return &agentStageResult{
		Version:          agentStageVersion,
		RunID:            runID,
		PlanID:           currentPlan.PlanID,
		Agent:            request.Agent,
		Model:            request.Model,
		AutomationMode:   policy.Mode,
		ApprovalRequired: policy.ApprovalRequired,
		StopBeforeApply:  policy.StopBeforeApply,
		GenerationHash:   generationHash,
		Drafted:          draftedCount,
		Warned:           warnedCount,
		Statuses:         statuses,
		NextCommand: "aoci index entries check " +
			runID,
	}, nil
}
