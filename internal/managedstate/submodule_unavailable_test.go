package managedstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// Review of #215: after `git submodule deinit` the gitlink and team
// configuration are unchanged, yet Maintain offered the submodule's Entries
// as orphans and a batch removed them. A recorded gitlink whose checkout is
// missing locally keeps its governed Entries; re-init finds them current.
func TestDetectKeepsEntriesOfADeinitializedSubmodule(t *testing.T) {
	source := filepath.Join(t.TempDir(), "svc")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	gitStateFixture(t, source, "init", "-q")
	writeStateFixture(t, source, "service.go", "package svc\n")
	gitStateFixture(t, source, "add", "service.go")
	gitStateFixture(t, source, "commit", "-qm", "init")
	root := t.TempDir()
	gitStateFixture(t, root, "init", "-q")
	writeStateFixture(t, root, "README.md", "# super\n")
	gitStateFixture(t, root, "add", "README.md")
	gitStateFixture(t, root, "commit", "-qm", "init")
	gitStateFixture(t, root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "svc")
	gitStateFixture(t, root, "commit", "-qm", "add svc")

	policy := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction)
	cfg := &config.Config{ManagedScope: &policy, RecurseSubmodules: true}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot["svc/service.go"]; !ok {
		t.Fatalf("fixture precondition: the submodule file is governed: %v", snapshot)
	}
	published := baseline.NewBaseline(snapshot)
	published.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired}
	if err := baseline.Save(root, published); err != nil {
		t.Fatal(err)
	}
	document, _ := index.Parse("===段 " + filepath.ToSlash(root) + "/===\nREADME.md[T.T.3.T]: F:readme | R:- | A:- | S:-\n" +
		"===段 " + filepath.ToSlash(root) + "/svc/===\nservice.go[T.T.5.T]: F:service | R:- | A:- | S:-\n")
	index.ResolveRelPaths(document, root)

	gitStateFixture(t, root, "submodule", "deinit", "-q", "-f", "svc")
	state, err := Load(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if state.ScopeChangeRequired {
		t.Fatal("a local deinit changes no team policy")
	}
	detected, err := Detect(root, cfg, document, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range detected.Orphan {
		if path == "svc/service.go" {
			t.Fatalf("an Entry of a deinitialized submodule must not become an orphan: %v", detected.Orphan)
		}
	}
}
