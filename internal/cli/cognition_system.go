package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
	"github.com/spf13/cobra"
)

func newCognitionSystemCmd() *cobra.Command {
	command := &cobra.Command{Use: "system", Short: cliMessage("cli.short.cognition_system")}
	command.AddCommand(newCognitionSystemRelationsCmd(), newCognitionSystemLineageCmd(),
		newCognitionSystemImpactCmd(), newCognitionSystemSnapshotCmd(), newCognitionSystemEvolutionCmd())
	return command
}

func loadSystemProjection() (*cognition.SystemProjection, error) {
	root, err := resolveRepoRoot()
	if err != nil {
		return nil, &ExitError{Code: ExitConfig, Msg: err.Error()}
	}
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		return nil, &ExitError{Code: ExitConfig, MachineCode: "cognition_system_configuration_invalid", Msg: cliMessage("cognition.system.error")}
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		return nil, &ExitError{Code: ExitInvalid, MachineCode: "cognition_system_layout_invalid", Msg: cliMessage("cognition.system.error")}
	}
	state, exists, err := baseline.Load(root)
	if err != nil {
		return nil, &ExitError{Code: ExitInvalid, MachineCode: "cognition_system_baseline_invalid", Msg: cliMessage("cognition.system.error")}
	}
	if !exists {
		state = nil
	}
	projection, err := cognition.BuildSystemProjection(set, state)
	if err != nil {
		return nil, &ExitError{Code: ExitInvalid, MachineCode: "cognition_system_projection_invalid", Msg: cliMessage("cognition.system.error")}
	}
	return projection, nil
}

func newCognitionSystemRelationsCmd() *cobra.Command {
	return &cobra.Command{Use: "relations", Short: cliMessage("cli.short.cognition_system_relations"), RunE: func(cmd *cobra.Command, _ []string) error {
		projection, err := loadSystemProjection()
		if err != nil {
			return err
		}
		if flagJSON {
			return writeDatabaseJSON(cmd, projection)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.system.relations.summary", len(projection.Relations), len(projection.Findings), projection.ProjectionIdentity))
		return nil
	}}
}

func newCognitionSystemLineageCmd() *cobra.Command {
	return &cobra.Command{Use: "lineage", Short: cliMessage("cli.short.cognition_system_lineage"), RunE: func(cmd *cobra.Command, _ []string) error {
		projection, err := loadSystemProjection()
		if err != nil {
			return err
		}
		report := struct {
			Version            string                    `json:"version"`
			ProjectionIdentity string                    `json:"projection_identity"`
			Lineage            []cognition.LineageRecord `json:"lineage"`
			Derived            bool                      `json:"derived"`
			NetworkAccessed    bool                      `json:"network_accessed"`
		}{cognition.CognitionLineageV1, projection.ProjectionIdentity, projection.Lineage, true, false}
		if flagJSON {
			return writeDatabaseJSON(cmd, report)
		}
		current := 0
		for _, record := range projection.Lineage {
			if record.Status == "current" {
				current++
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.system.lineage.summary", len(projection.Lineage), current, projection.ProjectionIdentity))
		return nil
	}}
}

func newCognitionSystemImpactCmd() *cobra.Command {
	var objectRef string
	command := &cobra.Command{Use: "impact", Short: cliMessage("cli.short.cognition_system_impact"), RunE: func(cmd *cobra.Command, _ []string) error {
		defer func() { objectRef = "" }()
		projection, err := loadSystemProjection()
		if err != nil {
			return err
		}
		impact, err := cognition.ResolveDatabaseImpact(projection, objectRef)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "database_impact_invalid", Msg: cliMessage("cognition.system.error")}
		}
		if flagJSON {
			return writeDatabaseJSON(cmd, impact)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.system.impact.summary", impact.DatabaseObjectRef, len(impact.AffectedCodeObjects), impact.Complete))
		return nil
	}}
	command.Flags().StringVar(&objectRef, "object", "", cliMessage("database.flag.object"))
	_ = command.MarkFlagRequired("object")
	return command
}

func newCognitionSystemSnapshotCmd() *cobra.Command {
	return &cobra.Command{Use: "snapshot", Short: cliMessage("cli.short.cognition_system_snapshot"), RunE: func(cmd *cobra.Command, _ []string) error {
		projection, err := loadSystemProjection()
		if err != nil {
			return err
		}
		snapshot, err := cognition.SnapshotSystemCognition(projection)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "cognition_snapshot_invalid", Msg: cliMessage("cognition.system.error")}
		}
		if flagJSON {
			return writeDatabaseJSON(cmd, snapshot)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.system.snapshot.summary", len(snapshot.Objects), snapshot.ProjectionIdentity))
		return nil
	}}
}

func newCognitionSystemEvolutionCmd() *cobra.Command {
	var snapshotFile string
	command := &cobra.Command{Use: "evolution", Short: cliMessage("cli.short.cognition_system_evolution"), RunE: func(cmd *cobra.Command, _ []string) error {
		defer func() { snapshotFile = "" }()
		previous, err := readCognitionSnapshot(snapshotFile)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "cognition_evolution_snapshot_invalid", Msg: cliMessage("cognition.system.error")}
		}
		projection, err := loadSystemProjection()
		if err != nil {
			return err
		}
		current, err := cognition.SnapshotSystemCognition(projection)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "cognition_snapshot_invalid", Msg: cliMessage("cognition.system.error")}
		}
		evolution, err := cognition.CompareCognitionSnapshots(previous, current)
		if err != nil {
			return &ExitError{Code: ExitInvalid, MachineCode: "cognition_evolution_snapshot_invalid", Msg: cliMessage("cognition.system.error")}
		}
		if flagJSON {
			return writeDatabaseJSON(cmd, evolution)
		}
		fmt.Fprintln(cmd.OutOrStdout(), cliMessage("cognition.system.evolution.summary", evolution.Summary.Created,
			evolution.Summary.Removed, evolution.Summary.SemanticChanged, evolution.Summary.LineageChanged,
			evolution.Summary.Unchanged, evolution.SystemChanged))
		return nil
	}}
	command.Flags().StringVar(&snapshotFile, "snapshot-file", "", cliMessage("cognition.system.flag.snapshot_file"))
	_ = command.MarkFlagRequired("snapshot-file")
	return command
}

func readCognitionSnapshot(path string) (*cognition.CognitionSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := jsonstrict.RejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot cognition.CognitionSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("cognition_evolution_snapshot_trailing_json")
	}
	return &snapshot, nil
}
