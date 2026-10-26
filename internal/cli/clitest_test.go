package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/testkit"
)

// runCLI executes one in-process aoci invocation against root the way a
// separate process would see it: --repo root is prepended, every root and
// command flag starts from its default, the locale follows the repository
// configuration or the init --locale argument, and nothing stays behind for
// the next invocation. The two streams come back apart with the exit code.
func runCLI(t testing.TB, root string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = executeCLI(append([]string{"--repo", root}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
}

// mustCLI is runCLI for an invocation that has to succeed; it fails the test
// with both streams on any other exit code and returns stdout.
func mustCLI(t testing.TB, root string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, root, args...)
	if code != ExitOK {
		t.Fatalf("aoci %s: exit %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

// runCLIBare is runCLI without --repo, for the cases whose subject is the
// repository root lookup itself.
func runCLIBare(t testing.TB, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = executeCLI(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

// exitError turns one finished invocation into the error the older helpers
// returned: nil for ExitOK, otherwise an *ExitError carrying the exit code
// and the diagnostics as its message, so an assertion on the code or on the
// message text keeps its shape.
func exitError(diagnostics string, code int) error {
	if code == ExitOK {
		return nil
	}
	return &ExitError{Code: code, Msg: strings.TrimSpace(diagnostics)}
}

// runCLIPrinted is runCLI with the process stdout captured as well and
// returned first: init, scan, hook and remove-entry print their human lines
// with fmt.Println, which no command writer sees.
func runCLIPrinted(t testing.TB, root string, args ...string) (printed, stdout, stderr string, code int) {
	t.Helper()
	printed = testkit.CaptureStdout(t, func() {
		stdout, stderr, code = runCLI(t, root, args...)
	})
	return printed, stdout, stderr, code
}

// runCLIStdin is runCLI with stdin delivering from reader, for the commands
// that read their request from the process stdin.
func runCLIStdin(t testing.TB, root string, stdin io.Reader, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = executeCLIWithInput(append([]string{"--repo", root}, args...), stdin, &out, &errOut)
	return out.String(), errOut.String(), code
}
