package bootstrapapply

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestOfficialInitSkeletonBootstrapsAndRollsBackExactly(t *testing.T) {
	root := t.TempDir()
	templateSource, err := textassets.Load("en-US", textassets.TemplateMinimalIndex)
	if err != nil {
		t.Fatal(err)
	}
	minimal, err := hooks.RenderTemplate("minimal-index.txt.tmpl", templateSource, hooks.NewTplData(root))
	if err != nil {
		t.Fatal(err)
	}
	writeBootstrapFile(t, root, "aoci.txt", minimal)
	envelope, approval := preparedFixture(t, root, nil)
	rootTarget := formalPostimageByPath(envelope, "aoci.txt")
	if rootTarget == nil || rootTarget.ExpectedPreimage != PreimageOfficialMinimal ||
		rootTarget.PreimageSHA256 == "" || rootTarget.PreimageContent != minimal {
		t.Fatalf("official skeleton preimage was not bound: %#v", rootTarget)
	}
	previousFault := bootstrapFault
	bootstrapFault = func(point string) error {
		if point == "after_publish_root" {
			return fmt.Errorf("test-only root-last interruption")
		}
		return nil
	}
	t.Cleanup(func() { bootstrapFault = previousFault })
	if _, err := Apply(root, envelope, approval); err == nil {
		t.Fatal("expected test-only interruption after Root publish")
	}
	pending, err := Pending(root)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending Bootstrap missing: pending=%v err=%v", pending, err)
	}
	bootstrapFault = previousFault
	result, err := Rollback(root, pending[0])
	if err != nil || result.Status != StatusRolledBack {
		t.Fatalf("Rollback failed: result=%#v err=%v", result, err)
	}
	if got := string(readOptional(t, filepath.Join(root, "aoci.txt"))); got != minimal {
		t.Fatal("Rollback did not restore the exact official minimal skeleton")
	}
}

func TestBootstrapEnvelopeDigestIgnoresAuditTimestampCopy(t *testing.T) {
	envelope := ApplyEnvelope{PreparedAt: "2026-08-01T00:00:00Z", Baseline: BaselinePostimage{Content: "candidate-a"}}
	first, err := envelopeDigest(&envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.PreparedAt = "2026-08-02T00:00:00Z"
	second, err := envelopeDigest(&envelope)
	if err != nil || first != second {
		t.Fatalf("Envelope digest must ignore its audit timestamp copy: first=%s second=%s err=%v", first, second, err)
	}
	envelope.Baseline.Content = "candidate-b"
	changed, err := envelopeDigest(&envelope)
	if err != nil || changed == first {
		t.Fatal("Envelope digest must continue binding formal candidate bytes")
	}
}

func TestPolicyBoundAutoBootstrapUsesFreshDefaultAndHonorsExplicitReview(t *testing.T) {
	root := t.TempDir()
	plan, candidate, preview := authoredFixture(t, root, nil)
	envelope, err := Prepare(root, &ApplyRequest{
		Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
		Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-08-02T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordPolicyBoundAutoApproval(root, envelope, "2026-08-02T00:00:01Z")
	if err != nil || approval.Mechanism != AutoApprovalMechanism {
		t.Fatalf("policy-bound approval failed: approval=%#v err=%v", approval, err)
	}
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != StatusApplied {
		t.Fatalf("policy-bound Apply failed: result=%#v err=%v", result, err)
	}
	repeated, err := Apply(root, envelope, approval)
	if err != nil || repeated.Status != StatusAlreadyApplied {
		t.Fatalf("policy-bound Apply was not idempotent: result=%#v err=%v", repeated, err)
	}

	reviewRoot := t.TempDir()
	reviewConfig := config.DefaultConfig()
	if err := reviewConfig.SetAutomationMode(config.AutomationModeReview); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(reviewRoot, reviewConfig); err != nil {
		t.Fatal(err)
	}
	reviewEnvelope, _ := preparedFixture(t, reviewRoot, nil)
	if _, err := RecordPolicyBoundAutoApproval(reviewRoot, reviewEnvelope, "2026-08-02T00:00:01Z"); err == nil ||
		!strings.Contains(err.Error(), machinecontract.CognitionAutoBlockerPolicyNotAuto) {
		t.Fatalf("explicit review repository received auto approval: %v", err)
	}

	offRoot := t.TempDir()
	offConfig := config.DefaultConfig()
	if err := offConfig.SetAutomationMode(config.AutomationModeOff); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(offRoot, offConfig); err != nil {
		t.Fatal(err)
	}
	offPlan, offCandidate, offPreview := authoredFixture(t, offRoot, nil)
	offEnvelope, err := Prepare(offRoot, &ApplyRequest{
		Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *offPlan,
		Candidate: *offCandidate, Preview: *offPreview, BaselineTimestamp: "2026-08-02T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordApproval(offEnvelope, "test-human", "2026-08-02T00:00:01Z", offEnvelope.EnvelopeDigest); err == nil ||
		!strings.Contains(err.Error(), "bootstrap_automation_off_apply_forbidden") {
		t.Fatalf("off repository created an Apply approval: %v", err)
	}
}

func TestPolicyBoundAutoBootstrapRejectsMatureGovernanceHistory(t *testing.T) {
	tests := []struct {
		name     string
		relative string
		content  string
	}{
		{name: "governance-receipt", relative: ".aoci/governance/entries-old.json", content: "{}\n"},
		{name: "historical-transaction", relative: ".aoci/transactions/history/bootstrap-old.json", content: "{}\n"},
		{name: "curation", relative: ".aoci/curation.json", content: "{}\n"},
		{name: "write-ledger", relative: ".aoci/ledger.jsonl", content: `{"op":"entries_apply"}` + "\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultConfig()
			if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
				t.Fatal(err)
			}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			plan, candidate, preview := authoredFixture(t, root, nil)
			envelope, err := Prepare(root, &ApplyRequest{
				Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
				Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-08-02T00:00:00Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			writeBootstrapFile(t, root, test.relative, test.content)
			if _, err := RecordPolicyBoundAutoApproval(root, envelope, "2026-08-02T00:00:01Z"); err == nil ||
				!strings.Contains(err.Error(), machinecontract.CognitionAutoBlockerGovernanceHistory) {
				t.Fatalf("mature governance history received auto approval: %v", err)
			}
		})
	}
}

func TestPolicyBoundAutoBootstrapAllowsReadOnlyRunLedger(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, candidate, preview := authoredFixture(t, root, nil)
	envelope, err := Prepare(root, &ApplyRequest{
		Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
		Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-08-02T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"rules", "overview", "verify", "check"} {
		ledger.Append(root, true, ledger.Event{Op: operation, Result: ledger.ResultOK})
	}
	approval, err := RecordPolicyBoundAutoApproval(root, envelope, "2026-08-02T00:00:01Z")
	if err != nil || approval.Mechanism != AutoApprovalMechanism {
		t.Fatalf("read-only current-run Ledger blocked Auto Bootstrap: approval=%#v err=%v", approval, err)
	}
}

func TestPolicyBoundAutoBootstrapAllows196TrackedExcludeOnlyObjects(t *testing.T) {
	root := t.TempDir()
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	paths := make([]string, 0, 196)
	for index := 0; index < 196; index++ {
		path := fmt.Sprintf("dist/chunk-%03d.js", index)
		writeBootstrapFile(t, root, path, "generated distribution artifact\n")
		paths = append(paths, path)
	}
	arguments := append([]string{"-C", root, "add", "--"}, paths...)
	if output, err := exec.Command("git", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, output)
	}
	plan, candidate, preview := authoredFixture(t, root, nil)
	if plan.SafeInventory.ReviewVisibleCount != 196 || plan.SafeInventory.RequiredHumanReview != 196 ||
		plan.SafeInventory.AutoBlockerCount != 0 {
		t.Fatalf("exclude-only inventory was misclassified: %#v", plan.SafeInventory)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionBootstrapApplyRequestV1,
		Plan: *plan, Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-08-06T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := EvaluateAutoEligibility(root, envelope)
	if err != nil || !projection.AutoReady || projection.AutoBlockerCount != 0 || projection.ReviewVisibleCount != 196 {
		t.Fatalf("196 exclude-only objects blocked Auto: projection=%#v err=%v", projection, err)
	}
	if _, err := RecordPolicyBoundAutoApproval(root, envelope, "2026-08-06T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyBoundAutoBootstrapBlocksSensitiveReadOptIn(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, ".env", "TEST_ONLY_SECRET=redacted\n")
	cfg := config.DefaultConfig()
	cfg.SafeInventoryHighRiskOptIn = []string{".env"}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	plan, candidate, preview := authoredFixture(t, root, []string{"code"})
	if plan.SafeInventory.AutoBlockerCount != 1 {
		t.Fatalf("sensitive read opt-in was not classified: %#v", plan.SafeInventory)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionBootstrapApplyRequestV1,
		Plan: *plan, Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-08-06T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := EvaluateAutoEligibility(root, envelope)
	if err != nil || projection.AutoReady || projection.AutoBlockerCount == 0 {
		t.Fatalf("sensitive read opt-in passed Auto: projection=%#v err=%v", projection, err)
	}
	if _, err := RecordPolicyBoundAutoApproval(root, envelope, "2026-08-06T00:00:01Z"); err == nil ||
		!strings.Contains(err.Error(), machinecontract.CognitionAutoBlockerSensitiveRead) {
		t.Fatalf("sensitive read opt-in received Auto approval: %v", err)
	}
}

func TestBootstrapApplyRootMetaAndCodeMatrix(t *testing.T) {
	for _, kinds := range [][]string{nil, {"code"}} {
		name := "root-meta"
		if len(kinds) > 0 {
			name = "root-meta-code"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if len(kinds) > 0 {
				writeBootstrapFile(t, root, "main.go", "package main\n")
			}
			envelope, approval := preparedFixture(t, root, kinds)
			beforeSource := readOptional(t, filepath.Join(root, "main.go"))
			result, err := Apply(root, envelope, approval)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusApplied || !result.LayoutActivated || !result.FormalComplete || result.NetworkAccessed {
				t.Fatalf("unexpected result: %#v", result)
			}
			set, err := cognition.Load(root, "aoci.txt")
			if err != nil || set.CompositeIdentity != envelope.ProjectedCompositeIdentity {
				t.Fatalf("active layout mismatch: set=%#v err=%v", set, err)
			}
			if got := readOptional(t, filepath.Join(root, "main.go")); string(got) != string(beforeSource) {
				t.Fatal("business source changed during Bootstrap")
			}
			status, err := Status(root, result.TransactionID)
			if err != nil || status.Status != StatusApplied || status.RecoveryPending {
				t.Fatalf("terminal status mismatch: %#v err=%v", status, err)
			}
			repeated, err := Apply(root, envelope, approval)
			if err != nil || repeated.Status != StatusAlreadyApplied {
				t.Fatalf("repeated Apply not idempotent: %#v err=%v", repeated, err)
			}
		})
	}
}

func TestBootstrapApplyDatabaseAndCodeDatabaseMatrix(t *testing.T) {
	for _, kinds := range [][]string{{"database"}, {"code", "database"}} {
		t.Run(strings.Join(kinds, "+"), func(t *testing.T) {
			root := t.TempDir()
			if len(kinds) == 2 {
				writeBootstrapFile(t, root, "frontend/src/App.jsx", "export function App() { return null; }\n")
				writeBootstrapFile(t, root, "backend/src/api.js", "export const route = '/items';\n")
				writeBootstrapFile(t, root, "backend/src/service.js", "export const listItems = () => [];\n")
			}
			installBootstrapDatabaseEvidence(t, root)
			envelope, approval := preparedFixture(t, root, kinds)
			result, err := Apply(root, envelope, approval)
			if err != nil || result.Status != StatusApplied || result.NetworkAccessed {
				t.Fatalf("database Bootstrap failed: %#v err=%v", result, err)
			}
			value, exists, err := baseline.Load(root)
			if err != nil || !exists || value.DatabaseCognition == nil || len(value.DatabaseCognition.Entries) != 1 {
				t.Fatalf("Database Binding missing: %#v exists=%t err=%v", value, exists, err)
			}
			if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRepresentativeReactNodeMySQLBootstrap(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join("..", "cognitionplan", "testdata", "pilot-react-node-mysql")
	if err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.Contains(filepath.ToSlash(path), "/legacy/") {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeBootstrapFile(t, root, filepath.ToSlash(rel), string(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	installBootstrapDatabaseEvidence(t, root)
	envelope, approval := preparedFixture(t, root, []string{"code", "database"})
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != StatusApplied || result.NetworkAccessed {
		t.Fatalf("representative Bootstrap failed: %#v err=%v", result, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || len(set.Volumes["code"].Objects) != 3 || len(set.Volumes["database"].Objects) != 1 {
		t.Fatalf("representative scope mismatch: set=%#v err=%v", set, err)
	}
}

func TestBootstrapResumeAfterRootBeforeBaseline(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, "main.go", "package main\n")
	envelope, approval := preparedFixture(t, root, []string{"code"})
	previous := bootstrapFault
	bootstrapFault = func(step string) error {
		if step == "before_publish_baseline" {
			return os.ErrPermission
		}
		return nil
	}
	_, err := Apply(root, envelope, approval)
	bootstrapFault = previous
	if err == nil {
		t.Fatal("injected failure was ignored")
	}
	transactionID := transactionIdentity(envelope, approval)
	status, err := Status(root, transactionID)
	if err != nil || status.Status != StatusRecoveryRequiredActive || !status.LayoutActivated {
		t.Fatalf("active pending status mismatch: %#v err=%v", status, err)
	}
	result, err := Resume(root, transactionID)
	if err != nil || result.Status != StatusApplied {
		t.Fatalf("resume failed: %#v err=%v", result, err)
	}
}

func TestBootstrapRollbackMovesIncompletePostimages(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, "main.go", "package main\n")
	envelope, approval := preparedFixture(t, root, []string{"code"})
	previous := bootstrapFault
	bootstrapFault = func(step string) error {
		if step == "before_publish_code" {
			return os.ErrPermission
		}
		return nil
	}
	_, err := Apply(root, envelope, approval)
	bootstrapFault = previous
	if err == nil {
		t.Fatal("injected failure was ignored")
	}
	transactionID := transactionIdentity(envelope, approval)
	result, err := Rollback(root, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusRolledBack || len(result.RecoveredPaths) != 1 || result.RecoveredPaths[0] != "aoci.meta.txt" {
		t.Fatalf("rollback result mismatch: %#v", result)
	}
	for _, path := range envelope.WriteOrder {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("formal target remains after rollback: %s err=%v", path, err)
		}
	}
	status, err := Status(root, transactionID)
	if err != nil || status.Status != StatusRolledBack || status.RecoveryPending {
		t.Fatalf("rollback terminal status mismatch: %#v err=%v", status, err)
	}
}

func TestBootstrapFaultBoundariesConvergeDeterministically(t *testing.T) {
	if testing.Short() {
		t.Skip("full fault matrix runs in Full Confidence")
	}
	steps := []string{
		"before_stage_0", "after_stage_0", "before_recovery_intent", "after_recovery_intent",
		"before_publish_meta", "after_publish_meta", "before_publish_root", "after_publish_root",
		"before_publish_baseline", "after_publish_baseline", "before_internal_verify", "after_internal_verify",
		"before_completion_receipt", "after_completion_receipt", "before_ledger", "after_ledger",
		"before_transaction_archive", "after_transaction_archive",
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			envelope, approval := preparedFixture(t, root, nil)
			previous := bootstrapFault
			fired := false
			bootstrapFault = func(current string) error {
				if !fired && current == step {
					fired = true
					return os.ErrPermission
				}
				return nil
			}
			_, err := Apply(root, envelope, approval)
			bootstrapFault = previous
			if err == nil || !fired {
				t.Fatalf("fault %s was not observed: %v", step, err)
			}
			transactionID := transactionIdentity(envelope, approval)
			pending, pendingErr := Pending(root)
			if pendingErr != nil {
				t.Fatal(pendingErr)
			}
			var result *ApplyResult
			if len(pending) == 1 {
				status, statusErr := Status(root, transactionID)
				if statusErr != nil || status.Status == StatusRecoveryConflict {
					t.Fatalf("fault produced unrecoverable state: %#v err=%v", status, statusErr)
				}
				result, err = Resume(root, transactionID)
			} else {
				// Before Intent, retrying the same approved request reuses exact
				// staging. After archive, it returns already_applied.
				result, err = Apply(root, envelope, approval)
			}
			if err != nil || (result.Status != StatusApplied && result.Status != StatusAlreadyApplied) {
				t.Fatalf("fault did not converge: result=%#v err=%v", result, err)
			}
			if step == "after_ledger" {
				events, corrupt := ledger.Recent(root, 0)
				count := 0
				for _, event := range events {
					if event.Op == "cognition_bootstrap_apply" && event.RecoveryTransactionID == transactionID {
						count++
					}
				}
				if corrupt != 0 || count != 1 {
					t.Fatalf("Resume duplicated the terminal Ledger event: count=%d corrupt=%d", count, corrupt)
				}
			}
		})
	}
}

func TestBootstrapObjectVolumeFaultBoundariesConverge(t *testing.T) {
	if testing.Short() {
		t.Skip("full object-volume fault matrix runs in Full Confidence")
	}
	steps := []string{
		"before_publish_code", "after_publish_code",
		"before_publish_database", "after_publish_database",
		"before_publish_root", "after_publish_root",
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			root := t.TempDir()
			writeBootstrapFile(t, root, "app.go", "package fixture\n")
			installBootstrapDatabaseEvidence(t, root)
			envelope, approval := preparedFixture(t, root, []string{"code", "database"})
			previous := bootstrapFault
			fired := false
			bootstrapFault = func(current string) error {
				if !fired && current == step {
					fired = true
					return os.ErrPermission
				}
				return nil
			}
			_, err := Apply(root, envelope, approval)
			bootstrapFault = previous
			if err == nil || !fired {
				t.Fatalf("fault %s was not observed: %v", step, err)
			}
			transactionID := transactionIdentity(envelope, approval)
			status, statusErr := Status(root, transactionID)
			if statusErr != nil || status.Status == StatusRecoveryConflict {
				t.Fatalf("object fault produced invalid state: %#v err=%v", status, statusErr)
			}
			if step == "before_publish_root" {
				for _, path := range []string{"aoci.meta.txt", "aoci.code.txt", "aoci.database.txt"} {
					if targetStatusByPath(status, path).DiskState != StatePostimage {
						t.Fatalf("Root failure did not preserve the complete object prefix: %#v", status.Targets)
					}
				}
				if targetStatusByPath(status, "aoci.txt").DiskState != StatePreimage {
					t.Fatalf("Root appeared before its publication boundary: %#v", status.Targets)
				}
			}
			result, err := Resume(root, transactionID)
			if err != nil || result.Status != StatusApplied {
				t.Fatalf("object fault did not converge: %#v err=%v", result, err)
			}
		})
	}
}

func TestConcurrentBootstrapApplyAndRecoverySerialize(t *testing.T) {
	t.Run("same-approved-apply", func(t *testing.T) {
		root := t.TempDir()
		envelope, approval := preparedFixture(t, root, nil)
		results := make(chan *ApplyResult, 2)
		errors := make(chan error, 2)
		var wait sync.WaitGroup
		for index := 0; index < 2; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				result, err := Apply(root, envelope, approval)
				results <- result
				errors <- err
			}()
		}
		wait.Wait()
		close(results)
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		statuses := map[string]int{}
		for result := range results {
			statuses[result.Status]++
		}
		if statuses[StatusApplied] != 1 || statuses[StatusAlreadyApplied] != 1 {
			t.Fatalf("concurrent Apply outcomes were not idempotent: %#v", statuses)
		}
	})

	t.Run("resume-versus-rollback", func(t *testing.T) {
		root := t.TempDir()
		envelope, approval := preparedFixture(t, root, nil)
		previous := bootstrapFault
		bootstrapFault = func(step string) error {
			if step == "after_recovery_intent" {
				return os.ErrPermission
			}
			return nil
		}
		_, _ = Apply(root, envelope, approval)
		bootstrapFault = previous
		transactionID := transactionIdentity(envelope, approval)
		var wait sync.WaitGroup
		errors := make(chan error, 2)
		wait.Add(2)
		go func() {
			defer wait.Done()
			_, err := Resume(root, transactionID)
			errors <- err
		}()
		go func() {
			defer wait.Done()
			_, err := Rollback(root, transactionID)
			errors <- err
		}()
		wait.Wait()
		close(errors)
		successes := 0
		for err := range errors {
			if err == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("Resume/Rollback serialization successes=%d", successes)
		}
		status, err := Status(root, transactionID)
		if err != nil || (status.Status != StatusApplied && status.Status != StatusRolledBack) || status.RecoveryPending {
			t.Fatalf("concurrent recovery did not reach one terminal state: %#v err=%v", status, err)
		}
	})
}

func TestBootstrapMissingStagingAndNonPrefixDiskStateConflict(t *testing.T) {
	root := t.TempDir()
	writeBootstrapFile(t, root, "main.go", "package main\n")
	envelope, approval := preparedFixture(t, root, []string{"code"})
	previous := bootstrapFault
	bootstrapFault = func(step string) error {
		if step == "after_recovery_intent" {
			return os.ErrPermission
		}
		return nil
	}
	_, _ = Apply(root, envelope, approval)
	bootstrapFault = previous
	transactionID := transactionIdentity(envelope, approval)
	if err := os.Remove(filepath.Join(root, ".aoci", "transactions", "bootstrap-"+transactionID, "staging", "01.post")); err != nil {
		t.Fatal(err)
	}
	writeBootstrapFile(t, root, "aoci.txt", envelope.Targets[len(envelope.Targets)-1].Content)
	status, err := Status(root, transactionID)
	if err != nil || status.Status != StatusRecoveryConflict || !status.ThirdPartyConflict {
		t.Fatalf("invalid recovery state was accepted: %#v err=%v", status, err)
	}
}

func TestPrepareRejectsAnyExistingBaselineAndDrift(t *testing.T) {
	root := t.TempDir()
	plan, candidate, preview := authoredFixture(t, root, nil)
	writeBootstrapFile(t, root, ".aoci/baseline.json", "{\"version\":1,\"created_at\":\"2026-01-01T00:00:00Z\",\"updated_at\":\"2026-01-01T00:00:00Z\",\"files\":{}}\n")
	_, err := Prepare(root, &ApplyRequest{
		Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
		Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-07-30T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "bootstrap_baseline_present") {
		t.Fatalf("existing empty Baseline was accepted: %v", err)
	}
}

func TestBootstrapThirdPartyTargetNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	envelope, approval := preparedFixture(t, root, nil)
	writeBootstrapFile(t, root, "aoci.meta.txt", "third-party\n")
	_, err := Apply(root, envelope, approval)
	if err == nil {
		t.Fatal("third-party target was accepted")
	}
	data, _ := os.ReadFile(filepath.Join(root, "aoci.meta.txt"))
	if string(data) != "third-party\n" {
		t.Fatalf("third-party bytes changed: %q", data)
	}
}

func TestBootstrapApprovalReplayAndCandidateDriftFailClosed(t *testing.T) {
	root := t.TempDir()
	envelope, approval := preparedFixture(t, root, nil)
	forged := *envelope
	forged.RuntimeBoundary.Content += "# drift\n"
	forged.RuntimeBoundary.PostSHA256 = sha256Hex([]byte(forged.RuntimeBoundary.Content))
	forged.RuntimeBoundary.ByteSize = int64(len([]byte(forged.RuntimeBoundary.Content)))
	forged.EnvelopeDigest, _ = envelopeDigest(&forged)
	if _, err := Apply(root, &forged, approval); err == nil {
		t.Fatal("Approval was replayed against a different Envelope")
	}
	if _, err := os.Lstat(filepath.Join(root, "aoci.txt")); !os.IsNotExist(err) {
		t.Fatalf("forged Approval attempt wrote Root: %v", err)
	}
}

func TestBootstrapRuntimeAuditDoesNotChangeContentReplay(t *testing.T) {
	root := t.TempDir()
	envelope, approval := preparedFixture(t, root, nil)
	for _, op := range []string{"rules", "overview"} {
		ledger.Append(root, true, ledger.Event{Op: op, Result: ledger.ResultOK})
	}
	result, err := Apply(root, envelope, approval)
	if err != nil || result.Status != StatusApplied {
		t.Fatalf("runtime audit metadata destabilized Bootstrap replay: %#v err=%v", result, err)
	}
}

func preparedFixture(t testing.TB, root string, kinds []string) (*ApplyEnvelope, *Approval) {
	t.Helper()
	plan, candidate, preview := authoredFixture(t, root, kinds)
	envelope, err := Prepare(root, &ApplyRequest{
		Version: machinecontract.CognitionBootstrapApplyRequestV1, Plan: *plan,
		Candidate: *candidate, Preview: *preview, BaselineTimestamp: "2026-07-30T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, approval
}

func authoredFixture(t testing.TB, root string, kinds []string) (*cognitionplan.Plan, *cognitionplan.LayoutCandidate, *cognitionplan.Preview) {
	t.Helper()
	plan, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: kinds})
	if err != nil {
		t.Fatal(err)
	}
	candidate := validBootstrapCandidate(root, plan)
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("fixture preview invalid: %#v err=%v", preview, err)
	}
	return plan, candidate, preview
}

func validBootstrapCandidate(root string, plan *cognitionplan.Plan) *cognitionplan.LayoutCandidate {
	rootLines := []string{
		cognition.RootManifestMarker,
		"#Format-Version: cognition-volumes/v1",
		"#Locale: " + plan.Locale,
		"#Project: Model-authored Bootstrap fixture",
		"#Global-Invariants: Preserve evidence-bound model semantics",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
	}
	for _, kind := range plan.TargetKinds {
		if kind == "code" {
			rootLines = append(rootLines, "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled")
		} else {
			rootLines = append(rootLines, "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled")
		}
	}
	meta := strings.Join([]string{
		cognition.MetaVolumeMarker,
		"#Object-Protocol: repository-cognition-object/v2",
		"#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract",
		"#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
		"#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
	}, "\n") + "\n"
	assets := []cognitionplan.CandidateAsset{
		{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
		{AssetID: "meta", Path: "aoci.meta.txt", Content: meta},
	}
	for _, kind := range plan.TargetKinds {
		switch kind {
		case "code":
			sections := map[string][]string{}
			for _, object := range plan.Inventory {
				if object.Eligible {
					directory := filepath.ToSlash(filepath.Dir(object.Path))
					sections[directory] = append(sections[directory], filepath.Base(object.Path)+"[CD9S]: F:Model-authored fixture responsibility | R:- | A:- | S:Preserve the approved source boundary")
				}
			}
			directories := make([]string, 0, len(sections))
			for directory := range sections {
				directories = append(directories, directory)
			}
			sort.Strings(directories)
			parts := []string{cognition.CodeVolumeMarker}
			for _, directory := range directories {
				sort.Strings(sections[directory])
				sectionRoot := root
				if directory != "." {
					sectionRoot = filepath.Join(root, filepath.FromSlash(directory))
				}
				parts = append(parts, "===Code "+filepath.ToSlash(sectionRoot)+"/===", strings.Join(sections[directory], "\n"))
			}
			content := strings.Join(parts, "\n") + "\n"
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "code", Path: "aoci.code.txt", Content: content})
		case "database":
			sections := []string{}
			for _, evidence := range plan.Evidence {
				if strings.HasSuffix(evidence.ObjectRef, "/-") {
					continue
				}
				parts := strings.Split(strings.TrimPrefix(evidence.ObjectRef, "database://"), "/")
				sections = append(sections,
					"===Database/database://"+parts[0]+"/"+parts[1]+"/===\n"+
						parts[2]+"[DB9S]: F:Model-authored table responsibility | R:- | A:- | S:Preserve the frozen local Evidence binding",
				)
			}
			sort.Strings(sections)
			content := cognition.DatabaseMarker + "\n" + strings.Join(sections, "\n") + "\n"
			assets = append(assets, cognitionplan.CandidateAsset{AssetID: "database", Path: "aoci.database.txt", Content: content})
		}
	}
	candidate := &cognitionplan.LayoutCandidate{
		Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: assets, MappingResolutions: []cognitionplan.MappingResolution{},
	}
	candidate.SemanticAuthoringProvenance = &cognitionplan.SemanticAuthoringProvenance{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "bootstrapapply-test-host-run", PlanID: plan.PlanID,
		EvidenceBindingSHA256:  cognitionplan.SemanticAuthoringEvidenceBindingSHA256(plan),
		CandidatePayloadSHA256: cognitionplan.CandidatePayloadSHA256(candidate),
	}
	return candidate
}

func installBootstrapDatabaseEvidence(t testing.TB, root string) {
	t.Helper()
	zero := 0
	source := dbevidence.SourceConfig{
		SourceID: "primary", Engine: dbevidence.EngineMySQL, Database: "fixture",
		Namespaces: []string{"fixture"}, CredentialEnv: "FIXTURE_DB_PASSWORD",
		ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	}
	cfg := config.DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{source}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{
		Version: dbevidence.SourceManifestVersion, SourceID: source.SourceID, Engine: source.Engine,
		Database: source.Database, Namespaces: source.Namespaces, IncludeNamespaces: []string{},
		ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics:    dbevidence.CaseSemantics{IdentifierCase: "preserve", LowerCaseTableNames: &zero},
		BusinessDataRead: false,
	}
	table := dbevidence.TableEvidence{
		Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/fixture/items",
		Engine: source.Engine, SourceID: source.SourceID, Database: source.Database,
		Namespace: "fixture", Name: "items", Kind: "base_table",
		Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{},
		Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{},
	}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
}

func writeBootstrapFile(t testing.TB, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readOptional(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBootstrapContractsRejectUnknownDuplicateAndTrailingJSON(t *testing.T) {
	root := t.TempDir()
	envelope, _ := preparedFixture(t, root, nil)
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeApplyEnvelope(append(data, []byte("{}")...)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	unknown := strings.Replace(string(data), "{", "{\"unknown\":true,", 1)
	if _, err := DecodeApplyEnvelope([]byte(unknown)); err == nil {
		t.Fatal("unknown field was accepted")
	}
	duplicate := strings.Replace(string(data), "{", "{\"version\":\"cognition-bootstrap-apply-envelope/v1\",", 1)
	if _, err := DecodeApplyEnvelope([]byte(duplicate)); err == nil {
		t.Fatal("duplicate field was accepted")
	}
	crossVersion := strings.Replace(string(data), machinecontract.CognitionBootstrapApplyEnvelopeV1, "cognition-bootstrap-apply-envelope/v2", 1)
	if _, err := DecodeApplyEnvelope([]byte(crossVersion)); err == nil {
		t.Fatal("unknown Envelope version crossed the immutable v1 transaction contract")
	}
}

func TestBaselinePostimageRoundTrip(t *testing.T) {
	root := t.TempDir()
	envelope, _ := preparedFixture(t, root, nil)
	var value baseline.Baseline
	if err := json.Unmarshal([]byte(envelope.Baseline.Content), &value); err != nil || value.CreatedAt != envelope.PreparedAt {
		t.Fatalf("exact Baseline timestamp lost: %#v err=%v", value, err)
	}
}
