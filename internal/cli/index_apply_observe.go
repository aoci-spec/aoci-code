// 三类Apply的文件、草稿和Manifest状态观察。
//
// 本文件只读取事实，不执行Apply事务，也不解析人读成功文案。
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

// collectManifestFacts提取Plan、Agent、路径、尝试数量和最近审阅摘要。
func (observation *applyObservation) collectManifestFacts() {
	manifest := observation.BeforeManifest.Value
	if manifest == nil {
		if observation.Kind == applyKindHeader {
			observation.Attempted = 1
		}
		return
	}

	observation.PlanID = manifest.PlanID
	observation.Agent = manifest.AgentName
	observation.ReviewHash = latestApplyReviewHash(
		observation.Kind,
		manifest,
	)

	switch observation.Kind {
	case applyKindHeader:
		observation.Attempted = 1

	case applyKindEntries:
		for _, status := range manifest.Entries {
			if status.Status != "drafted" &&
				status.Status != "warned" {
				continue
			}

			observation.Attempted++
			observation.Paths = append(
				observation.Paths,
				status.Path,
			)
		}

	case applyKindCuration:
		for _, status := range manifest.Entries {
			if status.Status != "drafted" {
				continue
			}

			observation.Attempted++
			observation.Paths = append(
				observation.Paths,
				status.Path,
			)
		}
	}
}

// collectDraftFacts计算本次Apply消费的草稿摘要与Curation计数。
func (observation *applyObservation) collectDraftFacts() {
	switch observation.Kind {
	case applyKindHeader:
		hash, err := draft.HashFiles(
			observation.Root,
			observation.RunID,
			[]string{
				draft.HeaderFileName,
			},
		)
		if err == nil {
			observation.DraftHash = hash
		}

	case applyKindEntries:
		manifest := observation.BeforeManifest.Value
		if manifest == nil {
			return
		}

		hash, err := draft.HashFiles(
			observation.Root,
			observation.RunID,
			entryDraftNames(manifest),
		)
		if err == nil {
			observation.DraftHash = hash
		}

	case applyKindCuration:
		snapshot, err := loadCurationDraftSnapshot(
			observation.Root,
			observation.RunID,
		)
		if err != nil || snapshot == nil {
			return
		}

		observation.DraftHash = snapshot.Hash

		for _, decision := range snapshot.Document.Decisions {
			switch decision.Decision {
			case curation.DecisionInclude:
				observation.Include++

			case curation.DecisionExclude:
				observation.Exclude++
			}
		}

		if observation.Attempted == 0 {
			observation.Attempted =
				len(snapshot.Document.Decisions)
		}
	}
}

// latestApplyReviewHash提取本类Apply可接受的最近审阅摘要。
func latestApplyReviewHash(
	kind string,
	manifest *draft.Manifest,
) string {
	if manifest == nil {
		return ""
	}

	for position := len(manifest.Reviews) - 1; position >= 0; position-- {
		review := manifest.Reviews[position]

		switch kind {
		case applyKindEntries:
			if review.Action ==
				draft.ReviewActionCheck ||
				review.Action ==
					draft.ReviewActionDiff {
				return review.DraftHash
			}

		case applyKindHeader,
			applyKindCuration:
			if review.Action ==
				draft.ReviewActionDiff {
				return review.DraftHash
			}
		}
	}

	return ""
}

// readApplyFileState读取文件身份和内容摘要。
func readApplyFileState(
	path string,
) (applyFileState, error) {
	info, err := os.Stat(
		path,
	)
	if err != nil {
		if errors.Is(
			err,
			os.ErrNotExist,
		) {
			return applyFileState{}, nil
		}
		return applyFileState{}, err
	}

	if info.IsDir() {
		return applyFileState{}, fmt.Errorf("%s", cliMessage("apply.target_is_directory", path))
	}

	data, err := os.ReadFile(
		path,
	)
	if err != nil {
		return applyFileState{}, err
	}

	sum := sha256.Sum256(
		data,
	)

	return applyFileState{
		Exists: true,
		SHA256: hex.EncodeToString(
			sum[:],
		),
		Info: info,
	}, nil
}

// applyFileReplaced判断本次是否替换或创建了文件。
//
// AtomicWrite即使写回相同内容也会形成新的文件身份；内容摘要作为第二判据。
func applyFileReplaced(
	before,
	after applyFileState,
) bool {
	switch {
	case !before.Exists && after.Exists:
		return true

	case before.Exists && !after.Exists:
		return false

	case !before.Exists && !after.Exists:
		return false
	}

	beforeInfo, beforeOK := before.Info.(os.FileInfo)
	afterInfo, afterOK := after.Info.(os.FileInfo)

	if beforeOK &&
		afterOK &&
		!os.SameFile(
			beforeInfo,
			afterInfo,
		) {
		return true
	}

	return before.SHA256 != after.SHA256
}

// readApplyManifestState读取Manifest；Header旧批次不存在不算观察错误。
func readApplyManifestState(
	root,
	runID string,
) applyManifestState {
	manifest, err := draft.LoadManifest(
		root,
		runID,
	)
	if err != nil {
		if errors.Is(
			err,
			os.ErrNotExist,
		) {
			return applyManifestState{}
		}

		return applyManifestState{
			LoadErr: err,
		}
	}

	return applyManifestState{
		Exists: true,
		Value:  manifest,
	}
}

// newApplyApplication返回本次新增的Application记录。
func newApplyApplication(
	before,
	after applyManifestState,
) (draft.ApplicationRecord, bool) {
	if after.Value == nil {
		return draft.ApplicationRecord{}, false
	}

	beforeCount := 0
	if before.Value != nil {
		beforeCount =
			len(before.Value.Applications)
	}

	if len(after.Value.Applications) <= beforeCount {
		return draft.ApplicationRecord{}, false
	}

	return after.Value.Applications[len(after.Value.Applications)-1],
		true
}

// readApplyBackupStates读取Header时间戳备份及其文件身份。
func readApplyBackupStates(
	assetPath string,
) map[string]applyFileState {
	result := map[string]applyFileState{}

	matches, err := filepath.Glob(
		assetPath + ".backup.*",
	)
	if err != nil {
		return result
	}

	for _, path := range matches {
		state, readErr := readApplyFileState(
			path,
		)
		if readErr != nil {
			continue
		}
		result[path] = state
	}

	return result
}

// applyBackupCreated识别新建备份或同秒覆盖既有备份。
func applyBackupCreated(
	before map[string]applyFileState,
	assetPath string,
) bool {
	after := readApplyBackupStates(
		assetPath,
	)

	for path, afterState := range after {
		beforeState, found := before[path]
		if !found ||
			applyFileReplaced(
				beforeState,
				afterState,
			) {
			return true
		}
	}

	return false
}
