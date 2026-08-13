package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/businesssource"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

type fixedSnapshotCollector struct {
	manifest     dbevidence.SourceManifest
	snapshot     dbevidence.Snapshot
	files        map[string][]byte
	calls        int
	mutateSecond bool
}

func TestDatabaseSourceProposalRecognizesCanonicalOpenGaussCandidate(t *testing.T) {
	proposal := buildDatabaseSourceProposal(&cognitionplan.Plan{BusinessSourceManifest: businesssource.Manifest{
		Files: []businesssource.File{{Path: "deploy/opengauss.conf"}},
	}})
	if proposal == nil || len(proposal.EngineCandidates) != 1 || proposal.EngineCandidates[0] != "opengauss" {
		t.Fatalf("canonical openGauss candidate was not recognized: %#v", proposal)
	}
	if !proposal.SourceIDRequired || !proposal.DatabaseRequired || !proposal.CredentialEnvRequired || proposal.CredentialValueStored {
		t.Fatalf("openGauss proposal weakened source safety requirements: %#v", proposal)
	}
}

func TestDatabaseSourceProposalOffersOpenGaussForGenericSchemaEvidence(t *testing.T) {
	proposal := buildDatabaseSourceProposal(&cognitionplan.Plan{BusinessSourceManifest: businesssource.Manifest{
		Files: []businesssource.File{{Path: "database/schema.sql"}},
	}})
	want := []string{"mysql", "opengauss", "postgresql"}
	if proposal == nil || !reflect.DeepEqual(proposal.EngineCandidates, want) {
		t.Fatalf("generic schema candidates = %#v, want %#v", proposal, want)
	}
}

func TestDatabaseSourceProposalDoesNotTreatOpenGaussAliasesAsCanonical(t *testing.T) {
	for _, path := range []string{"deploy/gaussdb.conf", "deploy/og.conf", "deploy/open_gauss.conf"} {
		t.Run(path, func(t *testing.T) {
			proposal := buildDatabaseSourceProposal(&cognitionplan.Plan{BusinessSourceManifest: businesssource.Manifest{
				Files: []businesssource.File{{Path: path}},
			}})
			if proposal != nil {
				t.Fatalf("non-canonical openGauss alias was recognized: path=%q proposal=%#v", path, proposal)
			}
		})
	}
}

func TestOnboardingAcceptsOfficialLocaleRuntimeBoundaryWithoutRewrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	legacyBoundary, err := textassets.Load(textassets.LegacyLocale, textassets.TemplateAOCIGitignore)
	if err != nil {
		t.Fatal(err)
	}
	boundaryPath := filepath.Join(root, ".aoci", ".gitignore")
	if err := os.MkdirAll(filepath.Dir(boundaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boundaryPath, []byte(legacyBoundary), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(root, textassets.DefaultLocale, time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(boundaryPath)
	if err != nil || string(after) != legacyBoundary {
		t.Fatalf("supported Locale boundary was rewritten: %v", err)
	}
}

func TestOnboardingRejectsUnknownRuntimeBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	boundaryPath := filepath.Join(root, ".aoci", ".gitignore")
	if err := os.MkdirAll(filepath.Dir(boundaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(boundaryPath, []byte("*\n!.gitignore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Start(root, textassets.DefaultLocale, time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "onboarding_runtime_boundary_conflict") {
		t.Fatalf("unknown runtime boundary was accepted: %v", err)
	}
	if _, statErr := os.Stat(SessionPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("rejected boundary created onboarding session: %v", statErr)
	}
}

func (collector *fixedSnapshotCollector) Snapshot(_ context.Context, _ dbevidence.SourceConfig) (dbevidence.SourceManifest, dbevidence.Snapshot, map[string][]byte, error) {
	collector.calls++
	snapshot := collector.snapshot
	files := map[string][]byte{}
	for path, data := range collector.files {
		files[path] = append([]byte{}, data...)
	}
	if collector.mutateSecond && collector.calls == 2 {
		snapshot.SourceSnapshotSHA256 = strings.Repeat("f", 64)
	}
	return collector.manifest, snapshot, files, nil
}

func TestOnboardingPersistsBatchesAndReusesBootstrapApply(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	frozen := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	session, err := Start(root, "en-US", frozen)
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != SessionVersion || session.Operation != cognitionplan.OperationBootstrap ||
		session.BusinessRowsRead != 0 || session.DDLDMLStatements != 0 || session.NetworkAccessed ||
		session.BusinessSourceManifest.AggregateSHA256 == "" || session.SafeInventoryIdentity == "" {
		t.Fatalf("unsafe or incomplete session: %#v", session)
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.txt")); !os.IsNotExist(err) {
		t.Fatal("Start wrote a formal cognition asset")
	}
	ignored := exec.Command("git", "check-ignore", ".aoci/onboarding/active.json")
	ignored.Dir = root
	if output, err := ignored.CombinedOutput(); err != nil {
		t.Fatalf("onboarding session is not Git ignored: %v %s", err, output)
	}
	repeated, err := Start(root, "en-US", frozen.Add(time.Hour))
	if err != nil || repeated.OnboardingSessionID != session.OnboardingSessionID || repeated.FrozenBaselineTimestamp != session.FrozenBaselineTimestamp {
		t.Fatalf("idempotent Start changed frozen identity: %#v err=%v", repeated, err)
	}

	batch, err := Next(root, 1, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if batch.ObjectCount != 1 || batch.SemanticGenerated || batch.PendingCount < 2 {
		t.Fatalf("batch generated semantics or ignored dynamic limit: %#v", batch)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadRequired(root)
	if err != nil || len(restored.CompletedAuthoringTargets) != 1 {
		t.Fatalf("process restart lost progress: %#v err=%v", restored, err)
	}
	remaining, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	mismatchedRun := hostAuthoredCompletion(session, remaining)
	mismatchedRun.SemanticAuthoringDeclaration.AuthoringRunID = "different-onboarding-test-host-run"
	if _, err := CompleteTasks(root, mismatchedRun); err == nil {
		t.Fatal("one Session accepted two Host authoring runs")
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, remaining)); err != nil {
		t.Fatal(err)
	}

	candidate := onboardingCandidate(t, root, session.Plan)
	candidateData, _ := json.Marshal(candidate)
	preview, err := Preview(root, candidateData, nil)
	if err != nil || preview.ApprovalDigest == nil {
		t.Fatalf("model-authored candidate did not reach Preview: %#v err=%v", preview, err)
	}
	envelopeValue, err := Prepare(root)
	if err != nil {
		t.Fatal(err)
	}
	envelope, ok := envelopeValue.(*bootstrapapply.ApplyEnvelope)
	if !ok {
		t.Fatalf("Bootstrap reused wrong apply kernel: %T", envelopeValue)
	}
	approval, err := bootstrapapply.RecordApproval(envelope, "test-human", "2026-07-31T01:03:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	approvalData, _ := json.Marshal(approval)
	beforeInvalidApproval, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	invalidApproval := *approval
	invalidApproval.ApplyEnvelopeDigest = strings.Repeat("0", 64)
	invalidApprovalData, _ := json.Marshal(invalidApproval)
	invalidApprovalPath := filepath.Join(t.TempDir(), "invalid-approval.json")
	if err := os.WriteFile(invalidApprovalPath, invalidApprovalData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, invalidApprovalPath); err == nil {
		t.Fatal("invalid Approval was accepted by onboarding")
	}
	afterInvalidApproval, err := os.ReadFile(SessionPath(root))
	if err != nil || string(afterInvalidApproval) != string(beforeInvalidApproval) {
		t.Fatalf("invalid Approval changed persistent Session before rejection: %v", err)
	}
	crashState, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	approvalDigest := sha256.Sum256(approvalData)
	approvalRel, err := saveArtifactBytes(root, crashState, "approval-"+hex.EncodeToString(approvalDigest[:])+".json", approvalData)
	if err != nil {
		t.Fatal(err)
	}
	crashState.ApprovalArtifact = approvalRel
	crashState.ApprovalState = "approved_envelope_provided"
	crashState.TransactionState = "apply_pending"
	crashState.Revision++
	if err := save(root, crashState); err != nil {
		t.Fatal(err)
	}
	// Simulate Host termination after the existing D2 transaction committed but
	// before onboarding could persist its terminal projection.
	applied, err := bootstrapapply.Apply(root, envelope, approval)
	if err != nil || !applied.FormalComplete {
		t.Fatalf("D2 Bootstrap did not complete before simulated crash: %#v err=%v", applied, err)
	}
	final, err := Resume(root)
	if err != nil || final.Status != "completed" || final.TransactionState != "applied" || final.NextAction != "none" ||
		!final.StructureValid || !final.GovernanceAligned || !final.CheckOK || final.GuideStage != "aligned" ||
		final.ApprovalState != "consumed" {
		t.Fatalf("terminal onboarding state not resumable: %#v err=%v", final, err)
	}
	rootData, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil || !strings.HasPrefix(string(rootData), cognition.RootManifestMarker) {
		t.Fatalf("existing Bootstrap transaction did not own formal assets: %v", err)
	}
}

func TestFreshBootstrapV2BindsHostAuthoringBeforePreview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != SessionVersion || session.Version == LegacySessionVersion || session.Plan.SemanticAuthoringRequirement == nil {
		t.Fatalf("new Fresh request did not enter the provenance-required v2 contract: %#v", session)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Version != BatchVersion || batch.SemanticAuthoringRequirement == nil || batch.SemanticGenerated {
		t.Fatalf("Host authoring requirement missing or program semantics claimed: %#v", batch)
	}
	replayed, err := Next(root, 1, 1)
	if err != nil || replayed.BatchID != batch.BatchID || len(replayed.Tasks) != len(batch.Tasks) {
		t.Fatalf("active Host batch was not replayed exactly: batch=%#v err=%v", replayed, err)
	}
	beforeCompletion, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	invalid := hostAuthoredCompletion(session, batch)
	for _, mutate := range []func(*Completion){
		func(value *Completion) { value.SemanticAuthoringDeclaration = nil },
		func(value *Completion) { value.SemanticAuthoringDeclaration.Origin = "program" },
		func(value *Completion) { value.SemanticAuthoringDeclaration.AuthoringRunID = "" },
		func(value *Completion) { value.SemanticAuthoringDeclaration.DiscoveryPlanID = strings.Repeat("a", 64) },
		func(value *Completion) {
			value.SemanticAuthoringDeclaration.EvidenceBindingSHA256 = strings.Repeat("a", 64)
		},
	} {
		attempt := invalid
		if invalid.SemanticAuthoringDeclaration != nil {
			declaration := *invalid.SemanticAuthoringDeclaration
			attempt.SemanticAuthoringDeclaration = &declaration
		}
		mutate(&attempt)
		if _, err := CompleteTasks(root, attempt); err == nil {
			t.Fatalf("untrusted Host declaration was accepted: %#v", attempt)
		}
		after, readErr := os.ReadFile(SessionPath(root))
		if readErr != nil || string(after) != string(beforeCompletion) {
			t.Fatalf("rejected declaration changed Session: err=%v", readErr)
		}
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidate := onboardingCandidate(t, root, session.Plan)
	validReceipt := *candidate.SemanticAuthoringProvenance
	for _, mutate := range []func(*cognitionplan.LayoutCandidate){
		func(value *cognitionplan.LayoutCandidate) { value.SemanticAuthoringProvenance = nil },
		func(value *cognitionplan.LayoutCandidate) {
			value.SemanticAuthoringProvenance.AuthoringRunID = "different-host-run"
		},
	} {
		attempt := *candidate
		receipt := validReceipt
		attempt.SemanticAuthoringProvenance = &receipt
		mutate(&attempt)
		data, _ := json.Marshal(&attempt)
		before, readErr := os.ReadFile(SessionPath(root))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err := Preview(root, data, nil); err == nil {
			t.Fatalf("Candidate without the persisted Host declaration binding reached Preview: %#v", attempt)
		}
		after, readErr := os.ReadFile(SessionPath(root))
		if readErr != nil || string(after) != string(before) {
			t.Fatalf("rejected Candidate changed Session: err=%v", readErr)
		}
		if _, statErr := os.Lstat(filepath.Join(root, "aoci.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("rejected Candidate changed formal Root: %v", statErr)
		}
	}
	data, _ := json.Marshal(candidate)
	preview, err := Preview(root, data, nil)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil {
		t.Fatalf("fully bound Host Candidate did not reach Preview: preview=%#v err=%v", preview, err)
	}
}

func TestPersistedV1OnboardingResumesWithoutCrossVersionUpgrade(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, Completion{Version: LegacyCompletionVersion, SessionID: session.OnboardingSessionID}); err == nil {
		t.Fatal("a new v1 Fresh completion became a bypass for the v2 session")
	}
	session.Version = LegacySessionVersion
	session.SemanticAuthoringDeclaration = nil
	session.ActiveAuthoringBatch = nil
	session.Revision++
	if err := save(root, session); err != nil {
		t.Fatal(err)
	}
	restored, err := LoadRequired(root)
	if err != nil || restored.Version != LegacySessionVersion {
		t.Fatalf("persisted v1 Session was upgraded in place: session=%#v err=%v", restored, err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil || batch.Version != LegacyBatchVersion || batch.SemanticAuthoringRequirement != nil {
		t.Fatalf("persisted v1 Session did not retain its contract: batch=%#v err=%v", batch, err)
	}
	if _, err := CompleteTasks(root, Completion{Version: CompletionVersion, SessionID: restored.OnboardingSessionID,
		BatchID: batch.BatchID, CompletedTasks: restored.PendingAuthoringTargets,
		SemanticAuthoringDeclaration: &cognitionplan.SemanticAuthoringDeclaration{}}); err == nil {
		t.Fatal("v2 Completion was mixed into a persisted v1 Session")
	}
	if _, err := CompleteTasks(root, Completion{Version: LegacyCompletionVersion, SessionID: restored.OnboardingSessionID,
		CompletedTasks: append([]string{}, restored.PendingAuthoringTargets...)}); err != nil {
		t.Fatal(err)
	}
	candidate := onboardingCandidate(t, root, session.Plan)
	data, _ := json.Marshal(candidate)
	preview, err := Preview(root, data, nil)
	if err != nil || preview.ApprovalDigest == nil {
		t.Fatalf("persisted v1 Session could not resume with the additive Candidate receipt: preview=%#v err=%v", preview, err)
	}
	after, err := LoadRequired(root)
	if err != nil || after.Version != LegacySessionVersion {
		t.Fatalf("v1 Session changed version during resume: session=%#v err=%v", after, err)
	}
}

func TestNewProjectAutoOnboardingNeedsNoTTYAndClosesGovernance(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC))
	if err != nil || session.Operation != cognitionplan.OperationBootstrap {
		t.Fatalf("new project did not enter Bootstrap: session=%#v err=%v", session, err)
	}
	if policy := EffectiveAutomationPolicy(session); policy.Mode != config.AutomationModeAuto ||
		policy.Source != machinecontract.CognitionAutomationPolicyFreshDefault {
		t.Fatalf("Fresh project did not pin the default Auto policy: %#v", policy)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidate := onboardingCandidate(t, root, session.Plan)
	candidateData, _ := json.Marshal(candidate)
	if _, err := Preview(root, candidateData, nil); err != nil {
		diagnostic, diagnosticErr := cognitionplan.ValidateCandidate(root, session.Plan, onboardingCandidate(t, root, session.Plan))
		t.Fatalf("Preview failed: %v diagnostic=%#v diagnostic_err=%v", err, diagnostic, diagnosticErr)
	}
	completed, err := Resume(root)
	if err != nil || completed.Status != "completed" || completed.NextAction != "none" ||
		!completed.StructureValid || !completed.GovernanceAligned || !completed.CheckOK || completed.GuideStage != "aligned" ||
		completed.ApprovalState != "consumed" || completed.NetworkAccessed || completed.BusinessRowsRead != 0 || completed.DDLDMLStatements != 0 ||
		completed.AuthorizationProjection == nil ||
		completed.AuthorizationProjection.AutomationPolicySource != machinecontract.CognitionAutomationPolicyFreshDefault {
		t.Fatalf("auto onboarding did not close governance: session=%#v err=%v", completed, err)
	}
}

func TestAIPlatformShapeExcludeOnlyFreshBootstrapAutoCompletes(t *testing.T) {
	root := t.TempDir()
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	paths := []string{"app.js"}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("export const app = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 196; index++ {
		relative := filepath.ToSlash(filepath.Join("dist", fmt.Sprintf("asset-%03d.js", index)))
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(relative))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), []byte("generated artifact\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, relative)
	}
	arguments := append([]string{"-C", root, "add", "--"}, paths...)
	if output, err := exec.Command("git", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("git add fixture: %v %s", err, output)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 5, 15, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if session.Plan.SafeInventory.ReviewVisibleCount != 196 || session.Plan.SafeInventory.AutoBlockerCount != 0 {
		t.Fatalf("ai-platform-shaped exclusions were misclassified: %#v", session.Plan.SafeInventory)
	}
	batch, err := Next(root, 300, 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidateData, _ := json.Marshal(onboardingCandidate(t, root, session.Plan))
	if _, err := Preview(root, candidateData, nil); err != nil {
		diagnosticCandidate := onboardingCandidate(t, root, session.Plan)
		diagnostic, diagnosticErr := cognitionplan.ValidateCandidate(root, session.Plan, diagnosticCandidate)
		t.Fatalf("ai-platform-shaped Preview failed: %v diagnostic=%#v diagnostic_err=%v", err, diagnostic, diagnosticErr)
	}
	completed, err := Resume(root)
	if err != nil || completed.Status != "completed" || completed.AuthorizationProjection == nil ||
		!completed.AuthorizationProjection.AutoReady || completed.AuthorizationProjection.ReviewVisibleCount != 196 ||
		completed.AuthorizationProjection.AutoBlockerCount != 0 || completed.BusinessRowsRead != 0 ||
		completed.DDLDMLStatements != 0 || completed.NetworkAccessed {
		t.Fatalf("ai-platform-shaped Fresh Bootstrap did not auto-complete: session=%#v err=%v", completed, err)
	}
}

func TestOnboardingExplicitReviewOffAndSessionPolicyPinning(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "review", mode: config.AutomationModeReview},
		{name: "off", mode: config.AutomationModeOff},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := config.DefaultConfig()
			if err := cfg.SetAutomationMode(test.mode); err != nil {
				t.Fatal(err)
			}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			session, err := Start(root, "en-US", time.Date(2026, 8, 6, 5, 0, 0, 0, time.UTC))
			if err != nil || EffectiveAutomationPolicy(session).Mode != test.mode {
				t.Fatalf("explicit policy was not pinned: session=%#v err=%v", session, err)
			}
			if test.mode == config.AutomationModeOff {
				if _, err := Next(root, 100, 8*1024*1024); err == nil || !strings.Contains(err.Error(), "onboarding_automation_off_authoring_forbidden") {
					t.Fatalf("off mode entered authoring: %v", err)
				}
				if _, err := Prepare(root); err == nil || !strings.Contains(err.Error(), "onboarding_automation_off_apply_forbidden") {
					t.Fatalf("off mode reached Apply preparation: %v", err)
				}
				if _, statErr := os.Lstat(filepath.Join(root, "aoci.txt")); !os.IsNotExist(statErr) {
					t.Fatalf("off mode wrote formal cognition: %v", statErr)
				}
				return
			}
			batch, err := Next(root, 100, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
				t.Fatal(err)
			}
			candidateData, _ := json.Marshal(onboardingCandidate(t, root, session.Plan))
			if _, err := Preview(root, candidateData, nil); err != nil {
				t.Fatal(err)
			}
			// A later team-config edit cannot promote this persisted review Session.
			changed, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := changed.SetAutomationMode(config.AutomationModeAuto); err != nil {
				t.Fatal(err)
			}
			if err := config.Save(root, changed); err != nil {
				t.Fatal(err)
			}
			resumed, err := Resume(root)
			if err != nil || EffectiveAutomationPolicy(resumed).Mode != config.AutomationModeReview ||
				resumed.ApprovalState == "policy_bound_auto" || resumed.TransactionState == "apply_pending" {
				t.Fatalf("review Session self-promoted after config edit: session=%#v err=%v", resumed, err)
			}
			if _, err := Prepare(root); err == nil || !strings.Contains(err.Error(), "onboarding_automation_policy_drift") {
				t.Fatalf("review Session accepted a mid-Session Auto policy: %v", err)
			}
		})
	}
}

func TestOnboardingExplicitReviewStillRequiresHumanTTY(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := cfg.SetAutomationMode(config.AutomationModeReview); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 5, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidateData, _ := json.Marshal(onboardingCandidate(t, root, session.Plan))
	if _, err := Preview(root, candidateData, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root); err != nil {
		t.Fatal(err)
	}
	prepared, err := LoadRequired(root)
	if err != nil || prepared.ApprovalState != "interaction_required" || prepared.NextAction != "human_tty_digest_confirmation" ||
		prepared.ApprovalArtifact != "" || prepared.AuthorizationProjection != nil {
		t.Fatalf("explicit review no longer stops at human TTY: session=%#v err=%v", prepared, err)
	}
}

func TestFourLayoutAutoOnboardingMatrixClosesOnlyEnabledDomains(t *testing.T) {
	for _, test := range []struct {
		name     string
		code     bool
		database bool
	}{
		{name: "root_meta"},
		{name: "code_only", code: true},
		{name: "database_only", database: true},
		{name: "code_database", code: true, database: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.code {
				if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := config.DefaultConfig()
			if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
				t.Fatal(err)
			}
			if test.database {
				cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
					Database: "app", Namespaces: []string{"public"}, CredentialEnv: "TEST_ONLY_DSN",
					ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
			}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			if test.database {
				installOnboardingSavedEvidence(t, root)
			}
			session, err := Start(root, "en-US", time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if contains(session.Plan.TargetKinds, "code") != test.code || contains(session.Plan.TargetKinds, "database") != test.database {
				t.Fatalf("layout detection mismatch: target_kinds=%v", session.Plan.TargetKinds)
			}
			batch, err := Next(root, 100, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
				t.Fatal(err)
			}
			candidate := onboardingCandidate(t, root, session.Plan)
			candidateData, _ := json.Marshal(candidate)
			preview, err := Preview(root, candidateData, nil)
			if err != nil {
				diagnostic, diagnosticErr := cognitionplan.ValidateCandidate(root, session.Plan, candidate)
				t.Fatalf("Preview failed: %v preview=%#v diagnostic=%#v diagnostic_err=%v candidate=%s", err, preview, diagnostic, diagnosticErr, candidateData)
			}
			if _, err := Prepare(root); err != nil {
				t.Fatal(err)
			}
			completed, err := Resume(root)
			if err != nil || completed.Status != "completed" || completed.GovernanceResult != "aligned" ||
				!completed.StructureValid || !completed.GovernanceAligned || !completed.CheckOK || completed.GuideStage != "aligned" ||
				completed.NextAction != "none" || completed.NetworkAccessed || completed.BusinessRowsRead != 0 || completed.DDLDMLStatements != 0 {
				t.Fatalf("four-layout onboarding did not close: session=%#v err=%v", completed, err)
			}
			set, err := cognition.Load(root, "aoci.txt")
			if err != nil {
				t.Fatal(err)
			}
			_, codePresent := set.Volumes[cognition.ScopeCode]
			_, databasePresent := set.Volumes[cognition.ScopeDatabase]
			if codePresent != test.code || databasePresent != test.database {
				t.Fatalf("Bootstrap created a disabled domain: code=%t database=%t", codePresent, databasePresent)
			}
		})
	}
}

func TestOfficialInitZeroEntrySkeletonUsesPolicyAutoBootstrapWithoutTTY(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	templateSource, err := textassets.Load("en-US", textassets.TemplateMinimalIndex)
	if err != nil {
		t.Fatal(err)
	}
	minimal, err := hooks.RenderTemplate("minimal-index.txt.tmpl", templateSource, hooks.NewTplData(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 2, 4, 0, 0, 0, time.UTC))
	if err != nil || session.Operation != cognitionplan.OperationBootstrap || session.Plan.Layout != machinecontract.CognitionPlannerUninitialized {
		t.Fatalf("official init skeleton did not enter Bootstrap: session=%#v err=%v", session, err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidateData, _ := json.Marshal(onboardingCandidate(t, root, session.Plan))
	if _, err := Preview(root, candidateData, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root); err != nil {
		t.Fatal(err)
	}
	prepared, err := LoadRequired(root)
	if err != nil || prepared.ApprovalState != "policy_bound_auto" || prepared.NextAction != "auto_apply" {
		t.Fatalf("official init skeleton requested TTY: session=%#v err=%v", prepared, err)
	}
	completed, err := Resume(root)
	if err != nil || completed.Status != "completed" || !completed.GovernanceAligned || completed.GuideStage != "aligned" {
		t.Fatalf("official init skeleton did not close onboarding: session=%#v err=%v", completed, err)
	}
}

func TestOnboardingSessionRejectsConcurrentCASUpdate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(root, "en-US", time.Date(2026, 7, 31, 1, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	first, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	first.Revision++
	first.PendingWarnings = append(first.PendingWarnings, "first_writer")
	if err := save(root, first); err != nil {
		t.Fatalf("first CAS update failed: %v", err)
	}
	second.Revision++
	second.PendingWarnings = append(second.PendingWarnings, "stale_writer")
	if err := save(root, second); err == nil {
		t.Fatal("stale onboarding session update bypassed CAS")
	}
	current, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.PendingWarnings) != 1 || current.PendingWarnings[0] != "first_writer" {
		t.Fatalf("stale writer changed session state: %#v", current.PendingWarnings)
	}
}

func TestOnboardingHostDeliveryRequiresMarkerAndVerifiedIntegrity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(root, "en-US", time.Date(2026, 7, 31, 1, 45, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	receipt := HostDeliveryReceipt{Version: machinecontract.OverviewDeliveryReceiptV1, Scope: "all",
		BodySHA256: strings.Repeat("a", 64), BodyBytes: 1024, EndMarkerObserved: true}
	if _, err := RecordHostDelivery(root, receipt); err == nil {
		t.Fatal("unverified Host delivery was accepted as complete")
	}
	receipt.BodySHA256Verified = true
	session, err := RecordHostDelivery(root, receipt)
	if err != nil || session.HostDeliveryReceipt == nil || !session.HostDeliveryReceipt.Confirmed {
		t.Fatalf("verified Host delivery was not persisted: %#v err=%v", session, err)
	}
}

func TestOnboardingDeidentifiedFiftyObjectSafeInventoryForNewAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "uninitialized"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			git := exec.Command("git", "init", "-q")
			git.Dir = root
			if output, err := git.CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, output)
			}
			if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte(".env\n.runtime/\nredis-data/\nnode_modules/\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			legacyLines := []string{"#AOCI-CLI Complete Index", "#Project: Deidentified React Express fixture", "#[Tag dictionary]", "#A Layer: C Code"}
			webEntries, serverEntries := []string{}, []string{}
			paths := []string{}
			for index := 0; index < 50; index++ {
				directory, extension, body := "web/components", ".tsx", "export const component = 1;\n"
				if index >= 25 {
					directory, extension, body = "server/routes", ".js", "module.exports = {};\n"
				}
				relative := filepath.ToSlash(filepath.Join(directory, fmt.Sprintf("object-%02d%s", index, extension)))
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(relative))), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, relative)
				entry := fmt.Sprintf("%s[CD9S]: F:Represent deidentified fixture object %02d | R:- | A:- | S:Preserve deterministic fixture behavior", filepath.Base(relative), index)
				if index < 25 {
					webEntries = append(webEntries, entry)
				} else {
					serverEntries = append(serverEntries, entry)
				}
			}
			legacyLines = append(legacyLines, "===Web "+filepath.ToSlash(filepath.Join(root, "web", "components"))+"/===")
			legacyLines = append(legacyLines, webEntries...)
			legacyLines = append(legacyLines, "===Server "+filepath.ToSlash(filepath.Join(root, "server", "routes"))+"/===")
			legacyLines = append(legacyLines, serverEntries...)
			for path, body := range map[string]string{".env": "PASSWORD=redacted\n", ".runtime/mysql/data/app.ibd": "runtime\n", "redis-data/dump.rdb": "runtime\n", "node_modules/pkg/index.js": "generated\n"} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(path))), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if legacy {
				legacyText := strings.Join(legacyLines, "\n") + "\n"
				if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(legacyText), 0o644); err != nil {
					t.Fatal(err)
				}
				fingerprints := map[string]baseline.Fingerprint{}
				for _, relative := range append(append([]string{}, paths...), "aoci.txt") {
					fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
					if err != nil {
						t.Fatal(err)
					}
					fingerprints[relative] = fingerprint
				}
				value, err := baseline.NewBaselineAt(fingerprints, "2026-07-31T01:50:00Z")
				if err != nil {
					t.Fatal(err)
				}
				if err := baseline.Save(root, value); err != nil {
					t.Fatal(err)
				}
			}
			arguments := append([]string{"-C", root, "add", "--"}, paths...)
			if legacy {
				arguments = append(arguments, "aoci.txt")
			}
			if output, err := exec.Command("git", arguments...).CombinedOutput(); err != nil {
				t.Fatalf("git add fixture: %v %s", err, output)
			}
			session, err := Start(root, "en-US", time.Date(2026, 7, 31, 1, 51, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if len(session.BusinessSourceManifest.Files) != 50 || session.BusinessSourceManifest.SafeInventory.BuiltinSensitiveExcluded == 0 ||
				session.BusinessSourceManifest.SafeInventory.RuntimeExcluded < 2 || session.BusinessSourceManifest.SafeInventory.GeneratedExcluded == 0 {
				t.Fatalf("unsafe runtime entered the fifty-object fixture: %#v", session.BusinessSourceManifest.SafeInventory)
			}
			for _, file := range session.BusinessSourceManifest.Files {
				if strings.HasPrefix(file.Path, ".env") || strings.Contains(file.Path, ".runtime") || strings.Contains(file.Path, "redis-data") || strings.Contains(file.Path, "node_modules") {
					t.Fatalf("excluded content entered Business Source Manifest: %s", file.Path)
				}
			}
		})
	}
}

func TestOnboardingDatabaseCollectionRequiresTwoDeterministicSnapshots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Locale = "en-US"
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
		Database: "app", Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN",
		ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "lower"}, BusinessDataRead: false}
	tables := []dbevidence.TableEvidence{}
	for _, name := range []string{"orders", "tenants", "users"} {
		tables = append(tables, dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/" + name,
			Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table",
			Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "integer", Nullable: false}},
			UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}})
	}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, tables)
	if err != nil {
		t.Fatal(err)
	}
	collector := &fixedSnapshotCollector{manifest: manifest, snapshot: snapshot, files: files}
	updated, err := CollectDatabaseEvidence(context.Background(), root, collector)
	if err != nil {
		t.Fatal(err)
	}
	if collector.calls != 2 || updated.BusinessRowsRead != 0 || updated.DDLDMLStatements != 0 ||
		updated.LastSuccessPoint != "database_evidence_deterministic_and_accepted" || updated.EvidenceIdentity == session.EvidenceIdentity {
		t.Fatalf("database onboarding invariants failed: calls=%d state=%#v", collector.calls, updated)
	}
	databaseTargets := 0
	for _, target := range updated.PendingAuthoringTargets {
		if strings.HasPrefix(target, "database:") {
			databaseTargets++
		}
	}
	if databaseTargets != 3 {
		t.Fatalf("accepted Evidence did not create table authoring targets: %#v", updated.PendingAuthoringTargets)
	}

	otherRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherRoot, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(otherRoot, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(otherRoot, "en-US", time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	nondeterministic := &fixedSnapshotCollector{manifest: manifest, snapshot: snapshot, files: files, mutateSecond: true}
	if _, err := CollectDatabaseEvidence(context.Background(), otherRoot, nondeterministic); err == nil || !strings.Contains(err.Error(), "snapshot_nondeterministic") {
		t.Fatalf("nondeterministic database Evidence was accepted: %v", err)
	}
	if _, _, exists, err := dbevidence.LoadSnapshot(otherRoot, "primary"); err != nil || exists {
		t.Fatalf("failed determinism check wrote Evidence: exists=%t err=%v", exists, err)
	}
}

func TestLegacyOnboardingUsesMigrationMappingAndOneApprovedApply(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := "#AOCI-CLI Complete Index\n#Project: Onboarding Legacy\n#[Tag dictionary]\n#A Layer: C Code\n" +
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" +
		"main.go[CD9S]: F:Run the legacy fixture | R:- | A:main | S:Preserve approved startup behavior\n" +
		"===Self " + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CD9S]: F:Describe the repository cognition contract | R:code:src/main.go | A:FRAS | S:Preserve model ownership\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	baselineValue, err := baseline.NewBaselineAt(files, "2026-07-31T03:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	baselineData, _ := baseline.MarshalExact(baselineValue)
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), baselineData, 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 7, 31, 3, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if session.Operation != cognitionplan.OperationMigration || session.Plan.Mapping == nil ||
		session.Plan.Mapping.Coverage.LegacyEntryDispositionTotal != 2 || session.Plan.Mapping.Coverage.LegacySelfEntryTotal != 1 {
		t.Fatalf("Legacy onboarding lost explicit disposition coverage: %#v", session)
	}
	if policy := EffectiveAutomationPolicy(session); policy.Mode != config.AutomationModeLegacy ||
		policy.Source != machinecontract.CognitionAutomationPolicyLegacy {
		t.Fatalf("Legacy repository was silently promoted: %#v", policy)
	}
	allTasks := append([]string{}, session.PendingAuthoringTargets...)
	if _, err := CompleteTasks(root, Completion{Version: "cognition-onboarding-completion/v1", SessionID: session.OnboardingSessionID, CompletedTasks: allTasks}); err != nil {
		t.Fatal(err)
	}
	candidate := legacyOnboardingCandidate(root, session.Plan, legacy)
	candidateData, _ := json.Marshal(candidate)
	snapshot, err := loadMigrationSnapshot(root, session)
	if err != nil {
		t.Fatal(err)
	}
	template, err := migrationapply.BuildMappingTemplate(root, snapshot, session.Plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	authored := authorOnboardingMapping(t, template)
	mappingData, _ := json.Marshal(authored)
	preview, err := Preview(root, candidateData, mappingData)
	if err != nil || preview.ApprovalDigest == nil {
		t.Fatalf("Legacy onboarding Preview failed: %#v err=%v", preview, err)
	}
	envelopeValue, err := Prepare(root)
	if err != nil {
		t.Fatal(err)
	}
	envelope, ok := envelopeValue.(*migrationapply.ApplyEnvelope)
	if !ok {
		t.Fatalf("Legacy onboarding bypassed Migration D2: %T", envelopeValue)
	}
	preparedSession, err := LoadRequired(root)
	if err != nil || preparedSession.ApprovalState != "interaction_required" ||
		preparedSession.NextAction != "human_tty_digest_confirmation" || preparedSession.ApprovalArtifact != "" {
		t.Fatalf("Migration no longer stops at human approval: session=%#v err=%v", preparedSession, err)
	}
	approval, err := migrationapply.RecordApproval(envelope, "test-human", "2026-07-31T03:02:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	approvalData, _ := json.Marshal(approval)
	approvalPath := filepath.Join(t.TempDir(), "migration-approval.json")
	if err := os.WriteFile(approvalPath, approvalData, 0o600); err != nil {
		t.Fatal(err)
	}
	resultValue, err := Apply(root, approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := resultValue.(*migrationapply.ApplyResult)
	if !ok || !result.FormalComplete || result.ActiveLayout != machinecontract.CognitionPlannerVolumes {
		t.Fatalf("Legacy onboarding did not reach aligned Volumes: %#v", resultValue)
	}
	completed, err := LoadRequired(root)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.StructureValid || !completed.GovernanceAligned || !completed.CheckOK ||
		completed.GuideStage != volumegovernance.ResultAligned || completed.NextAction != "none" {
		t.Fatalf("Legacy ownership migration did not align Verify/Check/Guide: %#v", completed)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range set.Volumes[cognition.OwnerCode].Objects {
		if object.CanonicalRef == "code:aoci.txt" {
			t.Fatal("Legacy onboarding duplicated Root-owned aoci.txt into Code")
		}
	}
}

func legacyOnboardingCandidate(root string, plan *cognitionplan.Plan, legacy string) *cognitionplan.LayoutCandidate {
	rootLines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + plan.Locale,
		"#Project: Reviewed onboarding migration", "#Global-Invariants: Preserve approved semantic ownership",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled"}
	metaLines := []string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing", "#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
		"#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T"}
	for index := 0; index < 20; index++ {
		metaLines = append(metaLines, fmt.Sprintf("#Reviewed-Rule-%02d: model-owned semantic calibration", index))
	}
	mainLine := findLegacyLine(legacy, "main.go[")
	code := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + mainLine + "\n"
	candidate := &cognitionplan.LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: []cognitionplan.CandidateAsset{{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
			{AssetID: "meta", Path: "aoci.meta.txt", Content: strings.Join(metaLines, "\n") + "\n"},
			{AssetID: "code", Path: "aoci.code.txt", Content: code}}}
	for _, record := range plan.Mapping.Records {
		if record.Mode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		resolution := cognitionplan.MappingResolution{UnitID: record.UnitID, TargetAsset: record.TargetAsset, TargetRef: record.TargetRef,
			Reviewer: "fixture-model", SemanticReviewed: true}
		if record.UnitKind == "entry" {
			if record.LegacySelfEntry {
				resolution.TargetAsset = cognition.OwnerRoot
				resolution.TargetRef = ""
			} else {
				resolution.TargetAsset = cognition.OwnerCode
				resolution.TargetRef = "code:src/main.go"
			}
		} else {
			resolution.TargetAsset = "root"
		}
		candidate.MappingResolutions = append(candidate.MappingResolutions, resolution)
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	return candidate
}

func authorOnboardingMapping(t *testing.T, template *migrationapply.MigrationMapping) *migrationapply.MigrationMapping {
	t.Helper()
	result := *template
	result.Records = append([]migrationapply.MappingRecord{}, template.Records...)
	used := map[string]bool{}
	targetByKey := map[string][]migrationapply.TargetRange{}
	semanticTargets := []migrationapply.TargetRange{}
	for _, target := range result.TargetRanges {
		targetByKey[target.Kind+"\x00"+target.SHA256] = append(targetByKey[target.Kind+"\x00"+target.SHA256], target)
		if target.Kind != "entry" && target.Object == "" {
			semanticTargets = append(semanticTargets, target)
		}
	}
	targetEntrySHA := map[string]bool{}
	for _, target := range result.TargetRanges {
		if target.Kind == "entry" {
			targetEntrySHA[target.SHA256] = true
		}
	}
	selfParent := ""
	for _, record := range result.Records {
		if record.SourceKind == "entry" && !targetEntrySHA[record.SourceSHA256] {
			selfParent = record.SourceIdentity
			break
		}
	}
	if selfParent == "" {
		t.Fatal("Legacy self-entry source missing")
	}
	selfSources := map[string]bool{selfParent: true}
	for changed := true; changed; {
		changed = false
		for _, record := range result.Records {
			if selfSources[record.ParentSourceIdentity] && !selfSources[record.SourceIdentity] {
				selfSources[record.SourceIdentity] = true
				changed = true
			}
		}
	}
	var selfTarget migrationapply.TargetRange
	for _, target := range semanticTargets {
		if target.Asset == cognition.OwnerRoot && target.Kind == "root_semantic" {
			selfTarget = target
			break
		}
	}
	if selfTarget.Identity == "" {
		t.Fatal("Root semantic target missing")
	}
	used[selfTarget.Identity] = true
	tasks := map[string]migrationapply.MappingAuthoringTask{}
	for _, task := range template.AuthoringTasks {
		tasks[task.TaskID] = task
	}
	pop := func(values []migrationapply.TargetRange) (migrationapply.TargetRange, bool) {
		for _, value := range values {
			if !used[value.Identity] {
				return value, true
			}
		}
		return migrationapply.TargetRange{}, false
	}
	removedTasks := map[string]bool{}
	selfSourceIDs, selfEvidenceRefs := []string{}, []string{}
	for index := range result.Records {
		record := &result.Records[index]
		if record.MappingMode == machinecontract.CognitionMappingStructuralOnly {
			continue
		}
		if selfSources[record.SourceIdentity] {
			removedTasks[record.AuthoringTaskID] = true
			selfSourceIDs = append(selfSourceIDs, record.SourceIdentity)
			selfEvidenceRefs = append(selfEvidenceRefs, "legacy:"+record.SourceSHA256)
			record.SemanticRole = "model_reviewed_root_ownership"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = cognition.OwnerRoot, "", selfTarget.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			record.AuthoringTaskID, record.MappingGroupID = "author:self-entry-root", "group:self-entry-root"
			record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			continue
		}
		target, found := pop(targetByKey[record.SourceKind+"\x00"+record.SourceSHA256])
		if found {
			record.SemanticRole = "preserved_model_owned_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode, record.AuthoringTaskID = machinecontract.CognitionMappingPreserved, ""
		} else {
			target, found = pop(semanticTargets)
			if !found {
				t.Fatalf("no target for %s", record.SourceIdentity)
			}
			record.SemanticRole = "model_reviewed_migration_semantics"
			record.TargetAsset, record.TargetObject, record.TargetSemanticRangeIdentity = target.Asset, target.Object, target.Identity
			record.MappingMode = machinecontract.CognitionMigrationModelRegenerated
			task := tasks[record.AuthoringTaskID]
			task.TargetAsset, task.TargetObject = target.Asset, target.Object
			task.CandidateRangeIdentities = []string{target.Identity}
			task.Status, task.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
			tasks[task.TaskID] = task
		}
		record.ReviewStatus, record.Reviewer = machinecontract.CognitionMigrationSemanticReviewed, "fixture-reviewer"
		used[target.Identity] = true
	}
	result.AuthoringTasks = []migrationapply.MappingAuthoringTask{}
	sourceEvidenceIdentity := ""
	for _, task := range tasks {
		if sourceEvidenceIdentity == "" {
			sourceEvidenceIdentity = task.SourceEvidenceIdentity
		}
		if task.Status == machinecontract.CognitionMigrationSemanticReviewed && !removedTasks[task.TaskID] {
			result.AuthoringTasks = append(result.AuthoringTasks, task)
		}
	}
	sort.Strings(selfSourceIDs)
	sort.Strings(selfEvidenceRefs)
	selfEvidenceRefs = onboardingUniqueStrings(selfEvidenceRefs)
	result.AuthoringTasks = append(result.AuthoringTasks, migrationapply.MappingAuthoringTask{
		TaskID: "author:self-entry-root", SourceIdentities: selfSourceIDs, SourceEvidenceRefs: selfEvidenceRefs,
		SourceEvidenceIdentity: sourceEvidenceIdentity, TargetAsset: cognition.OwnerRoot,
		CandidateRangeIdentities: []string{selfTarget.Identity}, Status: machinecontract.CognitionMigrationSemanticReviewed,
		Reviewer: "fixture-reviewer",
	})
	sort.Slice(result.AuthoringTasks, func(i, j int) bool { return result.AuthoringTasks[i].TaskID < result.AuthoringTasks[j].TaskID })
	result.MappingGroups = append(result.MappingGroups, migrationapply.MappingGroup{
		MappingGroupID: "group:self-entry-root", SourceIdentities: selfSourceIDs,
		TargetRangeIdentities: []string{selfTarget.Identity}, AuthoringTaskID: "author:self-entry-root",
		ReviewStatus: machinecontract.CognitionMigrationSemanticReviewed, Reviewer: "fixture-reviewer",
	})
	return &result
}

func onboardingUniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func findLegacyLine(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func onboardingCandidate(t *testing.T, root string, plan *cognitionplan.Plan) *cognitionplan.LayoutCandidate {
	t.Helper()
	rootLines := []string{cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: " + plan.Locale,
		"#Project: Model-authored onboarding fixture", "#Global-Invariants: Preserve deterministic fixture boundaries",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled"}
	for _, kind := range plan.TargetKinds {
		if kind == "code" {
			rootLines = append(rootLines, "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled")
		} else if kind == "database" {
			rootLines = append(rootLines, "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled")
		}
	}
	meta := strings.Join([]string{cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing", "#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]", "#A Layer: C Code", "#B Module: D Domain", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
		"#[Tag dictionary: database]", "#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T"}, "\n") + "\n"
	assets := []cognitionplan.CandidateAsset{{AssetID: "root", Path: "aoci.txt", Content: strings.Join(rootLines, "\n") + "\n"},
		{AssetID: "meta", Path: "aoci.meta.txt", Content: meta}}
	if contains(plan.TargetKinds, "code") {
		lines := []string{}
		for _, object := range plan.Inventory {
			if object.Eligible {
				lines = append(lines, filepath.Base(object.Path)+"[CD9S]: F:Represent the model-authored fixture responsibility | R:- | A:- | S:Preserve deterministic onboarding evidence")
			}
		}
		sort.Strings(lines)
		code := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(root) + "/===\n" + strings.Join(lines, "\n") + "\n"
		assets = append(assets, cognitionplan.CandidateAsset{AssetID: "code", Path: "aoci.code.txt", Content: code})
	}
	if contains(plan.TargetKinds, "database") {
		byNamespace := map[string][]string{}
		for _, evidence := range plan.Evidence {
			slash := strings.LastIndex(evidence.ObjectRef, "/")
			if slash < 0 || slash == len(evidence.ObjectRef)-1 {
				t.Fatalf("invalid test-only Evidence object_ref: %s", evidence.ObjectRef)
			}
			namespace, name := evidence.ObjectRef[:slash+1], evidence.ObjectRef[slash+1:]
			byNamespace[namespace] = append(byNamespace[namespace], name+"[DB7S]: F:Represent the model-authored database fixture responsibility | R:- | A:id | S:Preserve deterministic onboarding evidence")
		}
		namespaces := make([]string, 0, len(byNamespace))
		for namespace := range byNamespace {
			namespaces = append(namespaces, namespace)
		}
		sort.Strings(namespaces)
		var database strings.Builder
		database.WriteString(cognition.DatabaseMarker + "\n")
		for _, namespace := range namespaces {
			sort.Strings(byNamespace[namespace])
			database.WriteString("===Database/" + namespace + "===\n")
			database.WriteString(strings.Join(byNamespace[namespace], "\n") + "\n")
		}
		assets = append(assets, cognitionplan.CandidateAsset{AssetID: "database", Path: "aoci.database.txt", Content: database.String()})
	}
	candidate := &cognitionplan.LayoutCandidate{Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: assets, MappingResolutions: []cognitionplan.MappingResolution{}}
	if plan.Operation == cognitionplan.OperationBootstrap {
		candidate.SemanticAuthoringProvenance = &cognitionplan.SemanticAuthoringProvenance{
			Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
			AuthoringRunID: "onboarding-test-host-run", PlanID: plan.PlanID,
			EvidenceBindingSHA256:  cognitionplan.SemanticAuthoringEvidenceBindingSHA256(plan),
			CandidatePayloadSHA256: cognitionplan.CandidatePayloadSHA256(candidate),
		}
	}
	return candidate
}

func hostAuthoredCompletion(session *Session, batch *AuthoringBatch) Completion {
	requirement := batch.SemanticAuthoringRequirement
	if requirement == nil {
		panic("test Host completion requires a semantic authoring requirement")
	}
	taskIDs := make([]string, 0, len(batch.Tasks))
	for _, task := range batch.Tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	return Completion{
		Version: CompletionVersion, SessionID: session.OnboardingSessionID, BatchID: batch.BatchID, CompletedTasks: taskIDs,
		SemanticAuthoringDeclaration: &cognitionplan.SemanticAuthoringDeclaration{
			Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
			AuthoringRunID: "onboarding-test-host-run", DiscoveryPlanID: requirement.DiscoveryPlanID,
			EvidenceBindingSHA256: requirement.EvidenceBindingSHA256,
		},
	}
}

func installOnboardingSavedEvidence(t *testing.T, root string) {
	t.Helper()
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/items",
		Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: "items", Kind: "base_table",
		Columns:    []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
		PrimaryKey: &dbevidence.KeyConstraint{Name: "items_pkey", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{},
		ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
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

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
