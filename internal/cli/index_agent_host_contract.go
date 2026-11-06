// The runtime command binding, Windows invocation discipline, and Help contract of the Host Agent Guide.
//
// Design boundary:
//   - buildAgentGuide stays a deterministic domain constructor independent of the machine's location;
//   - the current aoci binary's absolute path and the repository root (--repo) are bound only right before the Guide is really printed, so a command runs from any working directory;
//   - Windows commands use the PowerShell call operator & and double quotes;
//   - the ordinary CLI's --json output on Windows is ASCII-safe JSON that PowerShell 5 can capture directly;
//   - POSIX commands use single quotes to protect spaces and shell metacharacters;
//   - the Stage Help and the Guide disclose the real sizes, batch caps, and phase behaviour of the protocol code;
//   - stable natural language is loaded from textassets by language-independent ID, and Go injects only machine facts.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// agentRepositoryRootArgument quotes the repository root a bound command names
// with --repo, under the executable's own rules: absolute, free of control
// characters, and quotable for the target shell. The Host's shell need not
// stand in the repository (a session often starts above or beside it), so a
// command that does not name its repository resolves the wrong tree or none.
func agentRepositoryRootArgument(
	goos,
	root string,
) (string, error) {
	root = normalizeAgentExecutablePath(root)
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s", cliMessage("host.repository_root.empty"))
	}
	if strings.ContainsAny(root, "\x00\r\n") {
		return "", fmt.Errorf("%s", cliMessage("host.repository_root.control_character"))
	}
	if !isAbsoluteAgentExecutablePath(goos, root) {
		return "", fmt.Errorf("%s", cliMessage("host.repository_root.not_absolute", root))
	}
	if goos == "windows" {
		if strings.ContainsAny(root, "\"`$") {
			return "", fmt.Errorf("%s", cliMessage("host.repository_root.powershell_unsafe"))
		}
		return `"` + root + `"`, nil
	}
	return "'" + strings.ReplaceAll(root, "'", `'"'"'`) + "'", nil
}

// agentCommandPrefixFor converts a validated absolute executable path and the
// repository root into a target-shell command prefix: the quoted executable
// followed by --repo and the quoted root.
//
// PowerShell interpolation characters are rejected because this contract uses
// double-quoted invocation paths. POSIX paths are protected with single quotes.
func agentCommandPrefixFor(
	goos,
	executable,
	root string,
) (string, error) {
	rootArgument, err := agentRepositoryRootArgument(goos, root)
	if err != nil {
		return "", err
	}
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

		return `& "` + executable + `" --repo ` + rootArgument, nil
	}

	return "'" +
		strings.ReplaceAll(
			executable,
			"'",
			`'"'"'`,
		) +
		"' --repo " + rootArgument, nil
}

// bindAgentCommand binds a bare domain-layer aoci command into the absolute execution command of the current machine.
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

// bindAgentGuideCommands binds every machine command in the Guide.
func bindAgentGuideCommands(
	commands *agentGuideCommands,
	prefix string,
) {
	if commands == nil {
		return
	}

	fields := []*string{
		&commands.Guide,
		&commands.Onboard,
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
		&commands.ScopeActivate,
		&commands.ScopeAcknowledge,
		&commands.ScopeBudget,
		&commands.DatabaseSnapshot,
		&commands.DatabaseBaselineAccept,
	}

	for _, field := range fields {
		*field = bindAgentCommand(
			*field,
			prefix,
		)
	}
}

// finalizeVolumeAgentGuideRuntimeContract binds every executable CLI command
// in a Volumes guide to the currently running binary and to the repository
// the Guide was computed for. MCP tool actions remain machine next-actions
// and are not represented as shell commands here.
func finalizeVolumeAgentGuideRuntimeContract(
	guide *volumeAgentGuide,
	root string,
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
		root,
	)
}

func finalizeVolumeAgentGuideRuntimeContractFor(
	guide *volumeAgentGuide,
	goos,
	executable,
	root string,
) error {
	if guide == nil {
		return nil
	}

	prefix, err := agentCommandPrefixFor(goos, executable, root)
	if err != nil {
		return fmt.Errorf("%s", cliMessage(
			"guide.executable_bind_failed",
			localeSafeCLIDetail(err.Error()),
		))
	}

	bindAgentGuideCommands(&guide.Commands, prefix)
	return nil
}
