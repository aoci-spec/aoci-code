package scopechange

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

const legacyBootstrapDatabaseDescriptor = "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled\n"

func buildLegacyDatabaseBootstrapScopeFixture(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rootPreimage := []byte(cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n" +
		"#Project: legacy Database Bootstrap Scope fixture\n#Global-Invariants: deterministic fixture bytes\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.txt", string(rootPreimage)+legacyBootstrapDatabaseDescriptor)
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")
	write("aoci.database.txt", cognition.DatabaseMarker+"\n")

	cfg := config.DefaultConfig()
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	cfg.LedgerEnabled = true
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
	files["aoci.txt"] = baseline.HashBytes("aoci.txt", rootPreimage)
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
	return root, rootPreimage
}

func TestOrdinaryScopeChangeReconcilesOnlyProvenLegacyDatabaseBootstrapRoot(t *testing.T) {
	root, _ := buildLegacyDatabaseBootstrapScopeFixture(t)
	rootBefore := mustReadChangeFixture(t, filepath.Join(root, "aoci.txt"))
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "no-op-future-path", Action: machinecontract.ScopeRoleExclude,
			Pattern: "future-never-present.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "test-only no-op policy change",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "scope-test", Order: 100, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	candidates := CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}}
	preview, err := Build(root, "2026-08-12T00:00:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	currentRoot, err := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if err != nil || preview.SourceGuard["aoci.txt"].SHA256 != currentRoot.SHA256 ||
		preview.Baseline.Files["aoci.txt"].SHA256 != currentRoot.SHA256 {
		t.Fatalf("Plan did not reconcile the proven historical Root: current=%#v preview=%#v err=%v", currentRoot, preview, err)
	}
	result, err := Apply(root, preview, nil)
	if err != nil || result.Status != "applied" {
		t.Fatalf("ordinary Scope Change did not apply: result=%#v err=%v", result, err)
	}
	if rootAfter := mustReadChangeFixture(t, filepath.Join(root, "aoci.txt")); !bytes.Equal(rootBefore, rootAfter) {
		t.Fatal("Scope Change rewrote the live Root while reconciling its Baseline binding")
	}
	after, exists, err := baseline.Load(root)
	if err != nil || !exists || after.Files["aoci.txt"].SHA256 != currentRoot.SHA256 {
		t.Fatalf("Scope Change did not advance only the Root Baseline binding: exists=%t baseline=%#v err=%v", exists, after, err)
	}
}

func TestOrdinaryScopeChangeRejectsUnprovenRootDrift(t *testing.T) {
	root, _ := buildLegacyDatabaseBootstrapScopeFixture(t)
	path := filepath.Join(root, "aoci.txt")
	if err := os.WriteFile(path, append(mustReadChangeFixture(t, path), []byte("# unrelated Root drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(root, "2026-08-12T00:01:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}})
	if err == nil || err.Error() != "managed_scope_formal_volume_baseline_drift: aoci.txt" {
		t.Fatalf("unproven Root drift was not rejected exactly: %v", err)
	}
}

func TestOrdinaryScopeChangeReconcilesLegacyRootWithoutTrailingNewline(t *testing.T) {
	root, rootPreimage := buildLegacyDatabaseBootstrapScopeFixture(t)
	rootPreimage = bytes.TrimSuffix(rootPreimage, []byte("\n"))
	liveRoot := append(append([]byte{}, rootPreimage...), '\n')
	liveRoot = append(liveRoot, []byte(strings.TrimSuffix(legacyBootstrapDatabaseDescriptor, "\n"))...)
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), liveRoot, 0o644); err != nil {
		t.Fatal(err)
	}
	active, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	active.Files["aoci.txt"] = baseline.HashBytes("aoci.txt", rootPreimage)
	if err := baseline.Save(root, active); err != nil {
		t.Fatal(err)
	}
	if err := config.MutateManagedScope(root, func(policy *managedscope.Policy) error {
		policy.Rules = append(policy.Rules, managedscope.Rule{RuleID: "no-op-no-trailing-newline", Action: machinecontract.ScopeRoleExclude,
			Pattern: "future-never-present.txt", PatternKind: machinecontract.ScopePatternFile, Reason: "test exact descriptor inverse",
			Source: machinecontract.ScopeRuleUser, CreatedBy: "scope-test", Order: 101, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := Build(root, "2026-08-12T00:02:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}})
	if err != nil {
		t.Fatalf("exact no-trailing-newline historical Root was not reconciled: %v", err)
	}
	want := baseline.HashBytes("aoci.txt", liveRoot)
	if preview.Baseline.Files["aoci.txt"].SHA256 != want.SHA256 {
		t.Fatalf("reconciled Root binding mismatch: got=%#v want=%#v", preview.Baseline.Files["aoci.txt"], want)
	}
}

func TestOrdinaryScopeChangeRejectsCanonicalDescriptorAtNonHistoricalPosition(t *testing.T) {
	root, rootPreimage := buildLegacyDatabaseBootstrapScopeFixture(t)
	firstVolume := bytes.Index(rootPreimage, []byte("#Volume:"))
	if firstVolume < 0 {
		t.Fatal("fixture Root has no Volume declaration")
	}
	wrong := append([]byte{}, rootPreimage[:firstVolume]...)
	wrong = append(wrong, []byte(legacyBootstrapDatabaseDescriptor)...)
	wrong = append(wrong, rootPreimage[firstVolume:]...)
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), wrong, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Build(root, "2026-08-12T00:03:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}})
	if err == nil || err.Error() != "managed_scope_formal_volume_baseline_drift: aoci.txt" {
		t.Fatalf("non-historical descriptor position was accepted: %v", err)
	}
}

func TestOrdinaryScopeChangeRejectsPreimageThatOldBootstrapWouldHaveRejected(t *testing.T) {
	root, rootPreimage := buildLegacyDatabaseBootstrapScopeFixture(t)
	rootPreimage = bytes.Replace(rootPreimage, []byte("#Global-Invariants: deterministic fixture bytes"),
		[]byte("#Global-Invariants: reserved id=database text"), 1)
	_, err := replayLegacyDatabaseDescriptor(rootPreimage)
	if err == nil {
		t.Fatal("test precondition failed: historical Bootstrap would not accept this preimage")
	}
	// Construct the superficially plausible postimage without using the guarded
	// replay helper; compatibility must still reject it.
	insertAt := bytes.Index(rootPreimage, []byte("#Volume: id=code"))
	if insertAt < 0 {
		t.Fatal("fixture has no Code descriptor")
	}
	lineEnd := bytes.IndexByte(rootPreimage[insertAt:], '\n')
	if lineEnd < 0 {
		t.Fatal("fixture Code descriptor has no line ending")
	}
	insertAt += lineEnd + 1
	liveRoot := append([]byte{}, rootPreimage[:insertAt]...)
	liveRoot = append(liveRoot, []byte(legacyBootstrapDatabaseDescriptor)...)
	liveRoot = append(liveRoot, rootPreimage[insertAt:]...)
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), liveRoot, 0o644); err != nil {
		t.Fatal(err)
	}
	active, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	active.Files["aoci.txt"] = baseline.HashBytes("aoci.txt", rootPreimage)
	if err := baseline.Save(root, active); err != nil {
		t.Fatal(err)
	}
	_, err = Build(root, "2026-08-12T00:04:00Z", CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []EntryCandidate{}, Dispositions: []EntryDisposition{}})
	if err == nil || err.Error() != "managed_scope_formal_volume_baseline_drift: aoci.txt" {
		t.Fatalf("state rejected by historical Bootstrap was accepted: %v", err)
	}
}
