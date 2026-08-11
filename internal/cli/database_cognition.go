package cli

import (
	"fmt"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/databasebootstrap"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

func newDatabaseCognitionCmd() *cobra.Command {
	command := &cobra.Command{Use: "cognition", Short: cliMessage("cli.short.database_cognition")}
	command.AddCommand(newDatabaseCognitionStatusCmd(), newDatabaseCognitionBootstrapCmd())
	return command
}

func newDatabaseCognitionBootstrapCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "bootstrap",
		Short: cliMessage("cli.short.database_cognition_bootstrap"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := loadDatabaseConfig()
			if err != nil {
				return err
			}
			pending, err := databasebootstrap.Pending(root)
			if err != nil {
				return databaseBootstrapExitError(err)
			}
			var result *databasebootstrap.Result
			if len(pending) == 1 {
				result, err = databasebootstrap.Resume(root, pending[0])
			} else if len(pending) > 1 {
				err = fmt.Errorf("database_bootstrap_recovery_ambiguous")
			} else {
				var preview *databasebootstrap.Preview
				preview, err = databasebootstrap.Prepare(root, time.Now())
				if err == nil {
					result, err = databasebootstrap.Apply(root, preview)
				}
			}
			if err != nil {
				return databaseBootstrapExitError(err)
			}
			return writeDatabaseBootstrapResult(cmd, result)
		},
	}
	command.AddCommand(newDatabaseCognitionBootstrapResumeCmd(), newDatabaseCognitionBootstrapRollbackCmd())
	return command
}

func newDatabaseCognitionBootstrapResumeCmd() *cobra.Command {
	var transactionID string
	command := &cobra.Command{Use: "resume", Short: cliMessage("cli.short.database_cognition_bootstrap_resume"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, _, err := loadDatabaseConfig()
		if err != nil {
			return err
		}
		result, err := databasebootstrap.Resume(root, transactionID)
		if err != nil {
			return databaseBootstrapExitError(err)
		}
		return writeDatabaseBootstrapResult(cmd, result)
	}}
	command.Flags().StringVar(&transactionID, "transaction", "", cliMessage("database.flag.transaction"))
	return command
}

func newDatabaseCognitionBootstrapRollbackCmd() *cobra.Command {
	var transactionID string
	command := &cobra.Command{Use: "rollback", Short: cliMessage("cli.short.database_cognition_bootstrap_rollback"), RunE: func(cmd *cobra.Command, _ []string) error {
		root, _, err := loadDatabaseConfig()
		if err != nil {
			return err
		}
		result, err := databasebootstrap.Rollback(root, transactionID)
		if err != nil {
			return databaseBootstrapExitError(err)
		}
		return writeDatabaseBootstrapResult(cmd, result)
	}}
	command.Flags().StringVar(&transactionID, "transaction", "", cliMessage("database.flag.transaction"))
	return command
}

func writeDatabaseBootstrapResult(cmd *cobra.Command, result *databasebootstrap.Result) error {
	if flagJSON {
		return writeDatabaseJSON(cmd, result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.bootstrap.result", result.Status, result.DatabaseReady, result.DatabaseEntryCount, result.NextAction))
	return nil
}

func databaseBootstrapExitError(err error) error {
	diagnostic := databasebootstrap.Diagnose(err)
	return &ExitError{
		Code: ExitInvalid, MachineCode: "database_bootstrap_stopped",
		Msg:     cliMessage("database.error.bootstrap_stopped", diagnostic.CauseCode, diagnostic.SafeNextAction),
		Details: diagnostic,
	}
}

func newDatabaseCognitionStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: cliMessage("cli.short.database_cognition_status"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, cfg, err := loadDatabaseConfig()
			if err != nil {
				return err
			}
			set, err := cognition.Load(root, cfg.IndexPath)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_cognition_invalid", Msg: cliMessage("database.error.cognition_invalid")}
			}
			state, exists, err := baseline.Load(root)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_cognition_baseline_invalid", Msg: cliMessage("database.error.cognition_baseline_invalid")}
			}
			if !exists || state == nil {
				state = baseline.NewBaseline(nil)
			}
			report := dbcognition.Assess(root, cfg.DatabaseSources, set, state)
			if flagJSON {
				return writeDatabaseJSON(cmd, report)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.cognition.summary",
				report.DatabaseVolumeState, report.ConfiguredSources, report.BlockingSourceCount, report.EvidenceTableCount, report.Summary.Current,
				report.Summary.Missing, report.Summary.Stale, report.Summary.Unbaselined,
				report.Summary.Orphan, report.Summary.EvidenceUnavailable, report.Summary.EvidenceInvalid,
				report.Summary.SourceDisabled, report.NextAction))
			for _, source := range report.Sources {
				if source.State == machinecontract.DatabaseCognitionCurrent {
					continue
				}
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.cognition.source",
					source.SourceID, source.State, source.ErrorCode))
			}
			return nil
		},
	}
}
