// D69 自动化治理配置测试。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestAutomationMissingMeansLegacy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		FilePath(root),
		[]byte("{\"version\":1}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != AutomationModeLegacy {
		t.Fatalf(
			"缺字段旧仓应为 legacy,得到 %q",
			cfg.EffectiveAutomationMode(),
		)
	}
	if cfg.HasDeclaredAutomationMode() {
		t.Fatal("legacy 兼容态不应物化 automation 块")
	}
}

func TestOnboardingAutomationDefaultsOnlyFreshBootstrapToAuto(t *testing.T) {
	cfg := DefaultConfig()
	fresh := cfg.ResolveOnboardingAutomation(true)
	if fresh.Mode != AutomationModeAuto || fresh.Source != machinecontract.CognitionAutomationPolicyFreshDefault {
		t.Fatalf("Fresh default policy mismatch: %#v", fresh)
	}
	legacy := cfg.ResolveOnboardingAutomation(false)
	if legacy.Mode != AutomationModeLegacy || legacy.Source != machinecontract.CognitionAutomationPolicyLegacy {
		t.Fatalf("legacy compatibility policy mismatch: %#v", legacy)
	}
	if err := cfg.SetAutomationMode(AutomationModeReview); err != nil {
		t.Fatal(err)
	}
	explicit := cfg.ResolveOnboardingAutomation(true)
	if explicit.Mode != AutomationModeReview || explicit.Source != machinecontract.CognitionAutomationPolicyTeamConfig {
		t.Fatalf("explicit team policy was not authoritative: %#v", explicit)
	}
}

func TestAutomationModesAndLegacyReset(t *testing.T) {
	cfg := DefaultConfig()

	for _, mode := range []string{
		AutomationModeOff,
		AutomationModeReview,
		AutomationModeAuto,
	} {
		if err := cfg.SetAutomationMode(mode); err != nil {
			t.Fatalf("设置 %s 失败: %v", mode, err)
		}
		if got := cfg.EffectiveAutomationMode(); got != mode {
			t.Fatalf("期望 %s,得到 %s", mode, got)
		}
	}

	if err := cfg.SetAutomationMode("manual"); err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != AutomationModeLegacy ||
		cfg.Automation != nil {
		t.Fatalf("manual 应归一为 legacy nil: %+v", cfg)
	}
}

func TestAutomationInvalidConfigRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		FilePath(root),
		[]byte(`{"version":1,"automation":{"mode":"wild"}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadBase(root); err == nil {
		t.Fatal("非法 automation mode 应拒绝加载")
	}
}

func TestAutomationTeamPolicyCannotBeOverriddenByLocal(t *testing.T) {
	root := t.TempDir()
	base := DefaultConfig()
	if err := base.SetAutomationMode(AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, base); err != nil {
		t.Fatal(err)
	}

	local := map[string]any{
		"version": 1,
		"automation": map[string]any{
			"mode": AutomationModeReview,
		},
	}
	localBytes, _ := json.Marshal(local)
	if err := os.WriteFile(
		LocalFilePath(root),
		append(localBytes, '\n'),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	effective, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if effective.EffectiveAutomationMode() != AutomationModeAuto {
		t.Fatalf(
			"local 不得覆盖团队 auto,得到 %s",
			effective.EffectiveAutomationMode(),
		)
	}

	if err := SaveLocal(root, effective); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if _, exists := saved["automation"]; exists {
		t.Fatal("SaveLocal 应移除越权 automation 键")
	}
}
