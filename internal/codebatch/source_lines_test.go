package codebatch

import "testing"

// A batch is bounded by what it asks a model to read, not only by what fits
// the wire: the cut stops before the candidate that would pass the line
// bound, a file larger than the bound travels alone, and an uncounted
// candidate (zero) never cuts.
func TestPrefixWithinBudgetCutsBySourceLines(t *testing.T) {
	lines := map[string]int{"a": 6000, "b": 9000, "c": 7000, "big": 50000, "d": 10, "unknown": 0}
	order := func(names ...string) []Candidate {
		out := make([]Candidate, 0, len(names))
		for _, name := range names {
			out = append(out, Candidate{Target: Target{ObjectRef: "code:" + name, Path: name}})
		}
		return out
	}
	budget := Budget{Lines: 20000, LineCount: func(candidate Candidate) int { return lines[candidate.Path] }}
	for _, test := range []struct {
		name  string
		items []Candidate
		limit int
		want  int
	}{
		{"stops before the candidate that passes the bound", order("a", "b", "c", "d"), 50, 2},
		{"a file over the bound still ships, alone", order("big", "d"), 50, 1},
		{"a large file ends the batch before it", order("d", "big", "a"), 50, 1},
		{"uncounted candidates never cut", order("unknown", "unknown", "a", "unknown"), 50, 4},
		{"the entry limit still applies", order("d", "d", "d"), 2, 2},
	} {
		if got := PrefixWithinBudget(test.items, test.limit, budget); got != test.want {
			t.Errorf("%s: took %d, want %d", test.name, got, test.want)
		}
	}
	// Both bounds hold at once: bytes cut first here.
	both := budget
	both.Bytes, both.Size = 150, func(Candidate) int { return 100 }
	if got := PrefixWithinBudget(order("d", "d", "d"), 50, both); got != 1 {
		t.Errorf("the byte bound must still cut: took %d", got)
	}
	// CountBatches walks the same cut.
	if got := CountBatches(order("a", "b", "c", "big", "d"), 50, budget); got != 3 {
		t.Errorf("CountBatches = %d, want 3 (a+b, big, c+d in reference order)", got)
	}
}
