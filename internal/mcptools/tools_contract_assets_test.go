package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPListToolsProtocolSnapshot locks the complete public protocol the real ListTools returns: tool
// names, count, descriptions, and Input Schemas, none of them optional.
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

// TestMCPReadToolDescriptionsDiscloseAuditWrites confirms through the real ListTools result that
// the cognition read tools do not misstate "no change to formal assets" as "zero file writes".
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
			"intent=cognition_optimization",
			"仅Volumes v1 Code",
			"完整替换Entry或原样完整Entry",
			"禁止字段Patch",
			"程序绝不生成语义或S",
			"不压缩Entry文本",
			"不降低C",
		} {
			if !strings.Contains(tool.Description, required) {
				t.Fatalf("aoci_maintain Description缺少合同%q: %s", required, tool.Description)
			}
		}

		return
	}

	t.Fatal("真实ListTools结果缺少aoci_maintain")
}

func TestMCPMaintainOptimizationInputSchemaIsAdditive(t *testing.T) {
	var listed []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(canonicalListTools(t), &listed); err != nil {
		t.Fatal(err)
	}

	for _, tool := range listed {
		if tool.Name != "aoci_maintain" {
			continue
		}
		if _, required := tool.InputSchema["required"]; required {
			t.Fatal("aoci_maintain additive optimization and approval fields must remain optional")
		}
		properties, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok || len(properties) != 7 {
			t.Fatalf("aoci_maintain properties changed unexpectedly: %#v", tool.InputSchema)
		}
		if include, ok := properties["include_instructions"].(map[string]any); !ok || include["type"] != "boolean" || include["description"] == "" {
			t.Fatalf("include_instructions must be a described boolean: %#v", properties["include_instructions"])
		}
		scope, ok := properties["scope"].(map[string]any)
		if !ok || scope["type"] != "string" {
			t.Fatalf("aoci_maintain ordinary scope schema is invalid: %#v", scope)
		}
		scopeEnum, ok := scope["enum"].([]any)
		if !ok || len(scopeEnum) != 3 || scopeEnum[0] != "code" || scopeEnum[1] != "database" || scopeEnum[2] != "all" {
			t.Fatalf("aoci_maintain ordinary scope enum changed: %#v", scope["enum"])
		}
		intent, ok := properties["intent"].(map[string]any)
		if !ok || intent["type"] != "string" {
			t.Fatalf("aoci_maintain intent schema is invalid: %#v", intent)
		}
		intentEnum, ok := intent["enum"].([]any)
		if !ok || len(intentEnum) != 3 || intentEnum[0] != "cognition_optimization" || intentEnum[1] != "human_approval" ||
			intentEnum[2] != "observe_review" {
			t.Fatalf("aoci_maintain intent enum is invalid: %#v", intent["enum"])
		}
		request, ok := properties["approval_request"].(map[string]any)
		if !ok || request["type"] != "object" || request["additionalProperties"] != false {
			t.Fatalf("aoci_maintain approval_request must be a closed object: %#v", properties["approval_request"])
		}
		requestProperties, _ := request["properties"].(map[string]any)
		kind, _ := requestProperties["kind"].(map[string]any)
		kinds, _ := kind["enum"].([]any)
		if len(requestProperties) != 2 || len(kinds) != 2 || kinds[0] != "scope_change" || kinds[1] != "onboarding" {
			t.Fatalf("aoci_maintain approval_request fields changed: %#v", request)
		}
		review, ok := properties["observe_review"].(map[string]any)
		reviewProperties, _ := review["properties"].(map[string]any)
		required, _ := review["required"].([]any)
		if !ok || review["additionalProperties"] != false || len(reviewProperties) != 2 || len(required) != 2 {
			t.Fatalf("aoci_maintain observe_review must be a closed two-field object: %#v", properties["observe_review"])
		}
		objectRefs, ok := properties["object_refs"].(map[string]any)
		if !ok || objectRefs["type"] != "array" {
			t.Fatalf("aoci_maintain object_refs schema is missing: %#v", properties)
		}
		items, ok := objectRefs["items"].(map[string]any)
		if !ok || items["type"] != "string" || items["pattern"] != `^code:.+` {
			t.Fatalf("aoci_maintain object_refs items are invalid: %#v", objectRefs)
		}
		return
	}

	t.Fatal("真实ListTools结果缺少aoci_maintain")
}

func TestMCPInputSchemasAvoidTypeArrays(t *testing.T) {
	var listed []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(canonicalListTools(t), &listed); err != nil {
		t.Fatal(err)
	}

	for _, tool := range listed {
		if path, found := findSchemaTypeArray(tool.InputSchema, "inputSchema"); found {
			t.Fatalf("%s uses a JSON Schema type array at %s", tool.Name, path)
		}
	}
}

func findSchemaTypeArray(value any, path string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "." + key
			if key == "type" {
				if _, isArray := child.([]any); isArray {
					return childPath, true
				}
			}
			if foundPath, found := findSchemaTypeArray(child, childPath); found {
				return foundPath, true
			}
		}
	case []any:
		for index, child := range typed {
			if foundPath, found := findSchemaTypeArray(child, fmt.Sprintf("%s[%d]", path, index)); found {
				return foundPath, true
			}
		}
	}
	return "", false
}

func canonicalListTools(t *testing.T) []byte {
	t.Helper()
	root := buildRepo(t)
	server, err := NewServer(root, "asset-test")
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
