// The migration cost of the historical case identity must be statable.
//
// The applied scope identity used to count the host filesystem's probe result, so the same repository got different governance
// identities on Linux and Windows. Once the identity became host-independent, a historical receipt recorded with "false"
// needs one governance Scope Change; this case locks the real shape of that migration: no role moves,
// no entry moves, the Whole-Index bytes are unchanged, and policy-bound auto can authorize it without a human.
//
// This is not an optimistic estimate; it is the criterion of whether the migration can land before GA. A real case divergence still becomes a
// real role change, routed to independent approval, outside this case's scope.
package scopechange

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestHistoricalCaseIdentityMigratesIdentityOnlyUnderAuto(t *testing.T) {
	root, _ := buildChangeFixture(t)
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeAuto)

	// The fixture changes the expected policy to production to manufacture a role change; here it goes back to full, so the only difference
	// left is the historical case bit, and the migration shape is visible.
	full := managedscope.DefaultPolicy(machinecontract.ScopeProfileFull)
	normalized, err := managedscope.Normalize(full)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &normalized
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	evaluation, err := managedscope.Build(root, normalized, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
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

	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.ManagedScope == nil {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	state.ManagedScope.PolicyIdentity = legacy
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	empty := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
	preview, err := Build(root, authorizationTestTime, empty)
	if err != nil {
		t.Fatalf("a historical case identity must still produce a plan: %v", err)
	}
	if preview.Plan.OldPolicyIdentity != legacy || preview.Plan.NewPolicyIdentity == legacy {
		t.Fatalf("the plan must migrate from the legacy identity: old=%s new=%s",
			preview.Plan.OldPolicyIdentity, preview.Plan.NewPolicyIdentity)
	}
	if len(preview.Plan.RoleChanges) != 0 || len(preview.Plan.EntryCreates) != 0 ||
		len(preview.Plan.EntryUpdates) != 0 || len(preview.Plan.EntryRemoves) != 0 ||
		len(preview.Plan.CoverageReductions) != 0 {
		t.Fatalf("the case-identity migration must be identity-only: roles=%d creates=%d updates=%d "+
			"removes=%d reductions=%d", len(preview.Plan.RoleChanges), len(preview.Plan.EntryCreates),
			len(preview.Plan.EntryUpdates), len(preview.Plan.EntryRemoves), len(preview.Plan.CoverageReductions))
	}
	if preview.IndexPostimage.PreimageSHA256 != preview.IndexPostimage.PostimageSHA256 {
		t.Fatal("an identity-only migration must leave the Whole-Index byte-identical")
	}
	if preview.Plan.InteractionRequired {
		t.Fatal("an identity-only migration carries no ratifiable blocker, so it must not demand a reviewer")
	}
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err != nil {
		t.Fatalf("policy-bound auto must authorize an identity-only migration: %v", err)
	}
}
