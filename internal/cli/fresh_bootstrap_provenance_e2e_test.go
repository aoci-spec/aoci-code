package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

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
