// aoci config —— 团队配置读写命令。
// 索引条目: config.go[CLI.Config.5.T]
//
// set 写回 config.json,必须以 config.LoadBase 取源,防 local 个人端点泄漏。
// automation_mode 是 automation.mode 的 CLI 扁平键:
// legacy/manual 清除字段恢复旧仓兼容态;off/review/auto 显式写团队策略。
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "config",
		Short: cliMessage("cli.short.config"),
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: cliMessage("cli.short.config_list"),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			cfg, err := config.Load(root)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if flagJSON {
				return writePlannerJSON(cmd, cfg)
			}
			out, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return err
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: cliMessage("cli.short.config_get"),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			cfg, err := config.Load(root)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}

			value, ok := configValue(cfg, args[0], flagJSON)
			if !ok {
				return &ExitError{
					Code: ExitConfig,
					Msg: cliMessage(
						"config.unknown_key",
						args[0],
						"exclude_dirs/exclude_root_dirs/exclude_files/curation_exclude/index_path/locale/hook_strict/ledger_enabled/installed_agents/automation_mode/cognition_refresh_threshold/overview_delivery.chunk_tokens/code_cognition_batch_entries/maintain_transport_budget_bytes",
					),
				}
			}
			if flagJSON {
				return writePlannerJSON(cmd, value)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), value)
			return err
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: cliMessage("cli.short.config_set"),
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}

			key, value := args[0], args[1]
			parseBool := func(raw string) (bool, error) {
				switch strings.ToLower(raw) {
				case "true":
					return true, nil
				case "false":
					return false, nil
				default:
					return false, errors.New(cliMessage("config.bad_boolean", raw))
				}
			}

			switch key {
			case "exclude_dirs":
				names, nameErr := directoryNames(value)
				if nameErr != nil {
					return &ExitError{Code: ExitConfig, Msg: nameErr.Error()}
				}
				cfg.ExcludeDirs = names
			case "exclude_root_dirs":
				names, nameErr := directoryNames(value)
				if nameErr != nil {
					return &ExitError{Code: ExitConfig, Msg: nameErr.Error()}
				}
				cfg.ExcludeRootDirs = names
			case "exclude_files":
				cfg.ExcludeFiles = splitCSV(value)
			case "curation_exclude":
				cfg.CurationExclude = splitCSV(value)
			case "index_path":
				cfg.IndexPath = value
			case "locale":
				if !textassets.IsOfficialLocale(value) {
					return &ExitError{
						Code: ExitConfig,
						Msg:  cliMessage("config.unsupported_locale", value),
					}
				}
				if err := prepareLocaleChange(root, cfg, value); err != nil {
					return &ExitError{Code: ExitConfig, Msg: err.Error()}
				}
			case "hook_strict":
				parsed, parseErr := parseBool(value)
				if parseErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Msg:  parseErr.Error(),
					}
				}
				cfg.HookStrict = parsed
			case "ledger_enabled":
				parsed, parseErr := parseBool(value)
				if parseErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Msg:  parseErr.Error(),
					}
				}
				cfg.LedgerEnabled = parsed
			case "automation_mode":
				if setErr := cfg.SetAutomationMode(value); setErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Msg:  setErr.Error(),
					}
				}
			case "cognition_refresh_threshold":
				parsed, parseErr := strconv.Atoi(value)
				if parseErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Msg:  cliMessage("config.bad_integer", value),
					}
				}
				if setErr := cfg.SetCognitionRefreshThreshold(parsed); setErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Msg: cliMessage(
							"config.cognition_refresh_threshold_range",
							machinecontract.CognitionRefreshThresholdMin,
							machinecontract.CognitionRefreshThresholdMax,
						),
					}
				}
			case "overview_delivery.chunk_tokens":
				parsed, parseErr := strconv.Atoi(value)
				if parseErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("config.bad_integer", value)}
				}
				if setErr := cfg.SetOverviewChunkTokens(parsed); setErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage(
						"config.overview_chunk_tokens_range",
						machinecontract.OverviewChunkTokensMin,
						machinecontract.OverviewChunkTokensMax,
					)}
				}
			case "code_cognition_batch_entries":
				parsed, parseErr := strconv.Atoi(value)
				if parseErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("config.bad_integer", value)}
				}
				if setErr := cfg.SetCodeCognitionBatchEntries(parsed); setErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage(
						"config.code_cognition_batch_entries_range",
						machinecontract.CodeCognitionBatchEntriesMin,
						machinecontract.CodeCognitionBatchEntriesMax,
					)}
				}
			case "maintain_transport_budget_bytes":
				parsed, parseErr := strconv.Atoi(value)
				if parseErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("config.bad_integer", value)}
				}
				if setErr := cfg.SetMaintainTransportBudgetBytes(parsed); setErr != nil {
					return &ExitError{Code: ExitConfig, Msg: cliMessage(
						"config.maintain_transport_budget_bytes_range",
						machinecontract.MaintainTransportBudgetBytesMin,
						machinecontract.MaintainTransportBudgetBytesMax,
					)}
				}
			case "installed_agents":
				return &ExitError{
					Code: ExitConfig,
					Msg:  cliMessage("config.installed_agents_managed"),
				}
			default:
				return &ExitError{
					Code: ExitConfig,
					Msg: cliMessage(
						"config.unknown_key",
						key,
						"exclude_dirs/exclude_root_dirs/exclude_files/curation_exclude/index_path/locale/hook_strict/ledger_enabled/automation_mode/cognition_refresh_threshold/overview_delivery.chunk_tokens/code_cognition_batch_entries/maintain_transport_budget_bytes",
					),
				}
			}

			if err := config.Save(root, cfg); err != nil {
				return errors.New(cliMessage("config.write_error", err))
			}
			if key == "locale" {
				if err := ensureManagedAgentsLocale(root, value); err != nil {
					return errors.New(cliMessage("config.agents_locale_error", err))
				}
				// This process can safely switch its remaining user-facing output
				// immediately. Long-running MCP processes retain their startup locale
				// and are covered by the explicit restart notice below.
				if err := textassets.SetActiveLocale(value); err != nil {
					return &ExitError{Code: ExitConfig, Msg: err.Error()}
				}
			}
			if flagJSON {
				// value is exactly what `aoci --json config get <key>` returns
				// right after this set. get reads the effective configuration
				// through config.Load (team layer merged with config.local.json)
				// and projects it with configValue in JSON mode, so the result is
				// reloaded and projected the same way instead of being taken from
				// the team-layer cfg that was just saved: a local override of the
				// key shows here exactly as the next get will show it.
				// The team file is already saved. If the merged view cannot be
				// loaded (a malformed config.local.json), the set still succeeded,
				// so report the saved team value rather than failing after the
				// write; the next get reports the local file's error itself.
				effective, loadErr := config.Load(root)
				if loadErr != nil {
					effective = cfg
				}
				value, ok := configValue(effective, key, true)
				if !ok {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("config.unknown_key", key, "")}
				}
				result := configSetJSONResult{OK: true, Key: key, Value: value}
				if key == "locale" {
					// Human mode prints these two facts as notices below. JSON
					// mode carries them as typed fields with the same numbers,
					// because a running MCP process keeps its startup locale and a
					// pending migration receipt was just written.
					result.RestartMCPRequired = true
					if cfg.LocaleMigration != nil {
						result.LocaleMigration = &configSetLocaleMigrationJSON{
							HeaderPending: cfg.LocaleMigration.HeaderPending,
							EntryPaths:    len(cfg.LocaleMigration.EntryPaths),
							CurationPaths: len(cfg.LocaleMigration.CurationPaths),
						}
					}
				}
				return writePlannerJSON(cmd, result)
			}
			if !flagQuiet {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cliMessage("config.saved", key))
				if key == "locale" {
					if cfg.LocaleMigration != nil {
						_, _ = fmt.Fprintln(cmd.OutOrStdout(), cliMessage(
							"config.locale_migration_pending",
							cfg.LocaleMigration.HeaderPending,
							len(cfg.LocaleMigration.EntryPaths),
							len(cfg.LocaleMigration.CurationPaths),
						))
					}
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), cliMessage("config.restart_mcp"))
				}
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd, getCmd, setCmd)
	registerCommand(cmd)
}

// configSetJSONResult is the `aoci --json config set` result. Value is the
// value the next `aoci --json config get <key>` returns. The two locale fields
// are set only when the key is locale and are omitted otherwise.
type configSetJSONResult struct {
	OK                 bool                          `json:"ok"`
	Key                string                        `json:"key"`
	Value              any                           `json:"value"`
	RestartMCPRequired bool                          `json:"restart_mcp_required,omitempty"`
	LocaleMigration    *configSetLocaleMigrationJSON `json:"locale_migration,omitempty"`
}

// configSetLocaleMigrationJSON summarizes the pending locale-migration receipt
// with the same three numbers the human config.locale_migration_pending line
// prints: Header pending, and the Entry and Curation path counts.
type configSetLocaleMigrationJSON struct {
	HeaderPending bool `json:"header_pending"`
	EntryPaths    int  `json:"entry_paths"`
	CurationPaths int  `json:"curation_paths"`
}

func configValue(cfg *config.Config, key string, jsonMode bool) (any, bool) {
	switch key {
	case "exclude_dirs":
		if jsonMode {
			return cfg.ExcludeDirs, true
		}
		return strings.Join(cfg.ExcludeDirs, ","), true
	case "exclude_root_dirs":
		if jsonMode {
			return append([]string{}, cfg.ExcludeRootDirs...), true
		}
		return strings.Join(cfg.ExcludeRootDirs, ","), true
	case "exclude_files":
		if jsonMode {
			return cfg.ExcludeFiles, true
		}
		return strings.Join(cfg.ExcludeFiles, ","), true
	case "curation_exclude":
		if jsonMode {
			return cfg.CurationExclude, true
		}
		return strings.Join(cfg.CurationExclude, ","), true
	case "index_path":
		return cfg.IndexPath, true
	case "locale":
		return cfg.Locale, true
	case "hook_strict":
		return cfg.HookStrict, true
	case "ledger_enabled":
		return cfg.LedgerEnabled, true
	case "installed_agents":
		if jsonMode {
			return cfg.InstalledAgents, true
		}
		return strings.Join(cfg.InstalledAgents, ","), true
	case "automation_mode":
		return cfg.EffectiveAutomationMode(), true
	case "cognition_refresh_threshold":
		return cfg.CognitionRefreshThreshold, true
	case "overview_delivery.chunk_tokens":
		return cfg.OverviewDelivery.ChunkTokens, true
	case "code_cognition_batch_entries":
		return cfg.CodeCognitionBatchLimit(), true
	case "maintain_transport_budget_bytes":
		return cfg.MaintainTransportBudget(), true
	default:
		return nil, false
	}
}

// directoryNames parses the value of exclude_dirs or exclude_root_dirs. Both
// lists match one path component, so the gitignore spelling `build/` is the
// name without its slash, a duplicate is one entry, and a path such as
// `app/build` can never match and is refused instead of saved as a dead entry.
func directoryNames(raw string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, part := range splitCSV(raw) {
		name := strings.TrimRight(part, `/\`)
		if name == "" || strings.ContainsAny(name, `/\`) {
			return nil, errors.New(cliMessage("config.bad_directory_name", part))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func splitCSV(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
