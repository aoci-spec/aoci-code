package mcptools

import (
	"strings"
	"testing"
)

// A delta's chunk spans report each Entry's formal ordinal, the number its
// Challenge asks for, not its position in the delta (audit G3-03).
func TestChunkSpansCarryFormalOrdinals(t *testing.T) {
	entries := []string{"a.go[CG5T]: F:a | R:- | A:- | S:-", "b.go[CG5T]: F:b | R:- | A:- | S:-", "c.go[CG5T]: F:c | R:- | A:- | S:-"}
	formal := []int{10, 250, 490}
	var builder strings.Builder
	sequence := make([]overviewChallengeTarget, 0, len(entries))
	for index, line := range entries {
		sequence = append(sequence, overviewChallengeTarget{ContentOffset: builder.Len(), Ordinal: formal[index]})
		builder.WriteString(line + "\n" + strings.Repeat("#padding\n", 700))
	}
	spans, err := planOverviewChunks(builder.String(), 4000, 0, sequence)
	if err != nil {
		t.Fatal(err)
	}
	seen := []int{}
	for _, span := range spans {
		if span.FirstOrdinal != 0 {
			seen = append(seen, span.FirstOrdinal)
		}
	}
	if len(seen) == 0 || seen[0] != 10 || seen[len(seen)-1] != spans[len(spans)-1].FirstOrdinal {
		t.Fatalf("chunk spans: %+v", spans)
	}
	for _, span := range spans {
		if span.FirstOrdinal > 0 && span.FirstOrdinal < 10 {
			t.Fatalf("a span reports a delta-local ordinal: %+v", spans)
		}
	}
}
