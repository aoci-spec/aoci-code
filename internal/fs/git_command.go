// Git command builder for untrusted repositories: the one channel through which the program reads a target repository's Git facts
// Index entry: git_command.go[CG8T]
//
// Threat: a scanned repository ships its own .git/config, and core.fsmonitor there makes git run an
// arbitrary program while it traverses the work tree (ls-files and the like), which is command execution
// inside the AOCI process. Reproduced on git 2.53.0: without hardening, one ls-files ran the configured
// program. A command-line -c outranks the repository configuration file, so every call explicitly
// disables the program entry points the repository controls before the caller's arguments are appended.
//
// Boundary: only the "repository-supplied configuration" source is neutralized here; the caller's environment
// variables belong to the operator's own trust domain and are not overridden. GIT_OPTIONAL_LOCKS=0 keeps read-only calls from writing the .git index lock.
package fs

import (
	"os"
	"os/exec"
)

// UntrustedRepositoryGitCommand builds one read-only git invocation against the root repository.
// args follow the safety arguments directly; the caller may append its own -c options and subcommand.
func UntrustedRepositoryGitCommand(root string, args ...string) *exec.Cmd {
	hardened := []string{
		"-C", root,
		// The program entry points a repository configuration can name, disabled one by one.
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.pager=",
	}
	command := exec.Command("git", append(hardened, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	hideChildConsole(command)
	return command
}
