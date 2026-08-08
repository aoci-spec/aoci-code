// `aoci index agent header stage` 把宿主 Agent 生成的索引头候选接入
// Header Draft/Diff/Apply 治理链。
//
// Stage 从 --request-file 或 --stdin-json 读取 UTF-8 JSON。Windows 推荐
// --request-file，避免 PowerShell 5 文本管道重编码中文。
//
// automation.mode:
//   - auto: Stage 成功后允许宿主 Agent 完成 Diff，并在机器硬闸通过后直接 Apply;
//   - review/legacy: Stage 与 Diff 后在 Apply 前等待批准;
//   - off: 在创建 Run 前硬拒。
//
// Stage 不修改正式 aoci.txt 或 Baseline，不调用模型或访问网络。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func newIndexAgentHeaderCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "header",
		Short: cliMessage("cli.short.agent_header"),
		Long:  indexAgentHeaderLongHelp(),
	}
	command.AddCommand(
		newIndexAgentHeaderStageCmd(),
	)
	return command
}

func newIndexAgentHeaderStageCmd() *cobra.Command {
	var stdinJSON bool
	var requestFile string
	var agentName string

	command := &cobra.Command{
		Use:   "stage",
		Short: cliMessage("cli.short.agent_header_stage"),
		Args:  cobra.NoArgs,
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			reader, source, err := loadAgentRequestInput(
				stdinJSON,
				requestFile,
				cmd.InOrStdin(),
				machinecontract.HeaderRequestMaxBytes,
				"Header Stage",
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

			request, err := readAgentHeaderStageRequest(
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

			result, err := stageAgentHeader(
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

			if flagJSON {
				encoder := json.NewEncoder(
					cmd.OutOrStdout(),
				)
				encoder.SetIndent(
					"",
					"  ",
				)
				return encoder.Encode(result)
			}

			fmt.Fprint(cmd.OutOrStdout(), cliMessage(
				"header.stage.created",
				result.RunID,
			))
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"automation.mode: %s | approval_required: %t | stop_before_apply: %t\n",
				result.AutomationMode,
				result.ApprovalRequired,
				result.StopBeforeApply,
			)
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"generation_hash: %s\n",
				result.GenerationHash,
			)

			for _, warning := range result.Warnings {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					cliMessage("header.warning", localeSafeCLIDetail(warning)),
				)
			}

			fmt.Fprintln(
				cmd.OutOrStdout(),
				cliMessage("agent.next", result.NextCommand),
			)

			if result.ApprovalRequired {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					cliMessage("agent.apply_after_approval", result.ApplyCommand),
				)
			} else {
				fmt.Fprintln(
					cmd.OutOrStdout(),
					cliMessage("agent.apply_after_gates", result.ApplyCommand),
				)
			}

			return nil
		},
	}

	command.Flags().BoolVar(
		&stdinJSON,
		"stdin-json",
		false,
		cliMessage("cli.flag.stdin_header"),
	)
	command.Flags().StringVar(
		&requestFile,
		"request-file",
		"",
		cliMessage("cli.flag.file_header"),
	)
	command.Flags().StringVar(&agentName, "agent", "", cliMessage("agent.flag.agent"))

	return command
}

func stageAgentHeader(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	indexPath string,
	request agentHeaderStageRequest,
) (*agentHeaderStageResult, error) {
	start := time.Now()

	if cfg == nil ||
		doc == nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("header.stage.state_incomplete")),
		}
	}

	if err := normalizeAndValidateAgentHeaderStageRequest(
		&request,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err:  err,
		}
	}
	requestLocale, explicitLocale, localeErr := index.DetectLocale(request.Header)
	requireExplicitLocale := cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration != nil
	if localeErr != nil || (requireExplicitLocale && !explicitLocale) || requestLocale != cfg.Locale {
		if localeErr == nil {
			localeErr = fmt.Errorf("%s", cliMessage("locale.header_marker_required", cfg.Locale))
		}
		return nil, &agentStageError{Code: ExitInvalid, Err: localeErr}
	}

	policy, err := guardHostAgentStageAutomation(
		cfg,
		"Header Stage",
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

	expectedStage := agentPlanStageHeaderRequired
	if request.Intent == agentHeaderStageIntentSemanticRefresh {
		expectedStage = agentPlanStageAligned
	}
	if currentPlan.Stage != expectedStage {
		if request.Intent == agentHeaderStageIntentSemanticRefresh {
			return nil, &agentStageError{
				Code: ExitConfig,
				Err: fmt.Errorf("%s", cliMessage(
					"header.stage.semantic_refresh_wrong_stage",
					currentPlan.Stage,
					currentPlan.NextAction,
				)),
			}
		}
		return nil, &agentStageError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"header.stage.wrong_stage",
				currentPlan.Stage,
				currentPlan.NextAction,
			)),
		}
	}

	if request.PlanID != currentPlan.PlanID {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"header.stage.plan_stale",
				shortAgentStageHash(
					request.PlanID,
				),
				shortAgentStageHash(
					currentPlan.PlanID,
				),
			)),
		}
	}

	warnings := inspectAgentHeaderCandidate(
		request.Header,
	)
	files := []string{draft.HeaderFileName}
	if cfg.LocaleMigration != nil {
		if request.ManagedIndexText == "" {
			return nil, &agentStageError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
				"locale.header_stage.managed_index_required",
			))}
		}
		candidateHeader, _ := index.ExtractHeader(request.ManagedIndexText)
		if strings.TrimSpace(candidateHeader) != strings.TrimSpace(request.Header) {
			return nil, &agentStageError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
				"locale.header_stage.managed_header_mismatch",
			))}
		}
		if _, validateErr := validateLocaleIndexCandidate(
			doc.RawText,
			request.ManagedIndexText,
			repoRoot,
			cfg.Locale,
			cfg.LocaleMigration,
		); validateErr != nil {
			return nil, &agentStageError{Code: ExitInvalid, Err: validateErr}
		}
		files = append(files, draft.LocaleIndexFileName)
	} else if request.ManagedIndexText != "" {
		return nil, &agentStageError{Code: ExitInvalid, Err: fmt.Errorf("%s", cliMessage(
			"locale.header_stage.managed_index_without_migration",
		))}
	}
	if request.Intent != "" {
		files = append(files, draft.HeaderIntentFileName)
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

	if err := draft.WriteFile(
		repoRoot,
		runID,
		draft.HeaderFileName,
		[]byte(request.Header+"\n"),
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"header.stage.write_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}
	if request.ManagedIndexText != "" {
		if err := draft.WriteFile(
			repoRoot,
			runID,
			draft.LocaleIndexFileName,
			[]byte(strings.TrimSuffix(request.ManagedIndexText, "\n")+"\n"),
		); err != nil {
			return nil, &agentStageError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
				"locale.header_stage.managed_draft_write_failed",
				localeSafeCLIDetail(err.Error()),
			))}
		}
	}
	if request.Intent != "" {
		if err := draft.WriteFile(
			repoRoot,
			runID,
			draft.HeaderIntentFileName,
			[]byte(request.Intent+"\n"),
		); err != nil {
			return nil, &agentStageError{Code: ExitInternal, Err: fmt.Errorf("%s", cliMessage(
				"header.stage.write_failed",
				localeSafeCLIDetail(err.Error()),
			))}
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
				"header.stage.hash_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	manifest := &draft.Manifest{
		RunID:            runID,
		Kind:             draft.KindHeader,
		GenerationSource: draft.GenerationSourceHostAgent,
		AgentName:        request.Agent,
		PlanID:           currentPlan.PlanID,
		IndexSHA256:      currentPlan.IndexSHA256,
		HeaderSHA256:     currentPlan.HeaderSHA256,
		GenerationHash:   generationHash,
		Model:            request.Model,
		Provider:         agentStageProvider,
		TokenSource:      ledger.TokenSourceMissing,
		Warnings:         warnings,
		Files:            files,
	}
	if err := draft.SaveManifest(
		repoRoot,
		manifest,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"header.stage.manifest_save_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:               "agent_header_stage",
			Source:           ledger.SourceAgent,
			PathsCount:       1,
			DurationMs:       time.Since(start).Milliseconds(),
			GenerationSource: draft.GenerationSourceHostAgent,
			AgentName:        request.Agent,
			Model:            request.Model,
			Provider:         agentStageProvider,
			TokenSrc:         ledger.TokenSourceMissing,
			DraftRunID:       runID,
			WarningsCount:    len(warnings),
		},
	)

	committed = true

	return &agentHeaderStageResult{
		Version:          agentHeaderStageVersion,
		RunID:            runID,
		PlanID:           currentPlan.PlanID,
		Agent:            request.Agent,
		Model:            request.Model,
		Intent:           request.Intent,
		AutomationMode:   policy.Mode,
		GenerationHash:   generationHash,
		Warnings:         warnings,
		ApprovalRequired: policy.ApprovalRequired,
		StopBeforeApply:  policy.StopBeforeApply,
		NextCommand: "aoci index header diff " +
			runID,
		ApplyCommand: "aoci index header apply " +
			runID,
	}, nil
}
