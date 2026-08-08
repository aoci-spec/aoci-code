package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestConfigSetCognitionRefreshThreshold(t *testing.T) {
	for _, current := range []struct {
		value string
		want  int
	}{
		{value: "5", want: 5},
		{value: "50", want: 50},
	} {
		t.Run(current.value, func(t *testing.T) {
			root := t.TempDir()
			if err := config.Save(root, legacyTestConfig()); err != nil {
				t.Fatal(err)
			}
			if err := runConfigForAutomation(
				t,
				root,
				"set",
				"cognition_refresh_threshold",
				current.value,
			); err != nil {
				t.Fatalf("set threshold %s: %v", current.value, err)
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CognitionRefreshThreshold != current.want {
				t.Fatalf("threshold = %d", cfg.CognitionRefreshThreshold)
			}
		})
	}
}

func TestConfigSetCognitionRefreshThresholdRejectsInvalid(t *testing.T) {
	for _, value := range []string{"0", "-1", "1.5", "100001"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			cfg := legacyTestConfig()
			cfg.CognitionRefreshThreshold = 30
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			if err := runConfigForAutomation(
				t,
				root,
				"set",
				"cognition_refresh_threshold",
				value,
			); err == nil {
				t.Fatalf("invalid threshold %q was accepted", value)
			}
			after, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if after.CognitionRefreshThreshold != 30 {
				t.Fatalf("invalid update changed threshold to %d", after.CognitionRefreshThreshold)
			}
		})
	}
}
