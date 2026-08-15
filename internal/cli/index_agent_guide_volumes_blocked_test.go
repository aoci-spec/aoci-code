// issue #8: 一个已 init、未 scan 的 Volumes v1 骨架(零条目)曾在 Guide 上走进死胡同 ——
// blocked 态只带裸的 finding code, 没有可执行的补救; Legacy Guide 早有 baseline_first
// 阶段, Volumes 却没接。这里钉死: 无 Baseline 的 Volumes Guide 必须携带 scan 命令与
// 结构化 stop; scan 之后同一仓库直接进入 authoring_required。
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func volumeGuideJSON(t *testing.T, root string) volumeAgentGuide {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	oldJSON := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = oldJSON })
	if err := writeVolumeAgentGuide(command, root, cfg, set, "codex"); err != nil {
		t.Fatal(err)
	}
	var guide volumeAgentGuide
	if err := json.Unmarshal(output.Bytes(), &guide); err != nil {
		t.Fatalf("guide is not JSON: %v\n%s", err, output.String())
	}
	return guide
}

func TestVolumeGuideWithoutBaselineCarriesScanRemediationThenAuthors(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}

	// 未 scan: 骨架存在、零条目、无 Baseline —— issue #8 的精确状态。
	guide := volumeGuideJSON(t, root)
	if guide.Stage != "blocked" {
		t.Fatalf("an unscanned Volumes skeleton must still report blocked, got %q", guide.Stage)
	}
	if guide.Commands.Scan == "" || !strings.Contains(guide.Commands.Scan, " scan") {
		t.Fatalf("blocked Guide without a Baseline must carry the scan command: %+v", guide.Commands)
	}
	if guide.Stop == nil || guide.Stop.RuleCode != "baseline_missing" || guide.Stop.Cause == "" || guide.Stop.SafeNextAction == "" {
		t.Fatalf("blocked Guide must carry a structured baseline_missing stop: %+v", guide.Stop)
	}
	joined := strings.Join(guide.Instructions, "\n")
	if !strings.Contains(joined, "aoci scan") {
		t.Fatalf("instructions must name aoci scan as the remediation:\n%s", joined)
	}
	if guide.AuthoringMeta != "" || guide.Batch != nil {
		t.Fatal("no authoring contract may be issued before a Baseline exists")
	}

	// scan 之后, 同一仓库不再阻塞, 直接进入 Volumes 的创作路径。
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("scan failed: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}
	guide = volumeGuideJSON(t, root)
	if guide.Stage != "authoring_required" || guide.Stop != nil || guide.Commands.Scan != "" {
		t.Fatalf("after scan the Guide must route to authoring, got stage=%q stop=%+v scan=%q", guide.Stage, guide.Stop, guide.Commands.Scan)
	}
	if guide.Batch == nil || guide.Batch.TotalTargets == 0 || guide.AuthoringMeta == "" {
		t.Fatalf("authoring_required Guide must carry the batch facts and formal Meta: %+v", guide.Batch)
	}
}
