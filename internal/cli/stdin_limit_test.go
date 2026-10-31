// CLI input must be bounded.
//
// A real experience: hook.go and update_entry.go both read without bound on io.ReadAll(os.Stdin),
// while index_agent_stage_protocol.go and index_agent_curation_protocol.go in the same repository
// were LimitReader(max+1) long ago. The two CLI channels missed the same discipline.
//
// The boundary itself is the criterion: exactly max must pass and max+1 must be judged oversize; one byte less
// and the two cannot be told apart. The hook passes on oversize, update-entry errors: one reading, two upper-layer verdicts.
package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestReadLimitedInputAcceptsExactlyTheLimit(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("a"), 1024)
	data, oversize, err := readLimitedInput(bytes.NewReader(body), 1024)
	if err != nil || oversize {
		t.Fatalf("exactly the limit must be accepted: oversize=%v err=%v", oversize, err)
	}
	if !bytes.Equal(data, body) {
		t.Fatal("accepted input must be returned whole")
	}
}

func TestReadLimitedInputRejectsOneByteOverTheLimit(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("a"), 1025)
	data, oversize, err := readLimitedInput(bytes.NewReader(body), 1024)
	if err != nil || !oversize {
		t.Fatalf("one byte over the limit must be reported oversize: oversize=%v err=%v", oversize, err)
	}
	if data != nil {
		t.Fatal("oversize input must not be handed to the caller")
	}
}

// countingReader records how much of the stream was actually consumed.
type countingReader struct {
	remaining int
	read      int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		p[i] = 'a'
	}
	r.remaining -= n
	r.read += n
	return n, nil
}

func TestReadLimitedInputStopsAtTheLimitInsteadOfDrainingTheStream(t *testing.T) {
	t.Parallel()
	reader := &countingReader{remaining: 8 << 20}
	if _, oversize, err := readLimitedInput(reader, 1024); err != nil || !oversize {
		t.Fatalf("oversize=%v err=%v", oversize, err)
	}
	if reader.read > 1025 {
		t.Fatalf("a bounded read must stop at the limit, consumed %d bytes of an 8 MiB stream", reader.read)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin exploded") }

func TestReadLimitedInputPropagatesReaderFailure(t *testing.T) {
	t.Parallel()
	if _, oversize, err := readLimitedInput(failingReader{}, 1024); err == nil || oversize {
		t.Fatalf("a reader failure is neither success nor oversize: oversize=%v err=%v", oversize, err)
	}
}

// hook infrastructure fails open: an oversize payload is discarded and the
// write is allowed through, never buffered and never blocking the workflow.
func TestHookFailsOpenOnOversizeStdin(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	payload := strings.NewReader(`{"tool_name":"Edit","tool_input":{"file_path":"a.go"}}` +
		strings.Repeat(" ", int(hookInputMaxBytes)+1))
	_, stderr, code := runCLIStdin(t, root, payload, "--quiet", "hook", "pretool", "--agent", "claude", "--stdin-json")
	if code != ExitOK {
		t.Fatalf("hook must fail open on oversize stdin, got exit %d: %s", code, stderr)
	}
}

func TestUpdateEntryRejectsEntryAndStdinTogether(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	_, stderr, code := runCLIStdin(t, root, strings.NewReader("a.go[XUT5T]: F:from stdin | R:- | A:- | S:-"),
		"--quiet", "update-entry", "--path", "a.go", "--stdin",
		"--entry", "a.go[XUT5T]: F:from flag | R:- | A:- | S:-")
	if code != ExitConfig {
		t.Fatalf("two input modes at once must fail before any write, got exit %d: %s", code, stderr)
	}
}

func TestUpdateEntryRejectsOversizeStdinBeforeAnyWrite(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	before, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runCLIStdin(t, root, &countingReader{remaining: machinecontract.EntriesRequestMaxBytes + 1},
		"--quiet", "update-entry", "--path", "a.go", "--stdin")
	if code != ExitConfig {
		t.Fatalf("oversize stdin must be refused as a configuration error, got exit %d: %s", code, stderr)
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a refused oversize input must leave the formal Index untouched")
	}
}
