package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/databasebootstrap"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/spf13/cobra"
)

func newDatabaseBaselineCmd() *cobra.Command {
	command := &cobra.Command{Use: "baseline", Short: cliMessage("cli.short.database_baseline")}
	command.AddCommand(newDatabaseBaselineAcceptCmd())
	return command
}

func newDatabaseBaselineAcceptCmd() *cobra.Command {
	var sourceID, snapshotSHA string
	command := &cobra.Command{
		Use:   "accept",
		Short: cliMessage("cli.short.database_baseline_accept"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			_, snapshot, exists, err := dbevidence.LoadSnapshot(root, sourceID)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_evidence_invalid", Msg: cliMessage("database.error.evidence_invalid", sourceID)}
			}
			if !exists {
				return &ExitError{Code: ExitConfig, MachineCode: "database_snapshot_missing", Msg: cliMessage("database.error.snapshot_missing", sourceID)}
			}
			if err := dbevidence.AcceptSnapshot(root, snapshot, snapshotSHA); err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_snapshot_binding_mismatch", Msg: cliMessage("database.error.snapshot_binding", sourceID)}
			}
			bootstrapResult, err := autoBootstrapDatabaseCognition(root)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_bootstrap_stopped", Msg: cliMessage("database.error.bootstrap_stopped")}
			}
			result := struct {
				Version              int                       `json:"version"`
				SourceID             string                    `json:"source_id"`
				SourceSnapshotSHA256 string                    `json:"source_snapshot_sha256"`
				AcceptedTables       int                       `json:"accepted_tables"`
				BaselineRef          string                    `json:"baseline_ref"`
				BusinessDataRead     bool                      `json:"business_data_read"`
				DatabaseBootstrap    *databasebootstrap.Result `json:"database_bootstrap,omitempty"`
			}{1, sourceID, snapshot.SourceSnapshotSHA256, len(snapshot.Tables), ".aoci/database-baseline.json", false, bootstrapResult}
			if flagJSON {
				return writeDatabaseJSON(cmd, result)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.baseline.accepted", sourceID, len(snapshot.Tables), snapshot.SourceSnapshotSHA256))
			if bootstrapResult != nil {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.bootstrap.completed"))
			}
			return nil
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	command.Flags().StringVar(&snapshotSHA, "snapshot-sha", "", cliMessage("database.flag.snapshot_sha"))
	_ = command.MarkFlagRequired("source")
	_ = command.MarkFlagRequired("snapshot-sha")
	return command
}

func autoBootstrapDatabaseCognition(root string) (*databasebootstrap.Result, error) {
	cfg, err := config.LoadReadOnly(root)
	if err != nil || cfg.EffectiveAutomationMode() != config.AutomationModeAuto {
		return nil, err
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.Volumes[cognition.ScopeDatabase] != nil {
		return nil, nil
	}
	pending, err := databasebootstrap.Pending(root)
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		if len(pending) != 1 {
			return nil, fmt.Errorf("database_bootstrap_recovery_ambiguous")
		}
		return databasebootstrap.Resume(root, pending[0])
	}
	preview, err := databasebootstrap.Prepare(root, time.Now())
	if errors.Is(err, databasebootstrap.ErrNotReady) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return databasebootstrap.Apply(root, preview)
}

func newDatabaseEvidenceCmd() *cobra.Command {
	command := &cobra.Command{Use: "evidence", Short: cliMessage("cli.short.database_evidence")}
	command.AddCommand(newDatabaseEvidenceBundleCmd())
	return command
}

func newDatabaseEvidenceBundleCmd() *cobra.Command {
	var sourceID, objectRef string
	command := &cobra.Command{
		Use:   "bundle",
		Short: cliMessage("cli.short.database_evidence_bundle"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			_, snapshot, exists, err := dbevidence.LoadSnapshot(root, sourceID)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_evidence_invalid", Msg: cliMessage("database.error.evidence_invalid", sourceID)}
			}
			if !exists {
				return &ExitError{Code: ExitConfig, MachineCode: "database_snapshot_missing", Msg: cliMessage("database.error.snapshot_missing", sourceID)}
			}
			var record *dbevidence.SnapshotTable
			for index := range snapshot.Tables {
				if snapshot.Tables[index].ObjectRef == objectRef {
					record = &snapshot.Tables[index]
					break
				}
			}
			if record == nil {
				return &ExitError{Code: ExitConfig, MachineCode: "database_object_not_found", Msg: cliMessage("database.error.object_not_found", objectRef)}
			}
			table, err := dbevidence.LoadTableEvidence(root, *record)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_evidence_invalid", Msg: cliMessage("database.error.evidence_invalid", sourceID)}
			}
			existingEntry, codeRefs, err := databaseCognitionContext(root, objectRef)
			if err != nil {
				return &ExitError{Code: ExitInvalid, MachineCode: "database_cognition_invalid", Msg: cliMessage("database.error.cognition_invalid")}
			}
			bundle := dbevidence.BuildEvidenceBundle(table, record.TableEvidenceSHA256, nil, codeRefs, existingEntry)
			return writeDatabaseJSON(cmd, bundle)
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	command.Flags().StringVar(&objectRef, "object", "", cliMessage("database.flag.object"))
	_ = command.MarkFlagRequired("source")
	_ = command.MarkFlagRequired("object")
	return command
}

func databaseCognitionContext(root, objectRef string) (*string, []string, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, err
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return nil, nil, err
	}
	var existing *string
	codeRefs := []string{}
	inspect := func(objects []cognition.Object, databaseObjects bool) {
		for _, object := range objects {
			if databaseObjects && object.CanonicalRef == objectRef {
				line := object.CanonicalLine
				existing = &line
			}
			if !databaseObjects && object.Entry != nil && relationContains(object.Entry.R, objectRef) {
				codeRefs = append(codeRefs, object.CanonicalRef)
			}
		}
	}
	if set.LayoutMode == cognition.LayoutLegacyMonolithic {
		inspect(set.Root.Objects, false)
	} else {
		if code := set.Volumes["code"]; code != nil {
			inspect(code.Objects, false)
		}
		if database := set.Volumes["database"]; database != nil {
			inspect(database.Objects, true)
		}
	}
	return existing, codeRefs, nil
}

func relationContains(raw, target string) bool {
	for _, relation := range strings.Split(raw, ",") {
		if strings.TrimSpace(relation) == target {
			return true
		}
	}
	return false
}
