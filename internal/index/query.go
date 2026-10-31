// Search and runtime-discipline text generation.
//
// BuildRuntimeRules is the host Agent's behaviour contract. The complete index workflow must follow the Guide's
// automation.mode mapping:
//   - execute            → follow the current phase's Guide strictly; after Entries are applied, Verify and ask for
//     a new Guide; repair_required repairs the failed items automatically and re-Stages; only stopped stops;
//   - prepare_and_review → finish Stage, Check, and Diff, and stop before Apply;
//   - observe            → observe only: no candidates, no Stage, no Apply;
//   - blocked            → stop at once and wait for the maintainer;
//   - complete           → end the loop.
//
// R63, the pure model semantic generation contract:
// Header, tags, F/R/A/S, and Curation semantics must be generated item by item by the current model after reading the real content.
// The tool may only carry facts, batches, validation, audit, and disk writes; it cannot stand in for the model's understanding.
//
// F length is a soft semantic quality goal, not a machine hard gate. Semantic completeness comes first, and length must never block
// the Host Agent's automatic Stage, Check, or Apply.
//
// Complete cognition reuse:
// AOCI's theoretical premise is reading the complete current index before a task to build a system-wide view, not reading a few Entries on demand.
// As long as the complete index is still in a valid context and its version has not changed, it should keep being reused.
//
// Unified closing:
// Write-back happens in the final phase, after every business change, formatting, lint, test, and necessary quality check has finished.
// Changing a managed file again after maintenance invalidates the result.
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
