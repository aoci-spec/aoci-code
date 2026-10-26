package config

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// EffectiveHumanApprovalChannel names how a protected change may reach its
// person. Absent is tty_or_host_dialog: the operator types the digest phrase
// in a terminal or answers the dialog the MCP host shows on aoci_maintain's
// request.
func (configValue *Config) EffectiveHumanApprovalChannel() string {
	if configValue == nil || configValue.HumanApprovalChannel == "" {
		return machinecontract.HumanApprovalChannelTTYOrHostDialog
	}
	return configValue.HumanApprovalChannel
}

// AdmitsApprovalMechanism is the one answer to whether team policy accepts an
// approval recorded by mechanism. Only the host dialog is governed by
// human_approval_channel; the terminal and policy-bound mechanisms keep their
// own gates and pass here unchanged. A nil configuration admits no dialog.
func (configValue *Config) AdmitsApprovalMechanism(mechanism string) bool {
	if mechanism != machinecontract.ApprovalMechanismHostElicitationConfirmation {
		return true
	}
	return configValue != nil &&
		configValue.EffectiveHumanApprovalChannel() == machinecontract.HumanApprovalChannelTTYOrHostDialog
}

// SetHumanApprovalChannel validates and stores the team channel. The default
// is written as absent so a repository that never chose keeps its old bytes.
func (configValue *Config) SetHumanApprovalChannel(channel string) error {
	if channel == "" {
		return fmt.Errorf("human_approval_channel_invalid: %q", channel)
	}
	if err := validateHumanApprovalChannel(channel); err != nil {
		return err
	}
	if channel == machinecontract.HumanApprovalChannelTTYOrHostDialog {
		channel = ""
	}
	configValue.HumanApprovalChannel = channel
	return nil
}

func validateHumanApprovalChannel(channel string) error {
	if channel == "" {
		return nil
	}
	for _, known := range machinecontract.HumanApprovalChannels() {
		if channel == known {
			return nil
		}
	}
	return fmt.Errorf("human_approval_channel_invalid: %q", channel)
}
