// 本文件承载 Host-Agent Entries Stage 的机器输入协议、请求归一化、路径安全、
// Plan 目标绑定、源码 SHA-256 核对、草稿文件名碰撞检查和 Generation 状态预检。
//
// 输入防线:
//  1. 限制输入总大小;
//  2. 拒绝重复JSON字段、未知字段与尾随对象;
//  3. 区分plan_id、agent和source_sha256的缺失、null与错误类型;
//  4. 归一化plan_id、agent和model;
//  5. 校验协议版本、SHA-256、Agent标识和批量上限;
//  6. 路径经NormalizeRelPath;
//  7. 路径必须属于当前Plan且不得重复;
//  8. source_sha256必须等于当前Plan目标摘要;
//  9. 草稿文件名扁平化后不得碰撞;
//
// 10. 候选格式问题只标记warned，不静默修正。
//
// automation.mode权限由index_agent_automation.go统一裁决。
// 本文件不写磁盘、不创建Run、不修改正式索引或Baseline。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	agentStageVersion  = 1
	agentStageProvider = "host-agent"
)

var agentStageNameRe = regexp.MustCompile(
	`^[A-Za-z0-9._-]{1,64}$`,
)

type agentStageEntry struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
	Entry        string `json:"entry"`
}

type agentStageRequest struct {
	Version int               `json:"version"`
	PlanID  string            `json:"plan_id"`
	Agent   string            `json:"agent"`
	Model   string            `json:"model,omitempty"`
	Entries []agentStageEntry `json:"entries"`
}

type rawAgentStageRequest struct {
	PlanID  json.RawMessage `json:"plan_id"`
	Agent   json.RawMessage `json:"agent"`
	Entries json.RawMessage `json:"entries"`
}

type rawAgentStageEntry struct {
	Path         string          `json:"path"`
	SourceSHA256 json.RawMessage `json:"source_sha256"`
}

// agentStageResult同时返回草稿结果、团队停点及可选的Auto收口结果。
//
// review、legacy与off不返回auto_finalize；auto必须明确报告已应用或停止原因。
type agentStageResult struct {
	Version          int                        `json:"version"`
	RunID            string                     `json:"run_id"`
	PlanID           string                     `json:"plan_id"`
	Agent            string                     `json:"agent"`
	Model            string                     `json:"model,omitempty"`
	AutomationMode   string                     `json:"automation_mode"`
	ApprovalRequired bool                       `json:"approval_required"`
	StopBeforeApply  bool                       `json:"stop_before_apply"`
	GenerationHash   string                     `json:"generation_hash"`
	Drafted          int                        `json:"drafted"`
	Warned           int                        `json:"warned"`
	Statuses         []draft.EntryStatus        `json:"statuses"`
	AutoFinalize     *entriesAutoFinalizeResult `json:"auto_finalize,omitempty"`
	NextCommand      string                     `json:"next_command,omitempty"`
}

type preparedAgentStageEntry struct {
	Path      string
	DraftName string
	Line      string
	Status    draft.EntryStatus
}

type agentStageError struct {
	Code int
	Err  error
}

func (e *agentStageError) Error() string {
	if e == nil ||
		e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *agentStageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func readAgentStageRequest(
	reader io.Reader,
) (agentStageRequest, error) {
	if reader == nil {
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.nil"))
	}

	data, err := io.ReadAll(
		io.LimitReader(
			reader,
			machinecontract.EntriesRequestMaxBytes+1,
		),
	)
	if err != nil {
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.read_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	if len(data) > machinecontract.EntriesRequestMaxBytes {
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.too_large",
			machinecontract.EntriesRequestMaxBytes,
		))
	}
	if strings.TrimSpace(
		string(data),
	) == "" {
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.empty"))
	}

	if err := validateAgentStageRawContract(
		data,
	); err != nil {
		return agentStageRequest{}, err
	}

	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.DisallowUnknownFields()

	var request agentStageRequest
	if err := decoder.Decode(&request); err != nil {
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.decode_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return agentStageRequest{}, fmt.Errorf("%s", cliMessage("agent.stdin.multiple"))
		}
		return agentStageRequest{}, fmt.Errorf("%s", cliMessage(
			"agent.stdin.trailing",
			localeSafeCLIDetail(err.Error()),
		))
	}

	return request, nil
}

func validateAgentStageRawContract(
	data []byte,
) error {
	if err := validateAgentRequestJSON(data); err != nil {
		return err
	}

	var request rawAgentStageRequest
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
		request.Entries,
		"entries",
	); err != nil {
		return err
	}

	var entries []rawAgentStageEntry
	if err := json.Unmarshal(
		request.Entries,
		&entries,
	); err != nil {
		return nil
	}

	for position, entry := range entries {
		field := fmt.Sprintf(
			"entries[%d].source_sha256",
			position,
		)
		if _, err := requireRawJSONString(
			entry.SourceSHA256,
			field,
		); err != nil {
			return err
		}
	}

	return nil
}

func normalizeAndValidateAgentStageRequest(
	request *agentStageRequest,
) error {
	if request == nil {
		return fmt.Errorf("%s", cliMessage("entries.stage.request_nil"))
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

	if request.Version != agentStageVersion {
		return fmt.Errorf("%s", cliMessage(
			"agent.version.unsupported",
			request.Version,
			agentStageVersion,
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

	if len(request.Entries) == 0 {
		return fmt.Errorf("%s", cliMessage("entries.stage.entries_empty"))
	}
	if len(request.Entries) >
		machinecontract.EntriesBatchMaxItems {
		return fmt.Errorf("%s", cliMessage(
			"entries.stage.too_many",
			machinecontract.EntriesBatchMaxItems,
			len(request.Entries),
		))
	}

	return nil
}

func prepareAgentStageEntries(
	request agentStageRequest,
	currentPlan *agentPlan,
) ([]preparedAgentStageEntry, error) {
	if currentPlan == nil {
		return nil, fmt.Errorf("%s", cliMessage("entries.stage.plan_nil"))
	}

	targets := make(
		map[string]agentPlanTarget,
		len(currentPlan.Targets),
	)
	for _, target := range currentPlan.Targets {
		targets[target.Path] = target
	}

	seenPaths := map[string]bool{}
	draftOwners := map[string]string{}
	prepared := make(
		[]preparedAgentStageEntry,
		0,
		len(request.Entries),
	)

	for position, candidate := range request.Entries {
		rel, err := afs.NormalizeRelPath(
			candidate.Path,
		)
		if err != nil {
			return nil, fmt.Errorf("%s", cliMessage(
				"entries.stage.path_unsafe",
				candidate.Path,
				localeSafeCLIDetail(err.Error()),
			))
		}

		if seenPaths[rel] {
			return nil, fmt.Errorf("%s", cliMessage("entries.stage.path_duplicate", rel))
		}
		seenPaths[rel] = true

		target, exists := targets[rel]
		if !exists {
			return nil, fmt.Errorf("%s", cliMessage("entries.stage.path_not_target", rel))
		}

		sourceHash, err := normalizeRequiredSHA256(
			fmt.Sprintf(
				"entries[%d].source_sha256",
				position,
			),
			candidate.SourceSHA256,
		)
		if err != nil {
			return nil, err
		}
		if sourceHash != target.SourceSHA256 {
			return nil, fmt.Errorf("%s", cliMessage(
				"entries.stage.source_drift",
				rel,
				shortAgentStageHash(sourceHash),
				shortAgentStageHash(target.SourceSHA256),
			))
		}

		line := index.StripFences(
			candidate.Entry,
		)
		if strings.TrimSpace(line) == "" {
			return nil, fmt.Errorf("%s", cliMessage("entries.stage.entry_empty", rel))
		}

		draftName := entryDraftFileName(rel)
		if owner, collision :=
			draftOwners[draftName]; collision {
			return nil, fmt.Errorf("%s", cliMessage(
				"entries.stage.filename_collision",
				owner,
				rel,
				draftName,
			))
		}
		draftOwners[draftName] = rel

		status := "drafted"
		note := ""
		if violations := index.ValidateEntryLine(
			rel,
			line,
		); len(violations) > 0 {
			status = "warned"

			messages := make(
				[]string,
				0,
				len(violations),
			)
			for _, violation := range violations {
				messages = append(
					messages,
					"["+violation.Level+"] "+
						localeSafeCLIDetail(violation.Msg),
				)
			}
			note = strings.Join(
				messages,
				";",
			)
		}

		prepared = append(
			prepared,
			preparedAgentStageEntry{
				Path:      rel,
				DraftName: draftName,
				Line:      line,
				Status: draft.EntryStatus{
					Path:         rel,
					Status:       status,
					Note:         note,
					SourceSHA256: target.SourceSHA256,
				},
			},
		)
	}

	return prepared, nil
}
