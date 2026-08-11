package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func newCognitionOnboardCmd() *cobra.Command {
	command := &cobra.Command{Use: "onboard", Short: cliMessage("cli.short.cognition_onboard")}
	command.AddCommand(newOnboardStartCmd(), newOnboardStatusCmd(), newOnboardNextCmd(), newOnboardResumeCmd(),
		newOnboardCandidateCmd(), newOnboardPreviewCmd(), newOnboardPrepareCmd(), newOnboardApplyCmd(), newOnboardAbortCmd())
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
				return onboardingExitError(onboardingStrictContractError("submit_authoring_completion", "completion_file", err,
					[]string{"version", "onboarding_session_id", "batch_id", "completed_task_ids", "semantic_authoring_declaration"}))
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
		if err := decorateOnboardingBatchContract(root, batch, maxObjects, maxEvidence); err != nil {
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

func newOnboardCandidateCmd() *cobra.Command {
	command := &cobra.Command{Use: "candidate"}
	command.AddCommand(newOnboardCandidateBindCmd())
	return command
}

func newOnboardCandidateBindCmd() *cobra.Command {
	var candidatePayloadFile string
	command := &cobra.Command{Use: "bind", RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		candidatePayloadFile, err = filepath.Abs(candidatePayloadFile)
		if err != nil {
			return onboardingExitError(err)
		}
		data, err := readPlannerInput(candidatePayloadFile)
		if err != nil {
			return onboardingExitError(err)
		}
		binding, err := onboarding.BindCandidate(root, data)
		if err != nil {
			if diagnostic := onboardingCandidateDecodeDiagnostic("bind_candidate_payload", "candidate_payload_file", err); diagnostic != nil {
				return onboardingExitError(diagnostic)
			}
			return onboardingExitError(err)
		}
		if err := decorateCandidateBindingContract(root, candidatePayloadFile, binding); err != nil {
			return onboardingExitError(err)
		}
		if flagJSON {
			return writePlannerJSON(cmd, binding)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "candidate_payload_sha256=%s\n", binding.CandidatePayloadSHA256)
		if binding.NextActionContract != nil && binding.NextActionContract.Command != nil {
			fmt.Fprintln(cmd.OutOrStdout(), binding.NextActionContract.Command.DisplayCommand)
		}
		return nil
	}}
	command.Flags().StringVar(&candidatePayloadFile, "candidate-payload-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	_ = command.MarkFlagRequired("candidate-payload-file")
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
	var candidateFile, candidatePayloadFile, provenanceFile, mappingFile string
	command := &cobra.Command{Use: "preview", Short: cliMessage("cli.short.cognition_onboard_preview"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return onboardingExitError(err)
		}
		var candidate []byte
		usingCompleteCandidate := candidateFile != ""
		usingSplitCandidate := candidatePayloadFile != "" || provenanceFile != ""
		if usingCompleteCandidate == usingSplitCandidate || usingSplitCandidate && (candidatePayloadFile == "" || provenanceFile == "") {
			return onboardingExitError(&onboarding.ContractError{Stage: "preview", Field: "candidate_input", CauseCode: "onboarding_candidate_input_mode_invalid",
				Expected: "exactly one of --candidate-file or (--candidate-payload-file and --provenance-file)", Actual: "invalid flag combination", FormalWritesStarted: false})
		}
		if usingCompleteCandidate {
			candidate, err = readPlannerInput(candidateFile)
			if err != nil {
				return onboardingExitError(err)
			}
		} else {
			payloadData, readErr := readPlannerInput(candidatePayloadFile)
			if readErr != nil {
				return onboardingExitError(readErr)
			}
			payload, decodeErr := cognitionplan.DecodeCandidate(payloadData)
			if decodeErr != nil {
				return onboardingExitError(onboardingStrictContractError("preview", "candidate_payload_file", decodeErr,
					[]string{"version", "plan_id", "assets", "mapping_resolutions", "semantic_authoring_provenance"}))
			}
			if payload.SemanticAuthoringProvenance != nil {
				return onboardingExitError(&onboarding.ContractError{Stage: "preview", Field: "semantic_authoring_provenance", CauseCode: "onboarding_candidate_payload_provenance_present",
					Expected: "field omitted from payload file", Actual: "present", FormalWritesStarted: false})
			}
			provenanceData, readErr := readPlannerInput(provenanceFile)
			if readErr != nil {
				return onboardingExitError(readErr)
			}
			var provenance cognitionplan.SemanticAuthoringProvenance
			if decodeErr := decodeOnboardingCLI(provenanceData, &provenance); decodeErr != nil {
				return onboardingExitError(onboardingStrictContractError("preview", "provenance_file", decodeErr,
					[]string{"version", "origin", "authoring_run_id", "plan_id", "evidence_binding_sha256", "candidate_payload_sha256"}))
			}
			payload.SemanticAuthoringProvenance = &provenance
			candidate, err = json.Marshal(payload)
			if err != nil {
				return onboardingExitError(err)
			}
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
	command.Flags().StringVar(&candidatePayloadFile, "candidate-payload-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	command.Flags().StringVar(&provenanceFile, "provenance-file", "", cliMessage("cognition.plan.flag.candidate_file"))
	command.Flags().StringVar(&mappingFile, "mapping-file", "", cliMessage("cognition.migration.flag.mapping_file"))
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
		view, err := newOnboardingSessionView(root, session)
		if err != nil {
			return err
		}
		return writePlannerJSON(cmd, view)
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
	result := &ExitError{Code: ExitInvalid, MachineCode: "cognition_onboarding_invalid", Msg: cliMessage("onboarding.error", textassets.DiagnosticFacts(err.Error()))}
	var contractErr *onboarding.ContractError
	if errors.As(err, &contractErr) {
		result.Details = contractErr
	}
	return result
}

func onboardingStrictContractError(stage, field string, err error, allowed []string) *onboarding.ContractError {
	failedField, causeCode := strictOnboardingDiagnostic(field, err)
	return &onboarding.ContractError{Stage: stage, Field: failedField, CauseCode: causeCode,
		Expected: "strict versioned JSON contract", Actual: err.Error(), AllowedFields: append([]string{}, allowed...),
		FormalWritesStarted: false, Cause: err}
}

func onboardingCandidateDecodeDiagnostic(stage, field string, err error) *onboarding.ContractError {
	var contractErr *onboarding.ContractError
	if !errors.As(err, &contractErr) || contractErr.CauseCode != "onboarding_candidate_payload_invalid" || contractErr.Cause == nil {
		return nil
	}
	return onboardingStrictContractError(stage, field, contractErr.Cause,
		[]string{"version", "plan_id", "assets", "mapping_resolutions", "semantic_authoring_provenance"})
}

func strictOnboardingDiagnostic(inputField string, err error) (string, string) {
	failedField := inputField
	appendField := func(path string) {
		path = strings.TrimSpace(path)
		if path != "" {
			failedField += "." + path
		}
	}

	var duplicate *jsonstrict.DuplicateKeyError
	if errors.As(err, &duplicate) {
		appendField(duplicate.Path)
		return failedField, "onboarding_transport_duplicate_key"
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		appendField(typeError.Field)
		return failedField, "onboarding_transport_type_mismatch"
	}
	message := err.Error()
	const unknownPrefix = `json: unknown field "`
	if position := strings.Index(message, unknownPrefix); position >= 0 {
		unknown := message[position+len(unknownPrefix):]
		if end := strings.Index(unknown, `"`); end >= 0 {
			unknown = unknown[:end]
		}
		appendField(unknown)
		return failedField, "onboarding_transport_unknown_field"
	}
	if message == "onboarding_transport_trailing_json" || strings.Contains(message, "trailing JSON value") {
		return failedField, "onboarding_transport_trailing_json"
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) || errors.Is(err, io.ErrUnexpectedEOF) {
		return failedField, "onboarding_transport_syntax_invalid"
	}
	return failedField, "onboarding_transport_schema_invalid"
}
