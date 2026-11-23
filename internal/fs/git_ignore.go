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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// PathIgnoredByGit answers whether rel is hidden by Git's ignore authority and names the matching rule's source.
//
// One call covers all three sources, .gitignore, .git/info/exclude, and core.excludesFile, which is
// exactly why Git must be asked instead of parsing .gitignore here.
//
// Three semantic points:
//   - A tracked file always answers false. git check-ignore reports no ignore for a tracked path, which
//     matches what AOCI needs: a tracked file always enters the listing, and ignore rules do not apply to it.
//   - --no-index is not passed. With it a tracked file would also read as ignored, turning the immune case into a false fault.
//   - Verbose check-ignore also reports a matching negative rule with exit code 0; that rule makes the path visible.
//
// When the Git facts cannot be obtained it answers (false, ""): better to report nothing than to accuse a rule because of an environment problem.
func PathIgnoredByGit(root, rel string) (ignored bool, matchedRule string) {
	// Only the root's own repository decides, as the Safe Inventory's Git
	// listing does: in a root without .git, git would climb to an enclosing
	// repository and apply its rules to files scan never filters by them
	// (audit G4-17).
	if _, err := os.Lstat(filepath.Join(root, ".git")); err != nil {
		return false, ""
	}
	command := UntrustedRepositoryGitCommand(root,
		"check-ignore", "-v", "-z", "--stdin")
	command.Stdin = strings.NewReader(rel + "\x00")
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
	// -v -z separates source, line, pattern, and path with NULs and never quotes
	// their contents. Colons in a source path and tabs in a pattern cannot hide
	// the negative-rule marker or corrupt the reported rule source.
	fields := strings.Split(stdout.String(), "\x00")
	if len(fields) != 5 || fields[4] != "" || fields[2] == "" || strings.HasPrefix(fields[2], "!") {
		return false, ""
	}
	// The rule source is printed for a person: -z leaves its bytes raw, so a
	// control character in an excludes-file path is escaped the way Git's own
	// quoted output would show it rather than breaking or overwriting the line.
	return true, EscapeControlChars(strings.Join(fields[:3], ":"))
}

// EscapeControlChars renders control characters as Go escapes (\n, \r, \x01)
// and leaves every other character, a tab included, as is, for text shown on
// a terminal: a tab cannot break or overwrite the line.
func EscapeControlChars(text string) string {
	if strings.IndexFunc(text, lineBreakingControl) < 0 {
		return text
	}
	var out strings.Builder
	for _, r := range text {
		if lineBreakingControl(r) {
			quoted := strconv.QuoteRune(r)
			out.WriteString(quoted[1 : len(quoted)-1])
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func lineBreakingControl(r rune) bool {
	return r != '\t' && unicode.IsControl(r)
}

func asExitError(err error, target **exec.ExitError) bool {
	value, ok := err.(*exec.ExitError)
	if ok {
		*target = value
	}
	return ok
}
