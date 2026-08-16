package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

// A confirmation phrase binds an approval to one exact plan but says nothing
// about what the plan does. These tests lock the part the phrase cannot carry:
// the effect line has to name the real consequences, and it has to stay
// truthful about the posture.

func TestApprovalEffectNamesTheDestinationPostureOnce(t *testing.T) {
	preview := &scopechange.Preview{Plan: scopechange.Plan{
		Risk:                scopechange.Risk{ApprovalPolicyRelaxation: true},
		AuthorizationPolicy: scopechange.ApplyAuthorizationPolicy{EffectiveMode: machinecontract.ApplyAuthorizationAuto},
	}}
	// Preview.Baseline is the postimage, so reading the origin posture from it
	// would print the destination twice and tell the approver nothing. Anything
	// that reintroduces that read fails here.
	effect := managedScopeApprovalEffect(preview)
	if strings.Count(effect, machinecontract.ApplyAuthorizationAuto) != 1 {
		t.Fatalf("posture effect must name the destination exactly once, got %q", effect)
	}
	if !strings.Contains(effect, machinecontract.ApplyAuthorizationAuto) {
		t.Fatalf("posture effect must name the destination posture, got %q", effect)
	}
}

func TestApprovalEffectReportsEveryConsequence(t *testing.T) {
	preview := &scopechange.Preview{Plan: scopechange.Plan{
		EntryRemoves:        []scopechange.EntryChange{{Path: "a.go"}, {Path: "b.go"}},
		CoverageReductions:  []scopechange.CoverageReduction{{Path: "c.go"}},
		Risk:                scopechange.Risk{BudgetRelaxation: true, HighRiskOptIn: true},
		AuthorizationPolicy: scopechange.ApplyAuthorizationPolicy{EffectiveMode: machinecontract.ApplyAuthorizationReview},
	}}
	effect := managedScopeApprovalEffect(preview)
	for _, want := range []string{"2", "1"} {
		if !strings.Contains(effect, want) {
			t.Fatalf("effect must report the counts it was given, got %q", effect)
		}
	}
	if strings.Count(effect, ";") != 3 {
		t.Fatalf("four effects must be reported, got %q", effect)
	}
}

func TestApprovalEffectSaysSoWhenNothingIsLost(t *testing.T) {
	preview := &scopechange.Preview{Plan: scopechange.Plan{
		AuthorizationPolicy: scopechange.ApplyAuthorizationPolicy{EffectiveMode: machinecontract.ApplyAuthorizationReview},
	}}
	// Silence would read as "unknown consequence" and push the approver to guess.
	if effect := managedScopeApprovalEffect(preview); strings.TrimSpace(effect) == "" {
		t.Fatal("a plan with no destructive effect must still state that plainly")
	}
}
