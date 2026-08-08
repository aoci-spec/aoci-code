// Package cli是aoci-code的命令行入口与命令树装配。
// 索引条目: root.go[CRT9M]
//
// 命令注册采用【两代共存】模式:
//
//	第一代: 各命令文件在init中调用registerCommand自注册;
//	第二代: ai.go与doctor.go由newRootCmd显式挂载。
//
// 退出码契约:
//   - 0: 成功;
//   - 1: 漂移或普通检查失败;
//   - 2: 输入、索引或治理状态无效;
//   - 3: 未初始化、配置或参数错误;
//   - 10: 内部错误。
//
// 输出纪律:
//   - MCP子命令stdout是JSON-RPC协议流，必须直接透传，绝不缓冲或包裹;
//   - 普通CLI在--json下，失败且尚未输出业务JSON时，由根层输出唯一错误信封;
//   - Verify/Check等已经输出完整业务JSON再返回非零码时，不追加第二个JSON对象;
//   - Windows普通CLI的合法--json结果在最终写出时转为ASCII安全JSON，避免
//     PowerShell 5使用本地代码页捕获UTF-8中文时破坏JSON;
//   - 人读错误继续写stderr。
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

// 版本信息由Makefile经-ldflags注入。
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// 退出码契约常量。
const (
	ExitOK       = 0
	ExitDrift    = 1
	ExitInvalid  = 2
	ExitConfig   = 3
	ExitInternal = 10
)

// 全局持久参数。
var (
	flagRepo  string
	flagJSON  bool
	flagQuiet bool
)

// ExitError携带业务退出码。
//
// MachineCode是可选的领域级机器分类，例如write_conflict。它不改变进程
// 退出码，只允许已经掌握底层失败类别的调用方把精确错误语义透传给JSON消费者。
// 未设置MachineCode时继续按Code执行既有drift/invalid/config/internal映射。
//
// Msg与Err都为空表示命令已经输出完整业务报告，根层只返回退出码，不能重复输出。
type ExitError struct {
	Code        int
	MachineCode string
	Err         error
	Msg         string
	Details     any
}

// Error实现error接口。
func (e *ExitError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}

	if e.Err != nil {
		return e.Err.Error()
	}

	return fmt.Sprintf(
		"exit code %d",
		e.Code,
	)
}

// ExitCode实现exitCoder接口。
func (e *ExitError) ExitCode() int {
	return e.Code
}

// exitCoder由携带退出码的错误实现。
type exitCoder interface {
	ExitCode() int
}

// subCommands是第一代自注册命令槽。
var subCommands []*cobra.Command

// registerCommand供各命令文件init自注册。
func registerCommand(
	command *cobra.Command,
) {
	subCommands = append(
		subCommands,
		command,
	)
}

// resolveRepoRoot定位仓库根。
func resolveRepoRoot() (string, error) {
	return config.FindRepoRoot(
		".",
		flagRepo,
	)
}

// newRootCmd装配根命令。
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "aoci",
		Short:         cliMessage("cli.short.root"),
		Long:          rootLongHelp(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version: fmt.Sprintf(
			"%s (commit %s, built %s)",
			version,
			commit,
			buildDate,
		),
		PersistentPreRunE: enforceCognitionRecoveryGate,
	}

	root.PersistentFlags().StringVar(
		&flagRepo,
		"repo",
		"",
		cliMessage("cli.flag.repo"),
	)

	root.PersistentFlags().BoolVar(
		&flagJSON,
		"json",
		false,
		cliMessage("cli.flag.json"),
	)

	root.PersistentFlags().BoolVar(
		&flagQuiet,
		"quiet",
		false,
		cliMessage("cli.flag.quiet"),
	)

	root.AddCommand(
		subCommands...,
	)

	root.AddCommand(
		newAICmd(),
	)

	root.AddCommand(
		newDoctorCmd(),
	)

	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	initializeCobraFlags(root)
	root.SetUsageTemplate(localizedUsageTemplate())
	decorateHostAgentHelp(
		root,
	)
	refreshCommandLocalization(root)
	installCLIHelpAssetGuard(root)

	return root
}

func enforceCognitionRecoveryGate(command *cobra.Command, _ []string) error {
	root, err := resolveRepoRoot()
	if err != nil {
		// Repository-specific commands retain their existing localized root
		// error. Commands such as version/help may not need a repository.
		return nil
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return &ExitError{Code: ExitInvalid, Msg: cliMessage("cognition.bootstrap.pending_inspection_failed")}
	}
	if len(pending) == 0 {
		return nil
	}
	path := command.CommandPath()
	allowed := path == "aoci index agent guide" || path == "aoci mcp"
	for _, transaction := range pending {
		switch transaction.Operation {
		case "bootstrap":
			allowed = allowed || path == "aoci cognition bootstrap status" ||
				path == "aoci cognition bootstrap resume" || path == "aoci cognition bootstrap rollback"
		case "migration":
			allowed = allowed || path == "aoci cognition migration status" ||
				path == "aoci cognition migration resume" || path == "aoci cognition migration rollback"
		case "reversal":
			allowed = allowed || path == "aoci cognition migration reversal status" ||
				path == "aoci cognition migration reversal resume"
		case "scope":
			allowed = allowed || path == "aoci baseline scope status" || path == "aoci baseline scope resume" ||
				path == "aoci scope status" || path == "aoci scope resume" || path == "aoci scope rollback"
		}
	}
	if allowed {
		return nil
	}
	if len(pending) == 1 && pending[0].Operation == "bootstrap" {
		return &ExitError{
			Code: ExitInvalid, MachineCode: "bootstrap_recovery_pending",
			Msg: cliMessage("cognition.bootstrap.pending_gate", pending[0].ID),
		}
	}
	names := make([]string, 0, len(pending))
	for _, transaction := range pending {
		names = append(names, transaction.Filename)
	}
	return &ExitError{
		Code: ExitInvalid, MachineCode: "cognition_recovery_pending",
		Msg: cliMessage("cognition.transaction.pending_gate", strings.Join(names, ",")),
	}
}

// Execute是二进制进程入口。
func Execute() {
	code := executeCLI(
		os.Args[1:],
		os.Stdout,
		os.Stderr,
	)

	if code != ExitOK {
		os.Exit(code)
	}
}

// executeCLI执行一次完整CLI调用并返回进程退出码。
//
// 普通命令使用内存缓冲，以便判断失败前是否已经输出完整业务JSON。
// MCP必须直接透传，避免破坏长连接和JSON-RPC帧时序。
func executeCLI(
	args []string,
	stdout,
	stderr io.Writer,
) int {
	resetRootFlags()
	previousLocale := textassets.ActiveLocale()
	defer func() { _ = textassets.SetActiveLocale(previousLocale) }()
	if err := activateInvocationLocale(args); err != nil {
		return finishBufferedExecution(
			&ExitError{Code: ExitConfig, Err: err},
			requestedJSON(args),
			nil,
			nil,
			stdout,
			stderr,
		)
	}
	if err := textassets.ValidateRuntime(); err != nil {
		return finishBufferedExecution(
			&ExitError{Code: ExitInternal, Err: fmt.Errorf(
				"%s",
				cliMessage("cli.catalog_invalid", localeSafeCLIDetail(err.Error())),
			)},
			requestedJSON(args),
			nil,
			nil,
			stdout,
			stderr,
		)
	}

	if isMCPInvocation(
		args,
	) {
		root := newRootCmd()
		defer detachRegisteredCommands(root)

		root.SetArgs(
			args,
		)
		root.SetOut(
			stdout,
		)
		root.SetErr(
			stderr,
		)

		err := root.Execute()
		if helpErr := cliHelpExecutionError(root); helpErr != nil {
			err = helpErr
		}

		return finishDirectExecution(
			err,
			stderr,
		)
	}

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer

	root := newRootCmd()
	defer detachRegisteredCommands(root)

	root.SetArgs(
		args,
	)
	root.SetOut(
		&stdoutBuffer,
	)
	root.SetErr(
		&stderrBuffer,
	)

	err := root.Execute()
	if helpErr := cliHelpExecutionError(root); helpErr != nil {
		err = helpErr
	}

	return finishBufferedExecution(
		err,
		requestedJSON(args),
		stdoutBuffer.Bytes(),
		stderrBuffer.Bytes(),
		stdout,
		stderr,
	)
}

// detachRegisteredCommands keeps package-level command instances reusable by
// direct unit tests and subsequent in-process CLI invocations. A real CLI
// process exits after one invocation, but the library entry point is reusable.
func detachRegisteredCommands(root *cobra.Command) {
	for _, command := range subCommands {
		if command.Parent() == root {
			root.RemoveCommand(command)
		}
	}
}

// activateInvocationLocale resolves the project locale before Cobra builds its
// command tree, so Short/Long help, flag descriptions, errors, and command
// output all use one catalog. init --locale is honored before a config exists.
func activateInvocationLocale(args []string) error {
	locale := textassets.DefaultLocale
	if requested, ok := initLocaleArgument(args); ok {
		locale = requested
	} else if requested, ok := cognitionPlannerLocaleArgument(args); ok {
		locale = requested
	} else {
		explicitRepo := repoArgument(args)
		if root, err := config.FindRepoRoot(".", explicitRepo); err == nil {
			var cfg *config.Config
			var loadErr error
			if isCognitionPlannerInvocation(args) {
				cfg, loadErr = config.LoadReadOnly(root)
			} else {
				cfg, loadErr = config.Load(root)
			}
			if loadErr != nil {
				return loadErr
			}
			locale = cfg.Locale
		}
	}
	return textassets.SetActiveLocale(locale)
}

func cognitionPlannerLocaleArgument(args []string) (string, bool) {
	command, found := rootCommandToken(args)
	if !found || command != "cognition" {
		return "", false
	}
	for position := 0; position < len(args); position++ {
		argument := strings.TrimSpace(args[position])
		if argument == "--locale" && position+1 < len(args) {
			return strings.TrimSpace(args[position+1]), true
		}
		if strings.HasPrefix(argument, "--locale=") {
			return strings.TrimSpace(strings.TrimPrefix(argument, "--locale=")), true
		}
	}
	return "", false
}

func isCognitionPlannerInvocation(args []string) bool {
	command, found := rootCommandToken(args)
	return found && command == "cognition"
}

func repoArgument(args []string) string {
	for position := 0; position < len(args); position++ {
		argument := strings.TrimSpace(args[position])
		if argument == "--repo" && position+1 < len(args) {
			return strings.TrimSpace(args[position+1])
		}
		if strings.HasPrefix(argument, "--repo=") {
			return strings.TrimSpace(strings.TrimPrefix(argument, "--repo="))
		}
	}
	return ""
}

func initLocaleArgument(args []string) (string, bool) {
	command, found := rootCommandToken(args)
	if !found || command != "init" {
		return "", false
	}
	for position := 0; position < len(args); position++ {
		argument := strings.TrimSpace(args[position])
		if argument == "--locale" && position+1 < len(args) {
			return strings.TrimSpace(args[position+1]), true
		}
		if strings.HasPrefix(argument, "--locale=") {
			return strings.TrimSpace(strings.TrimPrefix(argument, "--locale=")), true
		}
	}
	return "", false
}

// resetRootFlags保证测试中的多次执行与真实独立进程语义一致。
func resetRootFlags() {
	flagRepo = ""
	flagJSON = false
	flagQuiet = false
}

// requestedJSON在Cobra完成解析前识别显式JSON请求。
func requestedJSON(
	args []string,
) bool {
	for _, argument := range args {
		switch argument {
		case "--json",
			"--json=true":
			return true
		}
	}

	return false
}

// isMCPInvocation仅在顶层命令token为mcp时启用JSON-RPC直接透传。
//
// 普通参数值也可能合法地等于mcp，例如`--agent mcp`。因此不能扫描全部参数
// 寻找任意mcp字符串，必须先排除根层持久参数，再读取唯一的顶层命令token。
func isMCPInvocation(
	args []string,
) bool {
	command, found := rootCommandToken(
		args,
	)

	return found &&
		command == "mcp"
}

// rootCommandToken在执行Cobra解析前提取顶层命令token。
//
// 这里只识别可合法出现在顶层命令之前的根层持久参数。遇到未知flag时返回
// 未识别，使该调用进入普通缓冲执行并由Cobra返回标准参数错误，而不是误入
// MCP协议直通路径。
func rootCommandToken(
	args []string,
) (string, bool) {
	for position := 0; position < len(args); position++ {
		argument := strings.TrimSpace(
			args[position],
		)

		switch {
		case argument == "":
			return "", false

		case argument == "--repo":
			if position+1 >= len(args) {
				return "", false
			}

			position++

		case strings.HasPrefix(
			argument,
			"--repo=",
		):
			continue

		case argument == "--json" ||
			argument == "--quiet":
			continue

		case strings.HasPrefix(
			argument,
			"--json=",
		) ||
			strings.HasPrefix(
				argument,
				"--quiet=",
			):
			continue

		case strings.HasPrefix(
			argument,
			"-",
		):
			return "", false

		default:
			return argument, true
		}
	}

	return "", false
}

// finishDirectExecution处理MCP等不能缓冲的执行路径。
func finishDirectExecution(
	err error,
	stderr io.Writer,
) int {
	if err == nil {
		return ExitOK
	}

	code := executionExitCode(
		err,
	)

	if isSilentReportedError(
		err,
	) {
		return code
	}

	fmt.Fprintln(
		stderr,
		cliMessage("cli.error_prefix"),
		localizedCLIErrorMessage(err, code),
	)

	return code
}

// finishBufferedExecution根据stdout是否已有业务报告裁决错误输出形式。
func finishBufferedExecution(
	err error,
	jsonMode bool,
	stdoutData,
	stderrData []byte,
	stdout,
	stderr io.Writer,
) int {
	writeBytes(
		stderr,
		localizedCLIDiagnostics(stderrData),
	)

	if err == nil {
		writeCLIData(
			stdout,
			stdoutData,
			jsonMode,
		)

		return ExitOK
	}

	code := executionExitCode(
		err,
	)

	if isSilentReportedError(
		err,
	) {
		writeCLIData(
			stdout,
			stdoutData,
			jsonMode,
		)

		return code
	}

	// stdout已有内容时，说明命令已经输出业务报告或当前尚为人读命令。
	// 为避免生成两个JSON对象，保留原输出并把错误继续写stderr。
	if len(bytes.TrimSpace(
		stdoutData,
	)) > 0 {
		writeCLIData(
			stdout,
			stdoutData,
			jsonMode,
		)

		fmt.Fprintln(
			stderr,
			cliMessage("cli.error_prefix"),
			localizedCLIErrorMessage(err, code),
		)

		return code
	}

	if jsonMode {
		if envelopeErr := writeCLIJSONError(
			stdout,
			err,
			code,
		); envelopeErr != nil {
			fmt.Fprintln(
				stderr,
				cliMessage("cli.json_error_failure"),
				envelopeErr,
			)
		}

		return code
	}

	fmt.Fprintln(
		stderr,
		cliMessage("cli.error_prefix"),
		localizedCLIErrorMessage(err, code),
	)

	return code
}

func localizedCLIErrorMessage(err error, code int) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if localeDiagnosticMatchesActiveLocale(message) {
		return message
	}
	machineCode := classifyCLIErrorCode(err, code)
	var localized string
	switch code {
	case ExitInvalid:
		localized = cliMessage("cli.localized_error.invalid", machineCode)
	case ExitConfig:
		localized = cliMessage("cli.localized_error.config", machineCode)
	case ExitInternal:
		localized = cliMessage("cli.localized_error.internal", machineCode)
	default:
		localized = cliMessage("cli.localized_error.command", machineCode)
	}
	if facts := textassets.DiagnosticFacts(message); facts != "" {
		localized += " " + cliMessage("cli.localized_error.preserved_facts", facts)
	}
	return localized
}

func localizedCLIDiagnostics(data []byte) []byte {
	if localeDiagnosticMatchesActiveLocale(string(data)) {
		return data
	}
	message := cliMessage("cli.localized_error.diagnostic_suppressed")
	if facts := textassets.DiagnosticFacts(string(data)); facts != "" {
		message += " " + cliMessage("cli.localized_error.preserved_facts", facts)
	}
	return []byte(message + "\n")
}

func containsHanText(value string) bool {
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

func containsASCIILetter(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
			return true
		}
	}
	return false
}

func localeDiagnosticMatchesActiveLocale(detail string) bool {
	switch textassets.ActiveLocale() {
	case textassets.DefaultLocale:
		return !containsHanText(detail)
	case textassets.LegacyLocale:
		return containsHanText(detail) || !containsASCIILetter(detail)
	default:
		return false
	}
}

func localeSafeCLIDetail(detail string) string {
	if !localeDiagnosticMatchesActiveLocale(detail) {
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			return cliMessage("cli.localized_detail_with_facts", facts)
		}
		return cliMessage("cli.localized_detail_unavailable")
	}
	return detail
}

// executionExitCode提取稳定退出码。
func executionExitCode(
	err error,
) int {
	var coded exitCoder

	if errors.As(
		err,
		&coded,
	) {
		return coded.ExitCode()
	}

	return ExitDrift
}

// isSilentReportedError识别已由命令输出完整业务报告的ExitError。
func isSilentReportedError(
	err error,
) bool {
	var exitErr *ExitError

	return errors.As(
		err,
		&exitErr,
	) &&
		exitErr.Msg == "" &&
		exitErr.Err == nil
}

func writeBytes(
	writer io.Writer,
	data []byte,
) {
	if len(data) == 0 {
		return
	}

	_, _ = writer.Write(
		data,
	)
}
