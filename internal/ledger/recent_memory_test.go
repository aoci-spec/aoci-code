package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestRecentLimitedHistoryKeepsOrderAndCorruptionCounts(t *testing.T) {
	root := t.TempDir()
	lines := []string{"broken-before"}
	want := make([]string, 0, 11)
	for i := range 11 {
		op := fmt.Sprintf("event-%02d", i)
		want = append(want, op)
		lines = append(lines, fmt.Sprintf(`{"op":%q}`, op))
		if i == 4 {
			lines = append(lines, "", "broken-middle", strings.Repeat("x", maxLedgerLineBytes+1))
		}
	}
	lines = append(lines, "broken-after")
	writeRawLedgerLines(t, root, lines)
	for _, limit := range []int{-1, 0, 1, 3, 5, 11, 20, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprintf("limit-%d", limit), func(t *testing.T) {
			events, corrupt := Recent(root, limit)
			if corrupt != 4 {
				t.Fatalf("corruption outside the retained window must still be counted: got %d", corrupt)
			}
			got := make([]string, len(events))
			for i, event := range events {
				got[i] = event.Op
			}
			expected := want
			if limit > 0 && limit < len(want) {
				expected = want[len(want)-limit:]
			}
			if !slices.Equal(got, expected) {
				t.Fatalf("recent events: got %v, want %v", got, expected)
			}
		})
	}
}

func TestRecentLimitedHistoryKeepsUnterminatedLastEvent(t *testing.T) {
	root := t.TempDir()
	path := ledgerPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"op\":\"first\"}\n{\"op\":\"last\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	events, corrupt := Recent(root, 1)
	if corrupt != 0 || len(events) != 1 || events[0].Op != "last" {
		t.Fatalf("unterminated last event: got %v, corrupt=%d", events, corrupt)
	}
}

func TestRecentLimitedHistoryDoesNotRetainDiscardedEvents(t *testing.T) {
	root := t.TempDir()
	path := ledgerPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const eventCount = 10000
	if err := os.WriteFile(path, []byte(strings.Repeat("{\"op\":\"event\"}\n", eventCount)), 0o644); err != nil {
		t.Fatal(err)
	}
	// Warm the decoder before measuring live storage rather than its caches.
	Recent(root, 1)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	events, corrupt := Recent(root, 3)
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(events)
	if corrupt != 0 || len(events) != 3 {
		t.Fatalf("recent events: len=%d corrupt=%d", len(events), corrupt)
	}
	const maxRetainedBytes = 2 << 20
	if after.HeapAlloc > before.HeapAlloc+maxRetainedBytes {
		t.Fatalf("three events retained %d bytes from %d discarded events; limit is %d", after.HeapAlloc-before.HeapAlloc, eventCount-3, maxRetainedBytes)
	}
	t.Logf("heap before=%d after=%d while retaining three events from %d", before.HeapAlloc, after.HeapAlloc, eventCount)
}

func BenchmarkRecentLimitedHistory(b *testing.B) {
	for _, count := range []int{1000, 100000} {
		b.Run(fmt.Sprintf("events-%d", count), func(b *testing.B) {
			root := b.TempDir()
			path := ledgerPath(root)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Repeat("{\"op\":\"event\"}\n", count)), 0o644); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				events, corrupt := Recent(root, 3)
				if corrupt != 0 || len(events) != 3 {
					b.Fatalf("recent events: len=%d corrupt=%d", len(events), corrupt)
				}
			}
		})
	}
}
