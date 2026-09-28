// aoci check S coverage line and the cognition_optimization hint: both are
// operator information on top of the existing conclusion, never an issue.
package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

// sCoverageCheckEntry is one Entry of a Legacy check fixture. The expected
// output lines are counted from this table, never written down twice.
type sCoverageCheckEntry struct {
	path       string
	importance int
	s          string
}

// buildSCoverageRepo mirrors buildUpdateRepo with a caller-chosen Entry set:
// every path but the index itself exists on disk and the Baseline is aligned,
// so the check conclusion stays clean and only the coverage lines vary.
func buildSCoverageRepo(t *testing.T, entries []sCoverageCheckEntry) string {
	t.Helper()
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	var builder strings.Builder
	builder.WriteString("#头部第一行\n#头部第二行\n===段" + rootSlash + "/===\n")
	for _, entry := range entries {
		if entry.path != "aoci.txt" {
			if err := os.WriteFile(filepath.Join(root, entry.path), []byte("package p\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintf(&builder, "%s[XC%dT]: F:x | R:- | A:- | S:%s\n", entry.path, entry.importance, entry.s)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, warnings, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil || len(warnings) > 0 {
		t.Fatalf("snapshot: %v %v", err, warnings)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

// expectedSCoverageLine derives the coverage line and the headline figures
// from the fixture table under the repository's effective budget policy.
func expectedSCoverageLine(t *testing.T, root string, entries []sCoverageCheckEntry) (line string, highEntries, percent int) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := cognitionbudget.Normalize(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	bands := []cognitionbudget.SCoverageBand{}
	for _, band := range policy.S {
		bands = append(bands, cognitionbudget.SCoverageBand{MinC: band.MinC, MaxC: band.MaxC})
	}
	highAbsent := 0
	for _, entry := range entries {
		present := entry.s != "-"
		for position := range bands {
			if entry.importance >= bands[position].MinC && entry.importance <= bands[position].MaxC {
				bands[position].Entries++
				if present {
					bands[position].SPresent++
				}
			}
		}
		if entry.importance >= machinecontract.HighImportanceMinC {
			highEntries++
			if !present {
				highAbsent++
			}
		}
	}
	if highEntries > 0 {
		percent = highAbsent * 100 / highEntries
	}
	line = cliMessage("check.s_coverage", machinecontract.HighImportanceMinC, highEntries-highAbsent, highEntries, percent,
		cognitionbudget.FormatSCoverageBands(bands))
	return line, highEntries, percent
}

// hintMarker is literal in every locale's hint text and in no other check line.
const hintMarker = "intent=cognition_optimization"

func TestCheckPrintsSCoverageAndHintsAboveThreshold(t *testing.T) {
	entries := []sCoverageCheckEntry{
		{"aoci.txt", 9, "-"},
		{"a.go", 9, "-"},
		{"b.go", 7, "Seven names the trap"},
		{"c.go", 5, "-"},
		{"d.go", 3, "Three explains the retry"},
	}
	root := buildSCoverageRepo(t, entries)
	wantLine, highEntries, percent := expectedSCoverageLine(t, root, entries)
	if !cognitionbudget.HighImportanceSAbsentHint(highEntries, percent) {
		t.Fatalf("fixture premise: %d%% absent over %d high-importance Entries must reach the %d%% hint threshold",
			percent, highEntries, machinecontract.HighImportanceSAbsentHintPercent)
	}
	output, err := runCheck(t, root)
	if err != nil {
		t.Fatalf("aligned fixture must stay committable, S coverage is not an issue: %v\n%s", err, output)
	}
	if !strings.Contains(output, wantLine) {
		t.Fatalf("check output lacks the coverage line %q:\n%s", wantLine, output)
	}
	wantHint := cliMessage("check.hint_s_coverage", percent, machinecontract.HighImportanceMinC, machinecontract.HighImportanceSAbsentHintPercent)
	if !strings.Contains(output, wantHint) {
		t.Fatalf("check output lacks the hint %q:\n%s", wantHint, output)
	}
	if !strings.Contains(output, cliMessage("check.ok")) {
		t.Fatalf("the hint must not change the conclusion:\n%s", output)
	}
}

func TestCheckPrintsSCoverageWithoutHintWhenHighImportanceSIsPresent(t *testing.T) {
	entries := []sCoverageCheckEntry{
		{"aoci.txt", 9, "The index is the single formal cognition asset"},
		{"a.go", 7, "Seven names the trap"},
		{"c.go", 5, "-"},
	}
	root := buildSCoverageRepo(t, entries)
	wantLine, highEntries, percent := expectedSCoverageLine(t, root, entries)
	if highEntries == 0 || cognitionbudget.HighImportanceSAbsentHint(highEntries, percent) {
		t.Fatalf("fixture premise: high-importance Entries with S must sit under the hint threshold, got %d%% of %d", percent, highEntries)
	}
	output, err := runCheck(t, root)
	if err != nil {
		t.Fatalf("aligned fixture must stay committable: %v\n%s", err, output)
	}
	if !strings.Contains(output, wantLine) {
		t.Fatalf("check output lacks the coverage line %q:\n%s", wantLine, output)
	}
	if strings.Contains(output, hintMarker) {
		t.Fatalf("hint printed under the threshold:\n%s", output)
	}
}

// The Volumes branch reads the same figures from the governance budget facts,
// which is the layout a real repository build reports through.
func TestVolumeCheckPrintsSCoverageFromGovernanceBudget(t *testing.T) {
	root, cfg := alignedVolumeCLIFixture(t, true, false)
	content := "\n===Code " + filepath.ToSlash(root) + "/===\nmain.go[CD7S]: F:run the deterministic CLI fixture | R:- | A:main | S:-\n"
	codeVolume, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	marker := strings.SplitN(string(codeVolume), "\n", 2)[0]
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(marker+content), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("baseline: %v exists=%v", err, exists)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	state.Files["aoci.code.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	policy, err := cognitionbudget.Normalize(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	bands := []cognitionbudget.SCoverageBand{}
	for _, band := range policy.S {
		coverage := cognitionbudget.SCoverageBand{MinC: band.MinC, MaxC: band.MaxC}
		if 7 >= band.MinC && 7 <= band.MaxC {
			coverage.Entries = 1
		}
		bands = append(bands, coverage)
	}
	const highEntries, percent = 1, 100
	if !cognitionbudget.HighImportanceSAbsentHint(highEntries, percent) {
		t.Fatal("fixture premise: one C7 Entry without S must reach the hint threshold")
	}
	wantLine := cliMessage("check.s_coverage", machinecontract.HighImportanceMinC, 0, highEntries, percent, cognitionbudget.FormatSCoverageBands(bands))
	wantHint := cliMessage("check.hint_s_coverage", percent, machinecontract.HighImportanceMinC, machinecontract.HighImportanceSAbsentHintPercent)

	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	for _, asJSON := range []bool{false, true} {
		flagRepo, flagJSON, flagQuiet = root, asJSON, false
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		if err := runCheckCommand(cmd, nil); err != nil {
			t.Fatalf("aligned Volumes fixture must stay committable (json=%v): %v\n%s", asJSON, err, output.String())
		}
		if asJSON {
			for _, want := range []string{`"ok": true`, `"high_importance_s_absent_percent": 100`, `"s_coverage": [`} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("check --json lacks %s:\n%s", want, output.String())
				}
			}
			continue
		}
		for _, want := range []string{cliMessage("check.ok"), wantLine, wantHint} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("Volumes check output lacks %q:\n%s", want, output.String())
			}
		}
	}
}
