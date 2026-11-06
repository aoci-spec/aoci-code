package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestScopeRuleAddBatch(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"svc-b", "services/svc-c", "svc-d"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	add := func(reason string, patterns ...string) ([]byte, error) {
		t.Helper()
		args := []string{"rule", "add", "later", "--action", "exclude", "--pattern-kind", "directory", "--reason", reason}
		for _, pattern := range patterns {
			args = append(args, "--pattern", pattern)
		}
		return runScopeCLI(t, root, args...)
	}
	readConfig := func() []byte {
		t.Helper()
		data, err := os.ReadFile(config.FilePath(root))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	readRules := func(out []byte, err error) []initialScopeRuleResult {
		t.Helper()
		var result struct {
			Version          string                   `json:"version"`
			Status           string                   `json:"status"`
			Rules            []initialScopeRuleResult `json:"rules"`
			BaselineAdvanced *bool                    `json:"baseline_advanced"`
			IndexChanged     *bool                    `json:"index_changed"`
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out, &result); err != nil || result.Version != machinecontract.ManagedScopePolicyV2 ||
			result.Status != "ready_for_initial_scan" || len(result.Rules) != 2 || result.BaselineAdvanced == nil ||
			*result.BaselineAdvanced || result.IndexChanged == nil || *result.IndexChanged {
			t.Fatalf("unexpected batch result: %s (%v)", out, err)
		}
		return result.Rules
	}
	first := readRules(add("later", "svc-b", "services/svc-c"))
	if first[0].RuleID == first[1].RuleID || first[0].Pattern == first[1].Pattern || !first[0].Added || !first[1].Added {
		t.Fatalf("expected two new rules: %+v", first)
	}
	configBefore := readConfig()
	second := readRules(add("later", `services\svc-c\`, "svc-b"))
	for index, rule := range first {
		rule.Added = false
		if second[index] != rule {
			t.Fatalf("replay: got %+v, want %+v", second[index], rule)
		}
	}
	assertRejected := func(want, reason string, patterns ...string) {
		t.Helper()
		if out, err := add(reason, patterns...); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %s for %v: %s (%v)", want, patterns, out, err)
		}
		if !bytes.Equal(configBefore, readConfig()) {
			t.Fatal("failed or repeated batch changed config")
		}
	}
	assertRejected("managed_scope_rule_invalid_directory", "later", "svc-d", "missing")
	assertRejected("managed_scope_rule_pattern_invalid", "later", "svc-d", "../outside")
	assertRejected("managed_scope_rule_targets_formal_asset", "later", "svc-d", ".aoci")
	assertRejected("managed_scope_rule_targets_formal_asset", "later", "svc-d", ".AOCI/cache")
	assertRejected("managed_scope_rule_id_invalid", "later", "svc-d", "svc-d/")
	assertRejected("managed_scope_rule_exists", "different reason", "svc-d", "svc-b")
	if err := baseline.Save(root, baseline.NewBaseline(nil)); err != nil {
		t.Fatal(err)
	}
	assertRejected("managed_scope_initial_baseline_exists", "later", "svc-b", "svc-d")
	if _, err := add("ordinary rule edit", "svc-d"); err != nil {
		t.Fatalf("single-pattern add on a scanned repository: %v", err)
	}
}
