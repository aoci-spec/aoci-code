package volumegovernance

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// The budget facts carry the same S coverage a Legacy Report does, measured
// over the Code Volume only: the database table in this fixture carries an S
// at C7 and must not move any figure, because a cognition_optimization pass
// (the remedy the hint names) reviews Code Entries alone.
func TestProjectedBudgetCarriesSCoverageOverTheCodeVolume(t *testing.T) {
	root, cfg := baseFixture(t, true, true)
	codeObjects := []struct {
		path       string
		importance int
		s          string
	}{
		{"main.go", 7, "Execution must remain deterministic"},
		{"router.go", 9, "-"},
		{"store.go", 7, "-"},
		{"util.go", 5, "-"},
		{"doc.go", 3, "Comment-only file"},
	}
	var builder strings.Builder
	builder.WriteString(cognition.CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n")
	for _, object := range codeObjects {
		fmt.Fprintf(&builder, "%s[CD%dS]: F:hold one fixture object | R:- | A:- | S:%s\n", object.path, object.importance, object.s)
	}
	writeFixtureFile(t, root, "aoci.code.txt", builder.String())
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	if set.Volumes[cognition.ScopeDatabase] == nil || set.Volumes[cognition.ScopeDatabase].ObjectCount == 0 {
		t.Fatal("fixture premise: the database Volume must carry an object")
	}
	policy, err := cognitionbudget.Normalize(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	wantBands := []cognitionbudget.SCoverageBand{}
	for _, band := range policy.S {
		wantBands = append(wantBands, cognitionbudget.SCoverageBand{MinC: band.MinC, MaxC: band.MaxC})
	}
	wantEntries, wantAbsent := 0, 0
	for _, object := range codeObjects {
		present := object.s != "-"
		for position := range wantBands {
			if object.importance >= wantBands[position].MinC && object.importance <= wantBands[position].MaxC {
				wantBands[position].Entries++
				if present {
					wantBands[position].SPresent++
				}
			}
		}
		if object.importance >= machinecontract.HighImportanceMinC {
			wantEntries++
			if !present {
				wantAbsent++
			}
		}
	}
	wantPercent := wantAbsent * 100 / wantEntries

	budget := AssessProjectedBudget(cfg, set)
	if len(budget.SCoverage) != len(wantBands) {
		t.Fatalf("bands got %+v want %+v", budget.SCoverage, wantBands)
	}
	for position := range wantBands {
		if budget.SCoverage[position] != wantBands[position] {
			t.Fatalf("band %d got %+v want %+v", position, budget.SCoverage[position], wantBands[position])
		}
	}
	if budget.HighImportanceEntries != wantEntries || budget.HighImportanceSAbsent != wantAbsent || budget.HighImportanceSAbsentPercent != wantPercent {
		t.Fatalf("headline got %d/%d (%d%%) want %d/%d (%d%%)", budget.HighImportanceSAbsent, budget.HighImportanceEntries,
			budget.HighImportanceSAbsentPercent, wantAbsent, wantEntries, wantPercent)
	}
	if len(budget.Violations) != 0 {
		t.Fatalf("S coverage must never become a violation: %+v", budget.Violations)
	}
}

func TestProjectedBudgetWithoutCodeVolumeReportsEmptySCoverage(t *testing.T) {
	root, cfg := baseFixture(t, false, true)
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	budget := AssessProjectedBudget(cfg, set)
	if budget.HighImportanceEntries != 0 || budget.HighImportanceSAbsent != 0 || budget.HighImportanceSAbsentPercent != 0 {
		t.Fatalf("database-only layout acquired code S coverage: %+v", budget)
	}
	for _, band := range budget.SCoverage {
		if band.Entries != 0 || band.SPresent != 0 {
			t.Fatalf("database-only layout filled a code band: %+v", budget.SCoverage)
		}
	}
}
