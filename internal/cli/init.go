// aoci init —— 初始化 .aoci、运行时Git边界、索引骨架、AGENTS.md与可选Agent接入。
//
// automation兼容规则:
//   - config.json原先不存在: 新仓首次初始化，显式写automation.mode=auto;
//   - config.json已存在但缺automation: 旧仓legacy，不在幂等init中静默改变;
//   - init本身纯确定性，设置auto不代表本次调用模型。
//
// .aoci Git边界:
//   - 首次初始化生成.aoci/.gitignore;
//   - 未知.aoci资产默认忽略;
//   - Baseline、团队配置和Curation正式资产白名单放行;
//   - 已有维护者文件绝不覆盖。
//
// 完整索引统一引导到agent guide。init输出必须准确解释当前团队模式:
// auto连续执行到Apply；review/legacy在Apply前停；off只观察。
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

var beforeInitBaselineAdvance = func() {}

func fingerprintInitPath(root, relativePath string) (baseline.Fingerprint, bool) {
	fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	return fingerprint, err == nil
}

func initAgentCandidatePaths(agent string) []string {
	switch agent {
	case "claude":
		return []string{".mcp.json", ".claude/settings.json"}
	case "codex":
		return []string{".codex/config.toml"}
	case "opencode":
		return []string{"opencode.json"}
	default:
		return nil
	}
}

// advanceInitBaseline把init产生的确定性文件变化与其他Baseline写入口串行化。
func advanceInitBaseline(
	root string,
	expectedPostimages map[string]baseline.Fingerprint,
) string {
	lock, err := afs.AcquireIndexLock(root)
	if err != nil {
		return cliMessage("init.lock_warning", err.Error())
	}
	defer lock.Release()

	currentBaseline, exists, loadErr := baseline.Load(root)
	if loadErr != nil {
		return cliMessage("init.baseline_load_warning", loadErr.Error())
	}
	if !exists || currentBaseline == nil {
		return ""
	}

	advanced := []string{}
	conflicted := []string{}
	for relativePath, expected := range expectedPostimages {
		fingerprint, ok := fingerprintInitPath(root, relativePath)
		if !ok || fingerprint.SHA256 != expected.SHA256 {
			conflicted = append(conflicted, relativePath)
			continue
		}
		baseline.UpdateOne(currentBaseline, relativePath, expected)
		advanced = append(advanced, relativePath)
	}
	if len(advanced) == 0 {
		if len(conflicted) > 0 {
			return cliMessage("init.baseline_concurrent_warning", strings.Join(conflicted, ", "))
		}
		return ""
	}
	if saveErr := baseline.SaveUnderIndexLock(root, currentBaseline); saveErr != nil {
		return cliMessage("init.baseline_save_warning", saveErr.Error())
	}
	note := cliMessage("init.baseline_advanced", strings.Join(advanced, ", "))
	if len(conflicted) > 0 {
		note += cliMessage("init.baseline_concurrent_suffix", strings.Join(conflicted, ", "))
	}
	return note
}

func initAgentGuideCommand(
	agent string,
) string {
	switch agent {
	case "claude", "codex", "cursor", "opencode":
		return "aoci index agent guide --agent " +
			agent +
			" --json"
	default:
		return "aoci index agent guide --agent " +
			"<codex|claude|cursor|opencode> --json"
	}
}

func init() {
	var agent string
	var withHooks bool
	var here bool
	var locale string
	var scopeProfile string

	command := &cobra.Command{
		Use:   "init",
		Short: cliMessage("cli.short.init"),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			if scopeProfile == "" {
				scopeProfile = "production"
			}
			if scopeProfile != "production" && scopeProfile != "full" && scopeProfile != "custom" {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("init.bad_scope_profile", scopeProfile)}
			}
			if locale != "" && !textassets.IsOfficialLocale(locale) {
				return &ExitError{
					Code: ExitConfig,
					Msg: fmt.Sprintf(
						"unsupported locale %q (available: en-US, zh-CN)",
						locale,
					),
				}
			}
			switch agent {
			case "", "claude", "codex", "cursor", "opencode", "all":
			default:
				return &ExitError{
					Code: ExitConfig,
					Msg:  cliMessage("init.bad_agent", agent),
				}
			}

			root, err := config.FindRepoRoot(
				".",
				flagRepo,
			)
			if err != nil {
				if !here {
					return &ExitError{
						Code: ExitConfig,
						Msg:  cliMessage("init.repo_not_found"),
					}
				}

				root, err = filepath.Abs(".")
				if err != nil {
					return err
				}
			}
			// OpenCode has two incompatible configuration shapes in active use.
			// Validate the project file before init creates any repository asset,
			// so JSONC/V2/conflicts fail closed with a zero-write repository tree.
			if agent == "opencode" {
				if preflightErr := hooks.ValidateOpenCodeMCPInstall(root); preflightErr != nil {
					return &ExitError{Code: ExitConfig, Msg: preflightErr.Error()}
				}
			}

			outputLines := []string{}
			configExisted := true
			newRepositoryAutomationMessage := ""
			var scopeProposal *managedscope.Proposal

			if _, statErr := os.Stat(
				config.FilePath(root),
			); statErr != nil {
				if os.IsNotExist(
					statErr,
				) {
					configExisted = false
				} else {
					return errors.New(cliMessage("init.config_stat_error", statErr))
				}
			}

			cfg, err := config.LoadBase(
				root,
			)
			if err != nil {
				return &ExitError{
					Code: ExitConfig,
					Msg:  err.Error(),
				}
			}
			volumeLayout, layoutErr := activeVolumeLayout(root, cfg.IndexPath)
			if layoutErr != nil {
				return &ExitError{Code: ExitConfig, Msg: layoutErr.Error()}
			}
			if !volumeLayout {
				if err := requireLegacyWriteLayout(root, cfg, true); err != nil {
					return &ExitError{Code: ExitConfig, Msg: err.Error()}
				}
			}
			if locale != "" {
				if err := prepareLocaleChange(root, cfg, locale); err != nil {
					return &ExitError{Code: ExitConfig, Msg: err.Error()}
				}
			}

			if !configExisted {
				if setErr := cfg.SetAutomationMode(
					config.AutomationModeAuto,
				); setErr != nil {
					return &ExitError{
						Code: ExitConfig,
						Err:  setErr,
					}
				}
				if governanceErr := cfg.SetNewProjectGovernance(scopeProfile); governanceErr != nil {
					return &ExitError{Code: ExitConfig, Err: governanceErr}
				}
				evaluation, evaluationErr := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
					WalkOptions: cfg.WalkOptions(), CurationExclude: cfg.CurationExclude})
				if evaluationErr != nil {
					return &ExitError{Code: ExitConfig, Err: evaluationErr}
				}
				proposal := managedscope.BuildProposal(evaluation, scopeProfile, len(cfg.SafeInventoryHighRiskOptIn))
				scopeProposal = &proposal
				if proposal.RequiresHumanApproval {
					return &ExitError{Code: ExitConfig, Err: fmt.Errorf(
						"managed_scope_auto_authorization_blocked: initial_scope_requires_machine_decision")}
				}

				newRepositoryAutomationMessage = cliMessage("init.automation_default")
			}

			paths := config.AOCIPaths(
				root,
				cfg.IndexPath,
			)

			var nextStepMessage string
			var fullIndexMessage string
			var headerDictionaryMessage string
			var minimalIndexContent string
			var volumeAssets initialVolumeAssets
			if !flagQuiet {
				nextStepMessage, err = initNextStepMessage()
				if err != nil {
					return err
				}
				fullIndexMessage, err = initFullIndexMessage(
					agent,
					cfg.EffectiveAutomationMode(),
				)
				if err != nil {
					return err
				}
			}
			if _, err = textassets.Load(
				textassets.ActiveLocale(),
				textassets.TemplateAgentsMD,
			); err != nil {
				return errors.New(cliMessage("init.agents_asset_error", err))
			}
			if _, statErr := os.Stat(paths.IndexPath); statErr != nil {
				if !os.IsNotExist(statErr) {
					return statErr
				}
				if !configExisted {
					volumeAssets, err = renderInitialVolumeAssets(root)
					if err != nil {
						return err
					}
				} else {
					if !flagQuiet {
						headerDictionaryMessage, err = initHeaderDictionaryMessage()
						if err != nil {
							return err
						}
					}
					minimalIndexContent, err = renderMinimalIndex(root)
					if err != nil {
						return err
					}
				}
			}

			// 所有本路径实际需要的嵌入文本均已验证；从这里开始才允许写入。
			for _, directory := range []string{
				".aoci",
				".aoci/verify_history",
				".aoci/hooks",
			} {
				if err := os.MkdirAll(
					filepath.Join(root, directory),
					0o755,
				); err != nil {
					return errors.New(cliMessage("init.mkdir_error", directory, err))
				}
			}
			gitignoreMessage, gitignoreErr := ensureAOCIRuntimeGitignore(root)
			if gitignoreErr != nil {
				return gitignoreErr
			}
			outputLines = append(
				outputLines,
				gitignoreMessage,
			)
			if newRepositoryAutomationMessage != "" {
				outputLines = append(
					outputLines,
					newRepositoryAutomationMessage,
				)
			}
			if scopeProposal != nil {
				outputLines = append(outputLines, cliMessage("init.scope_proposal", scopeProposal.GitTracked,
					scopeProposal.NewSourceFiles, scopeProposal.IndexObjects, scopeProposal.ObserveObjects,
					scopeProposal.ExcludeObjects, scopeProposal.SafetyExcluded, scopeProposal.EstimatedWholeIndexTokens,
					scopeProposal.RequiredHumanDecisions, scopeProposal.LargestDirectories))
			}

			expectedPostimages := map[string]baseline.Fingerprint{}

			skeletonCreated := false

			if _, statErr := os.Stat(
				paths.IndexPath,
			); statErr == nil {
				outputLines = append(outputLines, cliMessage("init.existing_skip", cfg.IndexPath))
			} else {
				if !configExisted {
					postimages, initErr := initializeVolumeFirst(root, cfg.IndexPath, volumeAssets)
					if initErr != nil {
						return initErr
					}
					for path, fingerprint := range postimages {
						expectedPostimages[path] = fingerprint
					}
					outputLines = append(outputLines, cliMessage("init.skeleton_created", "aoci.txt, aoci.meta.txt, aoci.code.txt"))
				} else {
					if writeErr := hooks.BackupThenWrite(paths.IndexPath, []byte(minimalIndexContent)); writeErr != nil {
						return writeErr
					}
					skeletonCreated = true
					if fingerprint, ok := fingerprintInitPath(root, cfg.IndexPath); ok {
						expectedPostimages[cfg.IndexPath] = fingerprint
					}
					outputLines = append(outputLines, cliMessage("init.skeleton_created", cfg.IndexPath))
				}
			}

			if err := config.Save(
				root,
				cfg,
			); err != nil {
				return err
			}

			outputLines = append(outputLines, cliMessage("init.config_ready"))
			beforeAgents, beforeAgentsExisted := fingerprintInitPath(root, "AGENTS.md")
			agentsMessage, err :=
				hooks.EnsureAgentsBlock(
					root,
				)
			if err != nil {
				return err
			}

			outputLines = append(
				outputLines,
				agentsMessage,
			)
			if fingerprint, ok := fingerprintInitPath(root, "AGENTS.md"); ok &&
				(!beforeAgentsExisted || beforeAgents.SHA256 != fingerprint.SHA256) {
				expectedPostimages["AGENTS.md"] = fingerprint
			}

			if agent != "" {
				agents := []string{
					agent,
				}

				if agent == "all" {
					agents = []string{
						"claude",
						"codex",
						"cursor",
					}
				}

				for _, agentName := range agents {
					beforeInstall := map[string]baseline.Fingerprint{}
					for _, relativePath := range initAgentCandidatePaths(agentName) {
						if fingerprint, ok := fingerprintInitPath(root, relativePath); ok {
							beforeInstall[relativePath] = fingerprint
						}
					}
					useHooks :=
						withHooks &&
							agentName ==
								"claude"

					message, installErr :=
						hooks.Install(
							root,
							agentName,
							useHooks,
						)
					if installErr != nil {
						return installErr
					}

					outputLines = append(
						outputLines,
						message,
					)
					for _, relativePath := range initAgentCandidatePaths(agentName) {
						after, ok := fingerprintInitPath(root, relativePath)
						if !ok {
							continue
						}
						before, existed := beforeInstall[relativePath]
						if !existed || before.SHA256 != after.SHA256 {
							expectedPostimages[relativePath] = after
						}
					}

					if !contains(
						cfg.InstalledAgents,
						agentName,
					) {
						cfg.InstalledAgents = append(
							cfg.InstalledAgents,
							agentName,
						)
					}
				}

				if withHooks &&
					agent != "claude" &&
					agent != "all" {
					outputLines = append(
						outputLines,
						cliMessage("init.hooks_agent_ignored"),
					)
				}

				if err := config.Save(
					root,
					cfg,
				); err != nil {
					return err
				}
			} else {
				if withHooks {
					outputLines = append(
						outputLines,
						cliMessage("init.hooks_without_agent"),
					)
				}

				if found := hooks.Detect(
					root,
				); len(found) > 0 {
					outputLines = append(
						outputLines,
						cliMessage("init.detected_agents", strings.Join(found, ", ")),
					)
				}
			}

			beforeInitBaselineAdvance()
			if baselineNote := advanceInitBaseline(root, expectedPostimages); baselineNote != "" {
				outputLines = append(
					outputLines,
					baselineNote,
				)
			}

			if !flagQuiet {
				fmt.Println(cliMessage("init.complete", root))

				for _, line := range outputLines {
					fmt.Println(
						"  - " +
							line,
					)
				}

				fmt.Println(nextStepMessage)
				fmt.Println(fullIndexMessage)

				if skeletonCreated {
					fmt.Println(headerDictionaryMessage)
				}
			}

			return nil
		},
	}

	command.Flags().StringVar(
		&agent,
		"agent",
		"",
		cliMessage("cli.flag.init_agent"),
	)

	command.Flags().BoolVar(
		&withHooks,
		"hooks",
		false,
		cliMessage("cli.flag.init_hooks"),
	)

	command.Flags().StringVar(
		&locale,
		"locale",
		"",
		cliMessage("cli.flag.init_locale"),
	)

	command.Flags().StringVar(
		&scopeProfile,
		"scope-profile",
		"production",
		cliMessage("cli.flag.init_scope_profile"),
	)

	command.Flags().BoolVar(
		&here,
		"here",
		false,
		cliMessage("cli.flag.init_here"),
	)

	registerCommand(
		command,
	)
}

// renderMinimalIndex is the single production assembly path for the initial
// index. Compatibility tests call it with a canonical root so their digest
// covers loading, template execution, variable substitution, and final bytes.
func renderMinimalIndex(root string) (string, error) {
	minimalIndex, err := textassets.Load(
		textassets.ActiveLocale(),
		textassets.TemplateMinimalIndex,
	)
	if err != nil {
		return "", errors.New(cliMessage("init.minimal_asset_error", err))
	}

	return hooks.RenderTemplate(
		"minimal-index.txt.tmpl",
		minimalIndex,
		hooks.NewTplData(root),
	)
}

func contains(
	list []string,
	value string,
) bool {
	for _, current := range list {
		if current == value {
			return true
		}
	}

	return false
}
