package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func newCognitionOnboardCmd() *cobra.Command {
	command := &cobra.Command{Use: "onboard", Short: cliMessage("cli.short.cognition_onboard")}
	command.AddCommand(newOnboardStartCmd(), newOnboardStatusCmd(), newOnboardNextCmd(), newOnboardResumeCmd(),
		newOnboardPreviewCmd(), newOnboardPrepareCmd(), newOnboardApplyCmd(), newOnboardAbortCmd())
	return command
}

func newOnboardStartCmd() *cobra.Command {
	var locale string
	command := &cobra.Command{Use: "start", Short: cliMessage("cli.short.cognition_onboard_start"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		if locale == "" {
			locale = textassets.ActiveLocale()
		}
		session, err := onboarding.Start(root, locale, time.Now())
		if err != nil {
			return onboardingExitError(err)
		}
		return writeOnboardingSession(cmd, root, session)
	}}
	command.Flags().StringVar(&locale, "locale", "", cliMessage("cognition.plan.flag.locale"))
	return command
}

func newOnboardStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: cliMessage("cli.short.cognition_onboard_status"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		session, err := onboarding.LoadRequired(root)
		if err != nil {
			return onboardingExitError(err)
		}
		return writeOnboardingSession(cmd, root, session)
	}}
}

func newOnboardNextCmd() *cobra.Command {
	var completionFile, receiptFile string
	var maxObjects int
	var maxEvidence int64
	var collectDatabase bool
	command := &cobra.Command{Use: "next", Short: cliMessage("cli.short.cognition_onboard_next"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		if completionFile != "" {
			data, err := readPlannerInput(completionFile)
			if err != nil {
				return onboardingExitError(err)
			}
			var completion onboarding.Completion
			if err := decodeOnboardingCLI(data, &completion); err != nil {
				return onboardingExitError(err)
			}
			if _, err := onboarding.CompleteTasks(root, completion); err != nil {
				return onboardingExitError(err)
			}
		}
		if receiptFile != "" {
			data, err := readPlannerInput(receiptFile)
			if err != nil {
				return onboardingExitError(err)
			}
			var receipt onboarding.HostDeliveryReceipt
			if err := decodeOnboardingCLI(data, &receipt); err != nil {
				return onboardingExitError(err)
			}
			if _, err := onboarding.RecordHostDelivery(root, receipt); err != nil {
				return onboardingExitError(err)
			}
		}
		if collectDatabase {
			if _, err := onboarding.CollectDatabaseEvidence(cmd.Context(), root, nil); err != nil {
				return onboardingExitError(err)
			}
		}
		batch, err := onboarding.Next(root, maxObjects, maxEvidence)
		if err != nil {
			return onboardingExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, batch)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.batch_summary", batch.ObjectCount, batch.EvidenceBytes, batch.CompletedCount, batch.PendingCount, batch.SemanticGenerated))
		return nil
	}}
	command.Flags().StringVar(&completionFile, "completion-file", "", cliMessage("onboarding.flag.completion_file"))
	command.Flags().StringVar(&receiptFile, "host-receipt-file", "", cliMessage("onboarding.flag.host_receipt_file"))
	command.Flags().IntVar(&maxObjects, "max-objects", 25, cliMessage("onboarding.flag.max_objects"))
	command.Flags().Int64Var(&maxEvidence, "max-evidence-bytes", 256*1024, cliMessage("onboarding.flag.max_evidence"))
	command.Flags().BoolVar(&collectDatabase, "collect-database", false, cliMessage("onboarding.flag.collect_database"))
	return command
}

func newOnboardResumeCmd() *cobra.Command {
	return &cobra.Command{Use: "resume", Short: cliMessage("cli.short.cognition_onboard_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		session, err := onboarding.Resume(root)
		if err != nil {
			return onboardingExitError(err)
		}
		return writeOnboardingSession(cmd, root, session)
	}}
}

func newOnboardPreviewCmd() *cobra.Command {
	var candidateFile, mappingFile string
	command := &cobra.Command{Use: "preview", Short: cliMessage("cli.short.cognition_onboard_preview"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		candidate, err := readPlannerInput(candidateFile)
		if err != nil {
			return onboardingExitError(err)
		}
		var mapping []byte
		if mappingFile != "" {
			mapping, err = readPlannerInput(mappingFile)
			if err != nil {
				return onboardingExitError(err)
			}
		}
		preview, err := onboarding.Preview(root, candidate, mapping)
		if err != nil {
			return onboardingExitError(err)
		}
		return writePlannerJSON(cmd, preview)
	}}
	command.Flags().StringVar(&candidateFile, "candidate-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	command.Flags().StringVar(&mappingFile, "mapping-file", "", cliMessage("cognition.migration.flag.mapping_file"))
	_ = command.MarkFlagRequired("candidate-file")
	return command
}

func newOnboardPrepareCmd() *cobra.Command {
	var actor string
	command := &cobra.Command{Use: "prepare", Short: cliMessage("cli.short.cognition_onboard_prepare"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		envelope, err := onboarding.Prepare(root)
		if err != nil {
			return onboardingExitError(err)
		}
		session, err := onboarding.LoadRequired(root)
		if err != nil {
			return onboardingExitError(err)
		}
		if session.ApprovalState == "policy_bound_auto" {
			return writePlannerJSON(cmd, struct {
				Session       *onboarding.Session `json:"session"`
				Envelope      any                 `json:"envelope"`
				Authorization struct {
					Mechanism   string `json:"mechanism"`
					TTYRequired bool   `json:"tty_required"`
					NextAction  string `json:"next_action"`
				} `json:"authorization"`
			}{Session: session, Envelope: envelope, Authorization: struct {
				Mechanism   string `json:"mechanism"`
				TTYRequired bool   `json:"tty_required"`
				NextAction  string `json:"next_action"`
			}{Mechanism: "policy_bound_auto", TTYRequired: false, NextAction: session.NextAction}})
		}
		var digest string
		var writeCount int
		var exact string
		switch value := envelope.(type) {
		case *bootstrapapply.ApplyEnvelope:
			digest, writeCount = value.EnvelopeDigest, len(value.WriteOrder)
			exact = hostInteractionCommand("cognition", "bootstrap", "approve", "--envelope-file", session.EnvelopeArtifact, "--actor", actor, "--json")
		case *migrationapply.ApplyEnvelope:
			digest, writeCount = value.EnvelopeDigest, len(value.WriteSet)
			exact = hostInteractionCommand("cognition", "migration", "approve", "--envelope-file", session.EnvelopeArtifact, "--actor", actor, "--json")
		}
		interaction := hostInteractionRequired{Version: hostInteractionVersion, InteractionRequired: true,
			InteractionKind: "human_tty_digest_confirmation", ExactCommand: exact, Digest: digest,
			SafeSummary: fmt.Sprintf("write_set_count=%d", writeCount), ConfirmationPhrase: "APPROVE " + strings.ToUpper(session.Operation) + " " + digest,
			ResumeAction: hostInteractionCommand("cognition", "onboard", "apply", "--approval-file", "{approval_file}", "--json"),
			TTYRequired:  true, ModelSelfApproval: false, FormalWritesStarted: false}
		return writePlannerJSON(cmd, struct {
			Session     *onboarding.Session     `json:"session"`
			Envelope    any                     `json:"envelope"`
			Interaction hostInteractionRequired `json:"interaction"`
		}{session, envelope, interaction})
	}}
	command.Flags().StringVar(&actor, "approval-actor", "human-operator", cliMessage("cognition.bootstrap.flag.actor"))
	return command
}

func newOnboardApplyCmd() *cobra.Command {
	var approvalFile string
	command := &cobra.Command{Use: "apply", Short: cliMessage("cli.short.cognition_onboard_apply"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		result, err := onboarding.Apply(root, approvalFile)
		if err != nil {
			return onboardingExitError(err)
		}
		return writePlannerJSON(cmd, result)
	}}
	command.Flags().StringVar(&approvalFile, "approval-file", "", cliMessage("cognition.bootstrap.flag.approval_file"))
	_ = command.MarkFlagRequired("approval-file")
	return command
}

func newOnboardAbortCmd() *cobra.Command {
	return &cobra.Command{Use: "abort", Short: cliMessage("cli.short.cognition_onboard_abort"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		session, err := onboarding.Abort(root)
		if err != nil {
			return onboardingExitError(err)
		}
		return writeOnboardingSession(cmd, root, session)
	}}
}

func writeOnboardingSession(cmd *cobra.Command, root string, session *onboarding.Session) error {
	if flagJSON {
		return writePlannerJSON(cmd, session)
	}
	policy := onboarding.EffectiveAutomationPolicy(session)
	if session.Operation == "bootstrap" && policy.Mode == "auto" {
		writeAutoOnboardingProgress(cmd, session)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_header"))
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_inventory", len(session.BusinessSourceManifest.Files)))
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_authoring", len(session.CompletedAuthoringTargets), len(session.PendingAuthoringTargets)))
	if session.Version == onboarding.SessionVersion {
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_semantic_authoring"))
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_security", session.BusinessRowsRead, session.DDLDMLStatements, session.NetworkAccessed))
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress_next", session.NextAction))
	return nil
}

func writeAutoOnboardingProgress(cmd *cobra.Command, session *onboarding.Session) {
	if session.Status == "completed" && session.NextAction == "none" {
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress.auto.complete"))
		return
	}
	switch session.LastSuccessPoint {
	case "safe_inventory_and_plan":
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress.auto.analyzing"))
	case "preview_ready", "envelope_prepared", "formal_assets_complete":
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress.auto.verifying"))
	default:
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("onboarding.progress.auto.establishing"))
	}
}

func decodeOnboardingCLI(data []byte, target any) error {
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("onboarding_transport_trailing_json")
	}
	return nil
}

func onboardingExitError(err error) error {
	return &ExitError{Code: ExitInvalid, MachineCode: "cognition_onboarding_invalid", Msg: cliMessage("onboarding.error", textassets.DiagnosticFacts(err.Error()))}
}
