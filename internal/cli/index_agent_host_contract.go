// Host-Agent Guide的运行时命令绑定、Windows调用纪律与Help契约。
//
// 设计边界:
//   - buildAgentGuide保持与机器位置无关的确定性领域构造器；
//   - Guide真正输出前才绑定当前aoci二进制绝对路径；
//   - Windows命令使用PowerShell调用运算符&和双引号；
//   - Windows普通CLI的--json输出为ASCII安全JSON，PowerShell 5可直接捕获；
//   - POSIX命令使用单引号保护空格与Shell元字符；
//   - Stage Help与Guide披露协议代码中的真实大小、批次上限和阶段行为；
//   - 稳定自然语言通过textassets按语言无关ID加载，Go只注入机器事实。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

// normalizeAgentExecutablePath normalizes separators across host platforms.
//
// filepath.ToSlash only recognizes the current OS separator, so simulated
// Windows paths need an explicit backslash conversion in Linux tests.
func normalizeAgentExecutablePath(
	executable string,
) string {
	executable = strings.ReplaceAll(
		executable,
		`\`,
		"/",
	)

	return filepath.ToSlash(
		executable,
	)
}

// isAbsoluteAgentExecutablePath validates absolute paths for the target OS.
//
// The explicit goos parameter keeps Windows path validation deterministic in
// Linux CI. Device paths are rejected because slash normalization does not
// preserve their special Windows semantics.
func isAbsoluteAgentExecutablePath(
	goos,
	executable string,
) bool {
	if goos != "windows" {
		return strings.HasPrefix(
			executable,
			"/",
		)
	}

	if len(executable) >= 3 &&
		((executable[0] >= 'A' && executable[0] <= 'Z') ||
			(executable[0] >= 'a' && executable[0] <= 'z')) &&
		executable[1] == ':' &&
		executable[2] == '/' {
		return true
	}

	if !strings.HasPrefix(
		executable,
		"//",
	) ||
		strings.HasPrefix(
			executable,
			"//?/",
		) ||
		strings.HasPrefix(
			executable,
			"//./",
		) {
		return false
	}

	parts := strings.Split(
		strings.TrimPrefix(
			executable,
			"//",
		),
		"/",
	)

	return len(parts) >= 2 &&
		parts[0] != "" &&
		parts[1] != ""
}

// resolveAgentExecutablePath obtains and validates the current executable.
// Any acquisition or absolute-path failure is terminal; callers must never
// substitute a PATH-resolved bare command.
func resolveAgentExecutablePath(
	goos string,
	executablePath func() (string, error),
	absolutePath func(string) (string, error),
) (string, error) {
	executable, err := executablePath()
	if err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"host.executable.resolve_failed", localeSafeCLIDetail(err.Error()),
		))
	}
	if strings.TrimSpace(
		executable,
	) == "" {
		return "", fmt.Errorf("%s", cliMessage("host.executable.empty"))
	}

	executable, err = absolutePath(
		executable,
	)
	if err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"host.executable.absolute_failed", localeSafeCLIDetail(err.Error()),
		))
	}

	executable = normalizeAgentExecutablePath(
		filepath.Clean(
			executable,
		),
	)

	if !isAbsoluteAgentExecutablePath(
		goos,
		executable,
	) {
		return "", fmt.Errorf("%s", cliMessage("host.executable.not_absolute", executable))
	}

	return executable, nil
}

// currentAgentExecutablePath returns the validated absolute process path.
//
// Windows separators are normalized to avoid command, JSON, and TOML escape
// ambiguity. Failures remain errors instead of falling back to PATH.
func currentAgentExecutablePath() (string, error) {
	return resolveAgentExecutablePath(
		runtime.GOOS,
		os.Executable,
		filepath.Abs,
	)
}

// agentCommandPrefixFor converts a validated absolute executable path into a
// target-shell command prefix.
//
// PowerShell interpolation characters are rejected because this contract uses
// double-quoted invocation paths. POSIX paths are protected with single quotes.
func agentCommandPrefixFor(
	goos,
	executable string,
) (string, error) {
	executable = normalizeAgentExecutablePath(
		executable,
	)

	if strings.ContainsAny(
		executable,
		"\x00\r\n",
	) {
		return "", fmt.Errorf("%s", cliMessage("host.executable.control_character"))
	}
	if !isAbsoluteAgentExecutablePath(
		goos,
		executable,
	) {
		return "", fmt.Errorf("%s", cliMessage("host.executable.not_absolute", executable))
	}

	if goos == "windows" {
		if strings.ContainsAny(
			executable,
			"\"`$",
		) {
			return "", fmt.Errorf("%s", cliMessage("host.executable.powershell_unsafe"))
		}

		return `& "` + executable + `"`, nil
	}

	return "'" +
		strings.ReplaceAll(
			executable,
			"'",
			`'"'"'`,
		) +
		"'", nil
}

// currentAgentCommandPrefix returns the current platform's safe prefix.
func currentAgentCommandPrefix() (string, error) {
	executable, err := currentAgentExecutablePath()
	if err != nil {
		return "", err
	}

	return agentCommandPrefixFor(
		runtime.GOOS,
		executable,
	)
}

// bindAgentCommand把领域层裸aoci命令绑定为当前机器绝对执行命令。
func bindAgentCommand(
	command,
	prefix string,
) string {
	command = strings.TrimSpace(
		command,
	)

	if command == "" {
		return ""
	}

	switch {
	case command == "aoci":
		return prefix

	case strings.HasPrefix(
		command,
		"aoci ",
	):
		return prefix +
			command[len("aoci"):]

	default:
		return command
	}
}

// bindAgentGuideCommands绑定Guide中的全部机器命令。
func bindAgentGuideCommands(
	commands *agentGuideCommands,
	prefix string,
) {
	if commands == nil {
		return
	}

	fields := []*string{
		&commands.Guide,
		&commands.Plan,
		&commands.Scan,
		&commands.HeaderShow,
		&commands.HeaderStage,
		&commands.EntriesStage,
		&commands.CurationStage,
		&commands.CurationDiff,
		&commands.CurationApply,
		&commands.Check,
		&commands.Diff,
		&commands.Apply,
		&commands.Verify,
		&commands.ScopePreview,
		&commands.ScopeStatus,
		&commands.ScopeAcknowledge,
	}

	for _, field := range fields {
		*field = bindAgentCommand(
			*field,
			prefix,
		)
	}
}

// agentRuntimeInstructionsFor返回指定平台的通用Guide运行时合同。
//
// 使用显式goos参数，使Windows对外文案能够在Linux CI中稳定测试。
func agentRuntimeInstructionsFor(
	goos string,
) ([]string, error) {
	instructions, err := textassets.RenderLines(
		textassets.ActiveLocale(),
		textassets.ContractHostRuntimeBaseInstructions,
		nil,
	)
	if err != nil {
		return nil, err
	}

	if goos == "windows" {
		windowsInstructions, err := textassets.RenderLines(
			textassets.ActiveLocale(),
			textassets.ContractHostRuntimeWindowsInstructions,
			nil,
		)
		if err != nil {
			return nil, err
		}
		instructions = append(
			instructions,
			windowsInstructions...,
		)
	}

	return instructions, nil
}

// finalizeAgentGuideRuntimeContract在输出前注入当前机器事实。
//
// 本函数不改变Plan、目标、停点或请求模板，只增强命令和平台说明。
func finalizeAgentGuideRuntimeContract(
	guide *agentGuide,
) error {
	if guide == nil ||
		guide.Plan == nil {
		return nil
	}

	executable, err := currentAgentExecutablePath()
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.executable_bind_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	return finalizeAgentGuideRuntimeContractFor(
		guide,
		runtime.GOOS,
		executable,
	)
}

// finalizeVolumeAgentGuideRuntimeContract binds every executable CLI command
// in a Volumes guide to the currently running binary. MCP tool actions remain
// machine next-actions and are not represented as shell commands here.
func finalizeVolumeAgentGuideRuntimeContract(
	guide *volumeAgentGuide,
) error {
	if guide == nil {
		return nil
	}

	executable, err := currentAgentExecutablePath()
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.executable_bind_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	return finalizeVolumeAgentGuideRuntimeContractFor(
		guide,
		runtime.GOOS,
		executable,
	)
}

func finalizeVolumeAgentGuideRuntimeContractFor(
	guide *volumeAgentGuide,
	goos,
	executable string,
) error {
	if guide == nil {
		return nil
	}

	prefix, err := agentCommandPrefixFor(goos, executable)
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.executable_bind_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	bindAgentGuideCommands(&guide.Commands, prefix)
	return nil
}

// finalizeAgentGuideRuntimeContractFor injects validated host facts. The
// explicit OS and executable make fail-closed behavior independently testable.
func finalizeAgentGuideRuntimeContractFor(
	guide *agentGuide,
	goos,
	executable string,
) error {
	if guide == nil ||
		guide.Plan == nil {
		return nil
	}

	prefix, err := agentCommandPrefixFor(
		goos,
		executable,
	)
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.executable_bind_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	bindAgentGuideCommands(
		&guide.Commands,
		prefix,
	)
	populateAgentNextActionContract(guide)

	runtimeInstructions, err :=
		agentRuntimeInstructionsFor(
			goos,
		)
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.runtime_asset_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	templateData, err := textassets.NumericTemplateData(textassets.ActiveLocale())
	if err != nil {
		return err
	}

	switch guide.Plan.Stage {
	case agentPlanStageHeaderRequired:
		stageInstructions, err := textassets.RenderLines(
			textassets.ActiveLocale(),
			textassets.ContractHostRuntimeHeaderStageLimit,
			templateData,
		)
		if err != nil {
			return err
		}
		runtimeInstructions = append(
			runtimeInstructions,
			stageInstructions...,
		)

	case agentPlanStageEntriesRequired:
		stageInstructions, err := textassets.RenderLines(
			textassets.ActiveLocale(),
			textassets.ContractHostRuntimeEntriesStageLimit,
			templateData,
		)
		if err != nil {
			return err
		}
		runtimeInstructions = append(
			runtimeInstructions,
			stageInstructions...,
		)

	case agentPlanStageCurationRequired:
		stageInstructions, err := textassets.RenderLines(
			textassets.ActiveLocale(),
			textassets.ContractHostRuntimeCurationStageLimit,
			templateData,
		)
		if err != nil {
			return err
		}
		runtimeInstructions = append(
			runtimeInstructions,
			stageInstructions...,
		)
	}

	if len(guide.Instructions) == 0 {
		guide.Instructions = runtimeInstructions
		return nil
	}

	combined := make(
		[]string,
		0,
		len(guide.Instructions)+
			len(runtimeInstructions),
	)

	combined = append(
		combined,
		guide.Instructions[0],
	)

	combined = append(
		combined,
		runtimeInstructions...,
	)

	combined = append(
		combined,
		guide.Instructions[1:]...,
	)

	guide.Instructions = combined

	return nil
}

func populateAgentNextActionContract(guide *agentGuide) {
	if guide == nil || guide.Plan == nil {
		return
	}
	action := agentGuideNextAction{Action: guide.Plan.NextAction, RequiredParameters: map[string]string{"agent": guide.Agent},
		Agent: guide.Agent, ExpectedPreimage: guide.Plan.IndexSHA256, PlanOrRunIdentity: guide.Plan.PlanID,
		TTYRequired: false, AutomaticallyRetryable: false, TransportCorrectionLimit: 1, SuccessNextAction: guide.Commands.Guide}
	switch guide.Plan.NextAction {
	case agentPlanActionScan:
		action.Command = guide.Commands.Scan
		action.SchemaVersion = "safe-inventory/v2"
		action.RequiredParameters["repo"] = guide.Plan.RepositoryRoot
	case agentPlanActionGenerateHead:
		action.Command = guide.Commands.HeaderStage
		action.SchemaVersion = "agent-header-stage-request/v1"
		action.RequestFile = "{request_file}"
		action.RequiredParameters["request_file"] = action.RequestFile
		action.AutomaticallyRetryable = true
	case agentPlanActionStageEntries:
		action.Command = guide.Commands.EntriesStage
		action.SchemaVersion = "agent-entries-stage-request/v1"
		action.RequestFile = "{request_file}"
		action.RequiredParameters["request_file"] = action.RequestFile
		action.AutomaticallyRetryable = true
	case agentPlanActionStageCuration:
		action.Command = guide.Commands.CurationStage
		action.SchemaVersion = "agent-curation-stage-request/v1"
		action.RequestFile = "{request_file}"
		action.RequiredParameters["request_file"] = action.RequestFile
		action.AutomaticallyRetryable = true
	case agentPlanActionScopePreview:
		action.Command = guide.Commands.ScopeStatus
		action.SchemaVersion = machinecontract.ManagedScopeStatusV2
	case agentPlanActionReviewObserved:
		action.Command = guide.Commands.ScopeStatus
		action.SchemaVersion = machinecontract.ManagedScopeStatusV2
	case agentPlanActionCompressCognition:
		action.Command = guide.Commands.ScopeStatus
		action.SchemaVersion = machinecontract.CognitionBudgetReportV1
	case agentPlanActionNone:
		action.Command = ""
		action.SchemaVersion = machinecontract.CapabilityManifestV1
	default:
		action.Command = guide.Commands.Guide
		action.SchemaVersion = "agent-guide-action/v1"
	}
	guide.NextActionContract = action
}

// decorateHostAgentHelp把真实协议上限和阶段合同写入生产命令树Help。
//
// 所有Long文本均整值赋予，不使用+=，保证newRootCmd在测试或嵌入环境中
// 被多次构造时不会重复累加说明。
func decorateHostAgentHelp(
	root *cobra.Command,
) {
	if root == nil {
		return
	}

	templateData, err := textassets.NumericTemplateData(textassets.ActiveLocale())
	if err != nil {
		failure := fmt.Sprintf("[text asset numeric data failed: %v]", err)
		decorateStageHelp(root, []string{"index", "agent", "stage"}, failure, failure)
		decorateStageHelp(root, []string{"index", "agent", "header", "stage"}, failure, failure)
		decorateStageHelp(root, []string{"index", "agent", "curation", "stage"}, failure, failure)
		return
	}

	decorateStageHelp(
		root,
		[]string{
			"index",
			"agent",
			"stage",
		},
		cliHelpTemplate(
			textassets.ContractHostHelpEntriesStageLong,
			templateData,
		),
		cliHelpTemplate(
			textassets.ContractHostHelpEntriesStageRequestFile,
			templateData,
		),
	)

	decorateStageHelp(
		root,
		[]string{
			"index",
			"agent",
			"header",
			"stage",
		},
		cliHelpTemplate(
			textassets.ContractHostHelpHeaderStageLong,
			templateData,
		),
		cliHelpTemplate(
			textassets.ContractHostHelpHeaderStageRequestFile,
			templateData,
		),
	)

	decorateStageHelp(
		root,
		[]string{
			"index",
			"agent",
			"curation",
			"stage",
		},
		cliHelpTemplate(
			textassets.ContractHostHelpCurationStageLong,
			templateData,
		),
		cliHelpTemplate(
			textassets.ContractHostHelpCurationStageRequestFile,
			templateData,
		),
	)

	guideCommand, _, err := root.Find(
		[]string{
			"index",
			"agent",
			"guide",
		},
	)

	if err == nil &&
		guideCommand != nil {
		guideCommand.Long = cliHelpTemplate(
			textassets.ContractHostHelpGuideLong,
			nil,
		)
	}
}

// decorateStageHelp更新一个Stage命令的Long与request-file参数说明。
func decorateStageHelp(
	root *cobra.Command,
	path []string,
	longText,
	requestFileUsage string,
) {
	command, _, err := root.Find(
		path,
	)

	if err != nil ||
		command == nil {
		return
	}

	command.Long = longText

	requestFileFlag := command.Flags().Lookup(
		"request-file",
	)

	if requestFileFlag != nil {
		requestFileFlag.Usage =
			requestFileUsage
	}

	stdinFlag := command.Flags().Lookup(
		"stdin-json",
	)

	if stdinFlag != nil {
		stdinFlag.Usage = cliHelpTemplate(
			textassets.ContractHostHelpStageStdinJSON,
			nil,
		)
	}
}
