package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestHumanApprovalChannelDefaultsToTerminalOrDialog(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EffectiveHumanApprovalChannel() != machinecontract.HumanApprovalChannelTTYOrHostDialog ||
		!cfg.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation) {
		t.Fatalf("an absent channel must admit the host dialog: %q", cfg.HumanApprovalChannel)
	}
	for _, mechanism := range []string{machinecontract.ApprovalMechanismInteractiveDigestConfirmation, machinecontract.ApprovalMechanismPolicyBoundAuto} {
		if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
			t.Fatal(err)
		}
		if !cfg.AdmitsApprovalMechanism(mechanism) {
			t.Fatalf("the channel must govern the host dialog alone, not %s", mechanism)
		}
	}
	if cfg.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation) {
		t.Fatal("tty_only admitted a host-dialog approval")
	}
	var absent *Config
	if absent.AdmitsApprovalMechanism(machinecontract.ApprovalMechanismHostElicitationConfirmation) {
		t.Fatal("a missing configuration admitted a host-dialog approval")
	}
	// Choosing the default again stores it as absent, so the bytes of a
	// repository that never chose do not change.
	if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOrHostDialog); err != nil || cfg.HumanApprovalChannel != "" {
		t.Fatalf("default channel stored as %q: %v", cfg.HumanApprovalChannel, err)
	}
	for _, invalid := range []string{"", "dialog", "TTY_ONLY"} {
		if err := cfg.SetHumanApprovalChannel(invalid); err == nil {
			t.Fatalf("invalid channel %q accepted", invalid)
		}
	}
}

func TestHumanApprovalChannelIsTeamOwned(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	local := `{"human_approval_channel": "tty_or_host_dialog"}` + "\n"
	if err := os.WriteFile(LocalFilePath(root), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveHumanApprovalChannel() != machinecontract.HumanApprovalChannelTTYOnly {
		t.Fatalf("a local file relaxed the team channel to %q", loaded.EffectiveHumanApprovalChannel())
	}
	team, err := os.ReadFile(FilePath(root))
	if err != nil || !strings.Contains(string(team), `"human_approval_channel": "tty_only"`) {
		t.Fatalf("team file does not carry the channel: %s %v", team, err)
	}
	broken := strings.Replace(string(team), `"tty_only"`, `"dialog_only"`, 1)
	if err := os.WriteFile(filepath.Join(root, ".aoci", "config.json"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "human_approval_channel_invalid") {
		t.Fatalf("an unknown channel was loaded: %v", err)
	}
	if _, err := LoadBase(root); err == nil || !strings.Contains(err.Error(), "human_approval_channel_invalid") {
		t.Fatalf("an unknown channel was loaded for editing: %v", err)
	}
}
