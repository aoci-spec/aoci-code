// Automation governance mode definitions, compatibility interpretation, and the team policy boundary.
//
// Index entry: automation.go[GCF7S]
//
// The four `aoci index update` modes D69 implemented:
//
//	legacy:
//	  the compatibility state of an old repository whose configuration field is missing. It keeps the pre-upgrade behaviour: an explicit
//	  update calls the user-configured model endpoint to generate a draft, and a person then runs check/diff/apply.
//	off:
//	  the team explicitly turned off the AI orchestration of update. The command only does deterministic drift detection and reporting,
//	  building no AI client, validating no key, creating no draft, sending no source.
//	review:
//	  update drafts automatically and calls the shared machine precheck, always stopping in the draft area to wait for a human
//	  diff/apply; a machine refusal or no draft items returns ExitInvalid.
//	auto:
//	  once the generation is complete, the machine precheck passes, and the P-23 digest agrees, update applies the whole batch
//	  through the atomic batch write-back; any failure keeps the draft and never applies partially.
//
// The automatic workflow never silently deletes a source file or an Entry without governance grounds. auto mode may
// atomically retire the formal Entries covered by a role transition once Scope Policy, Retention Review, the exact Envelope, CAS, and
// the recoverable transaction have all passed; an Orphan is still only reported.
//
// Automation is a team governance asset that only config.json may declare. config.Load restores the team value after
// merging the local layer, so a personal config.local.json cannot silently rewrite the team policy.
package config

import (
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	AutomationModeLegacy = "legacy"
	AutomationModeOff    = "off"
	AutomationModeReview = "review"
	AutomationModeAuto   = "auto"
)

// AutomationConfig is the automation block of config.json.
type AutomationConfig struct {
	Mode string `json:"mode"`
}

// AutomationPolicy is the machine-selected policy captured by a Plan and an
// Onboarding Session. Source explains whether the mode came from team config,
// the Fresh Bootstrap default, or the legacy compatibility boundary.
type AutomationPolicy struct {
	Mode   string `json:"mode"`
	Source string `json:"source"`
}

// ResolveOnboardingAutomation selects a policy without changing configuration.
// Missing automation stays legacy everywhere except a proven Fresh Bootstrap.
func (c *Config) ResolveOnboardingAutomation(freshBootstrap bool) AutomationPolicy {
	if c != nil && c.HasDeclaredAutomationMode() {
		return AutomationPolicy{Mode: c.EffectiveAutomationMode(), Source: machinecontract.CognitionAutomationPolicyTeamConfig}
	}
	if freshBootstrap {
		return AutomationPolicy{Mode: AutomationModeAuto, Source: machinecontract.CognitionAutomationPolicyFreshDefault}
	}
	return AutomationPolicy{Mode: AutomationModeLegacy, Source: machinecontract.CognitionAutomationPolicyLegacy}
}

// ParseAutomationMode normalizes user input into the canonical mode.
// manual is only a CLI-friendly alias and is normalized to legacy on write, that is, the block is removed.
func ParseAutomationMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", AutomationModeLegacy, "manual":
		return AutomationModeLegacy, nil
	case AutomationModeOff:
		return AutomationModeOff, nil
	case AutomationModeReview:
		return AutomationModeReview, nil
	case AutomationModeAuto:
		return AutomationModeAuto, nil
	default:
		return "", fmt.Errorf(
			"automation mode 非法: %q(可用: legacy/manual/off/review/auto)",
			raw,
		)
	}
}

// EffectiveAutomationMode returns the mode currently in effect.
// Automation=nil is the old-repository compatibility state and must not be read as auto or off on its own.
func (c *Config) EffectiveAutomationMode() string {
	if c == nil || c.Automation == nil {
		return AutomationModeLegacy
	}
	mode, err := ParseAutomationMode(c.Automation.Mode)
	if err != nil {
		return c.Automation.Mode
	}
	return mode
}

// HasDeclaredAutomationMode reports whether the team configuration declares automation explicitly.
// legacy is expressed by the missing field, so it returns false.
func (c *Config) HasDeclaredAutomationMode() bool {
	return c != nil && c.Automation != nil
}

// SetAutomationMode writes the canonical mode.
// legacy/manual set Automation to nil, removing the field from the JSON and restoring the old-repository compatibility state.
func (c *Config) SetAutomationMode(raw string) error {
	mode, err := ParseAutomationMode(raw)
	if err != nil {
		return err
	}
	if mode == AutomationModeLegacy {
		c.Automation = nil
		return nil
	}
	c.Automation = &AutomationConfig{Mode: mode}
	return nil
}

// normalizeAutomationConfig validates and normalizes the mode after loading.
func normalizeAutomationConfig(c *Config) error {
	if c == nil || c.Automation == nil {
		return nil
	}
	mode, err := ParseAutomationMode(c.Automation.Mode)
	if err != nil {
		return err
	}
	if mode == AutomationModeLegacy {
		c.Automation = nil
		return nil
	}
	c.Automation.Mode = mode
	return nil
}

// cloneAutomationConfig copies the team automation configuration.
// Load saves the team value before merging local and restores it afterwards, so the local layer cannot override it.
func cloneAutomationConfig(in *AutomationConfig) *AutomationConfig {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
