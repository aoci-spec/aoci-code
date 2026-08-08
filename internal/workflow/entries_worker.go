// 单文件Entry生成Worker与Prompt快照。
package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/llm"
	"github.com/aoci-spec/aoci-code/internal/prompt"
)

func flattenRel(
	rel string,
) string {
	return strings.ReplaceAll(
		rel,
		"/",
		"__",
	)
}

func draftOneEntry(
	ctx context.Context,
	root,
	runID string,
	cfg *config.Config,
	doc *index.Document,
	client *llm.Client,
	headerText,
	rawPath string,
	oldEntries map[string]string,
	curationDocument *curation.Document,
) fileOutcome {
	rel, err := afs.NormalizeRelPath(
		rawPath,
	)
	if err != nil {
		return fileOutcome{
			status: draft.EntryStatus{
				Path:   rawPath,
				Status: "skipped",
				Note:   "路径不合法: " + err.Error(),
			},
		}
	}

	preparation, skipReason := prepareEntrySource(
		root,
		rel,
		curationDocument,
	)
	if skipReason != "" {
		return fileOutcome{
			status: draft.EntryStatus{
				Path:   rel,
				Status: "skipped",
				Note:   skipReason,
			},
		}
	}

	input := prompt.EntryInput{
		RelPath:    rel,
		SourceText: preparation.SourceText,
		HeaderText: headerText,
		NeighborEntries: neighborEntries(
			doc,
			rel,
		),
		OldEntry: oldEntries[rel],
		Curation: preparation.Curation,
	}

	systemText, userText, err := prompt.BuildEntryMessages(
		input,
	)
	if err != nil {
		return fileOutcome{
			status: draft.EntryStatus{
				Path:   rel,
				Status: "failed",
				Note:   "prompt编译失败: " + err.Error(),
			},
		}
	}

	temperature := headerDraftTemperature

	completion, err := client.Complete(
		ctx,
		llm.CompletionRequest{
			Messages: []llm.Message{
				{
					Role:    "system",
					Content: systemText,
				},
				{
					Role:    "user",
					Content: userText,
				},
			},
			Temperature: &temperature,
		},
	)
	if err != nil {
		return fileOutcome{
			status: draft.EntryStatus{
				Path:   rel,
				Status: "failed",
				Note:   "端点调用失败: " + err.Error(),
			},
			nonExact: true,
		}
	}

	outcome := fileOutcome{}

	if completion.Usage.Source == llm.TokenSourceExact {
		outcome.inTok = completion.Usage.InputTokens
		outcome.outTok = completion.Usage.OutputTokens
	} else {
		outcome.nonExact = true
		outcome.inTok = ledger.EstimateTokens(
			systemText + userText,
		)
		outcome.outTok = ledger.EstimateTokens(
			completion.Text,
		)
	}

	line := index.StripFences(
		completion.Text,
	)
	if strings.TrimSpace(line) == "" {
		outcome.status = draft.EntryStatus{
			Path:   rel,
			Status: "failed",
			Note:   "端点返回空草稿",
		}
		return outcome
	}

	status := "drafted"
	note := ""

	violations := index.ValidateEntryLine(
		rel,
		line,
	)
	if len(violations) > 0 {
		messages := []string{}

		for _, violation := range violations {
			messages = append(
				messages,
				"["+violation.Level+"] "+
					violation.Msg,
			)
		}

		status = "warned"
		note = strings.Join(
			messages,
			";",
		)
	}

	draftName := flattenRel(rel) +
		".entry.txt"

	if err := draft.WriteFile(
		root,
		runID,
		draftName,
		[]byte(line+"\n"),
	); err != nil {
		outcome.status = draft.EntryStatus{
			Path:   rel,
			Status: "failed",
			Note:   "草稿落盘失败: " + err.Error(),
		}
		return outcome
	}

	outcome.files = append(
		outcome.files,
		draftName,
	)

	snapshotName, snapshotErr := writeEntrySnapshot(
		root,
		runID,
		rel,
		cfg.AI.PromptSnapshot,
		systemText,
		userText,
		preparation.SourceText,
	)
	if snapshotErr != nil {
		note = strings.TrimPrefix(
			note+
				";快照落盘失败(草稿本体不受影响): "+
				snapshotErr.Error(),
			";",
		)

		if status == "drafted" {
			status = "warned"
		}
	} else if snapshotName != "" {
		outcome.files = append(
			outcome.files,
			snapshotName,
		)
	}

	outcome.status = draft.EntryStatus{
		Path:         rel,
		Status:       status,
		Note:         note,
		SourceSHA256: preparation.SourceSHA256,
	}

	return outcome
}

func neighborEntries(
	doc *index.Document,
	rel string,
) []string {
	directory := path.Dir(rel)
	result := []string{}

	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			if entry.RelPath == "" ||
				entry.RelPath == rel {
				continue
			}

			if path.Dir(entry.RelPath) != directory {
				continue
			}

			result = append(
				result,
				entry.FullLine,
			)

			if len(result) >= maxNeighborEntries {
				return result
			}
		}
	}

	return result
}

func writeEntrySnapshot(
	root,
	runID,
	rel,
	mode,
	systemText,
	userText,
	source string,
) (
	string,
	error,
) {
	var body string

	switch mode {
	case "full":
		body = userText

	case "redacted", "":
		if source == "" {
			// 特殊文件没有正文副本；策展上下文本身可进入审计快照。
			body = userText
		} else {
			fingerprint := fmt.Sprintf(
				"[源码段已脱敏 sha256:%s len:%d]\n",
				ledger.EndpointHash(source),
				len(source),
			)

			body = strings.Replace(
				userText,
				source,
				fingerprint,
				1,
			)
		}

	case "none":
		return "", nil

	default:
		return "", nil
	}

	snapshot := "===== system =====\n" +
		systemText +
		"\n\n===== user =====\n" +
		body

	if !strings.HasSuffix(
		snapshot,
		"\n",
	) {
		snapshot += "\n"
	}

	name := flattenRel(rel) +
		".prompt.txt"

	if err := draft.WriteFile(
		root,
		runID,
		name,
		[]byte(snapshot),
	); err != nil {
		return "", err
	}

	return name, nil
}
