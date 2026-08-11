package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
	"github.com/spf13/cobra"
)

func TestProjectInitFreshBootstrapIsAlignedEndToEnd(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	for relative, content := range map[string]string{
		"cmd/app/main.go":              "package main\nfunc main() {}\n",
		"internal/store/store.go":      "package store\ntype Store struct{}\n",
		"internal/store/store_test.go": "package store\n",
		"testdata/fixture.txt":         "excluded fixture\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false", "--cognition=project"); err != nil {
		t.Fatalf("project init failed: %v", err)
	}
	session, exists, err := onboarding.Load(root)
	if err != nil || !exists {
		t.Fatalf("Fresh Session missing: exists=%t err=%v", exists, err)
	}
	planData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(session.PlanArtifact)))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.DecodePlan(planData)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := onboarding.Next(root, 100, 8*1024*1024)
	if err != nil || batch.SemanticAuthoringRequirement == nil {
		t.Fatalf("project authoring batch missing: batch=%#v err=%v", batch, err)
	}
	taskIDs := make([]string, 0, len(batch.Tasks))
	for _, task := range batch.Tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	declaration := &cognitionplan.SemanticAuthoringDeclaration{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "project-init-e2e-host-run", DiscoveryPlanID: batch.SemanticAuthoringRequirement.DiscoveryPlanID,
		EvidenceBindingSHA256: batch.SemanticAuthoringRequirement.EvidenceBindingSHA256,
	}
	if _, err := onboarding.CompleteTasks(root, onboarding.Completion{
		Version: onboarding.CompletionVersion, SessionID: session.OnboardingSessionID, BatchID: batch.BatchID,
		CompletedTasks: taskIDs, SemanticAuthoringDeclaration: declaration,
	}); err != nil {
		t.Fatal(err)
	}
	candidate := projectInitFreshCandidate(root, plan, declaration)
	candidateData, _ := json.Marshal(candidate)
	preview, err := onboarding.Preview(root, candidateData, nil)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("project Candidate Preview failed: preview=%#v err=%v", preview, err)
	}
	completed, err := onboarding.Resume(root)
	if err != nil || completed.Status != "completed" || !completed.CheckOK || !completed.GovernanceAligned {
		t.Fatalf("project Bootstrap did not align: session=%#v err=%v", completed, err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || !strings.Contains(string(set.Meta.Raw), "#B Module: A Application I Infrastructure G Governance") {
		t.Fatalf("project-specific Meta dictionary missing: err=%v meta=%s", err, set.Meta.Raw)
	}
	value, exists, err := baseline.Load(root)
	if err != nil || !exists || value.ManagedScope == nil {
		t.Fatalf("first governed Baseline missing: exists=%t value=%#v err=%v", exists, value, err)
	}
	if value.Files["internal/store/store.go"].Role != machinecontract.ScopeRoleIndex ||
		value.Files["internal/store/store_test.go"].Role != machinecontract.ScopeRoleObserve {
		t.Fatalf("initial Index/Observe roles not closed: %#v", value.Files)
	}
	if _, exists := value.Files["testdata/fixture.txt"]; exists {
		t.Fatal("excluded fixture entered the first Baseline")
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	steps := []struct {
		name   string
		invoke func(*cobra.Command) error
	}{
		{name: "verify", invoke: func(cmd *cobra.Command) error { return runVerify(cmd, nil) }},
		{name: "check", invoke: func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) }},
		{name: "guide", invoke: func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "opencode") }},
	}
	for _, step := range steps {
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		if err := step.invoke(cmd); err != nil {
			t.Fatalf("%s failed: %v output=%s", step.name, err, output.String())
		}
		if step.name == "check" && !strings.Contains(output.String(), `"ok": true`) {
			t.Fatalf("Check did not pass: %s", output.String())
		}
		if step.name == "guide" && (!strings.Contains(output.String(), `"stage": "aligned"`) ||
			!strings.Contains(output.String(), `"next_action": "none"`)) {
			t.Fatalf("Guide did not align: %s", output.String())
		}
	}
}

func projectInitFreshCandidate(root string, plan *cognitionplan.Plan, declaration *cognitionplan.SemanticAuthoringDeclaration) *cognitionplan.LayoutCandidate {
	rootText := strings.Join([]string{
		cognition.RootManifestMarker,
		"#Format-Version: cognition-volumes/v1",
		"#Locale: " + plan.Locale,
		"#Project: Project-specific Fresh initialization fixture",
		"#Global-Invariants: Preserve project evidence and governed object roles",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled",
	}, "\n") + "\n"
	meta := strings.Join([]string{
		cognition.MetaVolumeMarker,
		"#Object-Protocol: repository-cognition-object/v2",
		"#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract",
		"#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]",
		"#A Layer: C Code",
		"#B Module: A Application I Infrastructure G Governance",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
		"#[Tag dictionary: database]",
		"#A Layer: D Database",
		"#B Module: S Schema",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
	}, "\n") + "\n"
	sections := map[string][]string{}
	for _, object := range plan.Inventory {
		if !object.Eligible {
			continue
		}
		module := "A"
		if strings.HasPrefix(object.Path, "internal/") {
			module = "I"
		} else if object.Path == "AGENTS.md" || object.Path == "opencode.json" {
			module = "G"
		}
		directory := filepath.ToSlash(filepath.Dir(object.Path))
		sections[directory] = append(sections[directory], filepath.Base(object.Path)+"[C"+module+"9T]: F:Carries project-bound application or governance evidence | R:- | A:- | S:Preserve its frozen source binding")
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
	candidate := &cognitionplan.LayoutCandidate{
		Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: []cognitionplan.CandidateAsset{
			{AssetID: "root", Path: "aoci.txt", Content: rootText},
			{AssetID: "meta", Path: "aoci.meta.txt", Content: meta},
			{AssetID: "code", Path: "aoci.code.txt", Content: strings.Join(parts, "\n") + "\n"},
		},
		MappingResolutions: []cognitionplan.MappingResolution{},
	}
	candidate.SemanticAuthoringProvenance = &cognitionplan.SemanticAuthoringProvenance{
		Version: declaration.Version, Origin: declaration.Origin, AuthoringRunID: declaration.AuthoringRunID,
		PlanID: declaration.DiscoveryPlanID, EvidenceBindingSHA256: declaration.EvidenceBindingSHA256,
		CandidatePayloadSHA256: cognitionplan.CandidatePayloadSHA256(candidate),
	}
	return candidate
}

func TestFreshBootstrapProvenanceIsolatedEndToEnd(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	session, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC))
	if err != nil || session.Version != onboarding.SessionVersion || session.Operation != cognitionplan.OperationBootstrap {
		t.Fatalf("Fresh Plan failed: session=%#v err=%v", session, err)
	}
	assertAutoOnboardingStatus(t, root, "Analyzing the project...")
	batch, err := onboarding.Next(root, 100, 8*1024*1024)
	if err != nil || batch.SemanticAuthoringRequirement == nil {
		t.Fatalf("Host authoring requirement missing: batch=%#v err=%v", batch, err)
	}
	taskIDs := make([]string, 0, len(batch.Tasks))
	for _, task := range batch.Tasks {
		taskIDs = append(taskIDs, task.TaskID)
	}
	requirement := batch.SemanticAuthoringRequirement
	declaration := &cognitionplan.SemanticAuthoringDeclaration{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "isolated-e2e-host-run", DiscoveryPlanID: requirement.DiscoveryPlanID,
		EvidenceBindingSHA256: requirement.EvidenceBindingSHA256,
	}
	if _, err := onboarding.CompleteTasks(root, onboarding.Completion{
		Version: onboarding.CompletionVersion, SessionID: session.OnboardingSessionID, BatchID: batch.BatchID,
		CompletedTasks: taskIDs, SemanticAuthoringDeclaration: declaration,
	}); err != nil {
		t.Fatal(err)
	}
	assertAutoOnboardingStatus(t, root, "Establishing project cognition...")
	candidate := isolatedFreshCandidate(session.Plan, declaration)
	candidateData, _ := json.Marshal(candidate)
	preview, err := onboarding.Preview(root, candidateData, nil)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil {
		t.Fatalf("provenance Preview failed: preview=%#v err=%v", preview, err)
	}
	assertAutoOnboardingStatus(t, root, "Verifying safety boundaries...")
	completed, err := onboarding.Resume(root)
	if err != nil || completed.Status != "completed" || !completed.CheckOK || !completed.GovernanceAligned ||
		completed.NetworkAccessed || completed.BusinessRowsRead != 0 || completed.DDLDMLStatements != 0 {
		t.Fatalf("Prepare/Apply did not close isolated Fresh Bootstrap: session=%#v err=%v", completed, err)
	}
	assertAutoOnboardingStatus(t, root, "Project cognition established.")
	if pending, err := bootstrapapply.Pending(root); err != nil || len(pending) != 0 {
		t.Fatalf("Bootstrap Recovery remains pending: pending=%v err=%v", pending, err)
	}
	if pending, err := cognitiontxn.Pending(root); err != nil || len(pending) != 0 {
		t.Fatalf("shared Recovery remains pending: pending=%v err=%v", pending, err)
	}
	for _, command := range []string{"verify", "check"} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI([]string{"--repo", root, "--json", command}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("%s did not PASS: code=%d stdout=%s stderr=%s", command, code, stdout.String(), stderr.String())
		}
	}
}

func assertAutoOnboardingStatus(t *testing.T, root, expected string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "cognition", "onboard", "status"}, &stdout, &stderr)
	official := map[string]string{
		"Analyzing the project...":          "正在分析项目...",
		"Establishing project cognition...": "正在建立项目认知...",
		"Verifying safety boundaries...":    "正在验证安全边界...",
		"Project cognition established.":    "项目认知建立完成。",
	}
	output := strings.TrimSuffix(stdout.String(), "\n")
	if code != ExitOK || (output != expected && output != official[expected]) ||
		strings.Count(stdout.String(), "\n") != 1 || stderr.Len() != 0 {
		t.Fatalf("compact Auto UX mismatch: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func isolatedFreshCandidate(plan *cognitionplan.Plan, declaration *cognitionplan.SemanticAuthoringDeclaration) *cognitionplan.LayoutCandidate {
	root := strings.Join([]string{
		cognition.RootManifestMarker,
		"#Format-Version: cognition-volumes/v1",
		"#Locale: " + plan.Locale,
		"#Project: Host-authored isolated Fresh Bootstrap",
		"#Global-Invariants: Preserve the exact evidence-bound candidate",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
	}, "\n") + "\n"
	meta := strings.Join([]string{
		cognition.MetaVolumeMarker,
		"#Object-Protocol: repository-cognition-object/v2",
		"#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract",
		"#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]",
		"#A Layer: C Code",
		"#B Module: D Domain",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
		"#[Tag dictionary: database]",
		"#A Layer: D Database",
		"#B Module: B Business",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
	}, "\n") + "\n"
	candidate := &cognitionplan.LayoutCandidate{
		Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: []cognitionplan.CandidateAsset{
			{AssetID: "root", Path: filepath.ToSlash("aoci.txt"), Content: root},
			{AssetID: "meta", Path: filepath.ToSlash("aoci.meta.txt"), Content: meta},
		},
		MappingResolutions: []cognitionplan.MappingResolution{},
	}
	candidate.SemanticAuthoringProvenance = &cognitionplan.SemanticAuthoringProvenance{
		Version: declaration.Version, Origin: declaration.Origin, AuthoringRunID: declaration.AuthoringRunID,
		PlanID: declaration.DiscoveryPlanID, EvidenceBindingSHA256: declaration.EvidenceBindingSHA256,
		CandidatePayloadSHA256: cognitionplan.CandidatePayloadSHA256(candidate),
	}
	return candidate
}
