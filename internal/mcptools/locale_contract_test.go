package mcptools

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpHanTextPattern = regexp.MustCompile(`[一-龥]`)

func TestMCPToolProtocolParityAndEnglishSurface(t *testing.T) {
	englishRoot := buildRepo(t)
	setRepoLocale(t, englishRoot, textassets.DefaultLocale)
	english := listToolsForRepo(t, englishRoot)
	assertNoMCPHan(t, "English ListTools", string(mustJSON(t, english)))

	chineseRoot := buildRepo(t)
	setRepoLocale(t, chineseRoot, textassets.LegacyLocale)
	chinese := listToolsForRepo(t, chineseRoot)

	if got, want := protocolShape(t, english), protocolShape(t, chinese); got != want {
		t.Fatalf("localized MCP schemas changed protocol shape\nEnglish: %s\nChinese: %s", got, want)
	}
}

func TestEnglishMCPRulesAndOverviewContainNoChinese(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	indexText := "#Locale: en-US\n" +
		"#====Tag Dictionary====\n" +
		"#A Layer: X-Source\n" +
		"#B Module: Y-Core\n" +
		"#C Importance: 5-Supporting\n" +
		"#D Maturity: T-Stable\n" +
		"#E Scale: T-Tiny<100\n" +
		"====Project cognition====\n" +
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" +
		"a.go[XY5TT]: F:Defines the sample package | R:- | A:- | S:Keep the package declaration valid\n" +
		"====End====\n"
	if err := os.WriteFile(filepath.Join(root, cfg.IndexPath), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}

	session := connectMCPClient(t, root)
	for _, name := range []string{"aoci_rules", "aoci_overview"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("%s returned an MCP error: %s", name, resText(t, result))
		}
		assertNoMCPHan(t, name, resText(t, result))
	}

	for _, request := range []*mcp.CallToolParams{
		{Name: "aoci_get_entries", Arguments: map[string]any{"paths": []string{"src/a.go"}}},
		{Name: "aoci_search", Arguments: map[string]any{"keyword": "sample"}},
		{Name: "aoci_maintain"},
	} {
		result, err := session.CallTool(context.Background(), request)
		if err != nil {
			t.Fatalf("%s: %v", request.Name, err)
		}
		assertNoMCPHan(t, request.Name, resText(t, result))
	}

	badInput, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "aoci_get_entries"})
	if err != nil {
		t.Fatal(err)
	}
	if !badInput.IsError {
		t.Fatalf("aoci_get_entries without input did not fail: %s", resText(t, badInput))
	}
	assertNoMCPHan(t, "aoci_get_entries error", resText(t, badInput))
}

func TestMCPPanicAndComponentDiagnosticsFollowActiveLocale(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })

	for _, current := range []struct {
		locale     string
		panicValue string
		wantHan    bool
	}{
		{textassets.DefaultLocale, "内部处理器故障", false},
		{textassets.LegacyLocale, "internal handler failure", true},
	} {
		t.Run(current.locale, func(t *testing.T) {
			if err := textassets.SetActiveLocale(current.locale); err != nil {
				t.Fatal(err)
			}
			detail := localeSafeMCPDetail(current.panicValue)
			if containsHan(detail) != current.wantHan || strings.Contains(detail, current.panicValue) {
				t.Fatalf("localized MCP detail mismatch for %s: %q", current.locale, detail)
			}

			oldStderr := os.Stderr
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stderr = writer
			result := guard(func() *mcp.CallToolResult {
				panic(current.panicValue)
			})
			if closeErr := writer.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			os.Stderr = oldStderr
			logged, readErr := io.ReadAll(reader)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if closeErr := reader.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			combined := string(logged) + resText(t, result)
			if strings.Contains(combined, current.panicValue) || containsHan(combined) != current.wantHan {
				t.Fatalf("MCP panic shell mismatch for %s:\n%s", current.locale, combined)
			}
		})
	}
}

func setRepoLocale(t *testing.T, root, locale string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Locale = locale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func connectMCPClient(t *testing.T, root string) *mcp.ClientSession {
	t.Helper()
	server, err := newMCPServer(root, "locale-contract-test")
	if err != nil {
		t.Fatal(err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "locale-contract-test", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

func listToolsForRepo(t *testing.T, root string) []*mcp.Tool {
	t.Helper()
	session := connectMCPClient(t, root)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 9 {
		t.Fatalf("MCP tool count = %d, want 9", len(listed.Tools))
	}
	sort.Slice(listed.Tools, func(left, right int) bool {
		return listed.Tools[left].Name < listed.Tools[right].Name
	})
	return listed.Tools
}

func protocolShape(t *testing.T, tools []*mcp.Tool) string {
	t.Helper()
	data := mustJSON(t, tools)
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	var stripDescriptions func(any)
	stripDescriptions = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			delete(typed, "description")
			for _, child := range typed {
				stripDescriptions(child)
			}
		case []any:
			for _, child := range typed {
				stripDescriptions(child)
			}
		}
	}
	stripDescriptions(value)
	return string(mustJSON(t, value))
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoMCPHan(t *testing.T, surface, value string) {
	t.Helper()
	if mcpHanTextPattern.MatchString(value) {
		t.Fatalf("%s contains Chinese text:\n%s", surface, value)
	}
}

func containsHan(value string) bool {
	return strings.ContainsFunc(value, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
}

func TestOtherWriteMessagePreflightIsCompleteForOfficialLocales(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		for name, required := range map[string]map[string][]any{
			"report":   requiredReportMessages,
			"remove":   requiredRemoveMessages,
			"maintain": requiredMaintainWriteMessages,
		} {
			if fail := validateWriteMessages(required); fail != nil {
				t.Fatalf("%s %s message preflight failed: %+v", locale, name, fail)
			}
		}
	}
}
