// CLI 输入必须有界。
//
// 真实经历: hook.go 与 update_entry.go 都在 io.ReadAll(os.Stdin) 上无界读取,
// 而同一个仓库的 index_agent_stage_protocol.go 与 index_agent_curation_protocol.go
// 早就是 LimitReader(max+1) 的。两条 CLI 通道漏掉了同一条纪律。
//
// 边界本身是判据: 恰好 max 必须通过, max+1 必须判超限 —— 少读一字节这两者
// 就分不开。hook 超限放行, update-entry 超限报错: 同一个读法, 两种上层裁决。
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
	if _, oversize, err := readLimitedInput(failingReader{}, 1024); err == nil || oversize {
		t.Fatalf("a reader failure is neither success nor oversize: oversize=%v err=%v", oversize, err)
	}
}

// hook infrastructure fails open: an oversize payload is discarded and the
// write is allowed through, never buffered and never blocking the workflow.
func TestHookFailsOpenOnOversizeStdin(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	command := newHookCmd()
	command.SilenceUsage, command.SilenceErrors = true, true
	command.SetIn(strings.NewReader(`{"tool_name":"Edit","tool_input":{"file_path":"a.go"}}` +
		strings.Repeat(" ", int(hookInputMaxBytes)+1)))
	command.SetOut(io.Discard)
	command.SetArgs([]string{"pretool", "--agent", "claude", "--stdin-json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("hook must fail open on oversize stdin, got %v", err)
	}
}

func TestUpdateEntryRejectsEntryAndStdinTogether(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	command := newUpdateEntryCmd()
	command.SilenceUsage, command.SilenceErrors = true, true
	command.SetIn(strings.NewReader("a.go[XUT5T]: F:from stdin | R:- | A:- | S:-"))
	command.SetArgs([]string{"--path", "a.go", "--stdin",
		"--entry", "a.go[XUT5T]: F:from flag | R:- | A:- | S:-"})
	err := command.Execute()
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitConfig {
		t.Fatalf("two input modes at once must fail before any write, got %v", err)
	}
}

func TestUpdateEntryRejectsOversizeStdinBeforeAnyWrite(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	before, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })

	command := newUpdateEntryCmd()
	command.SilenceUsage, command.SilenceErrors = true, true
	command.SetIn(&countingReader{remaining: machinecontract.EntriesRequestMaxBytes + 1})
	command.SetArgs([]string{"--path", "a.go", "--stdin"})
	execErr := command.Execute()
	var exit *ExitError
	if !errors.As(execErr, &exit) || exit.Code != ExitConfig {
		t.Fatalf("oversize stdin must be refused as a configuration error, got %v", execErr)
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a refused oversize input must leave the formal Index untouched")
	}
}
