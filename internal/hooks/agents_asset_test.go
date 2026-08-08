// AGENTS文本资产迁移、兼容桥和真实物化结果的字节级测试。
package hooks

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func readAgentsGoldenHash(
	t *testing.T,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			"agents_new_file_output.sha256",
		),
	)
	if err != nil {
		t.Fatalf(
			"读取AGENTS Golden失败: %v",
			err,
		)
	}

	return string(data)
}

func loadAgentsAssetForTest(
	t *testing.T,
) string {
	return loadAgentsAssetForLocaleForTest(t, textassets.LegacyLocale)
}

func loadAgentsAssetForLocaleForTest(t *testing.T, locale string) string {
	t.Helper()

	value, err := textassets.Load(
		locale,
		textassets.TemplateAgentsMD,
	)
	if err != nil {
		t.Fatalf(
			"加载AGENTS文本资产失败: %v",
			err,
		)
	}

	return value
}

func configuredRepositoryLocaleForTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatalf("load repository Locale: %v", err)
	}
	if !textassets.IsOfficialLocale(cfg.Locale) {
		t.Fatalf("repository Locale is not official: %q", cfg.Locale)
	}
	return cfg.Locale
}

func managedAgentsBlockForTest(t *testing.T, document string) string {
	t.Helper()
	start := strings.Index(document, agentsBegin)
	end := strings.Index(document, agentsEnd)
	if start < 0 || end < start {
		t.Fatalf("AGENTS document does not contain one complete managed block:\n%s", document)
	}
	if strings.Count(document, agentsBegin) != 1 || strings.Count(document, agentsEnd) != 1 {
		t.Fatalf("AGENTS document must contain exactly one managed block")
	}
	return document[start : end+len(agentsEnd)]
}

func assertAgentsManagedStructure(t *testing.T, document string) {
	t.Helper()
	managed := managedAgentsBlockForTest(t, document)
	for _, token := range []string{
		"aoci.txt",
		"Header",
		"Entry",
		"F/R/A/S",
		"aoci_rules",
		"aoci_overview",
		"aoci_maintain",
		"aoci_update_entry",
		"aoci_report",
		"repair_required",
		"stopped",
		"source_sha256",
		".aoci",
	} {
		if !strings.Contains(managed, token) {
			t.Fatalf("AGENTS managed block is missing machine token %q:\n%s", token, managed)
		}
	}
	if strings.Contains(managed, "{{") || strings.Contains(managed, "}}") {
		t.Fatalf("AGENTS managed block contains an unresolved template variable:\n%s", managed)
	}
}

func TestAgentsNewFileOutputMatchesCompatibilityDigest(
	t *testing.T,
) {
	root := t.TempDir()
	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(actual)
	actualHash := fmt.Sprintf("%x", digest[:])
	expectedHash := strings.TrimSpace(readAgentsGoldenHash(t))

	if actualHash != expectedHash {
		t.Fatalf(
			"AGENTS生产物化兼容摘要不一致: actual=%s expected=%s",
			actualHash,
			expectedHash,
		)
	}
}

func TestEnsureAgentsBlockNewFileMatchesAsset(
	t *testing.T,
) {
	root := t.TempDir()
	asset := loadAgentsAssetForTest(t)

	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf(
			"EnsureAgentsBlock新建失败: %v",
			err,
		)
	}

	data, err := os.ReadFile(
		filepath.Join(
			root,
			"AGENTS.md",
		),
	)
	if err != nil {
		t.Fatalf(
			"读取新建AGENTS失败: %v",
			err,
		)
	}

	expected := strings.TrimRight(
		asset,
		"\n",
	) + "\n"

	if string(data) != expected {
		t.Fatalf(
			"AGENTS新建产物未与资产字节一致:\n%s",
			string(data),
		)
	}
}

func TestEnsureAgentsBlockPreservesOutsideBytes(
	t *testing.T,
) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			if err := textassets.SetActiveLocale(locale); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = textassets.SetActiveLocale(previous) }()

			root := t.TempDir()
			path := filepath.Join(root, "AGENTS.md")
			prefix := []byte("# user-owned \xe5\x89\x8d\xe8\xa8\x80\r\n\x00\n")
			suffix := []byte("\n\x00\r\n# user-owned suffix\n")
			before := append(append(append([]byte{}, prefix...), []byte(agentsBegin+"\nold managed text\n"+agentsEnd)...), suffix...)
			if err := os.WriteFile(path, before, 0o644); err != nil {
				t.Fatalf("write AGENTS fixture: %v", err)
			}

			if _, err := EnsureAgentsBlock(root); err != nil {
				t.Fatalf("EnsureAgentsBlock: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read rewritten AGENTS: %v", err)
			}
			asset := strings.TrimRight(loadAgentsAssetForLocaleForTest(t, locale), "\n")
			expected := append(append(append([]byte{}, prefix...), []byte(asset)...), suffix...)
			if string(after) != string(expected) {
				t.Fatalf("AGENTS unmanaged bytes changed for %s", locale)
			}
			assertAgentsManagedStructure(t, string(after))
		})
	}
}
