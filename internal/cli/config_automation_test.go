// D69 config get/set automation_mode 测试。
package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func runConfigForAutomation(
	t *testing.T,
	root string,
	args ...string,
) error {
	t.Helper()

	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() {
		flagRepo, flagQuiet = oldRepo, oldQuiet
	})

	command := findRegisteredCommand(t, "config")
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs(args)
	return command.Execute()
}

func TestConfigSetAutomationMode(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}

	if err := runConfigForAutomation(
		t,
		root,
		"set",
		"automation_mode",
		"review",
	); err != nil {
		t.Fatalf("set review 失败: %v", err)
	}

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != config.AutomationModeReview {
		t.Fatalf(
			"期望 review,得到 %s",
			cfg.EffectiveAutomationMode(),
		)
	}

	if err := runConfigForAutomation(
		t,
		root,
		"set",
		"automation_mode",
		"legacy",
	); err != nil {
		t.Fatalf("set legacy 失败: %v", err)
	}

	cfg, err = config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != config.AutomationModeLegacy ||
		cfg.Automation != nil {
		t.Fatalf("legacy 应清除配置块: %+v", cfg)
	}
}

func TestConfigSetAutomationModeRejectsInvalid(t *testing.T) {
	root := t.TempDir()
	cfg := legacyTestConfig()
	if err := cfg.SetAutomationMode(config.AutomationModeOff); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	err := runConfigForAutomation(
		t,
		root,
		"set",
		"automation_mode",
		"unbounded",
	)
	if err == nil {
		t.Fatal("非法 mode 应硬拒")
	}

	after, loadErr := config.LoadBase(root)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if after.EffectiveAutomationMode() != config.AutomationModeOff {
		t.Fatalf(
			"拒绝后原配置不得变化: %s",
			after.EffectiveAutomationMode(),
		)
	}
}
