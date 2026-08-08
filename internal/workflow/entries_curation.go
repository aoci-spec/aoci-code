// Entry生成前的文件画像、策展决策验证和事实输入准备。
package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/prompt"
)

type entrySourcePreparation struct {
	SourceText   string
	SourceSHA256 string
	Curation     *prompt.EntryCurationContext
}

func loadEntryCurationDocument(
	root string,
) (*curation.Document, error) {
	document, _, _, err := curation.Load(root)
	if err != nil {
		return nil, fmt.Errorf(
			"加载文件级策展资产失败: %w",
			err,
		)
	}

	return document, nil
}

// prepareEntrySource 只使用curation.ProfilePath判定empty/binary/oversize。
//
// 普通文件继续读取源码原文。
// 特殊文件只有在有效include决策与当前source_sha256匹配时才进入Prompt。
func prepareEntrySource(
	root,
	rel string,
	document *curation.Document,
) (
	entrySourcePreparation,
	string,
) {
	profile, err := curation.ProfilePath(
		root,
		rel,
	)
	if err != nil {
		return entrySourcePreparation{},
			"读取或画像失败: " + err.Error()
	}

	decision, hasDecision :=
		curation.DecisionByPath(
			document,
			rel,
		)

	validInclude :=
		hasDecision &&
			decision.Decision ==
				curation.DecisionInclude &&
			decision.SourceSHA256 ==
				profile.SourceSHA256

	special := profile.Reason ==
		curation.ProfileReasonEmpty ||
		profile.Reason ==
			curation.ProfileReasonBinary ||
		profile.Reason ==
			curation.ProfileReasonOversize

	if special && !validInclude {
		switch {
		case hasDecision &&
			decision.SourceSHA256 !=
				profile.SourceSHA256:
			return entrySourcePreparation{},
				"文件级策展决策已因source_sha256变化而失效"

		case hasDecision &&
			decision.Decision ==
				curation.DecisionExclude:
			return entrySourcePreparation{},
				"文件级策展决策为exclude，不进入Entry生成"

		default:
			return entrySourcePreparation{},
				"特殊文件尚未完成有效include策展"
		}
	}

	preparation := entrySourcePreparation{
		SourceSHA256: profile.SourceSHA256,
	}

	if validInclude {
		preparation.Curation =
			&prompt.EntryCurationContext{
				Role:          decision.Role,
				Reason:        decision.Reason,
				Confidence:    decision.Confidence,
				SourceSHA256:  profile.SourceSHA256,
				ProfileReason: profile.Reason,
				SizeBytes:     profile.SizeBytes,
				Lines:         profile.Lines,
				Ext:           profile.Ext,
			}
	}

	if special {
		return preparation, ""
	}

	data, err := os.ReadFile(
		filepath.Join(
			root,
			filepath.FromSlash(rel),
		),
	)
	if err != nil {
		return entrySourcePreparation{},
			"读取失败: " + err.Error()
	}

	if curation.HashBytes(data) !=
		profile.SourceSHA256 {
		return entrySourcePreparation{},
			"文件在画像后发生变化，请重新运行生成流程"
	}

	preparation.SourceText = string(data)
	return preparation, ""
}
