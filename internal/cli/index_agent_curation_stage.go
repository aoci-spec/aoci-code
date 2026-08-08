// Host-Agent Curation Stage。
//
// Stage只创建curation草稿、Manifest和Ledger，不修改正式curation.json、
// aoci.txt或Baseline。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

func newAgentCurationStageCmd() *cobra.Command {
	var stdinJSON bool
	var requestFile string
	var agentName string

	command := &cobra.Command{
		Use:   "stage",
		Short: cliMessage("cli.short.agent_curation_stage"),
		Args:  cobra.NoArgs,
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			reader, source, err :=
				loadAgentRequestInput(
					stdinJSON,
					requestFile,
					cmd.InOrStdin(),
					machinecontract.CurationRequestMaxBytes,
					"Curation Stage",
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

			request, err :=
				readAgentCurationStageRequest(
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

			doc, indexPath, err :=
				loadIndexForCLI(
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

			result, err := stageAgentCuration(
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
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}

			fmt.Fprint(cmd.OutOrStdout(), cliMessage(
				"curation.stage.created",
				result.RunID,
			))
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"include %d / exclude %d | approval_required=%t | stop_before_apply=%t\n",
				result.Include,
				result.Exclude,
				result.ApprovalRequired,
				result.StopBeforeApply,
			)
			fmt.Fprintln(
				cmd.OutOrStdout(),
				cliMessage("agent.next", result.NextCommand),
			)

			return nil
		},
	}

	command.Flags().BoolVar(
		&stdinJSON,
		"stdin-json",
		false,
		cliMessage("cli.flag.stdin_curation"),
	)
	command.Flags().StringVar(
		&requestFile,
		"request-file",
		"",
		cliMessage("cli.flag.file_curation"),
	)
	command.Flags().StringVar(&agentName, "agent", "", cliMessage("agent.flag.agent"))

	return command
}

func stageAgentCuration(
	repoRoot string,
	cfg *config.Config,
	doc *index.Document,
	indexPath string,
	request agentCurationStageRequest,
) (*agentCurationStageResult, error) {
	start := time.Now()

	if err := normalizeAgentCurationRequest(
		&request,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	policy, err :=
		guardHostAgentStageAutomation(
			cfg,
			"Curation Stage",
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
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	if currentPlan.Stage !=
		agentPlanStageCurationRequired {
		return nil, &agentStageError{
			Code: ExitConfig,
			Err: fmt.Errorf("%s", cliMessage(
				"curation.stage.wrong_stage",
				currentPlan.Stage,
			)),
		}
	}

	if request.PlanID != currentPlan.PlanID {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err: fmt.Errorf("%s", cliMessage(
				"curation.stage.plan_stale",
				shortAgentStageHash(
					request.PlanID,
				),
				shortAgentStageHash(
					currentPlan.PlanID,
				),
			)),
		}
	}

	prepared, err :=
		prepareAgentCurationDecisions(
			request,
			currentPlan,
		)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInvalid,
			Err:  err,
		}
	}

	runID, err := draft.NewRun(
		repoRoot,
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	runDirectory, err := draft.RunDir(
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
			_ = os.RemoveAll(
				runDirectory,
			)
		}
	}()

	draftDocument := &curation.Document{
		Version:   curation.Version,
		Decisions: prepared,
	}
	data, err := json.MarshalIndent(
		draftDocument,
		"",
		"  ",
	)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}
	data = append(data, '\n')

	if err := draft.WriteFile(
		repoRoot,
		runID,
		draft.CurationFileName,
		data,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	_, generationHash, err :=
		draft.ReadFilesSnapshot(
			repoRoot,
			runID,
			[]string{
				draft.CurationFileName,
			},
		)
	if err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	statuses := make(
		[]draft.EntryStatus,
		0,
		len(prepared),
	)
	includeCount := 0
	excludeCount := 0

	for _, decision := range prepared {
		statuses = append(
			statuses,
			draft.EntryStatus{
				Path:         decision.Path,
				Status:       "drafted",
				Note:         decision.Decision,
				SourceSHA256: decision.SourceSHA256,
			},
		)

		if decision.Decision ==
			curation.DecisionInclude {
			includeCount++
		} else {
			excludeCount++
		}
	}

	manifest := &draft.Manifest{
		RunID:            runID,
		Kind:             draft.KindCuration,
		GenerationSource: draft.GenerationSourceHostAgent,
		AgentName:        request.Agent,
		PlanID:           currentPlan.PlanID,
		IndexSHA256:      currentPlan.IndexSHA256,
		HeaderSHA256:     currentPlan.HeaderSHA256,
		CurationSHA256:   currentPlan.CurationSHA256,
		GenerationHash:   generationHash,
		Model:            request.Model,
		Provider:         agentStageProvider,
		TokenSource:      ledger.TokenSourceMissing,
		Entries:          statuses,
		Files: []string{
			draft.CurationFileName,
		},
	}

	if err := draft.SaveManifest(
		repoRoot,
		manifest,
	); err != nil {
		return nil, &agentStageError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	ledger.Append(
		repoRoot,
		cfg.LedgerEnabled,
		ledger.Event{
			Op:               "agent_curation_stage",
			Source:           ledger.SourceAgent,
			PathsCount:       len(prepared),
			DurationMs:       time.Since(start).Milliseconds(),
			GenerationSource: draft.GenerationSourceHostAgent,
			AgentName:        request.Agent,
			Model:            request.Model,
			Provider:         agentStageProvider,
			TokenSrc:         ledger.TokenSourceMissing,
			DraftRunID:       runID,
		},
	)

	committed = true

	return &agentCurationStageResult{
		Version:          agentCurationStageVersion,
		RunID:            runID,
		PlanID:           currentPlan.PlanID,
		Agent:            request.Agent,
		Model:            request.Model,
		AutomationMode:   policy.Mode,
		ApprovalRequired: policy.ApprovalRequired,
		StopBeforeApply:  policy.StopBeforeApply,
		GenerationHash:   generationHash,
		Decisions:        len(prepared),
		Include:          includeCount,
		Exclude:          excludeCount,
		NextCommand: "aoci index agent curation diff " +
			runID,
		DiffCommand: "aoci index agent curation diff " +
			runID,
		ApplyCommand: "aoci index agent curation apply " +
			runID,
	}, nil
}
