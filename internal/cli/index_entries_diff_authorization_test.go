// Entries Apply的Diff授权与历史兼容测试。
package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func entriesAuthorizationReview(
	action,
	hash string,
) draft.ReviewRecord {
	return draft.ReviewRecord{
		Action:    action,
		DraftHash: hash,
	}
}

func TestGuardReviewedDraftHashHostAgentRequiresDiff(
	t *testing.T,
) {
	hash := strings.Repeat(
		"a",
		64,
	)

	manifest := &draft.Manifest{
		RunID:            "20260721T190000Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceHostAgent,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				hash,
			),
		},
	}

	warning, err := guardReviewedDraftHash(
		manifest,
		hash,
	)

	if err == nil {
		t.Fatal(
			"Host-Agent只有Check时必须拒绝Apply",
		)
	}
	if warning != "" {
		t.Fatalf(
			"Host-Agent硬拒不得同时返回兼容Warning: %q",
			warning,
		)
	}

	for _, anchor := range []string{
		"Host-Agent",
		"Check只负责机器预检",
		"entries diff",
	} {
		if !strings.Contains(
			err.Error(),
			anchor,
		) {
			t.Fatalf(
				"Host-Agent缺Diff错误缺少%q: %v",
				anchor,
				err,
			)
		}
	}
}

func TestGuardReviewedDraftHashHostAgentDiffPasses(
	t *testing.T,
) {
	hash := strings.Repeat(
		"b",
		64,
	)

	manifest := &draft.Manifest{
		RunID:            "20260721T190001Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceHostAgent,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				hash,
			),
			entriesAuthorizationReview(
				draft.ReviewActionDiff,
				hash,
			),
		},
	}

	warning, err := guardReviewedDraftHash(
		manifest,
		hash,
	)

	if err != nil ||
		warning != "" {
		t.Fatalf(
			"Host-Agent同内容Check与Diff后应无声放行: warning=%q err=%v",
			warning,
			err,
		)
	}
}

func TestGuardReviewedDraftHashRecheckCannotReplaceOldDiff(
	t *testing.T,
) {
	oldHash := strings.Repeat(
		"c",
		64,
	)
	currentHash := strings.Repeat(
		"d",
		64,
	)

	manifest := &draft.Manifest{
		RunID:            "20260721T190002Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceHostAgent,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				oldHash,
			),
			entriesAuthorizationReview(
				draft.ReviewActionDiff,
				oldHash,
			),
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				currentHash,
			),
		},
	}

	_, err := guardReviewedDraftHash(
		manifest,
		currentHash,
	)

	if err == nil {
		t.Fatal(
			"Diff后改稿并重新Check仍必须重新Diff",
		)
	}

	for _, anchor := range []string{
		"最近 diff 草稿摘要",
		"重新运行 aoci index entries diff",
	} {
		if !strings.Contains(
			err.Error(),
			anchor,
		) {
			t.Fatalf(
				"旧Diff拒绝文案缺少%q: %v",
				anchor,
				err,
			)
		}
	}
}

func TestGuardReviewedDraftHashEndpointCheckRemainsValid(
	t *testing.T,
) {
	hash := strings.Repeat(
		"e",
		64,
	)

	manifest := &draft.Manifest{
		RunID:            "20260721T190003Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceEndpoint,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				hash,
			),
		},
	}

	warning, err := guardReviewedDraftHash(
		manifest,
		hash,
	)

	if err != nil ||
		warning != "" {
		t.Fatalf(
			"Endpoint同内容Check在过渡期应无声授权: warning=%q err=%v",
			warning,
			err,
		)
	}
}

func TestGuardReviewedDraftHashLegacyCheckRemainsValid(
	t *testing.T,
) {
	hash := strings.Repeat(
		"f",
		64,
	)

	manifest := &draft.Manifest{
		RunID: "20260721T190004Z",
		Kind:  draft.KindEntries,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				hash,
			),
		},
	}

	warning, err := guardReviewedDraftHash(
		manifest,
		hash,
	)

	if err != nil ||
		warning != "" {
		t.Fatalf(
			"来源为空的既有Check合同应保持: warning=%q err=%v",
			warning,
			err,
		)
	}
}

func TestGuardReviewedDraftHashLegacyWithoutReviewsWarns(
	t *testing.T,
) {
	manifest := &draft.Manifest{
		RunID: "20260721T190005Z",
		Kind:  draft.KindEntries,
	}

	warning, err := guardReviewedDraftHash(
		manifest,
		strings.Repeat("1", 64),
	)

	if err != nil {
		t.Fatalf(
			"完全无Review的历史批次应兼容放行: %v",
			err,
		)
	}
	if !strings.Contains(
		warning,
		"无 P-23 内容审阅记录",
	) ||
		!strings.Contains(
			warning,
			"旧批次兼容规则",
		) {
		t.Fatalf(
			"旧批次兼容必须明确警告: %q",
			warning,
		)
	}
}

func TestGuardReviewedDraftHashEndpointCheckMismatchRejects(
	t *testing.T,
) {
	manifest := &draft.Manifest{
		RunID:            "20260721T190006Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceEndpoint,
		Reviews: []draft.ReviewRecord{
			entriesAuthorizationReview(
				draft.ReviewActionCheck,
				strings.Repeat("2", 64),
			),
		},
	}

	_, err := guardReviewedDraftHash(
		manifest,
		strings.Repeat("3", 64),
	)

	if err == nil ||
		!strings.Contains(
			err.Error(),
			"P-23 防线",
		) ||
		!strings.Contains(
			err.Error(),
			"摘要",
		) {
		t.Fatalf(
			"Endpoint Check授权也不得绕过内容摘要漂移: %v",
			err,
		)
	}
}
