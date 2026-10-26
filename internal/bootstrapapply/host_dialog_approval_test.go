package bootstrapapply

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestHostDialogBootstrapApprovalAppliesWhileTheTeamAdmitsIt(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, "main.go", "package main\n")
	envelope, terminal := preparedFixture(t, root, []string{"code"})
	dialog, err := RecordHostDialogApproval(envelope, "host-dialog@codex", "2026-07-30T00:01:00Z")
	if err != nil || dialog.Mechanism != HostDialogApprovalMechanism {
		t.Fatalf("dialog approval: %#v %v", dialog, err)
	}
	if dialog.ApplyEnvelopeDigest != terminal.ApplyEnvelopeDigest || strings.Join(dialog.ApprovedWriteSet, ",") != strings.Join(terminal.ApprovedWriteSet, ",") {
		t.Fatalf("the dialog binds something other than the terminal: %#v %#v", dialog, terminal)
	}
	if err := ValidateApproval(envelope, dialog); err != nil {
		t.Fatalf("a dialog approval does not validate: %v", err)
	}
	if _, err := RecordHostDialogApproval(envelope, "not an actor!", "2026-07-30T00:01:00Z"); err == nil {
		t.Fatal("an invalid actor was recorded")
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := AdmitApprovalChannel(root, dialog); err == nil || !strings.Contains(err.Error(), "bootstrap_host_dialog_approval_disabled") {
		t.Fatalf("tty_only admitted a dialog approval: %v", err)
	}
	if err := AdmitApprovalChannel(root, terminal); err != nil {
		t.Fatalf("tty_only refused the terminal: %v", err)
	}
	if _, err := Apply(root, envelope, dialog); err == nil || !strings.Contains(err.Error(), "bootstrap_host_dialog_approval_disabled") {
		t.Fatalf("Apply admitted a dialog approval under tty_only: %v", err)
	}
	if pending, err := Pending(root); err != nil || len(pending) != 0 {
		t.Fatalf("a refused dialog approval left a transaction: %v %v", pending, err)
	}
}

func TestHostDialogBootstrapApprovalCompletesTheBootstrap(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, "main.go", "package main\n")
	envelope, _ := preparedFixture(t, root, []string{"code"})
	dialog, err := RecordHostDialogApproval(envelope, "host-dialog", "2026-07-30T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, envelope, dialog)
	if err != nil || !result.FormalComplete {
		t.Fatalf("a dialog approval did not complete the Bootstrap: %#v %v", result, err)
	}
}
