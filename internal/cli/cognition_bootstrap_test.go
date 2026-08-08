package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/bootstrapapply"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestCognitionBootstrapPrepareAndApplyMachineJSON(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	plan, err := cognitionplan.BootstrapPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := cognitionplan.LayoutCandidate{
		Version: machinecontract.CognitionLayoutCandidateV1, PlanID: plan.PlanID,
		Assets: []cognitionplan.CandidateAsset{
			{AssetID: "root", Path: "aoci.txt", Content: strings.Join([]string{
				cognition.RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: en-US",
				"#Project: Model-authored CLI fixture", "#Global-Invariants: Preserve exact approved bytes",
				"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
			}, "\n") + "\n"},
			{AssetID: "meta", Path: "aoci.meta.txt", Content: strings.Join([]string{
				cognition.MetaVolumeMarker, "#Object-Protocol: repository-cognition-object/v2", "#FRAS-Discipline: 2",
				"#FRAS-v2-Limits-Authority: machine-contract", "#S-Admission: non-inferable-and-error-preventing",
				"#Object-Kinds: code=file database=table", "#[Tag dictionary: code]", "#A Layer: C Code",
				"#B Module: D Domain", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T", "#[Tag dictionary: database]",
				"#A Layer: D Database", "#B Module: B Business", "#C Importance: 9 8 7 5 3 1", "#E Scale: L M S T",
			}, "\n") + "\n"},
		},
		MappingResolutions: []cognitionplan.MappingResolution{},
	}
	candidate.SemanticAuthoringProvenance = &cognitionplan.SemanticAuthoringProvenance{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "cli-bootstrap-test-host-run", PlanID: plan.PlanID,
		EvidenceBindingSHA256:  cognitionplan.SemanticAuthoringEvidenceBindingSHA256(plan),
		CandidatePayloadSHA256: cognitionplan.CandidatePayloadSHA256(&candidate),
	}
	preview, err := cognitionplan.ValidateCandidate(root, plan, &candidate)
	if err != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady {
		t.Fatalf("preview invalid: %#v err=%v", preview, err)
	}
	artifacts := t.TempDir()
	planPath := writeBootstrapJSON(t, artifacts, "plan.json", plan)
	candidatePath := writeBootstrapJSON(t, artifacts, "candidate.json", candidate)
	previewPath := writeBootstrapJSON(t, artifacts, "preview.json", preview)

	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{
		"--repo", root, "--json", "cognition", "bootstrap", "prepare",
		"--plan-file", planPath, "--candidate-file", candidatePath, "--preview-file", previewPath,
		"--baseline-timestamp", "2026-07-30T00:00:00Z",
	}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Prepare failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope bootstrapapply.ApplyEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope.Version != machinecontract.CognitionBootstrapApplyEnvelopeV1 {
		t.Fatalf("Prepare machine JSON invalid: %#v err=%v", envelope, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "aoci.txt")); !os.IsNotExist(err) {
		t.Fatalf("Prepare wrote formal Root: %v", err)
	}
	approval, err := bootstrapapply.RecordApproval(&envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		t.Fatal(err)
	}
	envelopePath := writeBootstrapJSON(t, artifacts, "envelope.json", envelope)
	approvalPath := writeBootstrapJSON(t, artifacts, "approval.json", approval)
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{
		"--repo", root, "--json", "cognition", "bootstrap", "apply",
		"--envelope-file", envelopePath, "--approval-file", approvalPath,
	}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Apply failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var result bootstrapapply.ApplyResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Status != bootstrapapply.StatusApplied || !result.FormalComplete {
		t.Fatalf("Apply machine JSON invalid: %#v err=%v", result, err)
	}
}

func writeBootstrapJSON(t *testing.T, directory, name string, value any) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
