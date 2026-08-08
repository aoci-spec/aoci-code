// Auto紧凑结果测试辅助。
package mcptools

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
)

func decodeAutoResult(t *testing.T, result *mcp.CallToolResult) autoResult {
	t.Helper()
	text := maintainResultText(t, result)
	var decoded autoResult
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("Auto结果不是有效JSON: %v\n%s", err, text)
	}
	return decoded
}

func sourceSHA256(t *testing.T, root, rel string) string {
	t.Helper()
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("计算源码指纹失败: %v", err)
	}
	return fingerprint.SHA256
}

func candidatePaths(result autoResult) map[string]bool {
	paths := make(map[string]bool, len(result.Candidates))
	for _, candidate := range result.Candidates {
		paths[candidate.Path] = true
	}
	return paths
}

func hasFinding(result autoResult, finding string) bool {
	for _, current := range result.Findings {
		if current.RuleCode == finding || current.Code == finding || current.Cause == finding || current.Message == finding {
			return true
		}
	}
	return false
}

func joinedFindingText(result autoResult) string {
	parts := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		parts = append(parts, strings.Join([]string{finding.RuleCode, finding.Code, finding.Cause, finding.Message}, ":"))
	}
	return strings.Join(parts, "\n")
}
