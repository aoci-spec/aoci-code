// 索引条目: index_score.go[CIX7O]
// 职责: `aoci index score` 子命令 —— 九维度索引质量评分的用户入口。
// 独立成文件的原因: index.go 曾超 600 行软限,新命令不再往里加;
// 本文件仅含构造函数,由 index.go 的 init() 一行接线挂载。
//
// tagparse 是第九维: 只让 ValidateEntryLineWith 已发现的“标签不可解析”Warning
// 在 score 中可见,不改变 Warning 级别,也不自动成为 check 的提交阻断项。
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

			start := time.Now()
			var score *indexgen.Score
			var projection *volumeReadProjection
			set, err := cognition.Load(repoRoot, cfg.IndexPath)
			if err != nil {
				code := ExitConfig
				if set != nil && set.LayoutMode == cognition.LayoutVolumesV1 {
					code = ExitInvalid
				}
				return &ExitError{Code: code, Err: err}
			}
			if set.LayoutMode == cognition.LayoutVolumesV1 {
				projection, err = buildVolumeReadProjection(repoRoot, cfg, set)
				if err == nil {
					score = projection.Score
				}
			} else {
				doc, _, loadErr := loadIndexForCLI(cmd, repoRoot, cfg)
				if loadErr != nil {
					return &ExitError{Code: ExitConfig, Err: loadErr}
				}
				if dimName == "" {
					score, err = indexgen.BuildScore(repoRoot, cfg, doc)
				} else {
					score, err = indexgen.BuildScoreWithLimit(repoRoot, cfg, doc, 0)
				}
			}
			if err != nil {
				return &ExitError{Code: ExitInternal, Err: err}
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
				if projection != nil {
					return encoder.Encode(projection.scoreReport())
				}
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
