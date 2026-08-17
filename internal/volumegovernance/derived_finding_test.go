package volumegovernance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

// One root cause must produce one blocker. A Managed Scope policy that no longer
// matches its receipt already reports scope_change_required; reporting it a
// second time as a business-source failure sent a real operator to investigate a
// subsystem that was working, and cost them a round of diagnosis that found
// nothing because there was nothing there.

func TestScopeDriftDoesNotAlsoReportABusinessSourceFailure(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFixtureCLI(t, root, "init", "--quiet")
	runFixtureCLI(t, root, "scan", "--quiet")

	// Drift the receipt away from the desired policy, which is exactly the state
	// the reported repository was in.
	baselinePath := filepath.Join(root, ".aoci", "baseline.json")
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	scope, ok := state["managed_scope"].(map[string]any)
	if !ok {
		t.Skip("fixture has no Managed Scope receipt to drift")
	}
	scope["policy_identity"] = strings.Repeat("a", 64)
	rewritten, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baselinePath, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}

	facts := assessFixture(t, root)
	codes := findingCodes(facts)
	if !codes["scope_change_required"] {
		t.Fatalf("fixture did not reach the reported state: %v", sortedCodes(codes))
	}
	// The derived report is the defect: it names a different subsystem and
	// carries no repair, so the operator looks in the wrong place.
	if codes["business_source_manifest_invalid"] {
		t.Fatalf("one root cause produced two blockers: %v", sortedCodes(codes))
	}
}

func TestOtherManifestFailuresKeepTheirExactCause(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		cause string
	}{
		{"curation", errors.New("business_source_curation_invalid"), "business_source_curation_invalid"},
		{"wrapped", fmt.Errorf("business_source_inventory_invalid: %w", errors.New("inner")), "business_source_inventory_invalid"},
		{"path suffix", errors.New("business_source_read_failed: src/secret/config.go"), "business_source_read_failed"},
		{"foreign", errors.New("something else entirely"), "business_source_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := businessSourceCause(test.err)
			if cause != test.cause {
				t.Fatalf("cause=%q, want %q", cause, test.cause)
			}
			// A governance fact must not carry a repository path.
			if strings.Contains(cause, "/") || strings.Contains(cause, " ") {
				t.Fatalf("cause leaked detail beyond the machine token: %q", cause)
			}
		})
	}
}

func TestScopeChangeCauseIsRecognisedWithoutMatchingText(t *testing.T) {
	// The sentinel exists so this recognition never depends on a message string,
	// which is what makes the single-blocker rule hold when the text changes.
	if !errors.Is(businesssource.ErrScopeChangeRequired, businesssource.ErrScopeChangeRequired) {
		t.Fatal("sentinel is not comparable")
	}
	wrapped := fmt.Errorf("build failed: %w", businesssource.ErrScopeChangeRequired)
	if !errors.Is(wrapped, businesssource.ErrScopeChangeRequired) {
		t.Fatal("sentinel must survive wrapping")
	}
	if businessSourceCause(businesssource.ErrScopeChangeRequired) != "business_source_scope_change_required" {
		t.Fatal("the sentinel must still carry its machine token for any caller that reports it")
	}
}

func TestAManifestFailureCarriesItsCauseIntoTheFinding(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFixtureCLI(t, root, "init", "--quiet")
	runFixtureCLI(t, root, "scan", "--quiet")

	// A corrupt Curation document is a manifest failure that is genuinely its
	// own problem, unlike scope drift, so it must still be reported -- and it
	// must arrive with the token that says which subsystem to look at.
	if err := os.WriteFile(curation.FilePath(root), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	facts := assessFixture(t, root)
	var reported *Finding
	for index := range facts.Findings {
		if facts.Findings[index].Code == "business_source_manifest_invalid" {
			reported = &facts.Findings[index]
		}
	}
	if reported == nil {
		t.Fatalf("a real manifest failure must still be reported: %v", sortedCodes(findingCodes(facts)))
	}
	// Without the cause the operator is told only that something in the
	// business-source subsystem failed, which is the state this change fixes.
	if reported.Cause == "" {
		t.Fatal("the finding reached the operator with its cause erased")
	}
	if !strings.HasPrefix(reported.Cause, "business_source_") {
		t.Fatalf("cause is not the machine token: %q", reported.Cause)
	}
}

func findingCodes(facts *Facts) map[string]bool {
	codes := map[string]bool{}
	for _, finding := range facts.Findings {
		codes[finding.Code] = true
	}
	return codes
}

func sortedCodes(codes map[string]bool) []string {
	result := make([]string, 0, len(codes))
	for code := range codes {
		result = append(result, code)
	}
	return sortedUnique(result)
}

func runFixtureCLI(t *testing.T, root string, args ...string) {
	t.Helper()
	binary := os.Getenv("AOCI_TEST_BINARY")
	if binary == "" {
		binary = filepath.Join("..", "..", "build", "aoci")
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(absolute); statErr != nil {
		t.Skipf("built binary unavailable (%v); run make build first", statErr)
	}
	command := exec.Command(absolute, append([]string{"--repo", root}, args...)...)
	if output, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("aoci %v: %v: %s", args, runErr, output)
	}
}

func assessFixture(t *testing.T, root string) *Facts {
	t.Helper()
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}
