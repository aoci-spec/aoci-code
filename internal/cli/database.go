package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/spf13/cobra"
)

func init() {
	registerCommand(newDatabaseCmd())
}

func newDatabaseCmd() *cobra.Command {
	command := &cobra.Command{Use: "database", Short: cliMessage("cli.short.database")}
	command.AddCommand(newDatabaseSourceCmd(), newDatabaseSnapshotCmd(), newDatabaseInventoryCmd(),
		newDatabaseVerifyCmd(), newDatabaseBaselineCmd(), newDatabaseEvidenceCmd(), newDatabaseCognitionCmd())
	return command
}

func newDatabaseSourceCmd() *cobra.Command {
	command := &cobra.Command{Use: "source", Short: cliMessage("cli.short.database_source")}
	command.AddCommand(newDatabaseSourceAddCmd(), newDatabaseSourceListCmd(), newDatabaseSourceAccessCmd(), newDatabaseSourceInspectCmd())
	return command
}

func newDatabaseSourceAddCmd() *cobra.Command {
	var sourceID, engine, database, credentialEnv string
	var namespaces, includeNamespaces, excludeNamespaces, includeTables, excludeTables []string
	var connectTimeout, queryTimeout int
	var disabled, replace bool
	command := &cobra.Command{
		Use:   "add",
		Short: cliMessage("cli.short.database_source_add"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			defer func() {
				sourceID, engine, database, credentialEnv = "", "", "", ""
				namespaces, includeNamespaces, excludeNamespaces, includeTables, excludeTables = nil, nil, nil, nil, nil
				connectTimeout, queryTimeout = 10, 30
				disabled, replace = false, false
			}()
			root, err := resolveRepoRoot()
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if credentialEnv == "" {
				credentialEnv = dbevidence.DefaultCredentialEnv(sourceID)
			}
			source := dbevidence.SourceConfig{
				SourceID: sourceID, Engine: dbevidence.Engine(engine), Database: database,
				Namespaces: namespaces, IncludeNamespaces: includeNamespaces, ExcludeNamespaces: excludeNamespaces,
				IncludeTables: includeTables, ExcludeTables: excludeTables, CredentialEnv: credentialEnv,
				ConnectTimeoutSeconds: connectTimeout, QueryTimeoutSeconds: queryTimeout, Enabled: !disabled,
			}
			if err := dbevidence.NormalizeSource(&source); err != nil {
				return &ExitError{Code: ExitConfig, MachineCode: "database_configuration_invalid", Msg: cliMessage("database.error.configuration_invalid")}
			}
			found := false
			for index := range cfg.DatabaseSources {
				if cfg.DatabaseSources[index].SourceID != source.SourceID {
					continue
				}
				if !replace {
					return &ExitError{Code: ExitConfig, MachineCode: "database_source_exists", Msg: cliMessage("database.error.source_exists", source.SourceID)}
				}
				cfg.DatabaseSources[index] = source
				found = true
				break
			}
			if !found {
				cfg.DatabaseSources = append(cfg.DatabaseSources, source)
			}
			if err := config.Save(root, cfg); err != nil {
				return &ExitError{Code: ExitConfig, MachineCode: "database_configuration_write_failed", Msg: cliMessage("database.error.configuration_write_failed", source.SourceID)}
			}
			if flagJSON {
				return writeDatabaseJSON(cmd, struct {
					Version         int                     `json:"version"`
					Source          dbevidence.SourceConfig `json:"source"`
					CredentialSaved bool                    `json:"credential_saved"`
					NetworkAccessed bool                    `json:"network_accessed"`
				}{1, source, false, false})
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.source.saved", source.SourceID, source.CredentialEnv))
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&sourceID, "source-id", "", cliMessage("database.flag.source_id"))
	flags.StringVar(&engine, "engine", "", cliMessage("database.flag.engine"))
	flags.StringVar(&database, "database-name", "", cliMessage("database.flag.database"))
	flags.StringSliceVar(&namespaces, "namespace", nil, cliMessage("database.flag.namespace"))
	flags.StringSliceVar(&includeNamespaces, "include-namespace", nil, cliMessage("database.flag.include_namespace"))
	flags.StringSliceVar(&excludeNamespaces, "exclude-namespace", nil, cliMessage("database.flag.exclude_namespace"))
	flags.StringSliceVar(&includeTables, "include-table", nil, cliMessage("database.flag.include_table"))
	flags.StringSliceVar(&excludeTables, "exclude-table", nil, cliMessage("database.flag.exclude_table"))
	flags.StringVar(&credentialEnv, "credential-env", "", cliMessage("database.flag.credential_env"))
	flags.IntVar(&connectTimeout, "connect-timeout", 10, cliMessage("database.flag.connect_timeout"))
	flags.IntVar(&queryTimeout, "query-timeout", 30, cliMessage("database.flag.query_timeout"))
	flags.BoolVar(&disabled, "disabled", false, cliMessage("database.flag.disabled"))
	flags.BoolVar(&replace, "replace", false, cliMessage("database.flag.replace"))
	_ = command.MarkFlagRequired("source-id")
	_ = command.MarkFlagRequired("engine")
	_ = command.MarkFlagRequired("database-name")
	return command
}

func newDatabaseSourceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: cliMessage("cli.short.database_source_list"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := loadDatabaseConfig()
			if err != nil {
				return err
			}
			sources := append([]dbevidence.SourceConfig{}, cfg.DatabaseSources...)
			sort.Slice(sources, func(i, j int) bool { return sources[i].SourceID < sources[j].SourceID })
			report := struct {
				Version         int                       `json:"version"`
				Sources         []dbevidence.SourceConfig `json:"sources"`
				CredentialSaved bool                      `json:"credential_saved"`
				NetworkAccessed bool                      `json:"network_accessed"`
			}{1, sources, false, false}
			if flagJSON {
				return writeDatabaseJSON(cmd, report)
			}
			if len(sources) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.source.none"))
				return nil
			}
			for _, source := range sources {
				fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.source.row", source.SourceID, source.Engine, source.Database, source.CredentialEnv, source.Enabled))
			}
			return nil
		},
	}
}

func newDatabaseSourceInspectCmd() *cobra.Command {
	var sourceID string
	command := &cobra.Command{
		Use:   "inspect",
		Short: cliMessage("cli.short.database_source_inspect"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, source, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			inspection, err := dbevidence.NewCollector().Inspect(cmd.Context(), source)
			if err != nil {
				return databaseSourceExitError(err)
			}
			if flagJSON {
				return writeDatabaseJSON(cmd, inspection)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.inspect.summary", inspection.SourceID, inspection.Engine, inspection.VisibleTables, len(inspection.VisibleNamespaces)))
			return nil
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	_ = command.MarkFlagRequired("source")
	return command
}

func newDatabaseSourceAccessCmd() *cobra.Command {
	var sourceID string
	command := &cobra.Command{
		Use:   "access",
		Short: cliMessage("cli.short.database_source_access"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, source, err := configuredDatabaseSource(sourceID)
			if err != nil {
				return err
			}
			plan, err := dbevidence.InspectAccess(cmd.Context(), source, nil)
			if err != nil {
				return databaseSourceExitError(err)
			}
			if flagJSON {
				return writeDatabaseJSON(cmd, plan)
			}
			fmt.Fprintln(cmd.OutOrStdout(), cliMessage("database.access.summary", plan.SourceID, plan.Status, plan.Provider, plan.Reference, plan.NextAction))
			return nil
		},
	}
	command.Flags().StringVar(&sourceID, "source", "", cliMessage("database.flag.source"))
	_ = command.MarkFlagRequired("source")
	return command
}

func loadDatabaseConfig() (string, *config.Config, error) {
	root, err := resolveRepoRoot()
	if err != nil {
		return "", nil, &ExitError{Code: ExitConfig, Msg: err.Error()}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return "", nil, &ExitError{Code: ExitConfig, Msg: err.Error()}
	}
	return root, cfg, nil
}

func configuredDatabaseSource(sourceID string) (string, dbevidence.SourceConfig, error) {
	root, cfg, err := loadDatabaseConfig()
	if err != nil {
		return "", dbevidence.SourceConfig{}, err
	}
	source, exists := dbevidence.FindSource(cfg.DatabaseSources, sourceID)
	if !exists {
		return "", dbevidence.SourceConfig{}, &ExitError{Code: ExitConfig, MachineCode: "database_source_not_found", Msg: cliMessage("database.error.source_not_found")}
	}
	return root, source, nil
}

func databaseSourceExitError(err error) error {
	var sourceErr *dbevidence.SourceError
	if !errors.As(err, &sourceErr) {
		return &ExitError{Code: ExitInternal, MachineCode: "database_internal", Msg: cliMessage("database.error.internal")}
	}
	code := ExitInternal
	if sourceErr.Code == "configuration_invalid" || sourceErr.Code == "credential_env_missing" || sourceErr.Code == "source_disabled" {
		code = ExitConfig
	}
	if sourceErr.Code == "credential_env_missing" {
		return &ExitError{Code: code, MachineCode: "database_" + sourceErr.Code,
			Msg: cliMessage("database.error.credential_env_missing", sourceErr.SourceID)}
	}
	return &ExitError{Code: code, MachineCode: "database_" + sourceErr.Code, Msg: cliMessage("database.error.source", sourceErr.SourceID, sourceErr.Code)}
}

func writeDatabaseJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
