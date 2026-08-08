// 检索与运行时纪律文本生成。
//
// BuildRuntimeRules是宿主Agent行为合同。完整索引工作流必须遵循Guide的
// automation.mode映射:
//   - execute            → 严格执行当前阶段Guide；Entries applied后Verify并
//     重新Guide，repair_required自动修复失败项并重新Stage，stopped才停止;
//   - prepare_and_review → 完成Stage、Check、Diff,在Apply前停止;
//   - observe            → 只观察,不生成候选、不Stage、不Apply;
//   - blocked            → 立即停止等待维护者;
//   - complete           → 结束循环。
//
// R63纯模型语义生成合同:
// Header、标签、F/R/A/S及Curation语义必须由当前模型阅读真实内容后逐项生成。
// 工具只能承担事实传递、批次、校验、审计与落盘,不能替代模型理解。
//
// F长度属于柔性语义质量目标,不是机器硬闸。语义完整优先,不得因长度阻断
// Host-Agent自动Stage、Check或Apply。
//
// 完整认知复用:
// AOCI的理论前提是任务前完整读取当前索引建立系统全貌,不是按需读取少量Entry。
// 只要完整索引仍处于有效上下文且版本未变化就应持续复用。
//
// 收尾统一制:
// 回写发生在全部业务修改、格式化、Lint、测试及必要质量检查结束后的最终阶段。
// 维护后再次修改受管文件会使结果失效。
package index

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

type tagFilter struct {
	key string
	op  string
	val string
}

func ParseTagFilter(
	value string,
) (*tagFilter, error) {
	_ = textassets.ContractUIMessages
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	operator := "="
	position := strings.Index(
		value,
		">=",
	)
	if position > 0 {
		operator = ">="
	} else {
		position = strings.Index(
			value,
			"=",
		)
		if position <= 0 {
			message, messageErr := textassets.Message(
				textassets.ActiveLocale(), "search.bad_filter_format", value,
			)
			if messageErr != nil {
				return nil, messageErr
			}
			return nil, errors.New(message)
		}
	}

	key := strings.ToUpper(
		strings.TrimSpace(
			value[:position],
		),
	)
	filterValue := strings.TrimSpace(
		value[position+len(operator):],
	)

	if key == "" ||
		filterValue == "" ||
		!strings.Contains(
			"ABCDE",
			key,
		) ||
		len(key) != 1 {
		message, messageErr := textassets.Message(
			textassets.ActiveLocale(), "search.bad_filter_dimension", value,
		)
		if messageErr != nil {
			return nil, messageErr
		}
		return nil, errors.New(message)
	}
	if operator == ">=" &&
		key != "C" {
		message, messageErr := textassets.Message(
			textassets.ActiveLocale(), "search.bad_filter_operator",
		)
		if messageErr != nil {
			return nil, messageErr
		}
		return nil, errors.New(message)
	}

	return &tagFilter{
		key: key,
		op:  operator,
		val: filterValue,
	}, nil
}

func (filter *tagFilter) match(
	entry *Entry,
) bool {
	got, ok := entry.TagsParsed[filter.key]
	if !ok ||
		got == "" {
		return false
	}
	if filter.op == "=" {
		return strings.EqualFold(
			got,
			filter.val,
		)
	}

	gotNumber, gotErr := strconv.Atoi(got)
	wantedNumber, wantedErr := strconv.Atoi(
		filter.val,
	)
	if gotErr != nil ||
		wantedErr != nil {
		return false
	}

	return gotNumber >= wantedNumber
}

func Search(
	doc *Document,
	keyword,
	tagFilterString string,
) (
	matched []*Entry,
	skippedUntagged int,
	err error,
) {
	filter, err := ParseTagFilter(
		tagFilterString,
	)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(keyword) == "" &&
		filter == nil {
		message, messageErr := textassets.Message(
			textassets.ActiveLocale(), "search.missing_query",
		)
		if messageErr != nil {
			return nil, 0, messageErr
		}
		return nil, 0, errors.New(message)
	}

	lowerKeyword := strings.ToLower(
		strings.TrimSpace(keyword),
	)
	matched = []*Entry{}

	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			if filter != nil {
				if len(entry.TagsParsed) == 0 {
					skippedUntagged++
					continue
				}
				if !filter.match(entry) {
					continue
				}
			}

			if lowerKeyword != "" &&
				!strings.Contains(
					strings.ToLower(
						entry.FullLine,
					),
					lowerKeyword,
				) {
				continue
			}

			matched = append(
				matched,
				entry,
			)
		}
	}

	return matched, skippedUntagged, nil
}

func BuildRuntimeRules(
	doc *Document,
	machineValues ...int,
) (string, error) {
	_ = doc
	threshold := machinecontract.CognitionRefreshThresholdDefault
	chunkTokens := machinecontract.OverviewChunkTokensDefault
	if len(machineValues) > 0 {
		threshold = machineValues[0]
	}
	if len(machineValues) > 1 {
		chunkTokens = machineValues[1]
	}
	if threshold < machinecontract.CognitionRefreshThresholdMin ||
		threshold > machinecontract.CognitionRefreshThresholdMax {
		return "", fmt.Errorf("invalid cognition refresh threshold: %d", threshold)
	}
	if chunkTokens < machinecontract.OverviewChunkTokensMin ||
		chunkTokens > machinecontract.OverviewChunkTokensMax {
		return "", fmt.Errorf("invalid overview chunk tokens: %d", chunkTokens)
	}

	rules, err := textassets.Load(
		textassets.ActiveLocale(),
		textassets.ContractRuntimeRules,
	)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(rules, "\n") {
		rules += "\n"
	}
	return rules + fmt.Sprintf(
		"\ncognition_refresh_threshold: %d\noverview_delivery.chunk_tokens: %d\n"+
			"overview_delivery.chunk_tokens_min: %d\noverview_delivery.chunk_tokens_max: %d\n",
		threshold, chunkTokens, machinecontract.OverviewChunkTokensMin, machinecontract.OverviewChunkTokensMax,
	), nil
}
