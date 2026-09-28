package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A repository initialized for Claude Code starts with the wider transport
// settings that host was measured to carry; any other host, and an existing
// team value, keep the defaults sized for Codex.
func TestInitWritesClaudeCodeTransportDefaultsOnly(t *testing.T) {
	claude := t.TempDir()
	if _, err := runInit(t, claude, "--agent=claude", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(claude)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaintainTransportBudgetBytes != machinecontract.MaintainTransportBudgetBytesClaudeCode ||
		cfg.OverviewDelivery.ChunkTokens != machinecontract.OverviewChunkTokensClaudeCode {
		t.Fatalf("claude init must size the transport for its measured window: budget=%d chunk=%d",
			cfg.MaintainTransportBudgetBytes, cfg.OverviewDelivery.ChunkTokens)
	}
	codex := t.TempDir()
	if _, err := runInit(t, codex, "--agent=codex", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadBase(codex)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaintainTransportBudgetBytes != 0 || cfg.MaintainTransportBudget() != machinecontract.MaintainTransportBudgetBytesDefault ||
		cfg.OverviewDelivery.ChunkTokens != machinecontract.OverviewChunkTokensDefault {
		t.Fatalf("codex init must keep the conservative defaults: budget=%d chunk=%d",
			cfg.MaintainTransportBudgetBytes, cfg.OverviewDelivery.ChunkTokens)
	}
}
