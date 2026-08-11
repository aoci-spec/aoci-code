package onboarding

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func freshRouteFixture(t *testing.T) (string, *Session) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	if _, err := Start(root, "en-US", time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := Next(root, 1, 1024*1024); err != nil {
		t.Fatal(err)
	}
	session, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, session
}

func TestInspectActiveFreshRouteIsReadOnlyAndBound(t *testing.T) {
	root, session := freshRouteFixture(t)
	before, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "bin", "aoci")
	route, active, err := InspectActiveFreshRoute(root, filepath.Join(root, "aoci.txt"), executable)
	if err != nil || !active || route == nil {
		t.Fatalf("active Fresh route unavailable: active=%t route=%#v err=%v", active, route, err)
	}
	if route.Version != RouteVersion || route.Status != "onboarding_in_progress" || route.FormalIndexAvailable ||
		route.OnboardingSessionID != session.OnboardingSessionID || route.SessionRevision != session.Revision ||
		route.PendingCount != len(session.PendingAuthoringTargets) || route.CompletedCount != 0 ||
		route.FormalWritesStarted || route.RecoveryPending || route.NextActionContract == nil ||
		route.NextActionContract.Command == nil || session.ActiveAuthoringBatch == nil ||
		route.ActiveBatchID != session.ActiveAuthoringBatch.BatchID ||
		route.NextActionContract.BatchID != session.ActiveAuthoringBatch.BatchID {
		t.Fatalf("invalid Fresh route: %#v", route)
	}
	command := route.NextActionContract.Command
	wantArguments := []string{
		"--repo", root, "cognition", "onboard", "next",
		"--max-objects", "1", "--max-evidence-bytes", "1048576", "--json",
	}
	if command.Executable != executable || !equalStrings(command.Arguments, wantArguments) {
		t.Fatalf("route command is not runtime/repository bound: %#v", command)
	}
	after, err := os.ReadFile(SessionPath(root))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("route inspection changed active Session: err=%v", err)
	}
}

func TestInspectActiveFreshRouteRequiresAbsentRootAndNoRecovery(t *testing.T) {
	root, _ := freshRouteFixture(t)
	indexPath := filepath.Join(root, "aoci.txt")
	if err := os.WriteFile(indexPath, []byte("formal root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if route, active, err := InspectActiveFreshRoute(root, indexPath, filepath.Join(root, "aoci")); err != nil || active || route != nil {
		t.Fatalf("formal Root did not suppress route: active=%t route=%#v err=%v", active, route, err)
	}
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	transactions := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(transactions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transactions, "bootstrap-test.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if route, active, err := InspectActiveFreshRoute(root, indexPath, filepath.Join(root, "aoci")); err == nil || active || route != nil {
		t.Fatalf("pending Recovery did not outrank route: active=%t route=%#v err=%v", active, route, err)
	}
}

func TestInspectActiveFreshRouteKeepsApplyPendingFinalizationAheadOfFormalRoot(t *testing.T) {
	root, session := freshRouteFixture(t)
	session.TransactionState = "apply_pending"
	session.ApprovalArtifact = ".aoci/onboarding/test/approval.json"
	session.EnvelopeArtifact = ".aoci/onboarding/test/apply-envelope.json"
	session.NextAction = "human_tty_digest_confirmation"
	session.Revision++
	if err := save(root, session); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "aoci.txt")
	if err := os.WriteFile(indexPath, []byte("formal root published by committed Bootstrap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "aoci")
	route, active, err := InspectActiveFreshRoute(root, indexPath, executable)
	if err != nil || !active || route == nil || !route.FormalIndexAvailable || !route.FormalWritesStarted ||
		route.TransactionState != "apply_pending" || route.NextActionContract == nil ||
		route.NextActionContract.Action != "resume" || !route.NextActionContract.FormalWritesStarted ||
		route.NextActionContract.Command == nil {
		t.Fatalf("apply-pending finalization was hidden by formal Root: active=%t route=%#v err=%v", active, route, err)
	}
	wantArgs := []string{"--repo", root, "cognition", "onboard", "resume", "--json"}
	if route.NextActionContract.Command.Executable != executable ||
		!equalStrings(route.NextActionContract.Command.Arguments, wantArgs) {
		t.Fatalf("finalization route is not exactly executable: %#v", route.NextActionContract.Command)
	}
	after, err := os.ReadFile(SessionPath(root))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("finalization route changed active Session: err=%v", err)
	}

	completed, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	completed.Status = "completed"
	completed.TransactionState = "applied"
	completed.NextAction = "none"
	completed.Revision++
	if err := save(root, completed); err != nil {
		t.Fatal(err)
	}
	if route, active, err := InspectActiveFreshRoute(root, indexPath, executable); err != nil || active || route != nil {
		t.Fatalf("completed Session shadowed formal cognition: active=%t route=%#v err=%v", active, route, err)
	}
}

func TestInspectActiveFreshRouteWithoutSessionPreservesNotInitializedState(t *testing.T) {
	root := t.TempDir()
	route, active, err := InspectActiveFreshRoute(root, filepath.Join(root, "aoci.txt"), filepath.Join(root, "aoci"))
	if err != nil || active || route != nil {
		t.Fatalf("absent Session was misreported as active onboarding: active=%t route=%#v err=%v", active, route, err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
