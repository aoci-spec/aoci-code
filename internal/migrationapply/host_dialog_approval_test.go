package migrationapply

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestHostDialogMigrationApprovalBindsLikeTheTerminalAndFollowsTheChannel(t *testing.T) {
	root := migrationFixture(t, false)
	envelope, terminal := preparedMigrationFixture(t, root, []string{"code"})
	dialog, err := RecordHostDialogApproval(envelope, "host-dialog@claude-code", "2026-07-30T00:01:00Z")
	if err != nil || dialog.Mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation {
		t.Fatalf("dialog approval: %#v %v", dialog, err)
	}
	if dialog.ApplyEnvelopeDigest != terminal.ApplyEnvelopeDigest || dialog.MappingSHA256 != terminal.MappingSHA256 ||
		strings.Join(dialog.ApprovedWriteSet, ",") != strings.Join(terminal.ApprovedWriteSet, ",") {
		t.Fatalf("the dialog binds something other than the terminal: %#v %#v", dialog, terminal)
	}
	if err := ValidateApproval(envelope, dialog); err != nil {
		t.Fatalf("a dialog approval does not validate: %v", err)
	}
	for _, bad := range []struct{ actor, at string }{{"", "2026-07-30T00:01:00Z"}, {"host-dialog", "yesterday"}} {
		if _, err := RecordHostDialogApproval(envelope, bad.actor, bad.at); err == nil {
			t.Fatalf("recorded with actor=%q at=%q", bad.actor, bad.at)
		}
	}
	if err := AdmitApprovalChannel(root, dialog); err != nil {
		t.Fatalf("the default channel refused the dialog: %v", err)
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
	if err := AdmitApprovalChannel(root, dialog); err == nil || !strings.Contains(err.Error(), "migration_host_dialog_approval_disabled") {
		t.Fatalf("tty_only admitted a dialog approval: %v", err)
	}
	if err := AdmitApprovalChannel(root, terminal); err != nil {
		t.Fatalf("tty_only refused the terminal: %v", err)
	}
	if _, err := Apply(root, envelope, dialog); err == nil || !strings.Contains(err.Error(), "migration_host_dialog_approval_disabled") {
		t.Fatalf("Apply admitted a dialog approval under tty_only: %v", err)
	}
}
