// observe_change_policy 默认就是 review_required, 所以改一个 Observe 角色文件(通常是
// 测试)就会阻塞创作。Legacy Plan 早有 agentPlanStageObservedReview 把这个态接到
// scope acknowledge, Volumes 却只报一个裸的 observed_pending finding、不给命令 ——
// 与 issue #8 修复前的 baseline_missing 是同一个死胡同, 而且复现频率高得多。
// 这里钉死: 有待复核 Observe 证据的 Volumes Guide 必须携带 scope_acknowledge 命令与
// 结构化 stop; acknowledge 之后同一仓库不再阻塞。
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func TestVolumeGuideWithPendingObserveCarriesAcknowledgeRemediation(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/a.go", "package src\n\nfunc A() int { return 1 }\n")
	// Observe 角色: 生产代码的伴生测试。
	write("src/a_test.go", "package src\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A() }\n")
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("scan failed: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}

	// 改动 Observe 文件: 治理进入 observed_pending。
	write("src/a_test.go", "package src\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"drift\")\n\t}\n}\n")

	guide := volumeGuideJSON(t, root)
	hasObserved := false
	for _, finding := range guide.Findings {
		if finding.Code == "observed_pending" {
			hasObserved = true
		}
	}
	if !hasObserved {
		t.Fatalf("changing an observe-role file must raise observed_pending: %+v", guide.Findings)
	}
	if guide.Commands.ScopeAcknowledge == "" || !strings.Contains(guide.Commands.ScopeAcknowledge, "scope acknowledge") {
		t.Fatalf("a pending-observe Guide must carry the acknowledge command: %+v", guide.Commands)
	}
	if guide.Commands.ScopeStatus == "" {
		t.Fatalf("a pending-observe Guide must expose scope status for the evidence review: %+v", guide.Commands)
	}
	if guide.Stop == nil || guide.Stop.RuleCode != "observed_pending" ||
		guide.Stop.Cause == "" || guide.Stop.SafeNextAction == "" {
		t.Fatalf("a pending-observe Guide must carry a structured observed_pending stop: %+v", guide.Stop)
	}
	if len(guide.Instructions) == 0 {
		t.Fatal("the Guide must tell the model to review the reported observe evidence before acknowledging")
	}

	// acknowledge 之后, 同一仓库不再因 Observe 证据阻塞。
	var ackOut, ackErr bytes.Buffer
	if code := executeCLI([]string{
		"--repo", root, "--quiet", "scope", "acknowledge", "--reviewed-by", "conformance-test",
	}, &ackOut, &ackErr); code != ExitOK {
		t.Fatalf("acknowledge failed: code=%d stdout=%s stderr=%s", code, ackOut.String(), ackErr.String())
	}
	guide = volumeGuideJSON(t, root)
	for _, finding := range guide.Findings {
		if finding.Code == "observed_pending" {
			t.Fatalf("acknowledge must clear observed_pending: %+v", guide.Findings)
		}
	}
	if guide.Commands.ScopeAcknowledge != "" || (guide.Stop != nil && guide.Stop.RuleCode == "observed_pending") {
		t.Fatalf("the remediation must disappear once nothing is pending: cmd=%q stop=%+v",
			guide.Commands.ScopeAcknowledge, guide.Stop)
	}
}
