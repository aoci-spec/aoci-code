package config

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// A repository whose config.json carries no cognition_budget block resolves its
// budget at runtime, and managedstate.Load compares that resolution against the
// identity stamped in .aoci/baseline.json. This test pins the whole resolution
// path, not just the policy literal, because the wedge is reachable only through
// EffectiveCognitionBudget: raising the new-project default must stay invisible
// here.
func TestNilBudgetConfigResolvesTheFrozenLegacyIdentity(t *testing.T) {
	const frozen = "a98988839d1818e2faa245e355c256be3698f7a3552edf87338cb8ce48444eb7"
	for _, cfg := range []*Config{nil, {}, {ManagedScope: &managedscope.Policy{}}} {
		identity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
		if err != nil {
			t.Fatalf("identity: %v", err)
		}
		if identity != frozen {
			t.Fatalf("a config with no cognition_budget block now resolves %s, not the frozen %s; "+
				"every pre-budget repository would be forced into a Scope Change on upgrade", identity, frozen)
		}
	}
}
