// aoci init: initializes .aoci, the runtime Git boundary, the index skeleton, AGENTS.md, and the optional Agent integrations.
//
// automation compatibility rules:
//   - config.json did not exist: a new repository's first initialization writes automation.mode=auto explicitly;
//   - config.json exists but lacks automation: an old legacy repository, not changed silently by an idempotent init;
//   - init itself is purely deterministic; setting auto does not mean this invocation calls a model.
//
// The .aoci Git boundary:
//   - the first initialization generates .aoci/.gitignore;
//   - unknown .aoci assets are ignored by default;
//   - the Baseline, the team configuration, and the Curation formal assets are allowlisted;
//   - an existing maintainer file is never overwritten.
//
// The complete index is uniformly routed to agent guide. The init output must explain the current team mode accurately:
// auto runs continuously through Apply; review/legacy stop before Apply; off only observes.
package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"os"
	"path"
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
	// A path through a link is not this repository's file; reading it would
	// follow the link out of the repository or block on a FIFO.
	if hooks.FirstSymlinkUnder(root, relativePath) != "" {
		return baseline.Fingerprint{}, false
	}
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
	case "qoder":
		// Qoder CLI reads the same project-level .mcp.json Claude Code does;
		// nothing else is written for it.
		return []string{".mcp.json"}
	case "pi":
		return []string{".pi/mcp.json"}
	default:
		return nil
	}
}

// advanceInitBaseline serializes the deterministic file changes init produces with the other Baseline write entries.
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
		existing, recorded := currentBaseline.Files[relativePath]
		// Under Managed Scope a path's role is the policy's decision: init
		// advances only paths the Baseline already records and keeps their
		// role, so an observe file stays observe and an excluded or not yet
		// scanned path is left for scan to classify.
		if currentBaseline.ManagedScope != nil && !recorded {
			continue
		}
		fingerprint, ok := fingerprintInitPath(root, relativePath)
		if !ok || fingerprint.SHA256 != expected.SHA256 {
			conflicted = append(conflicted, relativePath)
			continue
		}
		if recorded {
			expected.Role = existing.Role
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
	case "qoder", "pi":
		// The Guide has no Qoder- or Pi-specific text; both read an
		// mcpServers file and the same AGENTS.md block as Claude Code.
		return "aoci index agent guide --agent claude --json"
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
			case "", "claude", "codex", "cursor", "opencode", "qoder", "pi", "all":
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
			// Codex compaction integration also has user-owned configuration
			// conflicts. Prove it installable before init creates any repository
			// asset; hooks.Install repeats this check at the write boundary.
			if withHooks && (agent == "codex" || agent == "all") {
				if preflightErr := hooks.ValidateCodexCompactionInstall(root); preflightErr != nil {
					return &ExitError{Code: ExitConfig, Msg: preflightErr.Error()}
				}
			}
			// Refuse a symlinked or non-directory .aoci or .aoci/backups before
			// init creates any directory or writes AGENTS.md so backups can
			// never be redirected outside the repository or to another path.
			if preflightErr := hooks.ValidateAgentsBackupDir(root); preflightErr != nil {
				return &ExitError{Code: ExitConfig, Msg: preflightErr.Error()}
			}

			outputLines := []string{}
			configExisted := true
			newRepositoryAutomationMessage := ""
			var scopeProposal *managedscope.Proposal
			var initialInventory afs.SafeInventorySummary
			var initialIndexCount, outsideChildIndexCount int

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

			_, baselineExists, baselineErr := baseline.Load(root)
			beforeFirstScan := baselineErr == nil && !baselineExists
			if !configExisted {
				cfg.SetNewProjectArtifactExclusions()
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
				if transportErr := cfg.SetHostTransportDefaults(agent); transportErr != nil {
					return &ExitError{Code: ExitConfig, Err: transportErr}
				}
				evaluation, evaluationErr := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
					WalkOptions: cfg.WalkOptions(), CurationExclude: cfg.CurationExclude})
				if evaluationErr != nil {
					return &ExitError{Code: ExitConfig, Err: evaluationErr}
				}
				proposal := managedscope.BuildProposal(evaluation, scopeProfile, len(cfg.SafeInventoryHighRiskOptIn))
				scopeProposal = &proposal
				initialInventory, initialIndexCount = evaluation.SafeInventory, evaluation.IndexCount
				paths := make([]string, 0, len(evaluation.Index))
				for _, item := range evaluation.Index {
					paths = append(paths, item.Path)
				}
				outsideChildIndexCount = indexPathsOutsideChildRepositories(root, paths)
				if proposal.RequiresHumanApproval {
					return &ExitError{Code: ExitConfig, MachineCode: "managed_scope_auto_authorization_blocked",
						Msg: initialScopeBlockedDetail(evaluation, scopeProfile, len(cfg.SafeInventoryHighRiskOptIn))}
				}

				newRepositoryAutomationMessage = cliMessage("init.automation_default")
			} else if beforeFirstScan {
				// A config written first (database source add, config set) does
				// not make the first scan any less exposed to build output.
				if evaluation, evaluationErr := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{
					WalkOptions: cfg.WalkOptions(), CurationExclude: cfg.CurationExclude}); evaluationErr == nil {
					initialInventory, initialIndexCount = evaluation.SafeInventory, evaluation.IndexCount
					paths := make([]string, 0, len(evaluation.Index))
					for _, item := range evaluation.Index {
						paths = append(paths, item.Path)
					}
					outsideChildIndexCount = indexPathsOutsideChildRepositories(root, paths)
				}
			}

			paths := config.AOCIPaths(
				root,
				cfg.IndexPath,
			)
			// A missing index starts Volume-first wherever the Root can live at
			// aoci.txt, also under a configuration that predates it: the Legacy
			// layout is read-only, so a Legacy skeleton would leave nothing to do
			// but migrate an empty index. A custom index_path, which Volumes
			// cannot use as its Root, is refused below when the index is missing.
			volumeFirst := !configExisted || filepath.ToSlash(cfg.IndexPath) == "aoci.txt"

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
				if !volumeFirst {
					// A Volumes Root lives at aoci.txt. A custom index_path
					// used to get a Legacy skeleton here, a layout that is
					// read-only since 0.2.0: the user was handed an empty index
					// whose only next step was to migrate it. Refuse instead
					// and name the one change that lets init proceed.
					return &ExitError{Code: ExitConfig, MachineCode: "init_custom_index_path_unsupported",
						Msg: cliMessage("init.custom_index_path_unsupported", cfg.IndexPath)}
				}
				if volumeFirst {
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

			// Every embedded text this path really needs has been validated; only from here on may writes happen.
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
			gitattributesMessage, gitattributesErr := ensureGitattributes(root)
			if gitattributesErr != nil {
				return gitattributesErr
			}
			if gitattributesMessage != "" {
				outputLines = append(
					outputLines,
					gitattributesMessage,
				)
			}
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
					scopeProposal.RequiredHumanDecisions, formatDirectoryImpacts(scopeProposal.LargestDirectories)))
			}
			if beforeFirstScan && !initialInventory.GitRepository {
				switch {
				case initialInventory.ChildRepositories > 0:
					// A container whose every source sits in a child
					// repository has nothing outside Git's ignore scope.
					if outsideChildIndexCount == 0 {
						break
					}
					outputLines = append(outputLines, cliMessage("init.child_git_ignore_boundary", shellPathArgument(root)))
				case initialIndexCount > 0:
					outputLines = append(outputLines, cliMessage("init.no_ignore_authority", initialIndexCount, shellPathArgument(root)))
				}
			}

			expectedPostimages := map[string]baseline.Fingerprint{}

			skeletonCreated := false

			if _, statErr := os.Stat(
				paths.IndexPath,
			); statErr == nil {
				outputLines = append(outputLines, cliMessage("init.existing_skip", cfg.IndexPath))
			} else {
				if volumeFirst {
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

				var writtenHostPaths []string
				for _, agentName := range agents {
					// Refuse a host file reached through a link before reading
					// it: the fingerprint below would follow the link out of the
					// repository, or block on a FIFO.
					if link := hooks.FirstLinkedTarget(root, agentName, withHooks && (agentName == "claude" || agentName == "codex")); link != "" {
						return hooks.MCPPathSymlinkError(link)
					}
					beforeInstall := map[string]baseline.Fingerprint{}
					for _, relativePath := range initAgentCandidatePaths(agentName) {
						if fingerprint, ok := fingerprintInitPath(root, relativePath); ok {
							beforeInstall[relativePath] = fingerprint
						}
					}
					useHooks := withHooks &&
						(agentName == "claude" || agentName == "codex")

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
							writtenHostPaths = append(writtenHostPaths, relativePath)
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

				// It must happen before the first scan: miss this moment and the machine-bound host configuration enters
				// the Managed Scope in the index role, after which --force cannot advance the role,
				// and removal goes through the coverage-reduction Scope Change approval.
				hostIgnoreCandidates := []string{}
				for _, agentName := range agents {
					hostIgnoreCandidates = append(
						hostIgnoreCandidates,
						initAgentCandidatePaths(agentName)...,
					)
				}
				hostIgnoreMessage, hostIgnoreErr := ensureHostConfigGitignore(
					root,
					hostIgnoreCandidates,
				)
				if hostIgnoreErr != nil {
					return hostIgnoreErr
				}
				if hostIgnoreMessage != "" {
					outputLines = append(
						outputLines,
						hostIgnoreMessage,
					)
					if fingerprint, ok := fingerprintInitPath(root, ".gitignore"); ok {
						expectedPostimages[".gitignore"] = fingerprint
					}
				}

				// Without Git no ignore file keeps the host configuration out of
				// Managed Scope, so init excludes it by rule while the first scan
				// is still ahead: a later exclusion is a coverage reduction.
				if beforeFirstScan && !initialInventory.GitRepository {
					if excluded := excludeHostConfigWithoutGit(root, cfg, writtenHostPaths); len(excluded) > 0 {
						outputLines = append(outputLines, cliMessage("init.host_config_excluded", strings.Join(excluded, ", ")))
					}
				}

				if withHooks &&
					agent != "claude" &&
					agent != "codex" &&
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

			// A path Git hides never enters the Baseline at scan, so init does
			// not record it either: on a clone, init --agent would otherwise
			// rewrite the committed Baseline for a host configuration that
			// governs nothing, since the boundary init writes ignores it.
			for relativePath := range expectedPostimages {
				if ignored, _ := afs.PathIgnoredByGit(root, relativePath); ignored {
					delete(expectedPostimages, relativePath)
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
				// Measure the final index selection, including the files and
				// ignore rules init wrote after the scope proposal.
				if scopeProposal != nil {
					outputLines = append(outputLines, initAuthoringEstimateLines(root)...)
				}
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

// initialScopeBlockedDetail translates "the first scope needs a person to decide" into an actionable diagnosis.
//
// This block used to return a single machine-code line that named neither the kind of cause nor any path, and
// no other command could reproduce it before init (the pre-init scope show evaluates the legacy policy), so
// the user could only locate it by reading source (review correction). The three kinds of trigger each have their own way out, given here separately.
func initialScopeBlockedDetail(evaluation *managedscope.Evaluation, scopeProfile string, highRiskOptIns int) string {
	if highRiskOptIns > 0 {
		return cliMessage("init.scope_blocked_high_risk", highRiskOptIns)
	}
	// The criterion must share its source with BuildProposal's RequiresHumanApproval: that place now uses
	// AutoBlockerCount (explicitly accepted paths whose content will be read); if this place still used
	// RequiredHumanReview (any excluded tracked path), it would explain a block such as "this profile indexes no
	// path at all" as "some tracked file needs a decision", and the operator who follows it and deletes the file
	// runs into the same refusal, exactly the kind of wrong remedy this version removes.
	if evaluation != nil && evaluation.SafeInventory.AutoBlockerCount > 0 {
		named := make([]string, 0, initialScopeBlockedPathSample)
		for _, item := range evaluation.Exclude {
			// Named are those whose content will be read.
			if item.GitStatus != "tracked" {
				continue
			}
			named = append(named, item.Path)
			if len(named) == initialScopeBlockedPathSample {
				break
			}
		}
		sample := strings.Join(named, ", ")
		if remaining := evaluation.SafeInventory.AutoBlockerCount - len(named); remaining > 0 && len(named) > 0 {
			sample += fmt.Sprintf(" (+%d)", remaining)
		}
		return cliMessage("init.scope_blocked_tracked_sensitive", evaluation.SafeInventory.AutoBlockerCount, sample)
	}
	candidates := 0
	if evaluation != nil {
		candidates = evaluation.SafeInventory.FinalManagedCandidates
	}
	return cliMessage("init.scope_blocked_no_index", scopeProfile, candidates)
}

// The most paths a diagnosis names: enough to locate, without flushing a whole directory tree to the terminal.
const initialScopeBlockedPathSample = 5

// formatDirectoryImpacts renders the proposal's largest directories as text a
// person can read. The %v it replaced printed Go struct syntax, {./ 2 0 0 2},
// which names neither the fields nor their meaning.
func formatDirectoryImpacts(impacts []managedscope.DirectoryImpact) string {
	if len(impacts) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(impacts))
	for _, impact := range impacts {
		parts = append(parts, fmt.Sprintf("%s (index %d, observe %d, exclude %d)",
			impact.Directory, impact.Index, impact.Observe, impact.Exclude))
	}
	return strings.Join(parts, "; ")
}

// indexPathsOutsideChildRepositories counts the index-role paths of a non-Git
// container that sit in a directory no child Git repository holds: only those
// lie outside every ignore file's reach. Files directly at the container root
// are not counted: init writes its own there (AGENTS.md, .gitattributes, host
// configuration), and the remedy the warning hands excludes directories.
func indexPathsOutsideChildRepositories(root string, paths []string) int {
	inChild := map[string]bool{"": false, ".": false}
	var held func(dir string) bool
	held = func(dir string) bool {
		if value, known := inChild[dir]; known {
			return value
		}
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(dir), ".git"))
		value := err == nil || held(path.Dir(dir))
		inChild[dir] = value
		return value
	}
	outside := 0
	for _, item := range paths {
		if dir := path.Dir(item); dir != "." && !held(dir) {
			outside++
		}
	}
	return outside
}

// excludeHostConfigWithoutGit adds one exclude rule per host configuration
// file init wrote in this run (a user's own file it did not touch keeps its
// role), for a root no Git repository governs, and reports the paths. The
// rules go into the policy init saves next, on top of the effective policy
// when the config carries none yet; a rule that already exists is left alone.
func excludeHostConfigWithoutGit(root string, cfg *config.Config, candidates []string) []string {
	if cfg == nil {
		return nil
	}
	policy := cfg.EffectiveManagedScope()
	existing := map[string]bool{}
	for _, rule := range policy.Rules {
		existing[rule.RuleID] = true
	}
	var added []string
	for _, relativePath := range candidates {
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relativePath))); err != nil || !info.Mode().IsRegular() {
			continue
		}
		digest := sha256.Sum256([]byte(relativePath))
		ruleID := fmt.Sprintf("host-config-%x", digest[:6])
		if existing[ruleID] {
			continue
		}
		existing[ruleID] = true
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: ruleID, Action: machinecontract.ScopeRoleExclude,
			Pattern: relativePath, PatternKind: machinecontract.ScopePatternFile, Reason: "machine-bound agent host configuration",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "aoci init", Enabled: true})
		added = append(added, relativePath)
	}
	if len(added) == 0 {
		return nil
	}
	normalized, err := managedscope.Normalize(policy)
	if err != nil {
		return nil
	}
	cfg.ManagedScope = &normalized
	return added
}
