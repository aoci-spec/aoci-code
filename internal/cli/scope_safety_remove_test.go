package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// safety remove matches the stored, normalized path whatever spelling names
// it (audit G4-19).
func TestScopeSafetyRemoveMatchesANormalizedSpelling(t *testing.T) {
	root := scopeActivationFixture(t)
	var out, errs strings.Builder
	if code := executeCLI([]string{"--repo", root, "scope", "safety", "opt-in", "secrets/.env"}, &out, &errs); code != ExitOK {
		t.Fatalf("opt-in: %d %s", code, &errs)
	}
	out.Reset()
	errs.Reset()
	if code := executeCLI([]string{"--repo", root, "scope", "safety", "remove", "./secrets/.env"}, &out, &errs); code != ExitOK {
		t.Fatalf("remove by ./ spelling: %d %s", code, &errs)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.SafeInventoryHighRiskOptIn) != 0 {
		t.Fatalf("opt-in not removed: %v", cfg.SafeInventoryHighRiskOptIn)
	}
}
