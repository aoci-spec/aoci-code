package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestConfigSetOverviewChunkTokens(t *testing.T) {
	for _, current := range []struct {
		value string
		want  int
	}{
		{value: "4000", want: 4000},
		{value: "12000", want: 12000},
		{value: "24000", want: 24000},
	} {
		t.Run(current.value, func(t *testing.T) {
			root := t.TempDir()
			if err := config.Save(root, legacyTestConfig()); err != nil {
				t.Fatal(err)
			}
			if err := runConfigForAutomation(t, root, "set", "overview_delivery.chunk_tokens", current.value); err != nil {
				t.Fatalf("set chunk tokens %s: %v", current.value, err)
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.OverviewDelivery.ChunkTokens != current.want {
				t.Fatalf("chunk tokens = %d", cfg.OverviewDelivery.ChunkTokens)
			}
		})
	}
}

func TestConfigSetOverviewChunkTokensRejectsInvalid(t *testing.T) {
	for _, value := range []string{"3999", "24001", "20.5", "auto"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			cfg := legacyTestConfig()
			cfg.OverviewDelivery.ChunkTokens = 20000
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			if err := runConfigForAutomation(t, root, "set", "overview_delivery.chunk_tokens", value); err == nil {
				t.Fatalf("invalid chunk tokens %q were accepted", value)
			}
			after, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if after.OverviewDelivery.ChunkTokens != 20000 {
				t.Fatalf("invalid update changed chunk tokens to %d", after.OverviewDelivery.ChunkTokens)
			}
		})
	}
}
