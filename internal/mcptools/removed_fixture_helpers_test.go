// Fixture helpers that lived in the Legacy MCP test files retired with the
// Legacy authoring surfaces (0.2.0); the tests that still use them stay here.
package mcptools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// (from internal/mcptools/tools_maintain_test.go)
// maintainResultText提取MCP纯文本结果。
func maintainResultText(
	t *testing.T,
	result *mcp.CallToolResult,
) string {
	t.Helper()

	if result == nil ||
		len(result.Content) == 0 {
		t.Fatal("结果为空")
	}

	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("结果内容非文本类型")
	}

	return content.Text
}
