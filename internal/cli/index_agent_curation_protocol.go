// Host-Agent Curation Stage严格JSON协议与当前Plan批次绑定。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const agentCurationStageVersion = 1

type agentCurationDecision struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
	Decision     string `json:"decision"`
	Role         string `json:"role"`
	Reason       string `json:"reason"`
	Confidence   int    `json:"confidence"`
}

type agentCurationStageRequest struct {
	Version   int                     `json:"version"`
	PlanID    string                  `json:"plan_id"`
	Agent     string                  `json:"agent"`
	Model     string                  `json:"model,omitempty"`
	Decisions []agentCurationDecision `json:"decisions"`
}

type agentCurationStageResult struct {
	Version          int    `json:"version"`
	RunID            string `json:"run_id"`
	PlanID           string `json:"plan_id"`
	Agent            string `json:"agent"`
	Model            string `json:"model,omitempty"`
	AutomationMode   string `json:"automation_mode"`
	ApprovalRequired bool   `json:"approval_required"`
	StopBeforeApply  bool   `json:"stop_before_apply"`
	GenerationHash   string `json:"generation_hash"`
	Decisions        int    `json:"decisions"`
	Include          int    `json:"include"`
	Exclude          int    `json:"exclude"`
	NextCommand      string `json:"next_command"`
	DiffCommand      string `json:"diff_command"`
	ApplyCommand     string `json:"apply_command"`
}

func readAgentCurationStageRequest(
	reader io.Reader,
) (agentCurationStageRequest, error) {
	if reader == nil {
		return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage("curation.input.nil"))
	}

	data, err := io.ReadAll(
		io.LimitReader(
			reader,
			machinecontract.CurationRequestMaxBytes+1,
		),
	)
	if err != nil {
		return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage(
			"curation.input.read_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}
	if len(data) > machinecontract.CurationRequestMaxBytes {
		return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage(
			"curation.input.too_large",
			machinecontract.CurationRequestMaxBytes,
		))
	}

	if err := validateAgentCurationRawContract(
		data,
	); err != nil {
		return agentCurationStageRequest{}, err
	}

	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.DisallowUnknownFields()

	var request agentCurationStageRequest
	if err := decoder.Decode(
		&request,
	); err != nil {
		return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage(
			"curation.input.decode_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	var extra any
	if err := decoder.Decode(
		&extra,
	); err != io.EOF {
		if err == nil {
			return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage("curation.input.multiple"))
		}
		return agentCurationStageRequest{}, fmt.Errorf("%s", cliMessage(
			"curation.input.trailing",
			localeSafeCLIDetail(err.Error()),
		))
	}

	return request, nil
}

func normalizeAgentCurationRequest(
	request *agentCurationStageRequest,
) error {
	if request == nil {
		return fmt.Errorf("%s", cliMessage("curation.stage.request_nil"))
	}

	planID, err := normalizeRequiredSHA256(
		"plan_id",
		request.PlanID,
	)
	if err != nil {
		return err
	}
	request.PlanID = planID

	request.Agent = normalizeAgentAuditLabel(
		request.Agent,
	)
	request.Model = strings.TrimSpace(
		request.Model,
	)

	if request.Version !=
		agentCurationStageVersion {
		return fmt.Errorf("%s", cliMessage(
			"agent.version.unsupported",
			request.Version,
			agentCurationStageVersion,
		))
	}
	if !agentStageNameRe.MatchString(
		request.Agent,
	) {
		return fmt.Errorf("%s", cliMessage("agent.label.invalid"))
	}
	if len(request.Model) > 200 ||
		strings.ContainsAny(
			request.Model,
			"\r\n",
		) {
		return fmt.Errorf("%s", cliMessage("agent.model.invalid"))
	}
	if len(request.Decisions) == 0 {
		return fmt.Errorf("%s", cliMessage("curation.stage.decisions_empty"))
	}
	if len(request.Decisions) >
		machinecontract.CurationBatchMaxItems {
		return fmt.Errorf("%s", cliMessage(
			"curation.stage.too_many",
			machinecontract.CurationBatchMaxItems,
		))
	}

	return nil
}

func currentAgentCurationBatch(
	plan *agentPlan,
) []agentPlanCurationTarget {
	if plan == nil {
		return []agentPlanCurationTarget{}
	}

	limit := len(plan.CurationTargets)
	if limit > machinecontract.CurationBatchMaxItems {
		limit = machinecontract.CurationBatchMaxItems
	}

	return append(
		[]agentPlanCurationTarget{},
		plan.CurationTargets[:limit]...,
	)
}

func prepareAgentCurationDecisions(
	request agentCurationStageRequest,
	currentPlan *agentPlan,
) ([]curation.Decision, error) {
	expected := currentAgentCurationBatch(
		currentPlan,
	)

	mapped, err := mapAgentCurationBatchCandidates(
		request.Decisions,
		expected,
	)
	if err != nil {
		return nil, err
	}

	prepared := make(
		[]curation.Decision,
		0,
		len(expected),
	)

	for _, target := range expected {
		candidate := mapped[target.Path]

		sourceHash, err := normalizeRequiredSHA256(
			fmt.Sprintf(
				"decisions[%d].source_sha256",
				candidate.Position,
			),
			candidate.Decision.SourceSHA256,
		)
		if err != nil {
			return nil, err
		}
		if sourceHash != target.SourceSHA256 {
			return nil, fmt.Errorf("%s", cliMessage(
				"curation.stage.source_drift",
				target.Path,
				shortAgentStageHash(sourceHash),
				shortAgentStageHash(target.SourceSHA256),
			))
		}

		decision, err := curation.NormalizeDecision(
			curation.Decision{
				Path:         target.Path,
				SourceSHA256: sourceHash,
				Decision:     candidate.Decision.Decision,
				Role:         candidate.Decision.Role,
				Reason:       candidate.Decision.Reason,
				Confidence:   candidate.Decision.Confidence,
			},
			false,
		)
		if err != nil {
			return nil, err
		}

		prepared = append(
			prepared,
			decision,
		)
	}

	return prepared, nil
}
