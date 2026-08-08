// Host-Agent Header Stage的机器输入协议、请求归一化和草稿预检。
//
// automation.mode权限由index_agent_automation.go统一裁决。
//
// 输入防线:
//   - JSON总大小上限;
//   - 重复字段、未知字段和尾随对象拒绝;
//   - plan_id与agent区分缺失、null和错误类型;
//   - plan_id必须为SHA-256;
//   - agent作为非认证审计标签统一trim和小写;
//   - model禁止换行，intent只允许显式semantic_refresh;
//   - header不能为空且有独立字节上限;
//   - Markdown围栏只剥首尾，不改写候选正文。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/safety"
)

const (
	agentHeaderStageVersion               = 1
	agentHeaderStageIntentSemanticRefresh = "semantic_refresh"
)

type agentHeaderStageRequest struct {
	Version          int    `json:"version"`
	PlanID           string `json:"plan_id"`
	Agent            string `json:"agent"`
	Model            string `json:"model,omitempty"`
	Intent           string `json:"intent,omitempty"`
	Header           string `json:"header"`
	ManagedIndexText string `json:"managed_index_text"`
}

type rawAgentHeaderStageRequest struct {
	PlanID json.RawMessage `json:"plan_id"`
	Agent  json.RawMessage `json:"agent"`
}

// agentHeaderStageResult 返回当前模式的批准停点。
type agentHeaderStageResult struct {
	Version          int      `json:"version"`
	RunID            string   `json:"run_id"`
	PlanID           string   `json:"plan_id"`
	Agent            string   `json:"agent"`
	Model            string   `json:"model,omitempty"`
	Intent           string   `json:"intent,omitempty"`
	AutomationMode   string   `json:"automation_mode"`
	GenerationHash   string   `json:"generation_hash"`
	Warnings         []string `json:"warnings"`
	ApprovalRequired bool     `json:"approval_required"`
	StopBeforeApply  bool     `json:"stop_before_apply"`
	NextCommand      string   `json:"next_command"`
	ApplyCommand     string   `json:"apply_command"`
}

func readAgentHeaderStageRequest(
	reader io.Reader,
) (agentHeaderStageRequest, error) {
	if reader == nil {
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.nil"))
	}

	data, err := io.ReadAll(
		io.LimitReader(
			reader,
			machinecontract.HeaderRequestMaxBytes+1,
		),
	)
	if err != nil {
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.read_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}
	if len(data) > machinecontract.HeaderRequestMaxBytes {
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.too_large",
			machinecontract.HeaderRequestMaxBytes,
		))
	}
	if strings.TrimSpace(
		string(data),
	) == "" {
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.empty"))
	}

	if err := validateAgentHeaderRawContract(
		data,
	); err != nil {
		return agentHeaderStageRequest{}, err
	}

	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.DisallowUnknownFields()

	var request agentHeaderStageRequest
	if err := decoder.Decode(&request); err != nil {
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.decode_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.multiple"))
		}
		return agentHeaderStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.trailing",
			localeSafeCLIDetail(err.Error()),
		))
	}
	return request, nil
}

func validateAgentHeaderRawContract(
	data []byte,
) error {
	if err := validateAgentRequestJSON(data); err != nil {
		return err
	}

	var request rawAgentHeaderStageRequest
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

	return nil
}

func normalizeAndValidateAgentHeaderStageRequest(
	request *agentHeaderStageRequest,
) error {
	if request == nil {
		return fmt.Errorf("%s", cliMessage("header.stage.request_nil"))
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
	request.Intent = strings.ToLower(strings.TrimSpace(request.Intent))
	request.Header = index.StripFences(
		request.Header,
	)
	if strings.TrimSpace(request.ManagedIndexText) != "" {
		request.ManagedIndexText = index.StripFences(request.ManagedIndexText)
	}

	if request.Version !=
		agentHeaderStageVersion {
		return fmt.Errorf("%s", cliMessage(
			"agent.version.unsupported",
			request.Version,
			agentHeaderStageVersion,
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
	if request.Intent != "" &&
		request.Intent != agentHeaderStageIntentSemanticRefresh {
		return fmt.Errorf("%s", cliMessage(
			"header.stage.intent_invalid",
			request.Intent,
		))
	}
	if strings.TrimSpace(
		request.Header,
	) == "" {
		return fmt.Errorf("%s", cliMessage("header.stage.header_empty"))
	}
	if len([]byte(request.Header)) >
		machinecontract.HeaderTextMaxBytes {
		return fmt.Errorf("%s", cliMessage(
			"header.stage.header_too_large",
			machinecontract.HeaderTextMaxBytes,
		))
	}
	if len([]byte(request.ManagedIndexText)) > machinecontract.HeaderRequestMaxBytes {
		return fmt.Errorf("%s", cliMessage(
			"header.stage.managed_index_too_large",
			machinecontract.HeaderRequestMaxBytes,
		))
	}
	return nil
}

func inspectAgentHeaderCandidate(
	header string,
) []string {
	warnings := []string{}

	if line, message := index.ValidateHeaderText(
		header,
	); line > 0 {
		warnings = append(warnings, cliMessage(
			"header.stage.structure_warning",
			line,
			localeSafeCLIDetail(message),
		))
	}

	dict := index.ExtractTagDict(header)
	if dict == nil ||
		!dict.HasDict() {
		warnings = append(warnings, cliMessage("header.stage.dictionary_warning"))
	}

	if hits := safety.CheckForbiddenClaims(
		header,
	); len(hits) > 0 {
		warnings = append(warnings, cliMessage(
			"header.stage.safety_warning",
			localeSafeCLIDetail(safety.FormatHits("header candidate", hits)),
		))
	}
	return warnings
}
