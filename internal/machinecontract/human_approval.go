package machinecontract

// A protected change has always needed a person: in a terminal the operator
// types the digest phrase (interactive_digest_confirmation). The host dialog is
// the second channel. aoci_maintain with intent=human_approval asks the MCP
// host to show the change, as AOCI itself describes it, to the person at the
// keyboard, and records what they chose. The host vouches that a person
// answered; a team that does not accept that sets human_approval_channel to
// tty_only and every approval goes back to the terminal.
const (
	ApprovalMechanismHostElicitationConfirmation = "host_elicitation_confirmation"

	HumanApprovalChannelTTYOrHostDialog = "tty_or_host_dialog"
	HumanApprovalChannelTTYOnly         = "tty_only"

	MaintainIntentHumanApproval = "human_approval"

	HumanApprovalKindScopeChange = "scope_change"
	HumanApprovalKindOnboarding  = "onboarding"

	HumanApprovalDecisionApprove = "approve"
	HumanApprovalDecisionReject  = "reject"

	HumanApprovalStatusApproved              = "approved"
	HumanApprovalStatusAlreadyApproved       = "already_approved"
	HumanApprovalStatusRejected              = "rejected"
	HumanApprovalStatusDismissed             = "dismissed"
	HumanApprovalStatusHostDialogUnavailable = "host_dialog_unavailable"
	HumanApprovalStatusHostDialogDisabled    = "host_dialog_disabled"
	HumanApprovalStatusTTYRequired           = "tty_required"
	HumanApprovalStatusStale                 = "stale"

	// The next_action of a human_approval result.
	HumanApprovalActionApply     = "apply_approved_change"
	HumanApprovalActionTerminal  = "ask_person_to_approve_in_terminal"
	HumanApprovalActionAskPerson = "ask_person_what_to_change"
	HumanApprovalActionAskAgain  = "ask_again_when_person_is_ready"
	HumanApprovalActionRebuild   = "rebuild_pending_change"
)

func HumanApprovalChannels() []string {
	return []string{HumanApprovalChannelTTYOrHostDialog, HumanApprovalChannelTTYOnly}
}

func HumanApprovalKinds() []string {
	return []string{HumanApprovalKindScopeChange, HumanApprovalKindOnboarding}
}
