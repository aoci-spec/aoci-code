// 索引条目: index_entries.go[CIX7S]
//
// 职责:
// `aoci index entries`子命令组的挂载壳与共用辅助件。check/diff/apply已按
// D57模块纪律拆到独立文件，三者共同复用同一run解析与草稿内存快照。
//
// P-23三阶段审计:
//   - manifest.Entries永久保留AI初始generation state；
//   - check追加机器预检记录；
//   - diff追加内容对照审阅记录；
//   - apply追加application记录；
//   - 三阶段使用同一草稿内容SHA-256关联。
//
// 两层一致性防线:
//   - R52防run漂移：人审run与apply run必须一致；
//   - P-23防内容漂移：应用内容必须与有效审阅摘要一致。
//
// R62-B2A授权分层:
//   - Host-Agent标准草稿必须存在同内容Diff记录，Check不能单独授权Apply；
//   - 一旦Manifest形成过Diff，最近Diff就是Apply的唯一内容授权；
//   - Diff后修改草稿并重新Check，仍必须重新Diff；
//   - Endpoint内部Auto暂时保留同内容Check授权，避免迁移期打断Auto；
//   - 来源为空的人工或历史草稿继续保持既有Check授权合同；
//   - 完全没有Review的旧Manifest继续警告兼容放行。
//
// entryDraftSnapshot一次读取全部可应用entry文件，后续命令只消费内存内容，
// 避免摘要与实际校验、展示、应用之间出现TOCTOU。
package cli

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/spf13/cobra"
)

func entryDraftFileName(
	rel string,
) string {
	return strings.ReplaceAll(
		rel,
		"/",
		"__",
	) + ".entry.txt"
}

func resolveEntriesRunID(
	root string,
	args []string,
) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	runID, err := draft.LatestRunID(
		root,
		draft.KindEntries,
	)
	if err != nil {
		return "", fmt.Errorf("%s", cliMessage("entries.run_not_found", err))
	}

	return runID, nil
}

func entryDraftNames(
	manifest *draft.Manifest,
) []string {
	if manifest == nil {
		return []string{}
	}

	names := make(
		[]string,
		0,
		len(manifest.Entries),
	)

	for _, status := range manifest.Entries {
		if status.Status != "drafted" &&
			status.Status != "warned" {
			continue
		}

		names = append(
			names,
			entryDraftFileName(status.Path),
		)
	}

	return names
}

type entryDraftSnapshot struct {
	Hash  string
	Files map[string][]byte
}

func loadEntryDraftSnapshot(
	root,
	runID string,
	manifest *draft.Manifest,
) (*entryDraftSnapshot, error) {
	files, hash, err := draft.ReadFilesSnapshot(
		root,
		runID,
		entryDraftNames(manifest),
	)
	if err != nil {
		return nil, err
	}

	return &entryDraftSnapshot{
		Hash:  hash,
		Files: files,
	}, nil
}

func (snapshot *entryDraftSnapshot) line(
	rel string,
) (string, error) {
	if snapshot == nil {
		return "", fmt.Errorf("%s", cliMessage("entries.snapshot_empty"))
	}

	name := entryDraftFileName(rel)
	data, exists := snapshot.Files[name]
	if !exists {
		return "", fmt.Errorf("%s", cliMessage("entries.snapshot_file_missing", name))
	}

	return strings.TrimRight(
		string(data),
		"\n",
	), nil
}

func shortDraftHash(
	hash string,
) string {
	if len(hash) <= 16 {
		return hash
	}

	return hash[:16]
}

// latestEntriesDiffReview返回最近一次Diff内容审阅记录。
//
// 一旦Manifest形成Diff，后续Apply始终绑定最近Diff。之后新增的Check不能
// 覆盖或替代Diff授权。
func latestEntriesDiffReview(
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

// latestEntriesCheckReview返回最近一次机器Check记录。
//
// Check仅可授权Endpoint内部Auto和来源为空的人工或历史草稿；
// Host-Agent标准草稿不能用Check替代Diff。
func latestEntriesCheckReview(
	manifest *draft.Manifest,
) (draft.ReviewRecord, bool) {
	if manifest == nil {
		return draft.ReviewRecord{}, false
	}

	for position := len(manifest.Reviews) - 1; position >= 0; position-- {
		review := manifest.Reviews[position]

		if review.Action == draft.ReviewActionCheck {
			return review, true
		}
	}

	return draft.ReviewRecord{}, false
}

// validateEntriesReviewHash验证一条审阅记录与当前内存快照一致。
func validateEntriesReviewHash(
	review draft.ReviewRecord,
	currentHash string,
) error {
	if review.DraftHash == "" {
		return fmt.Errorf("%s", cliMessage("entries.review_hash_missing", review.Action))
	}

	if currentHash == "" {
		return fmt.Errorf("%s", cliMessage("entries.current_hash_empty"))
	}

	if review.DraftHash != currentHash {
		return fmt.Errorf("%s", cliMessage(
			"entries.review_hash_mismatch",
			review.Action,
			shortDraftHash(review.DraftHash),
			shortDraftHash(currentHash),
		))
	}

	return nil
}

// guardReviewedDraftHash是Entries P-23内容授权防线。
//
// 稳定合同：
//   - Host-Agent草稿必须有同内容Diff；
//   - 有Diff的任何草稿都只能由最近Diff授权；
//   - Endpoint和来源为空草稿在过渡期继续接受同内容Check；
//   - 完全无Review的旧草稿允许带警告兼容；
//   - 显式run_id只裁决Run选择，不能绕过摘要一致性。
func guardReviewedDraftHash(
	manifest *draft.Manifest,
	currentHash string,
) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("%s", cliMessage("entries.manifest_empty"))
	}

	diffReview, hasDiff :=
		latestEntriesDiffReview(manifest)
	if hasDiff {
		if err := validateEntriesReviewHash(
			diffReview,
			currentHash,
		); err != nil {
			return "", err
		}

		return "", nil
	}

	checkReview, hasCheck :=
		latestEntriesCheckReview(manifest)

	if manifest.GenerationSource ==
		draft.GenerationSourceHostAgent {
		if hasCheck {
			return "", fmt.Errorf("%s", cliMessage("entries.host_check_without_diff", manifest.RunID))
		}

		return "", fmt.Errorf("%s", cliMessage(
			"entries.host_diff_missing",
			manifest.RunID,
			manifest.RunID,
		))
	}

	// Endpoint内部Auto和来源为空的人工或历史草稿暂时保留Check授权。
	// 返回空Warning，确保既有Auto连续链和“内容审阅核对: ✓”输出不改变。
	if hasCheck {
		if err := validateEntriesReviewHash(
			checkReview,
			currentHash,
		); err != nil {
			return "", err
		}

		return "", nil
	}

	if len(manifest.Reviews) == 0 {
		return cliMessage("entries.legacy_review_warning"),
			nil
	}

	return "", fmt.Errorf("%s", cliMessage("entries.review_unrecognized"))
}

func newIndexEntriesCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "entries",
		Short: cliMessage("cli.short.index_entries"),
	}

	command.AddCommand(
		newEntriesCheckCmd(),
	)
	command.AddCommand(
		newEntriesDiffCmd(),
	)
	command.AddCommand(
		newEntriesApplyJSONCmd(),
	)
	command.AddCommand(
		newEntriesRecoverCmd(),
	)

	return command
}
