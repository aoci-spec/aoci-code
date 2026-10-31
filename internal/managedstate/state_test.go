package managedstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"os"
	"os/exec"
	"path/filepath"
)

func writeStateFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitStateFixture(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// A Baseline receipt established on a case-sensitive platform is accepted as it is by a platform of the opposite semantics when equivalence is provable:
// PolicyAligned=true, and the receipt identity is adopted as the current identity, so plans and commits share
// one identifier across platforms. A forged receipt identity still fails closed.
// A receipt historically recorded under case-insensitive semantics must stop at a Scope Change rather than be adopted silently.
//
// The equivalence bridge that once existed recognized such a receipt as the current identity when "provably equivalent path by path". It hid a
// defect, the host filesystem probe result inside the applied scope identity, rather than solving it: with a real case divergence between a rule and a path
// the two platforms could not both be aligned. The identity is host-independent now, and such
// historical receipts go through one governance migration that writes no formal asset meanwhile.
func TestHistoricalCaseInsensitiveReceiptIsHeldAtScopeChange(t *testing.T) {
	root := t.TempDir()
	gitStateFixture(t, root, "init", "-q")
	writeStateFixture(t, root, "src/main.go", "package src\n")
	gitStateFixture(t, root, "add", "src/main.go")

	policy := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	cfg := &config.Config{ManagedScope: &policy}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: fs.WalkOptions{}})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := managedscope.Normalize(cfg.EffectiveManagedScope())
	if err != nil {
		t.Fatal(err)
	}
	base, err := managedscope.Identity(normalized)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	for _, value := range []string{"managed-scope-applied-identity/v2", base, evaluation.SafeInventory.RulesIdentity, "false"} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	legacy := hex.EncodeToString(hash.Sum(nil))
	if legacy == evaluation.PolicyIdentity {
		t.Fatal("fixture precondition: the legacy preimage must differ from the canonical identity")
	}

	historical := baseline.NewBaseline(nil)
	historical.ManagedScope = &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: legacy,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
	}
	if err := baseline.Save(root, historical); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}

	state, err := Load(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.PolicyAligned || !state.ScopeChangeRequired {
		t.Fatalf("a historical case-insensitive receipt must not be adopted silently: %+v", state)
	}
	if state.ActivePolicyIdentity != legacy || state.DesiredPolicyIdentity != evaluation.PolicyIdentity {
		t.Fatalf("the migration must be visible as active vs desired: active=%s desired=%s",
			state.ActivePolicyIdentity, state.DesiredPolicyIdentity)
	}
	after, err := os.ReadFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("detecting the migration must not write the Baseline")
	}
}
