//go:build linux

package fs

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withRenameat2ExchangeAnswer(t *testing.T, answer error) {
	t.Helper()
	previous := renameat2Exchange
	renameat2Exchange = func(_, _ string) error { return answer }
	t.Cleanup(func() { renameat2Exchange = previous })
}

// casTestNeighbours lists every name in the target's directory other than the
// target itself, so a test can assert that nothing at all survives — intent,
// swap, capture, or a staging temporary — rather than only the names a glob
// happens to know about.
func casTestNeighbours(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func casTestWriteTarget(t *testing.T, data []byte, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "asset.txt")
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAtomicWriteCASRefusesUnsupportedExchange(t *testing.T) {
	for _, refusal := range []error{unix.EINVAL, unix.ENOSYS, unix.EOPNOTSUPP} {
		t.Run(refusal.Error(), func(t *testing.T) {
			withRenameat2ExchangeAnswer(t, refusal)
			original := []byte("original\n")
			path := casTestWriteTarget(t, original, 0o640)
			err := AtomicWriteCAS(path, []byte("ours\n"), casTestDigest(original))
			if !errors.Is(err, ErrAtomicCASUnavailable) {
				t.Fatalf("unsupported exchange must fail closed: %v", err)
			}
			if casTestRead(t, path) != string(original) {
				t.Fatal("unsupported exchange changed target")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Fatal("target permission changed")
			}
			if names := casTestNeighbours(t, path); len(names) != 0 {
				t.Fatalf("unexpected artifacts: %v", names)
			}
		})
	}
}

func TestAtomicWriteCASUnsupportedExchangePreservesExternalReplacement(t *testing.T) {
	original := []byte("original\n")
	path := casTestWriteTarget(t, original, 0o600)
	previous := renameat2Exchange
	renameat2Exchange = func(first, second string) error {
		replacement := first + ".external"
		if err := os.WriteFile(replacement, []byte("external\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, first); err != nil {
			t.Fatal(err)
		}
		return unix.EINVAL
	}
	t.Cleanup(func() { renameat2Exchange = previous })
	err := AtomicWriteCAS(path, []byte("ours\n"), casTestDigest(original))
	if !errors.Is(err, ErrAtomicCASUnavailable) {
		t.Fatalf("expected refusal: %v", err)
	}
	if casTestRead(t, path) != "external\n" {
		t.Fatal("external replacement lost")
	}
	if names := casTestNeighbours(t, path); len(names) != 0 {
		t.Fatalf("unexpected artifacts: %v", names)
	}
}

// Existing capture/intent formats still converge on a filesystem providing
// native exchange. No new unsupported-filesystem write is authorised by recovery.
func TestAtomicWriteCASConvergesEachEmulationCrashState(t *testing.T) {
	original := []byte("original\n")
	ours := []byte("ours\n")
	next := []byte("next\n")
	foreign := []byte("foreign\n")
	later := []byte("later\n")
	cases := []struct {
		name          string
		target        []byte
		swap          []byte // nil means absent
		captureLink   bool   // capture is a hard link to the target, as the emulation makes it
		captureBytes  []byte // capture is its own inode holding these bytes
		expected      []byte // digest the next writer presents
		wantErr       error  // nil means the write succeeds
		wantNotErr    error
		wantMessage   string
		wantCanonical []byte
		wantSwap      []byte // bytes retained in the swap, when the state is kept for a human
		wantResidue   int
	}{
		{
			name: "write: crash between the link and the first rename", target: original, swap: ours,
			captureLink: true, expected: original, wantCanonical: next,
		},
		{
			name: "write: crash between the two renames", target: ours, captureBytes: original,
			expected: ours, wantCanonical: next,
		},
		{
			name: "write: crash between the two renames, then a stale expectation", target: ours,
			captureBytes: original, expected: original,
			wantErr: ErrAtomicCASConflict, wantNotErr: ErrAtomicCASRecovery, wantCanonical: ours,
		},
		{
			name: "write: crash between the two renames with a boundary preimage is rolled back", target: ours,
			captureBytes: foreign, expected: foreign, wantCanonical: next,
		},
		{
			name: "rollback: crash between the link and the first rename", target: ours, swap: foreign,
			captureLink: true, expected: original,
			wantErr: ErrAtomicCASConflict, wantNotErr: ErrAtomicCASRecovery, wantCanonical: foreign,
		},
		{
			name: "rollback: crash between the link and the first rename, then the current expectation", target: ours,
			swap: foreign, captureLink: true, expected: foreign, wantCanonical: next,
		},
		{
			name: "rollback: crash between the two renames", target: foreign, captureBytes: ours,
			expected: foreign, wantCanonical: next,
		},
		{
			name: "re-exchange: crash between the link and the first rename retains both versions", target: foreign,
			swap: later, captureLink: true, expected: original,
			wantErr: ErrAtomicCASRecovery, wantMessage: "计划外版本", wantCanonical: foreign, wantSwap: later, wantResidue: 2,
		},
		{
			name: "re-exchange: crash between the two renames retains both versions", target: later,
			captureBytes: foreign, expected: original,
			wantErr: ErrAtomicCASRecovery, wantMessage: "计划外版本", wantCanonical: later, wantSwap: foreign, wantResidue: 2,
		},
	}
	for _, flag := range []struct {
		name   string
		answer error // nil keeps the real primitive
	}{{name: "flag honoured"}} {
		t.Run(flag.name, func(t *testing.T) {
			if flag.answer != nil {
				withRenameat2ExchangeAnswer(t, flag.answer)
			} else {
				skipUnlessTheExchangeFlagIsHonoured(t)
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					path := casTestWriteTarget(t, tc.target, 0o644)
					swapPath := path + ".aoci-cas.swap"
					if tc.swap != nil {
						if err := os.WriteFile(swapPath, tc.swap, 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if tc.captureLink {
						if err := os.Link(path, atomicExchangeCapturePath(swapPath)); err != nil {
							t.Fatal(err)
						}
					}
					if tc.captureBytes != nil {
						if err := os.WriteFile(atomicExchangeCapturePath(swapPath), tc.captureBytes, 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(path+".aoci-cas.intent", casTestIntent(t, original, ours), 0o644); err != nil {
						t.Fatal(err)
					}

					err := AtomicWriteCAS(path, next, casTestDigest(tc.expected))
					if tc.wantErr == nil {
						if err != nil {
							t.Fatalf("write must proceed after convergence: %v", err)
						}
					} else if !errors.Is(err, tc.wantErr) {
						t.Fatalf("want %v, got %v", tc.wantErr, err)
					}
					if tc.wantNotErr != nil && errors.Is(err, tc.wantNotErr) {
						t.Fatalf("must not report %v: %v", tc.wantNotErr, err)
					}
					if tc.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), tc.wantMessage)) {
						t.Fatalf("want message %q in %v", tc.wantMessage, err)
					}
					if got := casTestRead(t, path); got != string(tc.wantCanonical) {
						t.Fatalf("canonical bytes = %q, want %q", got, tc.wantCanonical)
					}
					if tc.wantSwap != nil {
						if got := casTestRead(t, swapPath); got != string(tc.wantSwap) {
							t.Fatalf("retained swap bytes = %q, want %q", got, tc.wantSwap)
						}
					}
					if _, statErr := os.Lstat(atomicExchangeCapturePath(swapPath)); !errors.Is(statErr, os.ErrNotExist) {
						t.Fatalf("the capture must never survive recovery: %v", statErr)
					}
					if names := casTestNeighbours(t, path); len(names) != tc.wantResidue {
						t.Fatalf("residue = %v, want %d entries", names, tc.wantResidue)
					}
				})
			}
		})
	}
}

func TestAtomicWriteCASUsesThePrimitiveWhereTheFlagIsHonoured(t *testing.T) {
	skipUnlessTheExchangeFlagIsHonoured(t)
	original := []byte("original\n")
	path := casTestWriteTarget(t, original, 0o644)
	capture := atomicExchangeCapturePath(path + ".aoci-cas.swap")
	if err := os.WriteFile(capture, []byte("sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := renameat2Exchange
	var answers []error
	renameat2Exchange = func(first, second string) error {
		err := previous(first, second)
		answers = append(answers, err)
		return err
	}
	t.Cleanup(func() { renameat2Exchange = previous })

	if err := AtomicWriteCAS(path, []byte("ours\n"), casTestDigest(original)); err != nil {
		t.Fatalf("the primitive must complete the write: %v", err)
	}
	if len(answers) != 1 || answers[0] != nil {
		t.Fatalf("the primitive must be asked exactly once and honour the flag: %v", answers)
	}
	if got := casTestRead(t, path); got != "ours\n" {
		t.Fatalf("canonical bytes = %q", got)
	}
	if got := casTestRead(t, capture); got != "sentinel\n" {
		t.Fatalf("the capture name was touched although the primitive was honoured: %q", got)
	}
	if names := casTestNeighbours(t, path); len(names) != 1 || names[0] != filepath.Base(capture) {
		t.Fatalf("only the sentinel may remain beside the target: %v", names)
	}
}

// Locks: a real answer such as ENOENT is propagated as-is; the fallback is not
// consulted, so a missing swap is never "exchanged" through link.
func TestExchangeAtomicPathsPropagatesRealRefusalsWithoutFallback(t *testing.T) {
	withRenameat2ExchangeAnswer(t, unix.ENOENT)
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := exchangeAtomicPaths(first, second)
	if !errors.Is(err, unix.ENOENT) || strings.Contains(err.Error(), "fallback") {
		t.Fatalf("real refusal must propagate untouched: %v", err)
	}
	if casTestRead(t, first) != "first\n" || casTestRead(t, second) != "second\n" {
		t.Fatal("a real refusal was retried through the fallback")
	}
	if _, statErr := os.Lstat(atomicExchangeCapturePath(second)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("a real refusal must not create the capture")
	}
}

func TestNormalizeAtomicExchangeArtifactsConvergesBothCaptureStates(t *testing.T) {
	dir := t.TempDir()
	swap := filepath.Join(dir, "asset.txt.aoci-cas.swap")
	capture := atomicExchangeCapturePath(swap)
	if err := normalizeAtomicExchangeArtifacts(swap); err != nil {
		t.Fatalf("no capture must be a no-op: %v", err)
	}

	if err := os.WriteFile(swap, []byte("swap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture, []byte("capture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeAtomicExchangeArtifacts(swap); err != nil {
		t.Fatalf("state (a) must converge: %v", err)
	}
	if _, err := os.Lstat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state (a) must remove the redundant capture")
	}
	if got := casTestRead(t, swap); got != "swap\n" {
		t.Fatalf("state (a) must keep the swap: %q", got)
	}

	if err := os.Remove(swap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capture, []byte("capture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := normalizeAtomicExchangeArtifacts(swap); err != nil {
		t.Fatalf("state (b) must converge: %v", err)
	}
	if _, err := os.Lstat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("state (b) must consume the capture")
	}
	if got := casTestRead(t, swap); got != "capture\n" {
		t.Fatalf("state (b) must promote the capture into the swap: %q", got)
	}
}

// skipUnlessTheExchangeFlagIsHonoured probes the temporary directory's
// filesystem with the real primitive so a test that asserts "the primitive is
// taken" is skipped, not failed, when the suite itself runs on DrvFs.
func skipUnlessTheExchangeFlagIsHonoured(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	if err := os.WriteFile(first, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := unix.Renameat2(unix.AT_FDCWD, first, unix.AT_FDCWD, second, unix.RENAME_EXCHANGE)
	if renameat2FlagUnsupported(err) {
		t.Skipf("the test filesystem refuses RENAME_EXCHANGE (%v); this test asserts the primitive path", err)
	}
	if err != nil {
		t.Fatalf("probe exchange failed for a reason other than the flag: %v", err)
	}
}
