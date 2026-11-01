package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

func pinDefaultLocale(t *testing.T) {
	t.Helper()
	previous := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
}

// The three commands a person runs by hand on a Volumes repository name the
// same files and the same next step. Until 0.2.0 verify printed "candidate
// structure is valid" and exited 1, check printed a token and a count, and
// status said nothing about alignment; the paths were reachable only through
// --json, which is where the getting-started walk-through ended up.
func TestVolumesHumanOutputNamesFilesAndNextStep(t *testing.T) {
	pinDefaultLocale(t)
	root, cfg := alignedVolumeCLIFixture(t, true, false)
	cfg.Locale = textassets.DefaultLocale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	// status comes first on purpose: it is a read, so the fingerprint cache
	// Verify and Check save must not appear because of it.
	status, stderr, code := runCLI(t, root, "status")
	if code != ExitOK || !strings.Contains(status, "Governance: aligned") {
		t.Fatalf("aligned status: exit %d\n%s%s", code, status, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "fingerprints.json")); err == nil {
		t.Fatal("status saved the fingerprint cache; it is a read")
	}
	verify, stderr, code := runCLI(t, root, "verify")
	if code != ExitOK || !strings.Contains(verify, "Governance: aligned") {
		t.Fatalf("aligned verify: exit %d\n%s%s", code, verify, stderr)
	}
	if strings.Contains(verify, "candidate") || strings.Contains(verify, "Next:") {
		t.Fatalf("aligned verify still reads as unfinished:\n%s", verify)
	}
	statusJSON := mustCLI(t, root, "--json", "status")
	for _, want := range []string{`"result": "aligned"`, `"aligned": true`, `"stale": 0`} {
		if !strings.Contains(statusJSON, want) {
			t.Fatalf("aligned status --json missing %s:\n%s", want, statusJSON)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	verify, stderr, code = runCLI(t, root, "verify")
	if code != ExitDrift {
		t.Fatalf("drifted verify exit %d, want %d\n%s%s", code, ExitDrift, verify, stderr)
	}
	for _, want := range []string{
		"Governance: authoring_required (1 findings)",
		"Stale Entries (1)",
		"\n  main.go\n",
		"Next: in the Agent session call no-argument aoci_maintain",
	} {
		if !strings.Contains(verify, want) {
			t.Fatalf("drifted verify missing %q:\n%s", want, verify)
		}
	}
	check, stderr, code := runCLI(t, root, "check")
	if code != ExitDrift {
		t.Fatalf("drifted check exit %d, want %d\n%s%s", code, ExitDrift, check, stderr)
	}
	for _, want := range []string{"Volumes governance: authoring_required (1 findings)", "\n  main.go\n", "Next: "} {
		if !strings.Contains(check, want) {
			t.Fatalf("drifted check missing %q:\n%s", want, check)
		}
	}
	status, stderr, code = runCLI(t, root, "status")
	if code != ExitOK {
		t.Fatalf("drifted status exit %d\n%s%s", code, status, stderr)
	}
	if !strings.Contains(status, "Governance: authoring_required — missing 0, stale 1, orphan 0, unbaselined 0, other 0") {
		t.Fatalf("drifted status does not summarize the drift:\n%s", status)
	}
	statusJSON = mustCLI(t, root, "--json", "status")
	for _, want := range []string{`"result": "authoring_required"`, `"aligned": false`, `"stale": 1`, `"findings": 1`} {
		if !strings.Contains(statusJSON, want) {
			t.Fatalf("drifted status --json missing %s:\n%s", want, statusJSON)
		}
	}
}

// The next step follows finalize's precedence: a hard blocker outranks a
// pending observe review, which outranks missing Evidence, which outranks
// ordinary authoring.
func TestVolumeGovernanceNextStepFollowsFinalizePrecedence(t *testing.T) {
	pinDefaultLocale(t)
	for _, test := range []struct {
		name  string
		facts volumegovernance.Facts
		want  string
	}{
		{"baseline missing", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "code_missing"}, {Code: "baseline_missing"}}}, "aoci scan"},
		{"recovery pending", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "recovery_pending"}}}, ".aoci/transactions"},
		{"scope change", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "observed_pending"}, {Code: "scope_change_required"}}}, "aoci scope activate"},
		{"locale mismatch", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "root_locale_mismatch"}}}, "aoci config set locale"},
		{"orphan", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "code_orphan"}, {Code: "code_stale"}}}, "aoci_remove_entry"},
		{"observe review", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			NextRequiredAction: machinecontract.ActionReviewObservedEvidence,
			Findings:           []volumegovernance.Finding{{Code: "observed_pending"}}}, "aoci scope acknowledge"},
		{"unknown blocker", volumegovernance.Facts{Result: volumegovernance.ResultBlocked,
			Findings: []volumegovernance.Finding{{Code: "cognition_budget_exceeded"}}}, "aoci index agent guide --json"},
		{"evidence", volumegovernance.Facts{Result: volumegovernance.ResultEvidenceRequired,
			Findings: []volumegovernance.Finding{{Code: "database_evidence_required"}}}, "aoci database snapshot"},
		{"bootstrap", volumegovernance.Facts{Result: volumegovernance.ResultEvidenceRequired,
			NextRequiredAction: machinecontract.DatabaseCognitionActionBootstrapVolume,
			Findings:           []volumegovernance.Finding{{Code: "database_evidence_bootstrap_required"}}}, "aoci database cognition bootstrap"},
		{"authoring", volumegovernance.Facts{Result: volumegovernance.ResultAuthoringRequired,
			Findings: []volumegovernance.Finding{{Code: "code_stale"}}}, "aoci_maintain"},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := test.facts
			got := volumeGovernanceNextStep(&facts)
			if !strings.Contains(got, test.want) {
				t.Fatalf("next step %q does not name %q", got, test.want)
			}
		})
	}
}

func TestFormatDirectoryImpactsReadsAsText(t *testing.T) {
	got := formatDirectoryImpacts([]managedscope.DirectoryImpact{
		{Directory: "./", Index: 2, Observe: 0, Exclude: 0, Total: 2},
		{Directory: "counter", Index: 1, Observe: 1, Exclude: 0, Total: 2},
	})
	want := "./ (index 2, observe 0, exclude 0); counter (index 1, observe 1, exclude 0)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if formatDirectoryImpacts(nil) != "-" {
		t.Fatalf("empty impacts rendered %q", formatDirectoryImpacts(nil))
	}
}

func TestScanAuthoringEstimateLineUsesSingularForOneRound(t *testing.T) {
	pinDefaultLocale(t)
	one := scanAuthoringEstimateLine(scanAuthoringEstimate{Targets: 6, Rounds: 1, BatchLimit: 50, TransportBudgetBytes: 24576})
	if !strings.Contains(one, "one Maintain round") || strings.Contains(one, "1 Maintain rounds") {
		t.Fatalf("one round rendered %q", one)
	}
	many := scanAuthoringEstimateLine(scanAuthoringEstimate{Targets: 120, Rounds: 3, BatchLimit: 50, TransportBudgetBytes: 24576})
	if !strings.Contains(many, "about 3 Maintain rounds") {
		t.Fatalf("three rounds rendered %q", many)
	}
}
