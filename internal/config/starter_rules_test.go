package config

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func starterRuleCount(rules []managedscope.Rule) int {
	count := 0
	for _, rule := range rules {
		if strings.HasPrefix(rule.RuleID, managedscope.StarterRuleIDPrefix) {
			count++
		}
	}
	return count
}

// Init is the only caller of SetNewProjectGovernance, and it calls it only
// after proving config.json did not exist, so this is the one place the
// starter rules may enter a repository. Every other resolution path — the
// bare DefaultPolicy, a config without a managed_scope block, the other
// profiles — must stay exactly as old repositories recorded it.
func TestSetNewProjectGovernanceWritesStarterRulesForProductionOnly(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if cfg.ManagedScope == nil || len(cfg.ManagedScope.Rules) != 7 || starterRuleCount(cfg.ManagedScope.Rules) != 7 {
		t.Fatalf("a new production repository must carry exactly the seven starter rules: %+v", cfg.ManagedScope)
	}
	for _, rule := range cfg.ManagedScope.Rules {
		if rule.Action != machinecontract.ScopeRoleObserve || rule.Source != machinecontract.ScopeRuleBuiltin {
			t.Fatalf("starter rule persisted with the wrong contract: %+v", rule)
		}
	}
	root := t.TempDir()
	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ManagedScope == nil || starterRuleCount(loaded.ManagedScope.Rules) != 7 {
		t.Fatalf("starter rules did not survive the config round trip: %+v", loaded.ManagedScope)
	}
	for _, profile := range []string{machinecontract.ScopeProfileFull, machinecontract.ScopeProfileCustom} {
		other := DefaultConfig()
		if err := other.SetNewProjectGovernance(profile); err != nil {
			t.Fatal(err)
		}
		if other.ManagedScope == nil || len(other.ManagedScope.Rules) != 0 {
			t.Fatalf("profile %q must not receive starter rules: %+v", profile, other.ManagedScope)
		}
	}
	if rules := managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction).Rules; len(rules) != 0 {
		t.Fatalf("DefaultPolicy is the preimage old repositories resolve and must stay rule-free: %+v", rules)
	}
	for _, old := range []*Config{nil, {}, DefaultConfig()} {
		if count := starterRuleCount(old.EffectiveManagedScope().Rules); count != 0 {
			t.Fatalf("a config without a managed_scope block resolved %d starter rules", count)
		}
	}
}
