// Entries Workflow消费正式文件级include决策的测试。
package workflow

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
)

func saveWorkflowIncludeDecision(
	t *testing.T,
	root,
	rel,
	role,
	reason string,
) {
	t.Helper()

	profile, err := curation.ProfilePath(
		root,
		rel,
	)
	if err != nil {
		t.Fatal(err)
	}

	document := &curation.Document{
		Version: curation.Version,
		Decisions: []curation.Decision{
			{
				Path:         rel,
				Decision:     curation.DecisionInclude,
				Role:         role,
				Reason:       reason,
				Confidence:   98,
				SourceSHA256: profile.SourceSHA256,
				Agent:        "codex",
				Model:        "test-model",
				UpdatedAt:    "2026-07-15T00:00:00Z",
			},
		},
	}

	if err := curation.Save(
		root,
		document,
	); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareEntrySourceAcceptsIncludedSpecialFiles(
	t *testing.T,
) {
	root, _, _ := buildRepo(t)

	if err := writeRepoFile(
		t,
		root,
		"empty.marker",
		"",
	); err != nil {
		t.Fatal(err)
	}
	if err := writeRepoFile(
		t,
		root,
		"binary.marker",
		"x\x00y",
	); err != nil {
		t.Fatal(err)
	}
	if err := writeRepoFile(
		t,
		root,
		"large.marker",
		strings.Repeat(
			"x",
			curation.OversizeBytes+1,
		),
	); err != nil {
		t.Fatal(err)
	}

	decisions := []curation.Decision{}
	for _, rel := range []string{
		"empty.marker",
		"binary.marker",
		"large.marker",
	} {
		profile, err := curation.ProfilePath(
			root,
			rel,
		)
		if err != nil {
			t.Fatal(err)
		}

		decisions = append(
			decisions,
			curation.Decision{
				Path:         rel,
				Decision:     curation.DecisionInclude,
				Role:         "特殊协议标记",
				Reason:       "存在本身具有独立系统语义",
				Confidence:   95,
				SourceSHA256: profile.SourceSHA256,
				Agent:        "codex",
				UpdatedAt:    "2026-07-15T00:00:00Z",
			},
		)
	}

	document := &curation.Document{
		Version:   curation.Version,
		Decisions: decisions,
	}

	for _, rel := range []string{
		"empty.marker",
		"binary.marker",
		"large.marker",
	} {
		preparation, skipReason :=
			prepareEntrySource(
				root,
				rel,
				document,
			)

		if skipReason != "" {
			t.Fatalf(
				"有效include不得再次跳过%s: %s",
				rel,
				skipReason,
			)
		}
		if preparation.Curation == nil {
			t.Fatalf(
				"%s应形成策展Prompt上下文",
				rel,
			)
		}
		if preparation.SourceText != "" {
			t.Fatalf(
				"%s特殊文件不应注入正文",
				rel,
			)
		}
	}
}

func TestRunEntriesDraftGeneratesIncludedEmptyFile(
	t *testing.T,
) {
	root, cfg, doc := buildRepo(t)
	cfg.AI.PromptSnapshot = "full"

	if err := writeRepoFile(
		t,
		root,
		"py.typed",
		"",
	); err != nil {
		t.Fatal(err)
	}

	saveWorkflowIncludeDecision(
		t,
		root,
		"py.typed",
		"声明Python包提供类型信息",
		"空文件存在本身是包级类型协议标记",
	)

	server := fakeEndpoint(
		t,
		"py.typed[XC5T]: F:声明Python包提供类型信息 | R:- | A:- | S:文件存在本身启用类型检查器识别。",
		true,
		http.StatusOK,
		nil,
	)
	defer server.Close()

	cfg.AI.BaseURL = server.URL

	result, err := RunEntriesDraft(
		context.Background(),
		root,
		cfg,
		doc,
		newTestClient(t, server.URL),
		[]string{"py.typed"},
		nil,
	)
	if err != nil {
		t.Fatalf(
			"有效include空文件生成失败: %v",
			err,
		)
	}

	if result.Drafted != 1 ||
		result.Skipped != 0 {
		t.Fatalf(
			"有效include应drafted而非skipped: %+v",
			result,
		)
	}

	snapshot, err := draft.ReadFile(
		root,
		result.RunID,
		"py.typed.prompt.txt",
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, anchor := range []string{
		"策展上下文",
		"声明Python包提供类型信息",
		"空文件存在本身是包级类型协议标记",
		"目标文件正文未注入",
	} {
		if !strings.Contains(
			string(snapshot),
			anchor,
		) {
			t.Fatalf(
				"策展Prompt快照缺少%q:\n%s",
				anchor,
				snapshot,
			)
		}
	}
}

func TestPrepareEntrySourceRejectsStaleSpecialDecision(
	t *testing.T,
) {
	root, _, _ := buildRepo(t)

	if err := writeRepoFile(
		t,
		root,
		"binary.marker",
		"a\x00b",
	); err != nil {
		t.Fatal(err)
	}

	profile, err := curation.ProfilePath(
		root,
		"binary.marker",
	)
	if err != nil {
		t.Fatal(err)
	}

	document := &curation.Document{
		Version: curation.Version,
		Decisions: []curation.Decision{
			{
				Path:         "binary.marker",
				Decision:     curation.DecisionInclude,
				Role:         "二进制协议标记",
				Reason:       "测试过期摘要拒绝",
				Confidence:   90,
				SourceSHA256: profile.SourceSHA256,
				Agent:        "codex",
				UpdatedAt:    "2026-07-15T00:00:00Z",
			},
		},
	}

	if err := writeRepoFile(
		t,
		root,
		"binary.marker",
		"c\x00d",
	); err != nil {
		t.Fatal(err)
	}

	_, skipReason := prepareEntrySource(
		root,
		"binary.marker",
		document,
	)

	if !strings.Contains(
		skipReason,
		"source_sha256",
	) {
		t.Fatalf(
			"过期特殊文件决策必须拒绝: %q",
			skipReason,
		)
	}
}
