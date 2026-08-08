// .aoci/curation.json的严格读写、规范化、摘要与决策合并。
//
// 写入纪律:
//   - 首次写入前创建.aoci目录;
//   - 旧文件备份为curation.json.bak;
//   - 正式文件经AtomicWrite落盘;
//   - 每个路径只保留一项当前决策;
//   - 输出按Path排序，保证Git差异稳定。
package curation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// FilePath 返回团队策展资产的绝对路径。
func FilePath(root string) string {
	return config.AOCIPaths(root, "").CurationPath
}

// HashBytes 返回完整SHA-256十六进制摘要。
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Load 读取并严格校验策展资产。
//
// 文件不存在时返回空Document、exists=false以及空字节摘要。
func Load(root string) (*Document, bool, string, error) {
	targetPath := FilePath(root)

	data, err := os.ReadFile(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewDocument(), false, HashBytes(nil), nil
		}
		return nil, false, "", fmt.Errorf(
			"读取策展资产失败 %s: %w",
			targetPath,
			err,
		)
	}

	document, err := DecodeDocument(
		data,
		true,
	)
	if err != nil {
		return nil, true, "", fmt.Errorf(
			"策展资产内容无效 %s: %w",
			targetPath,
			err,
		)
	}

	return document, true, HashBytes(data), nil
}

// Save 规范化、备份并原子写入策展资产。
func Save(root string, document *Document) error {
	normalized, err := NormalizeDocument(document, true)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return fmt.Errorf("策展资产序列化失败: %w", err)
	}
	data = append(data, '\n')

	targetPath := FilePath(root)

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("创建策展资产目录失败: %w", err)
	}

	if oldData, readErr := os.ReadFile(targetPath); readErr == nil {
		if backupErr := afs.AtomicWrite(targetPath+".bak", oldData); backupErr != nil {
			return fmt.Errorf("备份旧策展资产失败: %w", backupErr)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("读取旧策展资产失败: %w", readErr)
	}

	if err := afs.AtomicWrite(targetPath, data); err != nil {
		return fmt.Errorf("写入策展资产失败: %w", err)
	}

	return nil
}

// NormalizeDocument 校验版本、决策字段、路径唯一性并按Path排序。
func NormalizeDocument(document *Document, requireAudit bool) (*Document, error) {
	if document == nil {
		return nil, fmt.Errorf("策展Document为空")
	}

	version := document.Version
	if version == 0 {
		version = Version
	}
	if version != Version {
		return nil, fmt.Errorf(
			"策展版本不支持: %d(当前只接受%d)",
			version,
			Version,
		)
	}

	decisions := make([]Decision, 0, len(document.Decisions))
	seen := map[string]bool{}

	for _, rawDecision := range document.Decisions {
		decision, err := NormalizeDecision(rawDecision, requireAudit)
		if err != nil {
			return nil, err
		}

		if seen[decision.Path] {
			return nil, fmt.Errorf("策展资产含重复路径: %s", decision.Path)
		}
		seen[decision.Path] = true
		decisions = append(decisions, decision)
	}

	sort.Slice(decisions, func(left, right int) bool {
		return decisions[left].Path < decisions[right].Path
	})

	return &Document{
		Version:   Version,
		Decisions: decisions,
	}, nil
}

// NormalizeDecision 规范化并校验一项文件级决策。
//
// requireAudit=false用于Stage候选；Agent、UpdatedAt可由Apply阶段补齐。
func NormalizeDecision(raw Decision, requireAudit bool) (Decision, error) {
	rel, err := afs.NormalizeRelPath(raw.Path)
	if err != nil {
		return Decision{}, fmt.Errorf(
			"策展路径非法 %q: %w",
			raw.Path,
			err,
		)
	}

	decision := strings.ToLower(strings.TrimSpace(raw.Decision))
	if decision != DecisionInclude && decision != DecisionExclude {
		return Decision{}, fmt.Errorf(
			"策展decision非法(%s): 只允许include或exclude",
			rel,
		)
	}

	role := normalizeSingleLine(raw.Role)
	if role == "" {
		return Decision{}, fmt.Errorf("策展role不能为空: %s", rel)
	}
	if len([]rune(role)) > 160 {
		return Decision{}, fmt.Errorf("策展role超过160字符: %s", rel)
	}

	reason := normalizeSingleLine(raw.Reason)
	if reason == "" {
		return Decision{}, fmt.Errorf("策展reason不能为空: %s", rel)
	}
	if len([]rune(reason)) > 500 {
		return Decision{}, fmt.Errorf("策展reason超过500字符: %s", rel)
	}

	if raw.Confidence < 0 || raw.Confidence > 100 {
		return Decision{}, fmt.Errorf(
			"策展confidence必须在0至100之间: %s",
			rel,
		)
	}

	sourceHash := strings.ToLower(strings.TrimSpace(raw.SourceSHA256))
	if len(sourceHash) != 64 {
		return Decision{}, fmt.Errorf(
			"策展source_sha256必须是64位十六进制: %s",
			rel,
		)
	}
	if _, err := hex.DecodeString(sourceHash); err != nil {
		return Decision{}, fmt.Errorf(
			"策展source_sha256非法(%s): %w",
			rel,
			err,
		)
	}

	agent := strings.TrimSpace(raw.Agent)
	if agent != "" && !validAgentName(agent) {
		return Decision{}, fmt.Errorf("策展agent非法: %q", agent)
	}

	model := normalizeSingleLine(raw.Model)
	if len([]rune(model)) > 200 {
		return Decision{}, fmt.Errorf("策展model超过200字符: %s", rel)
	}

	updatedAt := strings.TrimSpace(raw.UpdatedAt)
	if requireAudit {
		if agent == "" {
			return Decision{}, fmt.Errorf("持久化策展决策缺少agent: %s", rel)
		}
		if updatedAt == "" {
			return Decision{}, fmt.Errorf(
				"持久化策展决策缺少updated_at: %s",
				rel,
			)
		}
		if _, err := time.Parse(time.RFC3339, updatedAt); err != nil {
			return Decision{}, fmt.Errorf(
				"策展updated_at不是RFC3339(%s): %w",
				rel,
				err,
			)
		}
	}

	return Decision{
		Path:         rel,
		Decision:     decision,
		Role:         role,
		Reason:       reason,
		Confidence:   raw.Confidence,
		SourceSHA256: sourceHash,
		Agent:        agent,
		Model:        model,
		UpdatedAt:    updatedAt,
	}, nil
}

// Merge 把一批已Stage的决策合并为每路径唯一的当前状态。
func Merge(
	base *Document,
	incoming []Decision,
	agent,
	model string,
	now time.Time,
) (*Document, error) {
	if base == nil {
		base = NewDocument()
	}

	normalizedBase, err := NormalizeDocument(base, true)
	if err != nil {
		return nil, err
	}

	agent = strings.TrimSpace(agent)
	if !validAgentName(agent) {
		return nil, fmt.Errorf("策展Apply agent非法: %q", agent)
	}
	model = normalizeSingleLine(model)

	byPath := make(
		map[string]Decision,
		len(normalizedBase.Decisions)+len(incoming),
	)
	for _, decision := range normalizedBase.Decisions {
		byPath[decision.Path] = decision
	}

	batchSeen := map[string]bool{}
	for _, rawDecision := range incoming {
		decision, err := NormalizeDecision(rawDecision, false)
		if err != nil {
			return nil, err
		}

		if batchSeen[decision.Path] {
			return nil, fmt.Errorf(
				"待合并策展批次含重复路径: %s",
				decision.Path,
			)
		}
		batchSeen[decision.Path] = true

		decision.Agent = agent
		decision.Model = model
		decision.UpdatedAt = now.UTC().Format(time.RFC3339)
		byPath[decision.Path] = decision
	}

	merged := NewDocument()
	for _, decision := range byPath {
		merged.Decisions = append(merged.Decisions, decision)
	}

	return NormalizeDocument(merged, true)
}

// DecisionByPath 返回指定路径的当前持久化决策。
func DecisionByPath(document *Document, path string) (Decision, bool) {
	if document == nil {
		return Decision{}, false
	}

	for _, decision := range document.Decisions {
		if decision.Path == path {
			return decision, true
		}
	}

	return Decision{}, false
}

func normalizeSingleLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}

func validAgentName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}

	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.':
		case char == '_':
		case char == '-':
		default:
			return false
		}
	}

	return true
}
