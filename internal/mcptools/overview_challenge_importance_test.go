package mcptools

import (
	"fmt"
	"reflect"
	"testing"
)

// Each stratum draws its most important Entry, and the tie-break moves with
// the delivered identity, so a slightly changed index is not asked the same
// low-information Entries again (scipy first build, A4).
func TestChallengeDrawsTheMostImportantEntryOfEachStratum(t *testing.T) {
	targets := make([]overviewChallengeTarget, 0, 100)
	for index := 0; index < 100; index++ {
		importance := 3
		if index%10 == 7 {
			importance = 8
		}
		targets = append(targets, overviewChallengeTarget{ObjectIdentity: fmt.Sprintf("code:f%03d.go", index),
			Tag: fmt.Sprintf("CG%dT", importance), CoreF: "x"})
	}
	ordinals := stratifiedChallengeOrdinals("identity-one", targets)
	for _, ordinal := range ordinals {
		if (ordinal-1)%10 != 7 {
			t.Fatalf("a stratum drew a C3 Entry beside its C8 one: %v", ordinals)
		}
	}
	flat := make([]overviewChallengeTarget, len(targets))
	for index := range targets {
		flat[index] = targets[index]
		flat[index].Tag = "CG5T"
	}
	if reflect.DeepEqual(stratifiedChallengeOrdinals("identity-one", flat), stratifiedChallengeOrdinals("identity-two", flat)) {
		t.Fatal("a different delivered identity drew the same Entries")
	}
	if !reflect.DeepEqual(stratifiedChallengeOrdinals("identity-one", flat), stratifiedChallengeOrdinals("identity-one", flat)) {
		t.Fatal("the same delivered identity must draw the same Entries")
	}
}
