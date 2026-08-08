// Host-Agent Manifest完整性、Plan过期分类与Curation集合诊断测试。
package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
)

func validHostAgentManifestForGuard() *draft.Manifest {
	return &draft.Manifest{
		RunID:            "20260716T100000Z",
		Kind:             draft.KindEntries,
		GenerationSource: draft.GenerationSourceHostAgent,
		AgentName:        "codex",
		PlanID:           strings.Repeat("a", 64),
		IndexSHA256:      strings.Repeat("b", 64),
		HeaderSHA256:     strings.Repeat("c", 64),
		GenerationHash:   strings.Repeat("d", 64),
		Provider:         agentStageProvider,
	}
}

func TestValidateHostAgentManifestStateRejectsDamage(
	t *testing.T,
) {
	tests := []struct {
		name     string
		mutate   func(*draft.Manifest)
		wantPart string
	}{
		{
			name: "missing_plan_id",
			mutate: func(manifest *draft.Manifest) {
				manifest.PlanID = ""
			},
			wantPart: "plan_id必须填写",
		},
		{
			name: "short_index_hash",
			mutate: func(manifest *draft.Manifest) {
				manifest.IndexSHA256 =
					strings.Repeat("a", 63)
			},
			wantPart: "当前长度63",
		},
		{
			name: "non_hex_generation_hash",
			mutate: func(manifest *draft.Manifest) {
				manifest.GenerationHash =
					"g" + strings.Repeat("a", 63)
			},
			wantPart: "不是合法SHA-256",
		},
		{
			name: "uppercase_hash",
			mutate: func(manifest *draft.Manifest) {
				manifest.PlanID =
					strings.Repeat("A", 64)
			},
			wantPart: "小写64位",
		},
		{
			name: "non_canonical_agent",
			mutate: func(manifest *draft.Manifest) {
				manifest.AgentName = "Codex"
			},
			wantPart: "不是规范化",
		},
		{
			name: "wrong_provider",
			mutate: func(manifest *draft.Manifest) {
				manifest.Provider = "endpoint"
			},
			wantPart: "provider",
		},
		{
			name: "header_intent_on_entries",
			mutate: func(manifest *draft.Manifest) {
				manifest.HeaderIntent = agentHeaderStageIntentSemanticRefresh
			},
			wantPart: "semantic_refresh",
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				manifest := validHostAgentManifestForGuard()
				current.mutate(manifest)

				err := validateHostAgentManifestState(
					manifest,
					draft.KindEntries,
					false,
				)
				if err == nil ||
					!strings.Contains(
						err.Error(),
						current.wantPart,
					) {
					t.Fatalf(
						"错误应包含%q: %v",
						current.wantPart,
						err,
					)
				}
			},
		)
	}
}

func TestValidateHostAgentManifestStateRequiresCurationHash(
	t *testing.T,
) {
	manifest := validHostAgentManifestForGuard()
	manifest.Kind = draft.KindCuration

	err := validateHostAgentManifestState(
		manifest,
		draft.KindCuration,
		true,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"curation_sha256必须填写",
		) {
		t.Fatalf(
			"Curation Manifest必须携带curation_sha256: %v",
			err,
		)
	}
}

func TestValidateHostAgentCurationTargetsReportsSetDifferences(
	t *testing.T,
) {
	firstHash := strings.Repeat("a", 64)
	secondHash := strings.Repeat("b", 64)

	expected := []agentPlanCurationTarget{
		{
			Path:         "a.bin",
			SourceSHA256: firstHash,
		},
		{
			Path:         "b.bin",
			SourceSHA256: secondHash,
		},
	}

	manifest := validHostAgentManifestForGuard()
	manifest.Kind = draft.KindCuration
	manifest.CurationSHA256 = strings.Repeat("e", 64)
	manifest.Entries = []draft.EntryStatus{
		{
			Path:         "a.bin",
			Status:       "drafted",
			SourceSHA256: firstHash,
		},
		{
			Path:         "a.bin",
			Status:       "drafted",
			SourceSHA256: firstHash,
		},
		{
			Path:         "extra.bin",
			Status:       "warned",
			SourceSHA256: strings.Repeat("c", 64),
		},
	}

	err := validateHostAgentCurationTargets(
		manifest,
		expected,
	)
	if err == nil {
		t.Fatal(
			"缺失、额外、重复和非法状态必须整批拒绝",
		)
	}

	message := err.Error()
	for _, part := range []string{
		"missing=[b.bin]",
		"extra=[extra.bin]",
		"duplicate=[a.bin]",
		`invalid_status=[extra.bin="warned"]`,
	} {
		if !strings.Contains(message, part) {
			t.Fatalf(
				"集合诊断缺少%q: %s",
				part,
				message,
			)
		}
	}
}

func TestValidateHostAgentCurationTargetsReportsSourceMismatch(
	t *testing.T,
) {
	expectedHash := strings.Repeat("a", 64)
	submittedHash := strings.Repeat("b", 64)

	manifest := validHostAgentManifestForGuard()
	manifest.Kind = draft.KindCuration
	manifest.CurationSHA256 = strings.Repeat("e", 64)
	manifest.Entries = []draft.EntryStatus{
		{
			Path:         "a.bin",
			Status:       "drafted",
			SourceSHA256: submittedHash,
		},
	}

	err := validateHostAgentCurationTargets(
		manifest,
		[]agentPlanCurationTarget{
			{
				Path:         "a.bin",
				SourceSHA256: expectedHash,
			},
		},
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"source_mismatch=[a.bin",
		) ||
		!strings.Contains(
			err.Error(),
			"bbbbbbbb…bbbbbbbb",
		) ||
		!strings.Contains(
			err.Error(),
			"aaaaaaaa…aaaaaaaa",
		) {
		t.Fatalf(
			"源码差异应显示路径及摘要首尾: %v",
			err,
		)
	}
}

func TestEntriesApplyClassifiesManifestDamageBeforeWriting(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	plan, _ := agentStageCurrentPlan(
		t,
		root,
	)
	target := agentStageFindTarget(
		t,
		plan,
		"new.go",
	)

	cfg, doc, indexPath :=
		agentPlanLoadDocument(t, root)

	result, err := stageAgentEntries(
		root,
		cfg,
		doc,
		indexPath,
		agentStageRequest{
			Version: agentStageVersion,
			PlanID:  plan.PlanID,
			Agent:   "codex",
			Entries: []agentStageEntry{
				{
					Path:         "new.go",
					SourceSHA256: target.SourceSHA256,
					Entry: "new.go[XAP7T]: F:Manifest损坏测试 | " +
						"R:- | A:- | S:-",
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	hash, err := draft.HashFiles(
		root,
		result.RunID,
		[]string{
			entryDraftFileName("new.go"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := draft.AppendReview(
		root,
		result.RunID,
		draft.ReviewRecord{
			Action:     draft.ReviewActionCheck,
			DraftHash:  hash,
			PathsCount: 1,
			Passed:     1,
		},
	); err != nil {
		t.Fatal(err)
	}

	manifest, err := draft.LoadManifest(
		root,
		result.RunID,
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.PlanID = "broken"
	if err := draft.SaveManifest(
		root,
		manifest,
	); err != nil {
		t.Fatal(err)
	}

	indexBefore := readEntriesIndex(
		t,
		root,
	)
	baselinePath := filepath.Join(
		root,
		".aoci",
		"baseline.json",
	)
	baselineBefore, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}

	output, applyErr := runEntriesApplyForAudit(
		t,
		root,
		result.RunID,
	)

	var exitErr *ExitError
	if !errors.As(applyErr, &exitErr) ||
		exitErr.Code != ExitInvalid {
		t.Fatalf(
			"Manifest损坏应ExitInvalid: %v\n%s",
			applyErr,
			output,
		)
	}
	if !strings.Contains(
		applyErr.Error(),
		"Manifest Generation State损坏",
	) ||
		!strings.Contains(
			applyErr.Error(),
			"重新Stage",
		) ||
		strings.Contains(
			applyErr.Error(),
			"Generation Plan已过期",
		) {
		t.Fatalf(
			"Manifest损坏诊断分类错误: %v",
			applyErr,
		)
	}

	if readEntriesIndex(t, root) != indexBefore {
		t.Fatal(
			"Manifest损坏拒绝不得修改正式索引",
		)
	}

	baselineAfter, err := os.ReadFile(
		baselinePath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(baselineAfter) !=
		string(baselineBefore) {
		t.Fatal(
			"Manifest损坏拒绝不得前移Baseline",
		)
	}
}

func TestNonHostAgentManifestKeepsCompatibility(
	t *testing.T,
) {
	manifest := &draft.Manifest{
		GenerationSource: draft.GenerationSourceEndpoint,
	}

	note, err := guardHostAgentGenerationPlan(
		nil,
		"",
		nil,
		manifest,
		draft.KindEntries,
		agentPlanStageEntriesRequired,
	)
	if err != nil || note != "" {
		t.Fatalf(
			"Endpoint草稿不应进入Host-Agent硬闸: note=%q err=%v",
			note,
			err,
		)
	}
}
