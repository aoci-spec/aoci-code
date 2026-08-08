package scopechange

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func buildSourceGuardFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "-C", root, "init", "-q")
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeChangeFixture(t, root, "main.go", "package sample\n")
	writeChangeFixture(t, root, "main_test.go", "package sample\n")
	writeChangeFixture(t, root, "aoci.txt", "# test index\n==="+filepath.ToSlash(root)+"/===\n"+
		"aoci.txt[A.IX.9.T]: F:index | R:- | A:- | S:-\n"+
		"main.go[C.RT.9.T]: F:production | R:- | A:- | S:-\n")
	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	value := baseline.NewBaseline(files)
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: cfg.CognitionBudget}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	return root
}

func reviewedObserveCandidates() CandidateSet {
	return CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{},
		ObserveReview: &ObserveReview{Paths: []string{"main_test.go"}, ReviewStatus: ReviewStatusReviewed, Reviewer: "source-guard-test"}}
}

func TestObserveAcknowledgeBindsLivePlanPreimageWithExistingIndexDebt(t *testing.T) {
	root := buildSourceGuardFixture(t)
	active, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	writeChangeFixture(t, root, "aoci.txt", string(mustReadChangeFixture(t, filepath.Join(root, "aoci.txt")))+"# legally advanced before Scope Plan\n")
	writeChangeFixture(t, root, "main.go", "package sample\n// indexed cognition debt\n")
	writeChangeFixture(t, root, "main_test.go", "package sample\n// reviewed observe change\n")

	preview, err := Build(root, "2026-08-05T00:00:00Z", reviewedObserveCandidates())
	if err != nil {
		t.Fatal(err)
	}
	rootNow, _ := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	mainNow, _ := baseline.HashFile(filepath.Join(root, "main.go"))
	if preview.SourceGuard["aoci.txt"].SHA256 != rootNow.SHA256 || preview.SourceGuard["main.go"].SHA256 != mainNow.SHA256 {
		t.Fatalf("source guard did not bind live Plan preimage: guard=%#v", preview.SourceGuard)
	}
	if preview.Baseline.Files["main.go"].SHA256 != active.Files["main.go"].SHA256 ||
		preview.Baseline.Files["main.go"].SHA256 == preview.SourceGuard["main.go"].SHA256 {
		t.Fatal("observe-only Baseline did not preserve independent indexed cognition debt")
	}
	result, err := Apply(root, preview, nil)
	if err != nil || result.Status != "applied" {
		t.Fatalf("observe acknowledgement rejected legal pre-Plan state: result=%#v err=%v", result, err)
	}
	after, exists, err := baseline.Load(root)
	if err != nil || !exists || after.Files["aoci.txt"].SHA256 != rootNow.SHA256 ||
		after.Files["main.go"].SHA256 != active.Files["main.go"].SHA256 ||
		after.Files["main_test.go"].SHA256 == active.Files["main_test.go"].SHA256 {
		t.Fatalf("acknowledgement advanced an unintended fingerprint: exists=%t err=%v baseline=%#v", exists, err, after)
	}
}

func TestLegacyRecoveryIntentWithoutSourceGuardRemainsReadable(t *testing.T) {
	root := buildSourceGuardFixture(t)
	writeChangeFixture(t, root, "main_test.go", "package sample\n// reviewed\n")
	preview, err := Build(root, "2026-08-05T00:00:30Z", reviewedObserveCandidates())
	if err != nil {
		t.Fatal(err)
	}
	legacy := *preview
	legacy.SourceGuard = nil
	legacy.PreviewID, err = previewIdentity(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacy.EnvelopeDigest = legacy.PreviewID
	intent := &RecoveryIntent{Version: machinecontract.ManagedScopeRecoveryV2, Operation: Operation,
		TransactionID: legacy.EnvelopeDigest[:24], Preview: legacy, Staging: nil, Preimages: nil,
		CreatedAt: "2026-08-05T00:00:30Z"}
	intent.RecoveryDigest, err = recoveryIdentity(*intent)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Encode(intent)
	if err != nil {
		t.Fatal(err)
	}
	var loaded RecoveryIntent
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	want, err := recoveryIdentity(loaded)
	if err != nil || want != loaded.RecoveryDigest || len(loaded.Preview.SourceGuard) != 0 {
		t.Fatalf("legacy source-guard-free recovery identity changed: loaded=%#v want=%s err=%v", loaded, want, err)
	}
}

func TestSourceGuardRejectsPostPlanRootSourceBaselineAndPolicyChanges(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		root := buildSourceGuardFixture(t)
		writeChangeFixture(t, root, "main_test.go", "package sample\n// reviewed\n")
		preview, err := Build(root, "2026-08-05T00:01:00Z", reviewedObserveCandidates())
		if err != nil {
			t.Fatal(err)
		}
		writeChangeFixture(t, root, "aoci.txt", string(mustReadChangeFixture(t, filepath.Join(root, "aoci.txt")))+"# post-Plan drift\n")
		intent := &RecoveryIntent{Preview: *preview}
		if err := validateSourceGuards(root, intent); err == nil || err.Error() != "managed_scope_source_guard_snapshot_changed" {
			t.Fatalf("post-Plan Root drift was not source-guarded: %v", err)
		}
	})

	t.Run("managed_source_zero_write_rollback_and_replan", func(t *testing.T) {
		root := buildSourceGuardFixture(t)
		writeChangeFixture(t, root, "main_test.go", "package sample\n// reviewed\n")
		preview, err := Build(root, "2026-08-05T00:02:00Z", reviewedObserveCandidates())
		if err != nil {
			t.Fatal(err)
		}
		formalBefore := map[string][]byte{}
		for _, rel := range []string{"aoci.txt", ".aoci/config.json", ".aoci/baseline.json"} {
			formalBefore[rel] = mustReadChangeFixture(t, filepath.Join(root, filepath.FromSlash(rel)))
		}
		originalFault := transactionFault
		transactionFault = func(point string) error {
			if point == "after_intent" {
				return errors.New("stop after immutable intent")
			}
			return nil
		}
		t.Cleanup(func() { transactionFault = originalFault })
		if _, err := Apply(root, preview, nil); err == nil {
			t.Fatal("fixture did not stop after Intent")
		}
		transactionFault = func(string) error { return nil }
		writeChangeFixture(t, root, "main.go", "package sample\n// post-Plan source drift\n")
		if _, err := Resume(root, preview.EnvelopeDigest[:24]); err == nil || err.Error() != "managed_scope_source_guard_snapshot_changed" {
			t.Fatalf("post-Plan managed source drift was not rejected: %v", err)
		}
		for rel, before := range formalBefore {
			if after := mustReadChangeFixture(t, filepath.Join(root, filepath.FromSlash(rel))); !bytes.Equal(after, before) {
				t.Fatalf("source guard failure changed formal asset %s", rel)
			}
		}
		rolledBack, err := Rollback(root, preview.EnvelopeDigest[:24])
		if err != nil || len(rolledBack.WrittenPaths) != 0 || len(rolledBack.RecoveredPaths) != 0 {
			t.Fatalf("zero-write rollback was not exact: result=%#v err=%v", rolledBack, err)
		}
		replanned, err := Build(root, "2026-08-05T00:03:00Z", reviewedObserveCandidates())
		if err != nil {
			t.Fatal(err)
		}
		applied, err := Apply(root, replanned, nil)
		if err != nil || applied.Status != "applied" {
			t.Fatalf("fresh Plan remained permanently blocked: result=%#v err=%v", applied, err)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{name: "baseline", mutate: func(t *testing.T, root string) {
			value, exists, err := baseline.Load(root)
			if err != nil || !exists {
				t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
			}
			fingerprint := value.Files["main.go"]
			fingerprint.SHA256 = strings.Repeat("0", 64)
			fingerprint.NormalizedSHA256 = strings.Repeat("0", 64)
			value.Files["main.go"] = fingerprint
			if err := baseline.Save(root, value); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "policy", mutate: func(t *testing.T, root string) {
			if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
				policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "post-plan-policy", Action: machinecontract.ScopeRoleExclude,
					Pattern: "future.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "test drift",
					Source: machinecontract.ScopeRuleUser, CreatedBy: "test", Order: 10, Enabled: true})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildSourceGuardFixture(t)
			writeChangeFixture(t, root, "main_test.go", "package sample\n// reviewed\n")
			preview, err := Build(root, "2026-08-05T00:04:00Z", reviewedObserveCandidates())
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, root)
			if _, err := Apply(root, preview, nil); err == nil {
				t.Fatalf("post-Plan %s change was not fail-closed: %v", test.name, err)
			}
		})
	}
}

func mustReadChangeFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
