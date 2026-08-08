// Entries Check与Diff的结构化业务报告模型。
//
// 人读输出和JSON输出必须消费同一批事实，不允许为JSON模式复制第二套校验、
// 快照或Diff判据。成功业务对象保持独立顶层协议，不套通用ok/data外壳。
//
// 工作流导航：
//   - Check通过时next_command只能指向Entries Diff；
//   - Check拒绝时省略next_command并提供重新Check的recovery；
//   - Diff成功后next_command才可以指向Entries Apply。
package cli

import (
	"encoding/json"
	"io"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

const entriesReportVersion = 1

// entriesFinding复用Cognition写入管线的唯一结构化Finding。
type entriesFinding = cognition.RepairFinding

// entriesCheckItem描述一个Manifest目标的预检结果。
type entriesCheckItem struct {
	Path             string           `json:"path"`
	GenerationStatus string           `json:"generation_status"`
	Outcome          string           `json:"outcome"`
	Note             string           `json:"note,omitempty"`
	Errors           []entriesFinding `json:"errors"`
	Warnings         []entriesFinding `json:"warnings"`
}

// entriesCheckReport是`entries check --json`的完整业务对象。
//
// 通过时给出Diff命令；拒绝时next_command必须省略，并通过recovery说明修正
// 草稿后重新Check。不得在Check报告中直接指示Agent继续Apply。
type entriesCheckReport struct {
	Version     int                `json:"version"`
	OK          bool               `json:"ok"`
	RunID       string             `json:"run_id"`
	DraftHash   string             `json:"draft_hash"`
	Total       int                `json:"total"`
	Passed      int                `json:"passed"`
	Warned      int                `json:"warned"`
	Rejected    int                `json:"rejected"`
	Skipped     int                `json:"skipped"`
	Items       []entriesCheckItem `json:"items"`
	NextCommand string             `json:"next_command,omitempty"`
	Recovery    string             `json:"recovery,omitempty"`
}

// entriesDiffItem描述一个目标在正式索引与草稿之间的对照。
type entriesDiffItem struct {
	Path       string `json:"path"`
	Status     string `json:"status"`
	Note       string `json:"note,omitempty"`
	Reviewed   bool   `json:"reviewed"`
	Change     string `json:"change"`
	OldEntry   string `json:"old_entry"`
	NewEntry   string `json:"new_entry"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// entriesDiffReport是`entries diff --json`的完整业务对象。
type entriesDiffReport struct {
	Version     int               `json:"version"`
	OK          bool              `json:"ok"`
	RunID       string            `json:"run_id"`
	DraftHash   string            `json:"draft_hash"`
	Total       int               `json:"total"`
	Reviewed    int               `json:"reviewed"`
	Skipped     int               `json:"skipped"`
	Items       []entriesDiffItem `json:"items"`
	NextCommand string            `json:"next_command"`
}

// writeEntriesJSON输出缩进JSON业务对象。
func writeEntriesJSON(
	writer io.Writer,
	value any,
) error {
	encoder := json.NewEncoder(
		writer,
	)
	encoder.SetIndent(
		"",
		"  ",
	)
	return encoder.Encode(
		value,
	)
}
