package scopechange

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func hostDialogPreview() *Preview {
	return &Preview{EnvelopeDigest: strings.Repeat("a", 64), Plan: Plan{InteractionRequired: true,
		WriteSet: []string{".aoci/baseline.json", "aoci.txt"}, RecoveryDirection: "restore_exact_preimage"}}
}

func TestHostDialogApprovalBindsWhatATerminalApprovalBinds(t *testing.T) {
	preview := hostDialogPreview()
	dialog, err := NewHostDialogApproval(preview, "host-dialog@codex", "2026-10-27T00:00:00Z")
	if err != nil || dialog.Mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation {
		t.Fatalf("dialog approval: %#v %v", dialog, err)
	}
	terminal, err := NewApproval(preview, "host-dialog@codex", "2026-10-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if dialog.EnvelopeDigest != terminal.EnvelopeDigest || dialog.ApprovedRecoveryPolicy != terminal.ApprovedRecoveryPolicy ||
		strings.Join(dialog.ApprovedWriteSet, ",") != strings.Join(terminal.ApprovedWriteSet, ",") || dialog.ApprovalDigest == terminal.ApprovalDigest {
		t.Fatalf("the two channels bind different things: %#v %#v", dialog, terminal)
	}
	// Recovery replays an Intent with the structural check alone.
	if err := validateApproval(preview, dialog); err != nil || !ApprovalBinds(preview, dialog) {
		t.Fatalf("a recorded dialog approval does not replay: %v", err)
	}
	forged := *dialog
	forged.Mechanism = "model_self_approval"
	if validateApproval(preview, &forged) == nil {
		t.Fatal("an unknown mechanism was accepted")
	}
	other := hostDialogPreview()
	other.EnvelopeDigest = strings.Repeat("b", 64)
	if ApprovalBinds(other, dialog) {
		t.Fatal("a dialog approval authorized another envelope")
	}
}

func TestHostDialogApprovalAdmissionFollowsChannelAndRisk(t *testing.T) {
	preview := hostDialogPreview()
	dialog, err := NewHostDialogApproval(preview, "host-dialog", "2026-10-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := NewApproval(preview, "operator", "2026-10-27T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := admitApprovalChannel(cfg, preview, dialog); err != nil {
		t.Fatalf("default channel refused the dialog: %v", err)
	}
	if err := cfg.SetHumanApprovalChannel(machinecontract.HumanApprovalChannelTTYOnly); err != nil {
		t.Fatal(err)
	}
	if err := admitApprovalChannel(cfg, preview, dialog); err == nil || !strings.Contains(err.Error(), "managed_scope_host_dialog_approval_disabled") {
		t.Fatalf("tty_only admitted the dialog: %v", err)
	}
	if err := admitApprovalChannel(cfg, preview, terminal); err != nil {
		t.Fatalf("tty_only refused the terminal: %v", err)
	}
	risky := hostDialogPreview()
	risky.Plan.Risk.HighRiskOptIn = true
	if HostDialogEligible(risky) {
		t.Fatal("a high-risk repository is eligible for the dialog")
	}
	if _, err := NewHostDialogApproval(risky, "host-dialog", "2026-10-27T00:00:00Z"); err == nil {
		t.Fatal("a dialog approval was minted for high-risk content")
	}
	if err := admitApprovalChannel(config.DefaultConfig(), risky, dialog); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_host_dialog_approval_not_eligible") {
		t.Fatalf("a dialog approval was admitted for high-risk content: %v", err)
	}
}

func TestApprovalEffectsNameEveryConsequenceInOrder(t *testing.T) {
	preview := hostDialogPreview()
	if effects := ApprovalEffects(preview); len(effects) != 1 || effects[0].Kind != "none" {
		t.Fatalf("a policy-only change: %#v", effects)
	}
	preview.Plan.EntryRemoves = []EntryChange{{Path: "a.go"}, {Path: "b.go"}}
	preview.Plan.CoverageReductions = []CoverageReduction{{Path: "a.go"}}
	preview.Plan.Risk.ApprovalPolicyRelaxation = true
	preview.Plan.AuthorizationPolicy.EffectiveMode = machinecontract.ApplyAuthorizationAuto
	preview.Plan.Risk.BudgetRelaxation = true
	preview.Plan.Risk.HighRiskOptIn = true
	kinds := []string{}
	for _, effect := range ApprovalEffects(preview) {
		kinds = append(kinds, effect.Kind)
	}
	if strings.Join(kinds, ",") != "entry_removes,coverage,posture,budget,high_risk" {
		t.Fatalf("effects out of order: %v", kinds)
	}
	summary := ApprovalEffectSummary(preview)
	if strings.Count(summary, "; ") != 4 || !strings.Contains(summary, "2") || !strings.Contains(summary, machinecontract.ApplyAuthorizationAuto) ||
		strings.Contains(summary, "text_asset_error") {
		t.Fatalf("summary %q", summary)
	}
}
