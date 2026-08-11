package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

type failingInputReader struct{}

func (failingInputReader) Read([]byte) (int, error) { return 0, errors.New("test reader failed") }

type countedInputReader struct {
	remaining int64
	read      int64
}

func (reader *countedInputReader) Read(buffer []byte) (int, error) {
	if reader.remaining == 0 {
		return 0, io.EOF
	}
	count := int64(len(buffer))
	if count > reader.remaining {
		count = reader.remaining
	}
	for index := int64(0); index < count; index++ {
		buffer[index] = 'x'
	}
	reader.remaining -= count
	reader.read += count
	return int(count), nil
}

func TestReadLimitedInputExactOversizeFailureAndBoundedConsumption(t *testing.T) {
	const limit = int64(32)
	exact := bytes.Repeat([]byte("x"), int(limit))
	data, tooLarge, err := readLimitedInput(bytes.NewReader(exact), limit)
	if err != nil || tooLarge || !bytes.Equal(data, exact) {
		t.Fatalf("exact input rejected: len=%d too_large=%v err=%v", len(data), tooLarge, err)
	}
	reader := &countedInputReader{remaining: limit + 1024}
	data, tooLarge, err = readLimitedInput(reader, limit)
	if err != nil || !tooLarge || data != nil || reader.read != limit+1 {
		t.Fatalf("oversize read was not bounded to max+1: len=%d read=%d too_large=%v err=%v", len(data), reader.read, tooLarge, err)
	}
	if _, _, err := readLimitedInput(failingInputReader{}, limit); err == nil {
		t.Fatal("reader failure was discarded")
	}
}

func governedCLIState(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, rel := range []string{"aoci.txt", ".aoci/baseline.json", ".aoci/transactions", ".aoci/recovery"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result[rel] = "missing"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result[rel] = string(data)
			continue
		}
		if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}
			key := filepath.ToSlash(relative)
			if entry.IsDir() {
				result[key] = "directory"
				return nil
			}
			data, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			result[key] = string(data)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func runStdinUpdateEntry(t *testing.T, root string, reader io.Reader, arguments ...string) error {
	t.Helper()
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })
	command := newUpdateEntryCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetIn(reader)
	command.SetArgs(arguments)
	return command.Execute()
}

func TestUpdateEntryStdinBoundariesAreExitConfigAndFormalZeroWrite(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	before := governedCLIState(t, root)
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		if after := governedCLIState(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("input rejection changed Index, Baseline, transaction, or recovery state\nbefore=%v\nafter=%v", before, after)
		}
	}

	t.Run("exact_limit_reaches_entry_validation", func(t *testing.T) {
		input := bytes.Repeat([]byte("x"), machinecontract.EntriesRequestMaxBytes)
		err := runStdinUpdateEntry(t, root, bytes.NewReader(input), "--path", "a.go", "--stdin", "--preview")
		tooLargeMessage := cliMessage("cli.update_entry.stdin_too_large", machinecontract.EntriesRequestMaxBytes)
		if err == nil || errors.Is(err, io.EOF) || strings.Contains(err.Error(), tooLargeMessage) {
			t.Fatalf("exact-limit input did not reach ordinary Entry validation: %v", err)
		}
		assertUnchanged(t)
	})

	t.Run("limit_plus_one", func(t *testing.T) {
		reader := &countedInputReader{remaining: machinecontract.EntriesRequestMaxBytes + 4096}
		err := runStdinUpdateEntry(t, root, reader, "--path", "a.go", "--stdin", "--preview")
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitConfig ||
			reader.read != machinecontract.EntriesRequestMaxBytes+1 {
			t.Fatalf("oversize stdin contract mismatch: err=%v read=%d", err, reader.read)
		}
		assertUnchanged(t)
	})

	t.Run("reader_failure", func(t *testing.T) {
		err := runStdinUpdateEntry(t, root, failingInputReader{}, "--path", "a.go", "--stdin", "--preview")
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitConfig {
			t.Fatalf("reader failure exit contract mismatch: %v", err)
		}
		assertUnchanged(t)
	})

	t.Run("entry_and_stdin_conflict", func(t *testing.T) {
		err := runStdinUpdateEntry(t, root, failingInputReader{}, "--path", "a.go", "--entry", "ignored", "--stdin", "--preview")
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != ExitConfig {
			t.Fatalf("conflicting input modes were not rejected before read: %v", err)
		}
		assertUnchanged(t)
	})
}

func TestHookInputFailureAndOversizeRemainFailOpen(t *testing.T) {
	root := t.TempDir()
	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = oldRepo })
	for name, reader := range map[string]io.Reader{
		"reader_failure": failingInputReader{},
		"oversize":       &countedInputReader{remaining: hookInputMaxBytes + 4096},
	} {
		t.Run(name, func(t *testing.T) {
			command := newHookCmd()
			command.SilenceUsage = true
			command.SilenceErrors = true
			command.SetIn(reader)
			command.SetArgs([]string{"pretool", "--stdin-json"})
			if err := command.Execute(); err != nil {
				t.Fatalf("hook infrastructure failure did not fail open: %v", err)
			}
			if counted, ok := reader.(*countedInputReader); ok && counted.read != hookInputMaxBytes+1 {
				t.Fatalf("hook oversize read was not bounded: %d", counted.read)
			}
		})
	}
}
