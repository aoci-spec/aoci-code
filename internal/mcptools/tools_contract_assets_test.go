package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPListToolsProtocolSnapshot锁定真实ListTools返回的完整公开协议：工具
// 名称、数量、说明和Input Schema缺一不可。
func TestMCPListToolsProtocolSnapshot(t *testing.T) {
	actual := canonicalListTools(t)
	goldenPath := filepath.Join("..", "..", "testdata", "golden", "mcp_list_tools.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	expected = canonicalGoldenBytes(expected)
	if !bytes.Equal(actual, expected) {
		t.Fatalf("MCP ListTools public protocol changed:\n%s", actual)
	}
}

func canonicalGoldenBytes(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func TestCanonicalGoldenBytesNormalizesCheckoutCRLF(t *testing.T) {
	actual := canonicalGoldenBytes([]byte("first\r\nsecond\r\n"))
	if !bytes.Equal(actual, []byte("first\nsecond\n")) {
		t.Fatalf("checkout line endings changed canonical Golden bytes: %q", actual)
	}
}

// TestMCPReadToolDescriptionsDiscloseAuditWrites通过真实ListTools结果确认
// 认知读取工具不会把“不修改正式资产”误写成“零文件写入”。
func TestMCPReadToolDescriptionsDiscloseAuditWrites(t *testing.T) {
	var listed []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(canonicalListTools(t), &listed); err != nil {
		t.Fatal(err)
	}

	targets := map[string]bool{
		"aoci_rules":       false,
		"aoci_overview":    false,
		"aoci_get_entries": false,
		"aoci_search":      false,
		"aoci_header":      false,
	}
	for _, tool := range listed {
		if _, ok := targets[tool.Name]; !ok {
			continue
		}
		targets[tool.Name] = true
		for _, required := range []string{
			"不会修改正式索引或Baseline",
			"可能追加本地审计记录",
		} {
			if !strings.Contains(tool.Description, required) {
				t.Fatalf("%s Description缺少Ledger副作用边界%q: %s", tool.Name, required, tool.Description)
			}
		}
		if strings.Contains(tool.Description, "本工具只读") {
			t.Fatalf("%s Description仍含无条件只读表述: %s", tool.Name, tool.Description)
		}
	}
	for name, found := range targets {
		if !found {
			t.Fatalf("真实ListTools结果缺少目标工具%s", name)
		}
	}
}

// TestMCPMaintainDescriptionDisclosesWritesAndGuideBoundary verifies the
// production ListTools contract, including its write effects and precondition.
func TestMCPMaintainDescriptionDisclosesWritesAndGuideBoundary(t *testing.T) {
	var listed []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(canonicalListTools(t), &listed); err != nil {
		t.Fatal(err)
	}

	for _, tool := range listed {
		if tool.Name != "aoci_maintain" {
			continue
		}

		for _, required := range []string{
			"format-only可能前移Baseline",
			"可能追加本地Ledger审计记录",
			"新仓",
			"不完整索引",
			"使用当前Guide",
			"本工具不能替代",
			"刷新待处理",
			"证明aligned",
		} {
			if !strings.Contains(tool.Description, required) {
				t.Fatalf("aoci_maintain Description缺少合同%q: %s", required, tool.Description)
			}
		}

		return
	}

	t.Fatal("真实ListTools结果缺少aoci_maintain")
}

func canonicalListTools(t *testing.T) []byte {
	t.Helper()
	root := buildRepo(t)
	server, err := newMCPServer(root, "asset-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "asset-test", Version: "test"},
		nil,
	)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 9 {
		t.Fatalf("MCP tool count changed: got=%d want=9", len(listed.Tools))
	}
	sort.Slice(listed.Tools, func(left, right int) bool {
		return listed.Tools[left].Name < listed.Tools[right].Name
	})
	data, err := json.MarshalIndent(listed.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return append(data, '\n')
}
