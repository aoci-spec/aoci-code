// Strict reading and writing, normalization, digest, and decision merging of .aoci/curation.json.
//
// Write discipline:
//   - the .aoci directory is created before the first write;
//   - the old file is backed up as curation.json.bak;
//   - the formal file lands through AtomicWrite;
//   - each path keeps only one current decision;
//   - the output is sorted by Path, so the Git diff is stable.
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

// FilePath returns the absolute path of the team curation asset.
func FilePath(root string) string {
	return config.AOCIPaths(root, "").CurationPath
}

// HashBytes returns the complete SHA-256 hexadecimal digest.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Load reads and strictly validates the curation asset.
//
// A missing file returns an empty Document, exists=false, and the empty-bytes digest.
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

// Save normalizes, backs up, and atomically writes the curation asset.
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

// NormalizeDocument validates the version, the decision fields, and path uniqueness, and sorts by Path.
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

// NormalizeDecision normalizes and validates one file-level decision.
//
// requireAudit=false serves a Stage candidate; Agent and UpdatedAt may be filled in at the Apply phase.
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

// Merge merges a batch of Staged decisions into the per-path unique current state.
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

// DecisionByPath returns the current persisted decision of the given path.
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
