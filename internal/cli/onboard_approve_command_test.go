package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
)

// The approval command runs from any directory: it names the repository and
// the envelope by absolute path (audit G5-09).
func TestOnboardingApproveCommandNamesTheEnvelopeAbsolutely(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	want := filepath.Join(root, ".aoci", "onboarding", "envelope.json")
	for _, envelope := range []any{&bootstrapapply.ApplyEnvelope{}, &migrationapply.ApplyEnvelope{}} {
		_, _, command := onboardingApproveCommand(root, ".aoci/onboarding/envelope.json", "tester", envelope)
		if !strings.Contains(command, want) || !strings.Contains(command, "--repo") {
			t.Fatalf("approval command does not carry the absolute envelope path %s: %s", want, command)
		}
	}
}
