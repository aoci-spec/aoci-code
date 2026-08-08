package scopechange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func TestMissingAuthoringTargetCannotBeAutoReducedForTransport(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	indexPath := filepath.Join(root, "aoci.txt")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "main_test.go[T.RT.5.T]: F:test | R:main.go | A:- | S:-\n", ""))
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint.Role = machinecontract.ScopeRoleIndex
	state.Files["aoci.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	candidates.Dispositions = nil
	setFixtureAuthorization(t, root, config.AutomationModeAuto, machinecontract.ScopeApprovalModeInherit)
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "transport-test-observe", Action: machinecontract.ScopeRoleObserve,
			Pattern: "main_test.go", PatternKind: machinecontract.ScopePatternFile, Reason: "test-only fixture",
			DecisionBasis: machinecontract.ScopeDecisionTransportConstraint, Source: machinecontract.ScopeRuleUser,
			CreatedBy: "coverage-test", Order: 0, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Plan.CoverageReductions) != 1 || preview.Plan.CoverageReductions[0].AuthoringState != "missing" ||
		!preview.Plan.Risk.CognitionCoverageReduction || !preview.Plan.Risk.TransportConstraintNotAllowed {
		t.Fatalf("coverage risk was not structured: %+v", preview.Plan)
	}
	if _, err := NewPolicyBoundApproval(root, preview, authorizationTestTime); err == nil ||
		!strings.Contains(err.Error(), "transport_constraint_not_allowed") {
		t.Fatalf("transport-driven auto reduction was not blocked: %v", err)
	}
}
