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
			out, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(out))
			return nil
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

			switch args[0] {
			case "exclude_dirs":
				fmt.Println(strings.Join(cfg.ExcludeDirs, ","))
			case "exclude_files":
				fmt.Println(strings.Join(cfg.ExcludeFiles, ","))
			case "curation_exclude":
				fmt.Println(strings.Join(cfg.CurationExclude, ","))
			case "index_path":
				fmt.Println(cfg.IndexPath)
			case "locale":
				fmt.Println(cfg.Locale)
			case "hook_strict":
				fmt.Println(cfg.HookStrict)
			case "ledger_enabled":
				fmt.Println(cfg.LedgerEnabled)
			case "installed_agents":
				fmt.Println(strings.Join(cfg.InstalledAgents, ","))
			case "automation_mode":
				fmt.Println(cfg.EffectiveAutomationMode())
			case "cognition_refresh_threshold":
				fmt.Println(cfg.CognitionRefreshThreshold)
			case "overview_delivery.chunk_tokens":
				fmt.Println(cfg.OverviewDelivery.ChunkTokens)
			default:
				return &ExitError{
					Code: ExitConfig,
					Msg: cliMessage(
						"config.unknown_key",
						args[0],
						"exclude_dirs/exclude_files/curation_exclude/index_path/locale/hook_strict/ledger_enabled/installed_agents/automation_mode/cognition_refresh_threshold/overview_delivery.chunk_tokens",
					),
				}
			}
			return nil
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
				cfg.ExcludeDirs = splitCSV(value)
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
						"exclude_dirs/exclude_files/curation_exclude/index_path/locale/hook_strict/ledger_enabled/automation_mode/cognition_refresh_threshold/overview_delivery.chunk_tokens",
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
			if !flagQuiet {
				fmt.Println(cliMessage("config.saved", key))
				if key == "locale" {
					if cfg.LocaleMigration != nil {
						fmt.Println(cliMessage(
							"config.locale_migration_pending",
							cfg.LocaleMigration.HeaderPending,
							len(cfg.LocaleMigration.EntryPaths),
							len(cfg.LocaleMigration.CurationPaths),
						))
					}
					fmt.Println(cliMessage("config.restart_mcp"))
				}
			}
			return nil
		},
	}

	cmd.AddCommand(listCmd, getCmd, setCmd)
	registerCommand(cmd)
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
