// Git ignore-status probe: the one channel that answers "is this path hidden by Git's ignore authority"
// Index entry: git_ignore.go[CG7T]
//
// Why it exists: Safe Inventory takes its file listing from Git, so an ignored file never enters the
// Baseline at all. When the ignored file is a formal cognition asset, the consequence is not one missing
// file but a Volume that cannot be enrolled and a Guide blocked throughout, far from the cause (one
// ignore rule somewhere). This turns "ignored, and by which rule" into a fact that can be answered directly.
package fs

import (
	"bytes"
	"os/exec"
	"strings"
)

// PathIgnoredByGit answers whether rel is hidden by Git's ignore authority and names the matching rule's source.
//
// One call covers all three sources, .gitignore, .git/info/exclude, and core.excludesFile, which is
// exactly why Git must be asked instead of parsing .gitignore here.
//
// Two semantic points:
//   - A tracked file always answers false. git check-ignore reports no ignore for a tracked path, which
//     matches what AOCI needs: a tracked file always enters the listing, and ignore rules do not apply to it.
//   - --no-index is not passed. With it a tracked file would also read as ignored, turning the immune case into a false fault.
//
// When the Git facts cannot be obtained it answers (false, ""): better to report nothing than to accuse a rule because of an environment problem.
func PathIgnoredByGit(root, rel string) (ignored bool, matchedRule string) {
	command := UntrustedRepositoryGitCommand(root,
		"-c", "core.quotepath=false", "check-ignore", "-v", "--", rel)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = nil
	if err := command.Run(); err != nil {
		// Exit code 1 is the ordinary "not ignored" answer, not a failure.
		var exitError *exec.ExitError
		if !asExitError(err, &exitError) || exitError.ExitCode() != 1 {
			return false, ""
		}
		return false, ""
	}
	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return false, ""
	}
	// -v prints "<source>:<line>:<pattern>\t<path>". The rule's source is worth more to the operator
	// than the pattern itself: it names the file to change.
	if tab := strings.IndexByte(line, '\t'); tab > 0 {
		line = line[:tab]
	}
	return true, line
}

func asExitError(err error, target **exec.ExitError) bool {
	value, ok := err.(*exec.ExitError)
	if ok {
		*target = value
	}
	return ok
}
