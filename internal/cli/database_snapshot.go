package cli

import (
	"errors"
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/spf13/cobra"
)

type databaseSnapshotResult struct {
	Version              int    `json:"version"`
	SourceID             string `json:"source_id"`
	Engine               string `json:"engine"`
	VisibleTables        int    `json:"visible_tables"`
	SourceSnapshotSHA256 string `json:"source_snapshot_sha256"`
	SnapshotRef          string `json:"snapshot_ref"`
	BusinessDataRead     bool   `json:"business_data_read"`
}

func newDatabaseSnapshotCmd() *cobra.Command {
	var sourceID string
	command := &cobra.Command{
		Use:   "snapshot",
		Short: cliMessage("cli.short.database_snapshot"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, source, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			manifest, snapshot, files, err := dbevidence.NewCollector().Snapshot(cmd.Context(), source)
			if err != nil {
				return databaseSourceExitError(err)
			}
			if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
				return &ExitError{Code: ExitInternal, MachineCode: "database_evidence_write_failed", Msg: cliMessage("database.error.evidence_write_failed", sourceID)}
			}
			result := databaseSnapshotResult{
				Version: 1, SourceID: snapshot.SourceID, Engine: string(snapshot.Engine),
				VisibleTables: len(snapshot.Tables), SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256,
				SnapshotRef: ".aoci/database/evidence/" + snapshot.SourceID + "/snapshot.json", BusinessDataRead: false,
			}
			if flagJSON {
				return writeDatabaseJSON(cmd, result)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.snapshot.summary", result.SourceID, result.VisibleTables, result.SourceSnapshotSHA256))
			return nil
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	_ = command.MarkFlagRequired("source")
	return command
}

func newDatabaseInventoryCmd() *cobra.Command {
	var sourceID string
	command := &cobra.Command{
		Use:   "inventory",
		Short: cliMessage("cli.short.database_inventory"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, source, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			_, snapshot, exists, err := dbevidence.LoadSnapshot(root, sourceID)
			if err != nil {
				return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftEvidenceInvalid), "database_evidence_invalid", ExitInvalid)
			}
			if !exists {
				return &ExitError{Code: ExitConfig, MachineCode: "database_snapshot_missing", Msg: cliMessage("database.error.snapshot_missing", sourceID)}
			}
			baseline, baselinePresent, err := dbevidence.LoadBaseline(root)
			if err != nil {
				return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftEvidenceInvalid), "database_baseline_invalid", ExitInvalid)
			}
			report := dbevidence.CompareSnapshot(snapshot, baseline, baselinePresent)
			return renderDatabaseDrift(cmd, report, false)
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	_ = command.MarkFlagRequired("source")
	return command
}

func newDatabaseVerifyCmd() *cobra.Command {
	var sourceID string
	command := &cobra.Command{
		Use:   "verify",
		Short: cliMessage("cli.short.database_verify"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, source, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			manifest, snapshot, files, err := dbevidence.NewCollector().Snapshot(cmd.Context(), source)
			if err != nil {
				var sourceErr *dbevidence.SourceError
				if errors.As(err, &sourceErr) {
					if sourceErr.Code == "evidence_invalid" {
						return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftEvidenceInvalid), "database_evidence_invalid", ExitInvalid)
					}
					if sourceErr.Code != "configuration_invalid" && sourceErr.Code != "credential_env_missing" && sourceErr.Code != "source_disabled" {
						return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftSourceUnavailable), "database_"+sourceErr.Code, ExitDrift)
					}
				}
				return databaseSourceExitError(err)
			}
			if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
				return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftEvidenceInvalid), "database_evidence_write_failed", ExitInvalid)
			}
			baseline, baselinePresent, err := dbevidence.LoadBaseline(root)
			if err != nil {
				return renderDatabaseSourceFailure(cmd, source, string(dbevidence.DriftEvidenceInvalid), "database_baseline_invalid", ExitInvalid)
			}
			report := dbevidence.CompareSnapshot(snapshot, baseline, baselinePresent)
			return renderDatabaseDrift(cmd, report, true)
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	_ = command.MarkFlagRequired("source")
	return command
}

func renderDatabaseSourceFailure(cmd *cobra.Command, source dbevidence.SourceConfig, status, errorCode string, exitCode int) error {
	report := dbevidence.DriftReport{
		Version: 1, SourceID: source.SourceID, Engine: source.Engine, SourceStatus: status,
		ErrorCode: errorCode, Items: []dbevidence.DriftItem{}, BusinessDataRead: false,
	}
	if flagJSON {
		if err := writeDatabaseJSON(cmd, report); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.inventory.source_failure", source.SourceID, status, errorCode))
	}
	return &ExitError{Code: exitCode}
}

func renderDatabaseDrift(cmd *cobra.Command, report dbevidence.DriftReport, verifiedLive bool) error {
	if flagJSON {
		if err := writeDatabaseJSON(cmd, report); err != nil {
			return err
		}
	} else {
		mode := cliMessage("database.inventory.cached")
		if verifiedLive {
			mode = cliMessage("database.inventory.live")
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.inventory.summary", report.SourceID, mode, report.Summary.Unchanged, report.Summary.New, report.Summary.Changed, report.Summary.Removed))
		if report.SourceIdentityChanged {
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.inventory.source_identity_changed"))
		}
		for _, item := range report.Items {
			if item.Status != dbevidence.DriftUnchanged {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.inventory.row", item.Status, item.ObjectRef))
			}
		}
	}
	if !report.BaselinePresent || report.SourceIdentityChanged || report.Summary.New+report.Summary.Changed+report.Summary.Removed > 0 {
		return &ExitError{Code: ExitDrift}
	}
	return nil
}
