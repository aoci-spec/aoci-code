// Curation请求的原始JSON字段和完整批次集合契约。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

type rawAgentCurationRequest struct {
	PlanID    json.RawMessage `json:"plan_id"`
	Agent     json.RawMessage `json:"agent"`
	Decisions json.RawMessage `json:"decisions"`
}

type rawAgentCurationDecision struct {
	Path         string          `json:"path"`
	SourceSHA256 json.RawMessage `json:"source_sha256"`
	Confidence   json.RawMessage `json:"confidence"`
}

type mappedAgentCurationCandidate struct {
	Position int
	Decision agentCurationDecision
}

func validateAgentCurationRawContract(
	data []byte,
) error {
	if err := validateAgentRequestJSON(data); err != nil {
		return err
	}

	var request rawAgentCurationRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return nil
	}

	if _, err := requireRawJSONString(
		request.PlanID,
		"plan_id",
	); err != nil {
		return err
	}
	if _, err := requireRawJSONString(
		request.Agent,
		"agent",
	); err != nil {
		return err
	}
	if err := requireRawJSONArray(
		request.Decisions,
		"decisions",
	); err != nil {
		return err
	}

	var decisions []rawAgentCurationDecision
	if err := json.Unmarshal(
		request.Decisions,
		&decisions,
	); err != nil {
		return nil
	}

	for position, decision := range decisions {
		sourceField := fmt.Sprintf(
			"decisions[%d].source_sha256",
			position,
		)
		if _, err := requireRawJSONString(
			decision.SourceSHA256,
			sourceField,
		); err != nil {
			return err
		}

		if err := validateRawCurationConfidence(
			position,
			decision.Path,
			decision.Confidence,
		); err != nil {
			return err
		}
	}

	return nil
}

func validateRawCurationConfidence(
	position int,
	path string,
	rawMessage json.RawMessage,
) error {
	field := fmt.Sprintf(
		"decisions[%d].confidence",
		position,
	)
	path = strings.TrimSpace(path)
	pathSuffix := ""
	if path != "" {
		pathSuffix = ": " + path
	}

	if len(rawMessage) == 0 {
		return fmt.Errorf("%s", cliMessage("curation.confidence.required", field, pathSuffix))
	}

	raw := bytes.TrimSpace(rawMessage)
	if bytes.Equal(raw, []byte("null")) {
		return fmt.Errorf("%s", cliMessage("curation.confidence.null", field, pathSuffix))
	}

	decoder := json.NewDecoder(
		bytes.NewReader(raw),
	)
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("%s", cliMessage(
			"curation.confidence.decode_failed",
			field,
			pathSuffix,
			localeSafeCLIDetail(err.Error()),
		))
	}

	switch typed := value.(type) {
	case json.Number:
		text := typed.String()
		if strings.ContainsAny(text, ".eE") {
			return fmt.Errorf("%s", cliMessage(
				"curation.confidence.fraction",
				field,
				text,
				pathSuffix,
			))
		}

		integer, err := strconv.ParseInt(
			text,
			10,
			64,
		)
		if err != nil {
			return fmt.Errorf("%s", cliMessage(
				"curation.confidence.integer",
				field,
				text,
				pathSuffix,
			))
		}
		if integer < 0 || integer > 100 {
			return fmt.Errorf("%s", cliMessage(
				"curation.confidence.range",
				field,
				integer,
				pathSuffix,
			))
		}

	case string:
		return fmt.Errorf("%s", cliMessage(
			"curation.confidence.string",
			field,
			typed,
			pathSuffix,
		))

	case bool:
		return fmt.Errorf("%s", cliMessage(
			"curation.confidence.bool",
			field,
			typed,
			pathSuffix,
		))

	default:
		return fmt.Errorf("%s", cliMessage("curation.confidence.type", field, pathSuffix))
	}

	return nil
}

// mapAgentCurationBatchCandidates先计算完整集合差异，再进入字段内容校验。
func mapAgentCurationBatchCandidates(
	candidates []agentCurationDecision,
	expected []agentPlanCurationTarget,
) (map[string]mappedAgentCurationCandidate, error) {
	expectedPaths := make(
		map[string]bool,
		len(expected),
	)
	for _, target := range expected {
		expectedPaths[target.Path] = true
	}

	counts := map[string]int{}
	mapped := map[string]mappedAgentCurationCandidate{}
	extraSet := map[string]bool{}

	for position, candidate := range candidates {
		rel, err := afs.NormalizeRelPath(
			candidate.Path,
		)
		if err != nil {
			return nil, fmt.Errorf("%s", cliMessage(
				"curation.path.unsafe",
				candidate.Path,
				localeSafeCLIDetail(err.Error()),
			))
		}

		counts[rel]++

		if !expectedPaths[rel] {
			extraSet[rel] = true
			continue
		}

		if _, exists := mapped[rel]; !exists {
			mapped[rel] = mappedAgentCurationCandidate{
				Position: position,
				Decision: candidate,
			}
		}
	}

	missing := []string{}
	extra := []string{}
	duplicate := []string{}

	for _, target := range expected {
		if counts[target.Path] == 0 {
			missing = append(
				missing,
				target.Path,
			)
		}
	}
	for path := range extraSet {
		extra = append(
			extra,
			path,
		)
	}
	for path, count := range counts {
		if count > 1 {
			duplicate = append(
				duplicate,
				path,
			)
		}
	}

	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicate)

	if len(missing) > 0 ||
		len(extra) > 0 ||
		len(duplicate) > 0 {
		parts := []string{}

		if len(missing) > 0 {
			parts = append(
				parts,
				"missing=["+
					strings.Join(missing, ",")+
					"]",
			)
		}
		if len(extra) > 0 {
			parts = append(
				parts,
				"extra=["+
					strings.Join(extra, ",")+
					"]",
			)
		}
		if len(duplicate) > 0 {
			parts = append(
				parts,
				"duplicate=["+
					strings.Join(duplicate, ",")+
					"]",
			)
		}

		return nil, fmt.Errorf("%s", cliMessage(
			"curation.batch.incomplete",
			strings.Join(parts, "; "),
		))
	}

	return mapped, nil
}
