// 普通开发任务的宿主组合沟通合同测试。
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

var ordinaryUserCommunicationAnchors = map[string][]string{
	textassets.LegacyLocale: {
		"普通任务", "需求理解", "源码", "实现", "测试", "风险", "工作树状态",
		"默认不主动展开", "用户主动询问", "真实阻断", "冲突", "审批", "人工裁决",
		"安全风险", "必须如实说明必要事实",
	},
	textassets.DefaultLocale: {
		"ordinary task", "understanding the request", "source investigation", "implementation",
		"tests", "risks", "worktree state", "Do not proactively narrate", "When the user asks",
		"real blocker", "conflict", "approval", "human decision", "safety risk",
		"report the necessary facts accurately",
	},
}

func assertOrdinaryUserCommunicationContract(t *testing.T, locale, name, text string) {
	t.Helper()

	anchors, exists := ordinaryUserCommunicationAnchors[locale]
	if !exists {
		t.Fatalf("missing explicit user-communication assertions for %s", locale)
	}
	for _, anchor := range anchors {
		if !strings.Contains(text, anchor) {
			t.Fatalf("%s %s contract is missing ordinary user-communication meaning %q:\n%s", locale, name, anchor, text)
		}
	}
}

// TestComposedHostContractsKeepInternalWorkOutOfOrdinaryMessages通过真实的
// AGENTS物化和aoci_rules构建管线分别核对三个宿主合同的稳定语义。
func TestComposedHostContractsKeepInternalWorkOutOfOrdinaryMessages(t *testing.T) {
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
			agentsData, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
			if err != nil {
				t.Fatal(err)
			}
			runtimeRules, err := index.BuildRuntimeRules(nil)
			if err != nil {
				t.Fatal(err)
			}

			assertOrdinaryUserCommunicationContract(t, locale, "installed AGENTS", string(agentsData))
			assertOrdinaryUserCommunicationContract(t, locale, "aoci_rules", runtimeRules)
			combined := string(agentsData) + "\n" + runtimeRules
			for _, obsolete := range []string{
				"最终报告必须把业务文件和 AOCI 托管资产分开列出",
				"最终报告须分开列出",
				"the final report must list business files and AOCI-managed assets separately",
			} {
				if strings.Contains(combined, obsolete) {
					t.Fatalf("%s host contracts require proactive internal-asset narration %q", locale, obsolete)
				}
			}
		})
	}

	repositoryData, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	locale := configuredRepositoryLocaleForTest(t)
	assertAgentsManagedStructure(t, string(repositoryData))
	want := strings.TrimRight(loadAgentsAssetForLocaleForTest(t, locale), "\n")
	if got := managedAgentsBlockForTest(t, string(repositoryData)); got != want {
		t.Fatalf("repository AGENTS managed block does not match configured %s asset", locale)
	}
}
