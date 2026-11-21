package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestScopeBudgetPolicyFileRequiresEOF(t *testing.T) {
	for _, tc := range []struct {
		name    string
		suffix  string
		invalid bool
	}{
		{name: "one value"},
		{name: "trailing whitespace", suffix: " \t\r\n"},
		{name: "second value", suffix: "\n{}", invalid: true},
		{name: "trailing garbage", suffix: "\nextra", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			policyPath := filepath.Join(root, "policy.json")
			if err := os.WriteFile(policyPath, append(payload, tc.suffix...), 0o644); err != nil {
				t.Fatal(err)
			}

			stdout, stderr, code := runCLI(t, root, "scope", "budget", "set", "--policy-file", policyPath, "--json")
			if tc.invalid {
				var failure cliJSONErrorEnvelope
				if err := json.Unmarshal([]byte(stdout), &failure); err != nil || code != ExitInvalid ||
					failure.OK || failure.ErrorCode != "managed_scope_invalid" ||
					!strings.Contains(failure.Message, "cognition_budget_policy_invalid") {
					t.Errorf("expected a policy error: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				after, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("invalid policy changed config.json")
				}
				return
			}

			if code != ExitOK {
				t.Fatalf("valid policy: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			updated, err := config.LoadReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := updated.EffectiveCognitionBudget().WholeIndex.TargetTokens; got != policy.WholeIndex.TargetTokens {
				t.Fatalf("saved target = %d, want %d", got, policy.WholeIndex.TargetTokens)
			}
		})
	}
}
