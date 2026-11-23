package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// Qoder CLI reads the project-level .mcp.json that Claude Code reads (verified
// against @qoder-ai/qodercli 1.1.64: `qoder mcp list` reports the server
// Connected from that file alone). `init --agent qoder` therefore writes that
// one file plus the AGENTS.md block, records the host, installs no hook, and
// keeps the conservative transport defaults because Qoder's tool-result window
// has not been measured.
func TestInitQoderWritesSharedMCPJSONAndNoHooks(t *testing.T) {
	root := t.TempDir()
	// init writes its human report straight to the process stdout, so the
	// report is captured there as well.
	captured, stdout, stderr, code := runCLIPrinted(t, root, "init", "--agent", "qoder", "--hooks", "--locale", "en-US")
	if code != ExitOK {
		t.Fatalf("init --agent qoder failed: code=%d stdout=%s stderr=%s", code, captured, stderr)
	}
	out := captured + stdout
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		t.Fatalf("init --agent qoder must write .mcp.json: %v", err)
	}
	if !strings.Contains(string(data), `"aoci"`) || !strings.Contains(string(data), filepath.ToSlash(root)) {
		t.Fatalf(".mcp.json must bind the aoci server to this repository: %s", data)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("qoder has no hook surface, so no Claude Code hook settings may be written: %v", err)
	}
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, cliMessage("init.hooks_agent_ignored")) {
		t.Fatalf("--hooks must be reported as ignored for qoder: %s", out)
	}
	hint, err := textassets.Message(textassets.DefaultLocale, "hook.qoder_shares_mcp_json")
	if err != nil {
		t.Fatalf("hook.qoder_shares_mcp_json is missing from the %s catalog: %v", textassets.DefaultLocale, err)
	}
	if !strings.Contains(out, hint) {
		t.Fatalf("init --agent qoder must say Qoder reads the shared .mcp.json: %s", out)
	}
	if strings.Contains(out, "[text_asset_error") {
		t.Fatalf("init --agent qoder printed a missing text asset: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("the AGENTS.md block is the second half of the integration: %v", err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	recorded := false
	for _, agent := range cfg.InstalledAgents {
		if agent == "qoder" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatalf("installed_agents must record qoder: %#v", cfg.InstalledAgents)
	}
	if cfg.MaintainTransportBudgetBytes != 0 || cfg.OverviewDelivery.ChunkTokens != machinecontract.OverviewChunkTokensDefault {
		t.Fatalf("qoder keeps the conservative transport defaults until its window is measured: budget=%d chunk=%d",
			cfg.MaintainTransportBudgetBytes, cfg.OverviewDelivery.ChunkTokens)
	}
	if guide := initAgentGuideCommand("qoder"); !strings.Contains(guide, "--agent claude") {
		t.Fatalf("the Guide for qoder is the Claude Code guide, which reads the same files: %q", guide)
	}
}
