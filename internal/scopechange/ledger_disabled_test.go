package scopechange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A team that disabled the Ledger can still apply a Scope Change. The
// terminal event used to be required after the formal writes, so the change
// stayed applied but pending, and resume and rollback refused for the same
// missing Ledger: no command could close it (audit G1).
func TestScopeChangeCompletesWithTheLedgerDisabled(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := NewPolicyBoundApproval(root, preview, authorizationTestTime)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyAuthorized(root, preview, nil, receipt)
	if err != nil || result.Status != "applied" {
		t.Fatalf("a Scope Change must complete without a Ledger: %v %+v", err, result)
	}
	pending, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "scope-*.json"))
	if len(pending) != 0 {
		t.Fatalf("no Scope receipt may stay pending: %v", pending)
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "ledger.jsonl")); err == nil {
		data, _ := os.ReadFile(filepath.Join(root, ".aoci", "ledger.jsonl"))
		if len(data) > 0 && strings.Contains(string(data), `"op":"managed_scope_change_apply"`) {
			t.Fatal("a disabled Ledger must not be written")
		}
	}
}
