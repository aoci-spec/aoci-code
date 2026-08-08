package cognitionplan

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestSemanticAuthoringProvenanceGate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("verified_host_model_receipt", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		preview, validateErr := ValidateCandidate(root, plan, candidate)
		if validateErr != nil || preview.Status != machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest == nil ||
			preview.SemanticAuthoringProvenance == nil || preview.SemanticAuthoringProvenance.Status != machinecontract.SemanticAuthoringStatusVerified {
			t.Fatalf("verified provenance did not pass: preview=%#v err=%v", preview, validateErr)
		}
	})

	t.Run("missing_receipt", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance = nil
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_provenance_missing")
	})

	t.Run("fake_host_model_without_bindings", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance = &SemanticAuthoringProvenance{
			Version:        machinecontract.SemanticAuthoringProvenanceV1,
			Origin:         machinecontract.SemanticAuthoringOriginHostModel,
			AuthoringRunID: "fake-host-model-run",
		}
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_plan_mismatch")
		assertPreviewHasRisk(t, root, plan, candidate, "semantic_authoring_evidence_mismatch")
		assertPreviewHasRisk(t, root, plan, candidate, "semantic_authoring_candidate_mismatch")
	})

	t.Run("program_origin", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance.Origin = "program"
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_origin_invalid")
	})

	t.Run("candidate_bytes_changed", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.Assets[0].Content += "# changed after receipt\n"
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_candidate_mismatch")
	})

	t.Run("evidence_binding_changed", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance.EvidenceBindingSHA256 = string(make([]byte, 64))
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_evidence_mismatch")
	})
}

func TestSemanticAuthoringProvenanceStrictJSONAndLegacyIdentity(t *testing.T) {
	if _, err := DecodeCandidate([]byte(`{"version":"cognition-layout-candidate/v2","plan_id":"p","assets":[],"mapping_resolutions":[]}`)); err == nil {
		t.Fatal("unknown Candidate version crossed the v1 contract")
	}
	data := []byte(`{"version":"cognition-layout-candidate/v1","plan_id":"p","assets":[],"mapping_resolutions":[],"semantic_authoring_provenance":{"version":"semantic-authoring-provenance/v1","origin":"host_model","authoring_run_id":"r","plan_id":"p","evidence_binding_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","candidate_payload_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","unknown":true}}`)
	if _, err := DecodeCandidate(data); err == nil {
		t.Fatal("unknown provenance JSON field was accepted")
	}

	candidate, err := DecodeCandidate([]byte(`{"version":"cognition-layout-candidate/v1","plan_id":"legacy-plan","assets":[{"asset_id":"root","path":"aoci.txt","content":"root\n"}],"mapping_resolutions":[]}`))
	if err != nil {
		t.Fatalf("legacy Candidate without provenance no longer decodes: %v", err)
	}
	if got, want := candidateIdentity(candidate), CandidatePayloadSHA256(candidate); got != want {
		t.Fatalf("nil provenance changed legacy Candidate identity: got=%s want=%s", got, want)
	}
	if got, want := candidateIdentity(candidate), "352164cd15fe05dffd0b4e419b447c7b443239990c1dce48638b39d83d9bff1b"; got != want {
		t.Fatalf("legacy Candidate identity bytes changed: got=%s want=%s", got, want)
	}
	withReceipt := *candidate
	withReceipt.SemanticAuthoringProvenance = &SemanticAuthoringProvenance{
		Version: machinecontract.SemanticAuthoringProvenanceV1, Origin: machinecontract.SemanticAuthoringOriginHostModel,
		AuthoringRunID: "run", PlanID: candidate.PlanID, EvidenceBindingSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CandidatePayloadSHA256: CandidatePayloadSHA256(candidate),
	}
	if candidateIdentity(&withReceipt) == candidateIdentity(candidate) {
		t.Fatal("provenance receipt did not bind the complete Candidate identity")
	}
}

func TestSemanticAuthoringProvenanceRejectsUntrustedOriginsAndRunIDs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"program", "framework", "fixture"} {
		t.Run("origin_"+origin, func(t *testing.T) {
			candidate := validCandidate(t, root, plan)
			candidate.SemanticAuthoringProvenance.Origin = origin
			assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_origin_invalid")
		})
	}
	for _, test := range []struct {
		name  string
		value string
	}{{"empty", ""}, {"leading_space", " run"}, {"trailing_space", "run "}, {"control", "run\nnext"}, {"too_long", strings.Repeat("r", 129)}} {
		t.Run("run_id_"+test.name, func(t *testing.T) {
			candidate := validCandidate(t, root, plan)
			candidate.SemanticAuthoringProvenance.AuthoringRunID = test.value
			assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_run_id_invalid")
		})
	}
}

func TestSemanticAuthoringProvenanceBindsPlanEvidenceAndAllCandidateSemantics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("plan", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance.PlanID = strings.Repeat("a", 64)
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_plan_mismatch")
	})
	t.Run("evidence", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance.EvidenceBindingSHA256 = strings.Repeat("a", 64)
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_evidence_mismatch")
	})
	t.Run("candidate_payload", func(t *testing.T) {
		candidate := validCandidate(t, root, plan)
		candidate.SemanticAuthoringProvenance.CandidatePayloadSHA256 = strings.Repeat("a", 64)
		assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_candidate_mismatch")
	})
	for _, semantic := range []string{"[CD9S]", "F:Model-authored", "R:-", "A:-", "S:Keep"} {
		t.Run("mutated_"+strings.Trim(semantic, "[]:-"), func(t *testing.T) {
			candidate := validCandidate(t, root, plan)
			candidate.Assets[2].Content = strings.Replace(candidate.Assets[2].Content, semantic, semantic+"changed", 1)
			assertProvenanceBlocked(t, root, plan, candidate, "semantic_authoring_candidate_mismatch")
		})
	}
	t.Run("copied_receipt", func(t *testing.T) {
		first := validCandidate(t, root, plan)
		second := validCandidate(t, root, plan)
		second.Assets[0].Content = strings.Replace(second.Assets[0].Content, "fixture project", "another project", 1)
		receipt := *first.SemanticAuthoringProvenance
		second.SemanticAuthoringProvenance = &receipt
		assertProvenanceBlocked(t, root, plan, second, "semantic_authoring_candidate_mismatch")
	})
}

func TestSemanticAuthoringRequirementIsPlanAndCandidateProjectionOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		t.Fatal(err)
	}
	requirement := plan.SemanticAuthoringRequirement
	if requirement == nil || requirement.Version != machinecontract.SemanticAuthoringRequirementV1 ||
		requirement.RequiredOrigin != machinecontract.SemanticAuthoringOriginHostModel || !requirement.AuthoringRunIDRequired ||
		requirement.DiscoveryPlanID != plan.PlanID || requirement.EvidenceBindingSHA256 != SemanticAuthoringEvidenceBindingSHA256(plan) ||
		!requirement.CandidatePayloadRequired || requirement.CandidatePayloadSHA256 != "" {
		t.Fatalf("Plan did not issue the exact Host authoring requirement: %#v", requirement)
	}
	candidate := validCandidate(t, root, plan)
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil || preview.SemanticAuthoringRequirement == nil ||
		preview.SemanticAuthoringRequirement.CandidatePayloadSHA256 != CandidatePayloadSHA256(candidate) {
		t.Fatalf("Preview did not project the Candidate binding requirement: preview=%#v err=%v", preview, err)
	}
}

func TestSemanticAuthoringBindingsSupersedeOnSourceEvidenceTargetOrLocaleDrift(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "main.go", "package main\n")
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
		if err != nil {
			t.Fatal(err)
		}
		candidate := validCandidate(t, root, plan)
		writeFile(t, root, "main.go", "package main\n// changed\n")
		assertSupersededWithoutApproval(t, root, plan, candidate)
	})
	t.Run("evidence", func(t *testing.T) {
		root := t.TempDir()
		installDatabaseEvidence(t, root)
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"database"}})
		if err != nil {
			t.Fatal(err)
		}
		candidate := validCandidate(t, root, plan)
		manifest, snapshot, exists, err := dbevidence.LoadSnapshot(root, "primary")
		if err != nil || !exists || len(snapshot.Tables) != 1 {
			t.Fatalf("test Evidence missing: snapshot=%#v exists=%t err=%v", snapshot, exists, err)
		}
		table, err := dbevidence.LoadTableEvidence(root, snapshot.Tables[0])
		if err != nil {
			t.Fatal(err)
		}
		table.Columns = append(table.Columns, dbevidence.Column{Ordinal: 2, Name: "name", NativeType: "varchar(20)", CanonicalType: "string", Nullable: true})
		changed, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
		if err != nil {
			t.Fatal(err)
		}
		if err := dbevidence.WriteSnapshot(root, manifest, changed, files); err != nil {
			t.Fatal(err)
		}
		if err := dbevidence.AcceptSnapshot(root, changed, changed.SourceSnapshotSHA256); err != nil {
			t.Fatal(err)
		}
		assertSupersededWithoutApproval(t, root, plan, candidate)
	})
	for _, change := range []struct {
		name   string
		mutate func(*Plan)
	}{{"target", func(plan *Plan) { plan.TargetKinds = nil }}, {"locale", func(plan *Plan) { plan.Locale = "zh-CN" }}} {
		t.Run(change.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "main.go", "package main\n")
			plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
			if err != nil {
				t.Fatal(err)
			}
			candidate := validCandidate(t, root, plan)
			change.mutate(plan)
			assertSupersededWithoutApproval(t, root, plan, candidate)
		})
	}
}

func assertSupersededWithoutApproval(t *testing.T, root string, plan *Plan, candidate *LayoutCandidate) {
	t.Helper()
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != machinecontract.CognitionPlannerSuperseded || preview.ApprovalDigest != nil ||
		!preview.FormalAssetProof.FormalAssetsUnchanged {
		t.Fatalf("drift did not fail closed as superseded: %#v", preview)
	}
}

func assertProvenanceBlocked(t *testing.T, root string, plan *Plan, candidate *LayoutCandidate, code string) {
	t.Helper()
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status == machinecontract.CognitionPlannerPreviewReady || preview.ApprovalDigest != nil || !hasRisk(preview.Risks, code) || preview.SemanticAuthoringProvenance != nil {
		t.Fatalf("untrusted provenance was not blocked for %s: %#v", code, preview)
	}
}

func assertPreviewHasRisk(t *testing.T, root string, plan *Plan, candidate *LayoutCandidate, code string) {
	t.Helper()
	preview, err := ValidateCandidate(root, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRisk(preview.Risks, code) {
		t.Fatalf("missing provenance risk %s: %#v", code, preview.Risks)
	}
}
