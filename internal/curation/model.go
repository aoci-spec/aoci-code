// Package curation 承载文件级语义策展的确定性事实、文件画像和持久化决策。
//
// 设计边界:
//   - 本包不调用模型、不创建索引条目、不修改Baseline;
//   - 决策只在source_sha256与当前文件一致时有效;
//   - 维护者config.curation_exclude优先于文件级决策;
//   - empty/binary/oversize属于可语义裁决对象;
//   - unreadable属于当前无法形成可信语义判断的技术跳过。
//
// Missing三分继续守恒:
//
//	ActionableMissing
//	+ CurationExcludedMissing
//	+ SkippedMissing
//	= Raw Missing
//
// PendingCuration是SkippedMissing的子集，不增加第四个互斥集合:
//   - 未裁决的empty/binary/oversize既是当前不可自动起草的SkippedMissing，
//     也是等待宿主Agent语义判断的PendingCuration;
//   - 有效include决策将其提升为ActionableMissing;
//   - 有效exclude决策将其转为CurationExcludedMissing。
package curation

const (
	// Version 是 .aoci/curation.json 的结构版本。
	Version = 1

	// DecisionInclude 表示该文件应形成文件级AOCI条目。
	DecisionInclude = "include"

	// DecisionExclude 表示该文件不应自动形成AOCI条目。
	DecisionExclude = "exclude"

	// ProfileReasonEmpty 表示文件物理为空。
	ProfileReasonEmpty = "empty"

	// ProfileReasonBinary 表示文件头部含NUL，疑似二进制。
	ProfileReasonBinary = "binary"

	// ProfileReasonOversize 表示文件超过自动语义读取上限。
	ProfileReasonOversize = "oversize"

	// ProfileReasonUnreadablePrefix 表示文件当前无法可靠读取或画像。
	ProfileReasonUnreadablePrefix = "unreadable:"
)

// Profile 是一个Missing文件的确定性物理画像。
type Profile struct {
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	Lines        int    `json:"lines,omitempty"`
	Ext          string `json:"ext,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Decision 是一项持久化文件级语义策展裁决。
//
// Role回答“该文件在系统中的语义角色是什么”；Reason回答“为何收录或排除”。
// Agent和Model用于审计，不用于推导决策有效性。
type Decision struct {
	Path         string `json:"path"`
	Decision     string `json:"decision"`
	Role         string `json:"role"`
	Reason       string `json:"reason"`
	Confidence   int    `json:"confidence"`
	SourceSHA256 string `json:"source_sha256"`
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

// Document 是 .aoci/curation.json 的完整持久化结构。
type Document struct {
	Version   int        `json:"version"`
	Decisions []Decision `json:"decisions"`
}

// PendingCandidate 是等待宿主Agent进行include/exclude语义裁决的文件。
type PendingCandidate struct {
	Path          string `json:"path"`
	SourceSHA256  string `json:"source_sha256"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Lines         int    `json:"lines,omitempty"`
	Ext           string `json:"ext,omitempty"`
	ProfileReason string `json:"profile_reason"`
}

// ExcludedMissing 是一项已经被治理裁决排除的原始Missing。
type ExcludedMissing struct {
	Path       string `json:"path"`
	Reason     string `json:"reason"`
	Confidence int    `json:"confidence,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Source     string `json:"source"`
}

// SkippedMissing 是当前不进入自动条目起草队列的原始Missing。
type SkippedMissing struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Classification 是Raw Missing的完整策展解释。
//
// Pending是Skipped的子集，Included是Actionable的子集。
// StaleDecisions只报告源码摘要不再匹配的旧裁决，不改变物理Missing事实。
type Classification struct {
	Missing          []string           `json:"missing"`
	Actionable       []string           `json:"actionable_missing"`
	Included         []string           `json:"included_missing"`
	CurationExcluded []ExcludedMissing  `json:"curation_excluded_missing"`
	Skipped          []SkippedMissing   `json:"skipped_missing"`
	Pending          []PendingCandidate `json:"pending_curation_missing"`
	StaleDecisions   []string           `json:"stale_curation_decisions"`

	Profiles map[string]Profile `json:"-"`
}

// NewDocument 返回切片非nil的空策展资产。
func NewDocument() *Document {
	return &Document{
		Version:   Version,
		Decisions: []Decision{},
	}
}

// ExcludedPaths 返回稳定顺序的排除路径清单。
func (c Classification) ExcludedPaths() []string {
	result := make(
		[]string,
		0,
		len(c.CurationExcluded),
	)

	for _, item := range c.CurationExcluded {
		result = append(
			result,
			item.Path,
		)
	}

	return result
}
