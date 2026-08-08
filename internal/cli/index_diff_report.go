// Header与Curation Diff的结构化业务报告。
//
// 两类治理对象保留独立领域协议，仅共享缩进JSON编码器。
// 人读与JSON必须消费同一报告，不能各自重新读取草稿或重新计算差异。
package cli

import (
	"encoding/json"
	"io"
)

const governanceDiffReportVersion = 1

// headerDiffReport是`index header diff --json`的完整业务对象。
type headerDiffReport struct {
	Version               int      `json:"version"`
	OK                    bool     `json:"ok"`
	RunID                 string   `json:"run_id"`
	DraftHash             string   `json:"draft_hash"`
	Change                string   `json:"change"`
	CurrentHeader         string   `json:"current_header"`
	DraftHeader           string   `json:"draft_header"`
	ManagedIndexCandidate bool     `json:"managed_index_candidate"`
	CurrentIndex          string   `json:"current_index,omitempty"`
	DraftIndex            string   `json:"draft_index,omitempty"`
	ManagedDiffText       string   `json:"managed_diff_text,omitempty"`
	DiffText              string   `json:"diff_text"`
	ManifestPresent       bool     `json:"manifest_present"`
	ReviewRecorded        bool     `json:"review_recorded"`
	LegacyCompatibility   bool     `json:"legacy_compatibility"`
	Warnings              []string `json:"warnings"`
	NextCommand           string   `json:"next_command"`
}

// curationDecisionView是Diff协议中的文件级语义决策。
//
// Path由外层item承载；Stage草稿尚未形成Agent和时间审计时，相应字段省略。
type curationDecisionView struct {
	Decision     string `json:"decision"`
	Role         string `json:"role"`
	Reason       string `json:"reason"`
	Confidence   int    `json:"confidence"`
	SourceSHA256 string `json:"source_sha256"`
	Agent        string `json:"agent,omitempty"`
	Model        string `json:"model,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// curationDiffItem描述一个路径的正式决策与草稿决策对照。
type curationDiffItem struct {
	Path        string                `json:"path"`
	Change      string                `json:"change"`
	OldExists   bool                  `json:"old_exists"`
	OldDecision *curationDecisionView `json:"old_decision,omitempty"`
	NewDecision curationDecisionView  `json:"new_decision"`
}

// curationDiffReport是`index agent curation diff --json`的完整业务对象。
type curationDiffReport struct {
	Version        int                `json:"version"`
	OK             bool               `json:"ok"`
	RunID          string             `json:"run_id"`
	DraftHash      string             `json:"draft_hash"`
	Total          int                `json:"total"`
	Include        int                `json:"include"`
	Exclude        int                `json:"exclude"`
	CurrentExists  bool               `json:"current_exists"`
	CurrentSHA256  string             `json:"current_sha256"`
	ReviewRecorded bool               `json:"review_recorded"`
	Items          []curationDiffItem `json:"items"`
	NextCommand    string             `json:"next_command"`
}

// writeGovernanceDiffJSON输出单一缩进JSON业务对象。
func writeGovernanceDiffJSON(
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
