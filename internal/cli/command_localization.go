package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

var localizedFlagNames = []string{
	"agent", "base-url", "clear-issues", "completed", "concurrency",
	"connect-timeout", "credential-env", "database-name", "deep", "dim", "disable", "disabled", "dry-run", "enable", "engine", "entry", "exclude-namespace", "exclude-table", "force",
	"help", "here", "hooks", "issue", "json", "key-env", "local",
	"include-namespace", "include-table", "locale", "max-input", "missing", "model", "namespace", "net", "next", "kind",
	"no-descriptions", "path", "paths", "phase", "preview", "prompt-snapshot",
	"provider", "query-timeout", "quiet", "replace", "repo", "request-file", "snapshot-sha", "source", "source-id", "source-sha256", "stdin",
	"stdin-json", "timeout", "token-accounting", "tool", "version", "object", "captured-at", "prepared-at",
	"snapshot-file", "plan-file", "candidate-file", "mapping-file", "preview-file", "baseline-timestamp",
	"envelope-file", "approval-file", "actor", "transaction",
	"action", "pattern", "pattern-kind", "reason", "created-by", "order", "enabled", "scope-profile", "reviewed-by", "safety-approval-file",
	"out-file",
}

var commandShortMessages = map[string]string{
	"aoci help":                                  "cli.short.help",
	"aoci completion":                            "cli.short.completion",
	"aoci completion bash":                       "cli.short.completion_bash",
	"aoci completion zsh":                        "cli.short.completion_zsh",
	"aoci completion fish":                       "cli.short.completion_fish",
	"aoci completion powershell":                 "cli.short.completion_powershell",
	"aoci init":                                  "cli.short.init",
	"aoci config":                                "cli.short.config",
	"aoci config list":                           "cli.short.config_list",
	"aoci config get":                            "cli.short.config_get",
	"aoci config set":                            "cli.short.config_set",
	"aoci scope":                                 "cli.short.scope",
	"aoci scope show":                            "cli.short.scope_show",
	"aoci scope explain":                         "cli.short.scope_explain",
	"aoci scope rule":                            "cli.short.scope_rule",
	"aoci scope rule list":                       "cli.short.scope_rule_list",
	"aoci scope rule add":                        "cli.short.scope_rule_add",
	"aoci scope rule update":                     "cli.short.scope_rule_update",
	"aoci scope rule remove":                     "cli.short.scope_rule_remove",
	"aoci scope rule reset":                      "cli.short.scope_rule_reset",
	"aoci scope safety":                          "cli.short.scope_safety",
	"aoci scope safety list":                     "cli.short.scope_safety_list",
	"aoci scope safety opt-in":                   "cli.short.scope_safety_opt_in",
	"aoci scope safety remove":                   "cli.short.scope_safety_remove",
	"aoci scope safety approve":                  "cli.short.scope_safety_approve",
	"aoci scope plan":                            "cli.short.scope_plan",
	"aoci scope preview":                         "cli.short.scope_preview",
	"aoci scope approve":                         "cli.short.scope_approve",
	"aoci scope apply":                           "cli.short.scope_apply",
	"aoci scope status":                          "cli.short.scope_status",
	"aoci scope resume":                          "cli.short.scope_resume",
	"aoci scope rollback":                        "cli.short.scope_rollback",
	"aoci scope acknowledge":                     "cli.short.scope_acknowledge",
	"aoci index":                                 "cli.short.index",
	"aoci index update":                          "cli.short.index_update",
	"aoci index header":                          "cli.short.index_header",
	"aoci index header show":                     "cli.short.index_header_show",
	"aoci index header draft":                    "cli.short.index_header_draft",
	"aoci index header diff":                     "cli.short.header_diff",
	"aoci index header apply":                    "cli.short.index_header_apply",
	"aoci index entries":                         "cli.short.index_entries",
	"aoci index entries check":                   "cli.short.index_entries_check",
	"aoci index entries diff":                    "cli.short.entries_diff",
	"aoci index entries apply":                   "cli.short.index_entries_apply",
	"aoci index entries recover":                 "cli.short.index_entries_recover",
	"aoci index build":                           "cli.short.index_build",
	"aoci index agent":                           "cli.short.agent",
	"aoci index agent plan":                      "cli.short.agent_plan",
	"aoci index agent guide":                     "cli.short.agent_guide",
	"aoci index agent stage":                     "cli.short.agent_stage",
	"aoci index agent header":                    "cli.short.agent_header",
	"aoci index agent header stage":              "cli.short.agent_header_stage",
	"aoci index agent curation":                  "cli.short.agent_curation",
	"aoci index agent curation stage":            "cli.short.agent_curation_stage",
	"aoci index agent curation diff":             "cli.short.agent_curation_diff",
	"aoci index agent curation apply":            "cli.short.agent_curation_apply",
	"aoci ai":                                    "cli.short.ai",
	"aoci ai status":                             "cli.short.ai_status",
	"aoci ai setup":                              "cli.short.ai_setup",
	"aoci ai test":                               "cli.short.ai_test",
	"aoci status":                                "cli.short.status",
	"aoci remove-entry":                          "cli.short.remove_entry",
	"aoci mcp":                                   "cli.short.mcp",
	"aoci scan":                                  "cli.short.scan",
	"aoci hook pretool":                          "cli.short.hook",
	"aoci hook":                                  "cli.short.hook_runtime",
	"aoci update-entry":                          "cli.short.update_entry",
	"aoci doctor":                                "cli.short.doctor",
	"aoci database":                              "cli.short.database",
	"aoci database source":                       "cli.short.database_source",
	"aoci database source add":                   "cli.short.database_source_add",
	"aoci database source list":                  "cli.short.database_source_list",
	"aoci database source access":                "cli.short.database_source_access",
	"aoci database source inspect":               "cli.short.database_source_inspect",
	"aoci database snapshot":                     "cli.short.database_snapshot",
	"aoci database inventory":                    "cli.short.database_inventory",
	"aoci database verify":                       "cli.short.database_verify",
	"aoci database baseline":                     "cli.short.database_baseline",
	"aoci database baseline accept":              "cli.short.database_baseline_accept",
	"aoci database evidence":                     "cli.short.database_evidence",
	"aoci database evidence bundle":              "cli.short.database_evidence_bundle",
	"aoci database cognition":                    "cli.short.database_cognition",
	"aoci database cognition status":             "cli.short.database_cognition_status",
	"aoci database cognition bootstrap":          "cli.short.database_cognition_bootstrap",
	"aoci database cognition bootstrap resume":   "cli.short.database_cognition_bootstrap_resume",
	"aoci database cognition bootstrap rollback": "cli.short.database_cognition_bootstrap_rollback",
	"aoci cognition":                             "cli.short.cognition",
	"aoci cognition plan":                        "cli.short.cognition_plan",
	"aoci cognition plan bootstrap":              "cli.short.cognition_plan_bootstrap",
	"aoci cognition plan migration":              "cli.short.cognition_plan_migration",
	"aoci cognition plan validate":               "cli.short.cognition_plan_validate",
	"aoci cognition bootstrap":                   "cli.short.cognition_bootstrap",
	"aoci cognition bootstrap prepare":           "cli.short.cognition_bootstrap_prepare",
	"aoci cognition bootstrap approve":           "cli.short.cognition_bootstrap_approve",
	"aoci cognition bootstrap apply":             "cli.short.cognition_bootstrap_apply",
	"aoci cognition bootstrap status":            "cli.short.cognition_bootstrap_status",
	"aoci cognition bootstrap resume":            "cli.short.cognition_bootstrap_resume",
	"aoci cognition bootstrap rollback":          "cli.short.cognition_bootstrap_rollback",
	"aoci cognition migration":                   "cli.short.cognition_migration",
	"aoci cognition migration snapshot":          "cli.short.cognition_migration_snapshot",
	"aoci cognition migration mapping":           "cli.short.cognition_migration_mapping",
	"aoci cognition migration mapping template":  "cli.short.cognition_migration_mapping_template",
	"aoci cognition migration mapping validate":  "cli.short.cognition_migration_mapping_validate",
	"aoci cognition migration prepare":           "cli.short.cognition_migration_prepare",
	"aoci cognition migration approve":           "cli.short.cognition_migration_approve",
	"aoci cognition migration apply":             "cli.short.cognition_migration_apply",
	"aoci cognition migration status":            "cli.short.cognition_migration_status",
	"aoci cognition migration resume":            "cli.short.cognition_migration_resume",
	"aoci cognition migration rollback":          "cli.short.cognition_migration_rollback",
	"aoci cognition migration reversal":          "cli.short.cognition_migration_reversal",
	"aoci cognition migration reversal prepare":  "cli.short.cognition_migration_reversal_prepare",
	"aoci cognition migration reversal approve":  "cli.short.cognition_migration_reversal_approve",
	"aoci cognition migration reversal apply":    "cli.short.cognition_migration_reversal_apply",
	"aoci cognition migration reversal status":   "cli.short.cognition_migration_reversal_status",
	"aoci cognition migration reversal resume":   "cli.short.cognition_migration_reversal_resume",
	"aoci cognition system":                      "cli.short.cognition_system",
	"aoci cognition system relations":            "cli.short.cognition_system_relations",
	"aoci cognition system lineage":              "cli.short.cognition_system_lineage",
	"aoci cognition system impact":               "cli.short.cognition_system_impact",
	"aoci cognition system snapshot":             "cli.short.cognition_system_snapshot",
	"aoci cognition system evolution":            "cli.short.cognition_system_evolution",
}

var commandLongMessages = map[string]func() string{
	"aoci help": func() string {
		return cliMessage("cli.long.help")
	},
	"aoci completion": func() string {
		return cliMessage("cli.long.completion")
	},
	"aoci completion bash": func() string {
		return cliMessage("cli.long.completion_bash", "aoci")
	},
	"aoci completion zsh": func() string {
		return cliMessage("cli.long.completion_zsh", "aoci")
	},
	"aoci completion fish": func() string {
		return cliMessage("cli.long.completion_fish", "aoci")
	},
	"aoci completion powershell": func() string {
		return cliMessage("cli.long.completion_powershell", "aoci")
	},
	"aoci":                     rootLongHelp,
	"aoci doctor":              doctorLongHelp,
	"aoci index":               indexLongHelp,
	"aoci index update":        indexUpdateLongHelp,
	"aoci verify":              verifyLongHelp,
	"aoci check":               checkLongHelp,
	"aoci scan":                scanLongHelp,
	"aoci remove-entry":        removeEntryLongHelp,
	"aoci ai":                  aiLongHelp,
	"aoci ai setup":            aiSetupLongHelp,
	"aoci ai test":             aiTestLongHelp,
	"aoci index build":         indexBuildLongHelp,
	"aoci index header draft":  headerDraftLongHelp,
	"aoci index score":         indexScoreLongHelp,
	"aoci index inventory":     indexInventoryLongHelp,
	"aoci index agent":         indexAgentLongHelp,
	"aoci index agent header":  indexAgentHeaderLongHelp,
	"aoci update-entry":        updateEntryLongHelp,
	"aoci index entries check": entriesCheckLongHelp,
}

var commandShortRenderers = map[string]func() string{
	"aoci verify":          verifyShortHelp,
	"aoci check":           checkShortHelp,
	"aoci index score":     indexScoreShortHelp,
	"aoci index inventory": indexInventoryShortHelp,
}

// refreshCommandLocalization rebinds package-initialized Cobra metadata to the
// active project locale on every invocation.
func refreshCommandLocalization(root *cobra.Command) {
	if root == nil {
		return
	}
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		path := command.CommandPath()
		if key, exists := commandShortMessages[path]; exists {
			command.Short = cliMessage(key)
		}
		if render, exists := commandShortRenderers[path]; exists {
			command.Short = render()
		}
		if render, exists := commandLongMessages[path]; exists {
			command.Long = render()
		}
		for _, flagName := range localizedFlagNames {
			if flag := command.LocalNonPersistentFlags().Lookup(flagName); flag != nil {
				localized, found, err := textassets.RelocalizeMessageExact(textassets.ActiveLocale(), flag.Usage)
				if err != nil {
					flag.Usage = fmt.Sprintf("[text asset flag localization failed: %v]", err)
				} else if found {
					flag.Usage = localized
				}
			}
			if flag := command.PersistentFlags().Lookup(flagName); flag != nil {
				localized, found, err := textassets.RelocalizeMessageExact(textassets.ActiveLocale(), flag.Usage)
				if err != nil {
					flag.Usage = fmt.Sprintf("[text asset flag localization failed: %v]", err)
				} else if found {
					flag.Usage = localized
				}
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func initializeCobraFlags(root *cobra.Command) {
	if root == nil {
		return
	}
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		command.InitDefaultHelpFlag()
		command.InitDefaultVersionFlag()
		if help := command.Flags().Lookup("help"); help != nil {
			help.Usage = cliMessage("cli.flag.help", command.DisplayName())
		}
		if versionFlag := command.Flags().Lookup("version"); versionFlag != nil {
			versionFlag.Usage = cliMessage("cli.flag.version", command.DisplayName())
		}
		if noDescriptions := command.Flags().Lookup("no-descriptions"); noDescriptions != nil {
			noDescriptions.Usage = cliMessage("cli.flag.no_descriptions")
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

func localizedUsageTemplate() string {
	return cliMessage("cli.help.usage") + `:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

` + cliMessage("cli.help.aliases") + `:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

` + cliMessage("cli.help.examples") + `:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

` + cliMessage("cli.help.available_commands") + `:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

` + cliMessage("cli.help.additional_commands") + `:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

` + cliMessage("cli.help.flags") + `:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

` + cliMessage("cli.help.global_flags") + `:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

` + cliMessage("cli.help.additional_topics") + `:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

` + cliMessage("cli.help.more_info", "{{.CommandPath}}") + `{{end}}
`
}
