// D69 init 新仓/旧仓自动化模式兼容测试。
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func TestInitNewRepoDefaultsAutomationAuto(t *testing.T) {
	root := t.TempDir()

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf("新仓 init 失败: %v", err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != config.AutomationModeAuto {
		t.Fatalf(
			"新仓应显式 auto,得到 %s",
			cfg.EffectiveAutomationMode(),
		)
	}
	if !cfg.HasDeclaredAutomationMode() {
		t.Fatal("新仓 auto 必须物化进团队 config.json")
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 || set.Volumes[cognition.ScopeCode] == nil || set.Volumes[cognition.ScopeDatabase] != nil {
		t.Fatalf("新仓 init 不应进入 Legacy Migration 或布局 Bootstrap: %#v", set)
	}
}

func TestFreshVolumeInitDoesNotEnterLegacyOnboarding(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("fresh init failed: %v", err)
	}
	var scanStdout bytes.Buffer
	var scanStderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "scan"}, &scanStdout, &scanStderr); code != ExitOK {
		t.Fatalf("fresh Volume-first scan failed: code=%d stdout=%s stderr=%s", code, scanStdout.String(), scanStderr.String())
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "cognition", "onboard", "start"}, &stdout, &stderr)
	if code != ExitInvalid || !strings.Contains(stdout.String(), `"error_code": "cognition_onboarding_invalid"`) ||
		!strings.Contains(stdout.String(), "onboarding_already_volumes") {
		t.Fatalf("fresh Volume-first onboarding routing changed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(onboarding.SessionPath(root)); !os.IsNotExist(err) {
		t.Fatalf("rejected Volume-first onboarding created a Session: %v", err)
	}
}

func TestInitExistingLegacyConfigPreserved(t *testing.T) {
	root := t.TempDir()

	// 模拟升级前旧仓: config.json 已存在但无 automation 字段。
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf("旧仓幂等 init 失败: %v", err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != config.AutomationModeLegacy {
		t.Fatalf(
			"旧仓不得静默变 auto,得到 %s",
			cfg.EffectiveAutomationMode(),
		)
	}
	if cfg.HasDeclaredAutomationMode() {
		t.Fatal("旧仓 legacy 不应被 init 物化为新字段")
	}
}
