// `aoci index build`首次条目批量起草。
//
// --missing生产路径必须读取正式curation.json。
// 有效include进入Actionable；exclude与Pending在构造AI Client前收敛。
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/workflow"
	"github.com/spf13/cobra"
)

// selectBuildMissingTargetsDetailed 保留纯内存单元测试兼容面。
func selectBuildMissingTargetsDetailed(
	inventory *indexgen.Inventory,
	cfg *config.Config,
) (
	[]string,
	int,
	[]indexgen.SkippedMissing,
) {
	if inventory == nil {
		return []string{},
			0,
			[]indexgen.SkippedMissing{}
	}

	rawMissing := make(
		[]string,
		0,
		len(inventory.Items),
	)
	for _, item := range inventory.Items {
		rawMissing = append(
			rawMissing,
			item.RelPath,
		)
	}

	classification := indexgen.ClassifyMissing(
		cfg,
		rawMissing,
		inventory,
	)

	return append(
			[]string{},
			classification.Actionable...,
		),
		len(classification.CurationExcluded),
		append(
			[]indexgen.SkippedMissing{},
			classification.Skipped...,
		)
}

func selectBuildMissingTargets(
	inventory *indexgen.Inventory,
	cfg *config.Config,
) ([]string, int) {
	targets, excluded, _ :=
		selectBuildMissingTargetsDetailed(
			inventory,
			cfg,
		)
	return targets, excluded
}

func buildMissingClassification(
	root string,
	cfg *config.Config,
	doc *index.Document,
) (
	indexgen.MissingClassification,
	error,
) {
	inventory, err := indexgen.BuildInventory(
		root,
		cfg,
		doc,
	)
	if err != nil {
		return indexgen.MissingClassification{},
			err
	}

	rawMissing := make(
		[]string,
		0,
		len(inventory.Items),
	)
	for _, item := range inventory.Items {
		rawMissing = append(
			rawMissing,
			item.RelPath,
		)
	}

	classification, _, err :=
		indexgen.BuildMissingClassification(
			root,
			cfg,
			doc,
			rawMissing,
		)
	if err != nil {
		return indexgen.MissingClassification{},
			err
	}

	return classification, nil
}

func newIndexBuildCmd() *cobra.Command {
	var useMissing bool
	var paths []string

	command := &cobra.Command{
		Use:   "build",
		Short: cliMessage("cli.short.index_build"),
		Long:  indexBuildLongHelp(),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
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

			if useMissing ==
				(len(paths) > 0) {
				return &ExitError{
					Code: ExitConfig,
					Err:  fmt.Errorf("%s", cliMessage("build.target_selection")),
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

			out := cmd.OutOrStdout()
			targets := []string{}

			if useMissing {
				classification, err :=
					buildMissingClassification(
						repoRoot,
						cfg,
						doc,
					)
				if err != nil {
					return &ExitError{
						Code: ExitInternal,
						Err:  err,
					}
				}

				targets = append(
					targets,
					classification.Actionable...,
				)

				if len(classification.Included) > 0 {
					fmt.Fprint(out, cliMessage("build.included", len(classification.Included)))
				}
				if len(
					classification.CurationExcluded,
				) > 0 {
					fmt.Fprint(out, cliMessage(
						"build.curation_excluded",
						len(classification.CurationExcluded),
					))
				}
				if len(classification.Skipped) > 0 {
					fmt.Fprint(out, cliMessage(
						"build.skipped",
						len(classification.Skipped),
						len(classification.Pending),
					))
				}
				if len(classification.Pending) > 0 {
					fmt.Fprint(out, cliMessage("build.pending", len(classification.Pending)))
				}

				if len(targets) == 0 {
					if len(classification.Pending) > 0 {
						fmt.Fprint(out, cliMessage(
							"build.no_actionable_pending",
							len(classification.Pending),
						))
					} else {
						fmt.Fprintln(out, cliMessage("build.no_actionable"))
					}
					return nil
				}
			} else {
				targets = append(
					targets,
					paths...,
				)
			}

			client, err := buildAIClient(cfg)
			if err != nil {
				return err
			}

			fmt.Fprint(out, cliMessage(
				"build.start",
				len(targets),
				cfg.AI.Model,
				displayConcurrency(cfg),
			))

			ctx, cancel := context.WithTimeout(
				context.Background(),
				singleCallTimeout(cfg)*
					time.Duration(len(targets)),
			)
			defer cancel()

			result, err := workflow.RunEntriesDraft(
				ctx,
				repoRoot,
				cfg,
				doc,
				client,
				targets,
				nil,
				workflow.WithProgress(
					entriesProgressPrinter(
						cmd.ErrOrStderr(),
					),
				),
			)
			if err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err:  wrapAIErr(cfg, err),
				}
			}

			fmt.Fprint(out, cliMessage("build.run", result.RunID))
			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)

			for _, status := range result.Statuses {
				line := fmt.Sprintf(
					"[%s] %s",
					status.Status,
					status.Path,
				)
				if status.Note != "" {
					line += " —— " + localeSafeCLIDetail(status.Note)
				}
				fmt.Fprintln(out, line)
			}

			fmt.Fprintln(
				out,
				"──────────────────────────────",
			)
			fmt.Fprint(out, cliMessage(
				"build.total",
				result.Drafted,
				result.Warned,
				result.Failed,
				result.Skipped,
			))
			fmt.Fprint(out, cliMessage(
				"build.tokens",
				result.InputTokens,
				result.OutputTokens,
				result.TokenSrc,
			))

			fmt.Fprintln(out, cliMessage("build.next"))

			return nil
		},
	}

	command.Flags().BoolVar(
		&useMissing,
		"missing",
		false,
		cliMessage("cli.flag.build_missing"),
	)
	command.Flags().StringSliceVar(
		&paths,
		"paths",
		nil,
		cliMessage("cli.flag.paths"),
	)

	return command
}
