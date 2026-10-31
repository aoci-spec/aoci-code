// `aoci index score`: the user entry to the nine-dimension index quality
// score. A Legacy index is scored by indexgen.BuildScore over its document;
// a Cognition Volumes repository by indexgen.BuildVolumeScore over the loaded
// set and the governance facts Verify and Check report, so the two layouts
// print the same dimensions and the Volumes numbers equal verify's. The
// rendering is shared. index.go wires the command in its init().
//
// tagparse is the ninth dimension: it only exposes the unparseable-tag
// Warning ValidateEntryLineWith already found, without changing the Warning
// level or making it a check blocker.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/spf13/cobra"
)

var scoreDimNames = []string{
	"format",
	"coverage",
	"freshness",
	"squota",
	"dict",
	"token",
	"agent_ready",
	"escale",
	"tagparse",
}

func newIndexScoreCmd() *cobra.Command {
	var dimName string

	cmd := &cobra.Command{
		Use:   "score",
		Short: indexScoreShortHelp(),
		Long:  indexScoreLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}

			if dimName != "" {
				valid := false
				for _, name := range scoreDimNames {
					if name == dimName {
						valid = true
						break
					}
				}
				if !valid {
					return &ExitError{Code: ExitConfig, Err: errors.New(
						cliMessage("score.bad_dimension", dimName, strings.Join(scoreDimNames, "/")))}
				}
			}

			set, err := loadCognitionSetForCLI(repoRoot, cfg)
			if err != nil {
				return &ExitError{Code: ExitConfig, Err: err}
			}
			if len(set.Warnings) > 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), cliMessage("inventory.parse_warnings", len(set.Warnings)))
			}

			start := time.Now()
			sampleLimit := indexgen.ScoreSampleLimit
			if dimName != "" {
				sampleLimit = 0
			}
			var score *indexgen.Score
			if set.LayoutMode == cognition.LayoutVolumesV1 {
				facts, factsErr := volumegovernance.Assess(repoRoot, cfg, set)
				if factsErr != nil {
					return &ExitError{Code: ExitInvalid, Err: factsErr}
				}
				score = indexgen.BuildVolumeScore(set, facts.ScoreInput(cfg, volumegovernance.AssessEScale(repoRoot, set)), sampleLimit)
			} else {
				score, err = indexgen.BuildScoreWithLimit(repoRoot, cfg, set.Root.Document, sampleLimit)
				if err != nil {
					return &ExitError{Code: ExitInternal, Err: err}
				}
			}

			ledger.Append(repoRoot, cfg.LedgerEnabled, ledger.Event{
				Op:         "index_score",
				Source:     ledger.SourceHuman,
				PathsCount: score.EntryCount,
				DurationMs: time.Since(start).Milliseconds(),
			})

			out := cmd.OutOrStdout()
			if dimName != "" {
				var target indexgen.Dimension
				for _, dimension := range score.Dimensions {
					if dimension.Name == dimName {
						target = dimension
						break
					}
				}

				if flagJSON {
					encoder := json.NewEncoder(out)
					encoder.SetIndent("", "  ")
					return encoder.Encode(target)
				}

				fmt.Fprintf(out, "AOCI score --dim %s @ %s\n", dimName, time.Now().UTC().Format(time.RFC3339))
				if (dimName == "dict" || dimName == "escale") && target.Total == 0 {
					fmt.Fprintln(out, cliMessage("score.not_determinable"))
					return nil
				}
				fmt.Fprintln(out, cliMessage("score.violations", target.Bad, target.Total, target.Note))
				fmt.Fprintln(out, "──────────────────────────────")
				for _, sample := range target.Samples {
					fmt.Fprintln(out, sample)
				}
				fmt.Fprintln(out, "──────────────────────────────")
				fmt.Fprintln(out, cliMessage("score.total", len(target.Samples)))
				return nil
			}

			if flagJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(score)
			}

			fmt.Fprintf(out, "AOCI score @ %s\n", time.Now().UTC().Format(time.RFC3339))
			fmt.Fprintln(out, cliMessage("score.summary", score.EntryCount, score.DiskCount, score.IndexTokens))
			fmt.Fprintln(out, "──────────────────────────────")

			for _, dimension := range score.Dimensions {
				mark := "✓"
				if dimension.Bad > 0 {
					mark = "✗"
				}
				if dimension.Name == "token" {
					avg := 0
					if score.EntryCount > 0 {
						avg = score.IndexTokens / score.EntryCount
					}
					fmt.Fprintln(out, cliMessage("score.token", dimension.Name, dimension.Total, avg))
					continue
				}
				if (dimension.Name == "dict" || dimension.Name == "escale") && dimension.Total == 0 {
					fmt.Fprintln(out, cliMessage("score.dimension_na", dimension.Name))
					continue
				}
				fmt.Fprintln(out, cliMessage("score.dimension", mark, dimension.Name, dimension.Bad, dimension.Total))
				for _, sample := range dimension.Samples {
					fmt.Fprintln(out, cliMessage("score.sample", sample))
				}
			}

			fmt.Fprintln(out, "──────────────────────────────")
			fmt.Fprintln(out, cliMessage("score.note"))
			return nil
		},
	}

	cmd.Flags().StringVar(
		&dimName,
		"dim",
		"",
		cliMessage("cli.flag.score_dim"),
	)
	return cmd
}
