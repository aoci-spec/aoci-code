package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// writeGitQueryDetail prints the tail of git's own stderr after a Safe
// Inventory git failure. The machine code names the step that failed; git's
// text names the reason (a killed process, a wall of "Filename too long", a
// corrupt repository). Until #97 that text was discarded and the operator had
// to shim git.exe to see it. It goes to stderr in every output mode because it
// is a diagnostic, never business output.
func writeGitQueryDetail(stderr io.Writer, err error) {
	var query *afs.GitQueryError
	if !errors.As(err, &query) || strings.TrimSpace(query.Stderr) == "" {
		return
	}
	fmt.Fprintln(stderr, cliMessage("cli.git_stderr_tail", query.Stderr))
}

// causedError keeps a localized message as the error text while leaving the
// underlying cause reachable through errors.As, so writeGitQueryDetail can
// find a Safe Inventory git failure behind a command-level message. The text,
// and therefore every exit code and envelope derived from it, is unchanged.
type causedError struct {
	message string
	cause   error
}

func (e *causedError) Error() string { return e.message }
func (e *causedError) Unwrap() error { return e.cause }

func withCause(message string, cause error) error {
	return &causedError{message: message, cause: cause}
}
