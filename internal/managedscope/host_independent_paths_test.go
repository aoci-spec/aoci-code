// Managed Scope path semantics must be host-independent.
//
// A real experience: Build once used filesystemCaseSensitive() to probe the current machine's filesystem and counted
// that boolean into the applied scope identity. So the same repository scanned on Linux and Windows got
// different governance identities: a repository's governance state depended on whose laptop ran the scan. 8cc35eb built
// an equivalence bridge (re-evaluating every path under the opposite semantics and publishing an alternative identity only when every role agreed) to hide the difference,
// but it worked only when "provably equivalent": with a real case divergence between a rule and a path, the Linux colleague and the
// Windows colleague could not both be aligned, and one of them got a scope_change_required with no real cause.
//
// Git's path semantics are exact and case-sensitive on every host, and so is the scope policy now. The bridge was removed together with
// that probe.
package managedscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// The identity keeps the v2 raw preimage with the case bit fixed to "true". This assertion is the boundary of the migration surface:
// as long as it holds, every receipt established on a case-sensitive filesystem stays valid as it is, and not one migrates.
func TestAppliedIdentityKeepsTheCaseSensitivePreimage(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	gitScopeFixture(t, root, "add", "src/main.go")

	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	result, err := Build(root, policy, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	base, err := Identity(normalized)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, value := range []string{"managed-scope-applied-identity/v2", base, result.SafeInventory.RulesIdentity, "true"} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	if want := hex.EncodeToString(hash.Sum(nil)); result.PolicyIdentity != want {
		t.Fatalf("applied identity no longer reproduces the case-sensitive v2 preimage:\n got  %s\n want %s\n"+
			"every existing receipt on a case-sensitive filesystem depends on this staying exact",
			result.PolicyIdentity, want)
	}
}

// Matching is exact by Git semantics. A rule and a path differing only in case must not match; whether they matched once depended on
// the host filesystem, exactly the source of the identity drifting with the machine.
func TestRuleMatchingIsExactRegardlessOfHost(t *testing.T) {
	rule := Rule{RuleID: "case-exact", Action: machinecontract.ScopeRoleExclude,
		Pattern: "Docs/**", PatternKind: machinecontract.ScopePatternGlob,
		Reason: "exact match discipline", Source: machinecontract.ScopeRuleUser, Enabled: true}
	if Match(rule, "docs/readme.md") {
		t.Fatal("a pattern differing only in case must not match; matching it makes the verdict " +
			"depend on the host filesystem")
	}
	if !Match(rule, "Docs/readme.md") {
		t.Fatal("the exact pattern must still match its exact path")
	}
}

// The bridge is gone: Evaluation no longer carries an alternative identity or a host probe result.
func TestEvaluationCarriesNoHostDerivedCaseFacts(t *testing.T) {
	root := t.TempDir()
	gitScopeFixture(t, root, "init", "-q")
	writeScopeFixture(t, root, "src/main.go", "package src\n")
	gitScopeFixture(t, root, "add", "src/main.go")

	result, err := Build(root, DefaultPolicy(machinecontract.ScopeProfileProduction), BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := mustMarshalEvaluation(t, result)
	for _, forbidden := range []string{"case_sensitive", "alternate_policy_identity"} {
		if containsField(raw, forbidden) {
			t.Fatalf("%q survives in the evaluation; a host-derived fact in the governed shape is "+
				"what let identity drift between machines", forbidden)
		}
	}
}

func mustMarshalEvaluation(t *testing.T, value *Evaluation) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func containsField(raw, name string) bool { return strings.Contains(raw, "\""+name+"\"") }
