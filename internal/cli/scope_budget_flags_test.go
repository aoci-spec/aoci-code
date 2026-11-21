package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func TestScopeBudgetSetRejectsMixedPolicySources(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	configPath := config.FilePath(root)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	policy := cfg.EffectiveCognitionBudget()
	policy.WholeIndex.TargetTokens--
	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	policyPath := testkit.WriteFile(t, root, "policy.json", string(payload))

	for _, flag := range []struct{ name, value string }{
		{"--mode", "enforce"},
		{"--target-tokens", "90000"},
		{"--warning-tokens", "150000"},
		{"--max-tokens", "200000"},
	} {
		t.Run(flag.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, root, "scope", "budget", "set", "--policy-file", policyPath, flag.name, flag.value, "--json")
			var failure cliJSONErrorEnvelope
			if err := json.Unmarshal([]byte(stdout), &failure); err != nil || code != ExitInvalid ||
				failure.OK || failure.ErrorCode != "managed_scope_invalid" ||
				!strings.Contains(failure.Message, "--policy-file") || !strings.Contains(failure.Message, flag.name) {
				t.Errorf("expected a flag conflict: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			after, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("mixed budget inputs changed config.json")
			}
		})
	}

	mustCLI(t, root, "scope", "budget", "set", "--policy-file", policyPath)
	saved, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.EffectiveCognitionBudget().WholeIndex; got != policy.WholeIndex {
		t.Fatalf("file-only budget = %+v, want %+v", got, policy.WholeIndex)
	}

	mustCLI(t, root, "scope", "budget", "set", "--mode", "enforce",
		"--target-tokens", "90000", "--warning-tokens", "150000", "--max-tokens", "200000")
	saved, err = config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	got := saved.EffectiveCognitionBudget()
	want := cognitionbudget.WholeIndexPolicy{TargetTokens: 90000, WarningTokens: 150000, MaxTokens: 200000}
	if got.Mode != "enforce" || got.WholeIndex != want {
		t.Fatalf("flag-only budget = %s %+v, want enforce %+v", got.Mode, got.WholeIndex, want)
	}
}
