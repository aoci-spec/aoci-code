package managedscope

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func initialApprovalFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "fixture"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func buildInitialProposal(t *testing.T, root string) Proposal {
	t.Helper()
	policy := DefaultPolicy(machinecontract.ScopeProfileProduction)
	normalized, err := Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := Build(root, normalized, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return BuildProposal(evaluation, machinecontract.ScopeProfileProduction, 0)
}

// A file the built-in rules EXCLUDE is never opened, so its presence cannot make
// initialization unsafe. Blocking on it made `aoci init` refuse outright on any
// repository that tracks a vendored dependency, a checked-in build output, or a
// test fixture under one of those directory names — and every remediation the
// refusal offered was wrong for that category: the opt-in accepts only
// `sensitive` paths, and it needs the config file that only `init` creates.
func TestExcludedTrackedFilesDoNotBlockInitialApproval(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"vendored framework source": {
			"src/main.go":             "package main\n\nfunc main() {}\n",
			"vendor/cordis/index.mjs": "export const x = 1\n",
		},
		"checked-in build output": {
			"src/main.go":    "package main\n\nfunc main() {}\n",
			"dist/bundle.js": "console.log(1)\n",
		},
		"tracked secret-shaped file": {
			"src/main.go": "package main\n\nfunc main() {}\n",
			".env":        "TOKEN=abc\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := initialApprovalFixture(t, files)
			proposal := buildInitialProposal(t, root)
			if proposal.RequiresHumanApproval {
				t.Fatalf("initialization is blocked by %d excluded tracked path(s).\n"+
					"An excluded path is never opened, so it cannot make initialization unsafe. "+
					"Approval must key on AutoBlockerCount, which counts only paths whose content "+
					"an explicit opt-in will actually read.",
					proposal.RequiredHumanDecisions)
			}
			if proposal.IndexObjects == 0 {
				t.Fatal("fixture precondition: the ordinary source file must still be indexable")
			}
		})
	}
}

// The genuine gate must survive: a path the operator explicitly opted into is
// one whose body will be read, and that still needs a human before auto.
func TestExplicitHighRiskOptInStillBlocksInitialApproval(t *testing.T) {
	root := initialApprovalFixture(t, map[string]string{
		"src/main.go": "package main\n\nfunc main() {}\n",
		".env":        "TOKEN=abc\n",
	})
	policy, err := Normalize(DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := Build(root, policy, BuildOptions{
		WalkOptions: afs.WalkOptions{HighRiskOptIn: []string{".env"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildProposal(evaluation, machinecontract.ScopeProfileProduction, 1); !got.RequiresHumanApproval {
		t.Fatal("an explicit high-risk opt-in no longer requires human approval; " +
			"that path's content is read, so removing this gate would be a real loosening")
	}
	if evaluation.SafeInventory.AutoBlockerCount == 0 {
		t.Fatal("an explicit opt-in did not register as an auto blocker")
	}
}
