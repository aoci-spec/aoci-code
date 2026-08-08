// 条目起草Prompt编译器。
//
// 普通模式:
//   - SourceText必须非空;
//   - 源码原文是唯一事实依据;
//   - 输出行为与引入文件级策展前保持一致。
//
// 文件级策展收录模式:
//   - Curation非nil;
//   - Role、Reason、ProfileReason和SourceSHA256必须完整;
//   - 特殊文件可不注入正文;
//   - 事实纪律切换到文档资产entry-curation-rules.txt。
package prompt

import (
	"errors"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// EntryCurationContext 是有效include决策提供给Prompt的语义事实。
//
// 它不包含Agent、Model和UpdatedAt，避免把治理审计元数据写进FRAS正文。
type EntryCurationContext struct {
	Role          string
	Reason        string
	Confidence    int
	SourceSHA256  string
	ProfileReason string
	SizeBytes     int64
	Lines         int
	Ext           string
}

// EntryInput 编译一条Entry候选所需的全部纯数据。
type EntryInput struct {
	RelPath          string
	SourceText       string
	HeaderText       string
	SuggestedSection string
	NeighborEntries  []string
	OldEntry         string
	Curation         *EntryCurationContext
}

type entryUserTemplateData struct {
	RelPath             string
	HeaderText          string
	HasSuggestedSection bool
	SuggestedSection    string
	NeighborEntries     []string
	IsUpdate            bool
	OldEntry            string
	Curation            *EntryCurationContext
	HasSourceText       bool
	SourceText          string
}

// BuildEntryMessages 编译Entry起草的system/user两段文本。
func BuildEntryMessages(
	in EntryInput,
) (
	string,
	string,
	error,
) {
	if strings.TrimSpace(in.RelPath) == "" {
		return "", "", errors.New(
			"EntryInput.RelPath 不能为空",
		)
	}
	if strings.TrimSpace(in.HeaderText) == "" {
		return "", "", errors.New(
			"EntryInput.HeaderText 不能为空(D40: 字典必须注入,防臆造标签)",
		)
	}

	curationMode := in.Curation != nil
	if !curationMode &&
		strings.TrimSpace(in.SourceText) == "" {
		return "", "", errors.New(
			"EntryInput.SourceText 不能为空(D37: 源码原文是唯一事实依据)",
		)
	}

	if curationMode {
		if err := validateEntryCurationContext(
			in.Curation,
		); err != nil {
			return "", "", err
		}
	}

	isUpdate := strings.TrimSpace(
		in.OldEntry,
	) != ""

	assetIDs := []textassets.ID{
		textassets.PromptEntryRole,
		textassets.PromptEntryOutputRules,
		textassets.PromptEntryFieldRules,
		textassets.PromptEntryDictRules,
	}
	if curationMode {
		assetIDs = append(assetIDs, textassets.PromptEntryCurationRules)
	} else {
		assetIDs = append(assetIDs, textassets.PromptEntryFactRules)
	}
	if isUpdate {
		assetIDs = append(assetIDs, textassets.PromptEntryUpdateRules)
	}

	var systemBuilder strings.Builder
	for position, assetID := range assetIDs {
		value, loadErr := loadPromptAsset(assetID)
		if loadErr != nil {
			return "", "", loadErr
		}
		if position > 0 {
			systemBuilder.WriteString("\n\n")
		}
		systemBuilder.WriteString(value)
	}

	oldEntry := ""
	if isUpdate {
		oldEntry = strings.TrimRight(
			in.OldEntry,
			"\n",
		) + "\n"
	}

	hasSourceText := strings.TrimSpace(
		in.SourceText,
	) != ""

	sourceText := ""
	if hasSourceText {
		sourceText = ensurePromptTrailingNewline(
			in.SourceText,
		)
	}

	user, err := textassets.Render(
		textassets.ActiveLocale(),
		textassets.PromptEntryUser,
		entryUserTemplateData{
			RelPath: in.RelPath,
			HeaderText: ensurePromptTrailingNewline(
				in.HeaderText,
			),
			HasSuggestedSection: strings.TrimSpace(
				in.SuggestedSection,
			) != "",
			SuggestedSection: in.SuggestedSection,
			NeighborEntries:  in.NeighborEntries,
			IsUpdate:         isUpdate,
			OldEntry:         oldEntry,
			Curation:         in.Curation,
			HasSourceText:    hasSourceText,
			SourceText:       sourceText,
		},
	)
	if err != nil {
		return "", "", err
	}

	return systemBuilder.String(),
		user,
		nil
}

func validateEntryCurationContext(
	context *EntryCurationContext,
) error {
	if context == nil {
		return errors.New(
			"EntryInput.Curation 不能为空",
		)
	}
	if strings.TrimSpace(context.Role) == "" {
		return errors.New(
			"EntryInput.Curation.Role 不能为空",
		)
	}
	if strings.TrimSpace(context.Reason) == "" {
		return errors.New(
			"EntryInput.Curation.Reason 不能为空",
		)
	}
	if strings.TrimSpace(
		context.ProfileReason,
	) == "" {
		return errors.New(
			"EntryInput.Curation.ProfileReason 不能为空",
		)
	}
	if len(strings.TrimSpace(
		context.SourceSHA256,
	)) != 64 {
		return errors.New(
			"EntryInput.Curation.SourceSHA256 必须是64位摘要",
		)
	}
	if context.Confidence < 0 ||
		context.Confidence > 100 {
		return errors.New(
			"EntryInput.Curation.Confidence 必须在0至100之间",
		)
	}

	return nil
}
