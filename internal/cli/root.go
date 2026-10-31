// Package cli is the command-line entry and command-tree assembly of aoci-code.
// Index entry: root.go[CRT9M]
//
// Command registration runs in a "two generations coexist" mode:
//
//	first generation: each command file registers itself through registerCommand in init;
//	second generation: ai.go and doctor.go are mounted explicitly by newRootCmd.
//
// Exit code contract:
//   - 0: success;
//   - 1: drift or an ordinary check failure;
//   - 2: invalid input, index, or governance state;
//   - 3: not initialized, or a configuration or argument error;
//   - 10: internal error.
//
// Output discipline:
//   - the MCP subcommand's stdout is the JSON-RPC protocol stream and must pass through directly, never buffered or wrapped;
//   - under --json, when an ordinary CLI command fails before any business JSON was written, the root layer writes the single error envelope;
//   - when Verify/Check and the like have already written complete business JSON before returning non-zero, no second JSON object is appended;
//   - on Windows, a legal --json result of an ordinary CLI command is converted to ASCII-safe JSON at the final write, so that
//     PowerShell 5 capturing UTF-8 Chinese through the local code page cannot break the JSON;
//   - human-readable errors keep going to stderr.
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/aoci-spec/aoci-code/internal/buildinfo"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The version information is injected into internal/buildinfo by the build through -ldflags; this is only an alias,
// so the command tree and the embeddable engine report one identity.
var (
	version   = buildinfo.Version
	commit    = buildinfo.Commit
	buildDate = buildinfo.BuildDate
)

// Exit code contract constants.
const (
	ExitOK       = 0
	ExitDrift    = 1
	ExitInvalid  = 2
	ExitConfig   = 3
	ExitInternal = 10
)

// Global persistent flags.
var (
	flagRepo  string
	flagJSON  bool
	flagQuiet bool
)

// ExitError carries the business exit code.
//
// MachineCode is the optional domain-level machine classification, such as write_conflict. It does not change the process
// exit code; it only lets a caller that already knows the underlying failure category pass the exact error semantics through to JSON consumers.
// Without MachineCode the existing drift/invalid/config/internal mapping by Code still applies.
//
// Msg and Err both empty means the command already wrote its complete business report; the root layer only returns the exit code and must not write again.
type ExitError struct {
	Code        int
	MachineCode string
	Err         error
	Msg         string
	Details     any
}

// Error implements the error interface.
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

// Unwrap exposes the wrapped cause so errors.As can reach a typed failure such
// as a Safe Inventory git query error; the exit mapping itself is unchanged.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode implements the exitCoder interface.
func (e *ExitError) ExitCode() int {
	return e.Code
}

// exitCoder is implemented by errors that carry an exit code.
type exitCoder interface {
	ExitCode() int
}

// subCommands is the first-generation self-registration slot.
var subCommands []*cobra.Command

// registerCommand lets each command file register itself in init.
func registerCommand(
	command *cobra.Command,
) {
	subCommands = append(
		subCommands,
		command,
	)
}

// resolveRepoRoot locates the repository root.
func resolveRepoRoot() (string, error) {
	return config.FindRepoRoot(
		".",
		flagRepo,
	)
}

// newRootCmd assembles the root command.
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
		newDoctorCmd(),
	)

	root.AddCommand(
		newUICmd(),
	)

	groupCommands(root, rootCommandGroups)
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	initializeCobraFlags(root)
	root.SetUsageTemplate(localizedUsageTemplate())
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
	// Every pending receipt must allow the command: with a layout receipt
	// beside an MCP write receipt, the narrower gate wins.
	allowed := true
	for _, transaction := range pending {
		if !recoveryGateAllows(transaction.Operation, path) {
			allowed = false
		}
	}
	if allowed {
		return nil
	}
	mcpReceiptsOnly := true
	for _, transaction := range pending {
		switch transaction.Operation {
		case "remove", "entries", "header", cognitiontxn.OperationUnknown:
		default:
			mcpReceiptsOnly = false
		}
	}
	if mcpReceiptsOnly {
		names := make([]string, 0, len(pending))
		for _, transaction := range pending {
			names = append(names, transaction.Filename)
		}
		return &ExitError{
			Code: ExitInvalid, MachineCode: "cognition_recovery_pending",
			Msg: cliMessage("cognition.transaction.pending_gate_mcp", strings.Join(names, ",")),
		}
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

// Execute is the binary's process entry.
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

// executeCLI runs one complete CLI invocation and returns the process exit code.
//
// Ordinary commands use an in-memory buffer so that, before a failure, it can be judged whether complete business JSON was already written.
// MCP must pass through directly, so the long connection and JSON-RPC frame timing are not broken.
func executeCLI(
	args []string,
	stdout,
	stderr io.Writer,
) int {
	return executeCLIWithInput(args, os.Stdin, stdout, stderr)
}

// executeCLIWithInput is executeCLI with an explicit stdin. The process entry
// point passes os.Stdin, which is what Cobra falls back to anyway; an
// in-process caller can hand a command the request it would otherwise read
// from the process stdin.
func executeCLIWithInput(
	args []string,
	stdin io.Reader,
	stdout,
	stderr io.Writer,
) int {
	resetRootFlags()
	// The reset runs again on the way out: a real process exits here, but the
	// library entry point is reused by tests and embedders, and --repo pointing
	// at a directory a finished test already deleted must not leak into the
	// next direct command execution.
	defer resetRootFlags()
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
		resetCommandFlags(root)

		root.SetArgs(
			args,
		)
		root.SetIn(
			stdin,
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
	resetCommandFlags(root)

	root.SetArgs(
		args,
	)
	root.SetIn(
		stdin,
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
		command, _ := rootCommandToken(args)
		hookInvocation := command == "hook"
		if root, err := config.FindRepoRoot(".", explicitRepo); err == nil {
			var cfg *config.Config
			var loadErr error
			if isCognitionPlannerInvocation(args) || hookInvocation {
				cfg, loadErr = config.LoadReadOnly(root)
			} else {
				cfg, loadErr = config.Load(root)
			}
			if loadErr != nil {
				// Locale selection must neither migrate configuration nor prevent
				// a hook from reaching its own fail-open or refresh-context path.
				if hookInvocation {
					return textassets.SetActiveLocale(textassets.DefaultLocale)
				}
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

// resetCommandFlags returns every registered command to the state a fresh
// process would see: flags an earlier in-process execution changed go back to
// their defaults, and an output writer a test set on the command itself is
// cleared so output reaches the executor's writers again. Commands are
// registered once and attached to each new root, so without this a
// `scan --force` in one test made the next test's plain `scan` a forced one,
// and a buffer one test installed on a command swallowed the next test's
// JSON. A real process runs one command; here this is a no-op.
func resetCommandFlags(root *cobra.Command) {
	for _, child := range root.Commands() {
		resetCommandState(child)
	}
}

func resetCommandState(command *cobra.Command) {
	command.SetOut(nil)
	command.SetErr(nil)
	for _, set := range []*pflag.FlagSet{command.Flags(), command.PersistentFlags()} {
		set.VisitAll(func(flag *pflag.Flag) {
			if !flag.Changed {
				return
			}
			if slice, ok := flag.Value.(pflag.SliceValue); ok {
				_ = slice.Replace(nil)
			} else {
				_ = flag.Value.Set(flag.DefValue)
			}
			flag.Changed = false
		})
	}
	for _, child := range command.Commands() {
		resetCommandState(child)
	}
}

// resetRootFlags makes repeated executions in tests match the semantics of real independent processes.
func resetRootFlags() {
	flagRepo = ""
	flagJSON = false
	flagQuiet = false
}

// requestedJSON recognizes an explicit JSON request before Cobra finishes parsing.
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

// isMCPInvocation enables JSON-RPC direct pass-through only when the top-level command token is mcp.
//
// An ordinary argument value may legally equal mcp too, such as `--agent mcp`. So the arguments must not be scanned for
// any mcp string; the root persistent flags are excluded first, and then the single top-level command token is read.
func isMCPInvocation(
	args []string,
) bool {
	command, found := rootCommandToken(
		args,
	)

	return found &&
		command == "mcp"
}

// rootCommandToken extracts the top-level command token before Cobra parses.
//
// It recognizes only the root persistent flags that may legally appear before the top-level command. On an unknown flag it returns
// unrecognized, so the invocation enters ordinary buffered execution and Cobra returns the standard argument error, instead of
// straying into the MCP protocol pass-through path.
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

// finishDirectExecution handles execution paths such as MCP that cannot be buffered.
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
	writeGitQueryDetail(stderr, err)

	return code
}

// finishBufferedExecution decides the error output form by whether stdout already holds a business report.
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

	// Content already on stdout means the command wrote its business report, or it is still a human-readable command.
	// To avoid producing two JSON objects, the original output is kept and the error keeps going to stderr.
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
		writeGitQueryDetail(stderr, err)

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
		writeGitQueryDetail(stderr, err)

		return code
	}

	fmt.Fprintln(
		stderr,
		cliMessage("cli.error_prefix"),
		localizedCLIErrorMessage(err, code),
	)
	writeGitQueryDetail(stderr, err)

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

// executionExitCode extracts the stable exit code.
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

// isSilentReportedError recognizes an ExitError whose command already wrote the complete business report.
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

// recoveryGateAllows names the commands one pending receipt kind leaves open:
// always the read-only Guide and the MCP server, then the kind's own status,
// resume, and rollback controls. MCP write receipts close through the MCP
// tools the Guide names (a foreign file, by hand), so their read-only surfaces
// and the CLI closures stay open while other writes wait. A Legacy Header
// receipt has no closure in this release (0.1.x wrote it and 0.1.x finishes
// it), so only its read surface stays open.
func recoveryGateAllows(operation, path string) bool {
	if path == "aoci index agent guide" || path == "aoci mcp" {
		return true
	}
	switch operation {
	case "bootstrap":
		return path == "aoci cognition bootstrap status" || path == "aoci cognition bootstrap resume" || path == "aoci cognition bootstrap rollback"
	case "migration":
		return path == "aoci cognition migration status" || path == "aoci cognition migration resume" || path == "aoci cognition migration rollback"
	case "reversal":
		return path == "aoci cognition migration reversal status" || path == "aoci cognition migration reversal resume"
	case "scope":
		return path == "aoci baseline scope status" || path == "aoci baseline scope resume" ||
			path == "aoci scope status" || path == "aoci scope resume" || path == "aoci scope rollback"
	case "remove", "entries", "header", cognitiontxn.OperationUnknown:
		if path == "aoci verify" || path == "aoci check" || path == "aoci status" || path == "aoci scope status" {
			return true
		}
		switch operation {
		case "remove":
			return path == "aoci remove-entry"
		case "entries":
			return path == "aoci update-entry"
		case "header":
			return path == "aoci index header show"
		}
	}
	return false
}
