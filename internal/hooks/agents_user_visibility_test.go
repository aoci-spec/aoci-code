// AGENTS不复制通用开发沟通方法的物化合同测试。
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

var removedAgentsCommunicationMethods = map[string][]string{
	textassets.LegacyLocale: {
		"用户可见沟通应聚焦",
		"需求理解、源码调查、实现、测试、风险",
	},
	textassets.DefaultLocale: {
		"User-visible communication for an ordinary task should focus",
		"understanding the request, source investigation, implementation, tests, risks",
	},
}

func assertAgentsOmitsGeneralCommunicationMethods(
	t *testing.T,
	locale,
	text string,
) {
	t.Helper()
	removed, exists := removedAgentsCommunicationMethods[locale]
	if !exists {
		t.Fatalf("missing explicit removed communication methods for %s", locale)
	}
	for _, phrase := range removed {
		if strings.Contains(text, phrase) {
			t.Fatalf("%s AGENTS still carries general communication method %q", locale, phrase)
		}
	}
}

func TestAgentsContractsOmitGeneralUserCommunicationMethods(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			previous := textassets.ActiveLocale()
			if err := textassets.SetActiveLocale(locale); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = textassets.SetActiveLocale(previous) }()

			root := t.TempDir()
			if _, err := EnsureAgentsBlock(root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			assertAgentsOmitsGeneralCommunicationMethods(t, locale, string(data))
		})
	}

	repositoryData, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	locale := configuredRepositoryLocaleForTest(t)
	assertAgentsManagedStructure(t, string(repositoryData))
	assertAgentsOmitsGeneralCommunicationMethods(t, locale, string(repositoryData))
	want := strings.TrimRight(loadAgentsAssetForLocaleForTest(t, locale), "\n")
	if got := managedAgentsBlockForTest(t, string(repositoryData)); got != want {
		t.Fatalf("repository AGENTS managed block does not match configured %s asset", locale)
	}
}
