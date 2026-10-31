// A Scope Change must measure source file drift with the same ruler as everyday governance.
//
// A real experience: a user on Windows/Codex registered two binary review files as observe, then went through the formal
// Scope flow, was stopped by managed_scope_index_source_stale, and upgrading to rc5 did not help. From that they
// concluded "rc3 cannot read the entries already in aoci.code.txt". Not one entry was lost: the precondition of that error
// was exactly that the file was already an index-role object in the Baseline. The real cause was internal/scopechange
// comparing bare SHAs, the only consumer in the repository bypassing baseline.EquivalentFingerprints;
// 313a3ab had already fixed the same thing in internal/volumegovernance without reaching here.
//
// core.autocrlf is the default of Git for Windows, so one rewrite: everyday Verify/Check/Guide
// go through EquivalentFingerprints, judge equivalent, report aligned, and the maintenance loop has nothing to do; the Scope Change
// goes through bare SHAs and fails hard. Neither side gives a way out, and the repository is stuck on a block no action can clear.
//
// Locked below: a line-ending-only rewrite no longer blocks and is reported truthfully, while a real content change, and a team that
// explicitly turns tolerance off, keep this fail-closed guard exactly as before.
package scopechange

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func mustMarshalPlan(t *testing.T, plan Plan) []byte {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rewriteFixtureLineEndings swaps LF for CRLF, reproducing the effect of a Windows checkout.
func rewriteFixtureLineEndings(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip to whichever form differs from what is on disk. A one-way LF to CRLF
	// rewrite over content that is already CRLF yields \r\r\n, which is a real
	// content change and would test the opposite of what these tests claim.
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	rewritten := normalized
	if bytes.Equal(raw, normalized) {
		rewritten = bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
	}
	if bytes.Equal(rewritten, raw) ||
		!bytes.Equal(bytes.ReplaceAll(rewritten, []byte("\r\n"), []byte("\n")), normalized) {
		t.Fatalf("fixture precondition broken: rewriting %s is not line-ending-only", rel)
	}
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setFixtureLineEndingTolerance(t *testing.T, root string, tolerate bool) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LineEndingTolerance = tolerate
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLineEndingOnlyIndexSourceNoLongerBlocksScopeChange(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	rewriteFixtureLineEndings(t, root, "main.go")

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatalf("a line-ending-only rewrite is the difference team policy already calls "+
			"equivalent, so it must not block Scope Change: %v", err)
	}

	// Tolerated but not silent: the postimage Baseline records the new bytes, and the approver must see that.
	reported := false
	for _, item := range preview.Plan.SourceLineEndingOnly {
		if item.Path == "main.go" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("a tolerated source must still be reported, not silently accepted: %+v",
			preview.Plan.SourceLineEndingOnly)
	}
}

func TestLineEndingOnlyIndexSourceStillBlocksWhenToleranceIsOff(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	setFixtureLineEndingTolerance(t, root, false)
	rewriteFixtureLineEndings(t, root, "main.go")

	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_index_source_stale") {
		t.Fatalf("a team that switched tolerance off must keep the strict judgement: %v", err)
	}
}

func TestGenuineIndexSourceChangeStillBlocksScopeChange(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	// A real content change: after publication the postimage Baseline stamps the new bytes' SHA onto the entry describing the old bytes.
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package sample\n\nfunc Added() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Build(root, authorizationTestTime, candidates)
	if err == nil || !strings.Contains(err.Error(), "managed_scope_index_source_stale") {
		t.Fatalf("a genuinely changed source must still fail closed: %v", err)
	}
}

func TestLineEndingOnlyReductionIsNotStaleCoverageDebt(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	// main_test.go is exactly the one this fixture demotes from index to observe; give it a line-ending rewrite.
	rewriteFixtureLineEndings(t, root, "main_test.go")

	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, reduction := range preview.Plan.CoverageReductions {
		if reduction.Path == "main_test.go" && reduction.AuthoringState == "stale" {
			t.Fatalf("a line-ending rewrite is not authoring debt, so it must not grade the "+
				"reduction stale: %+v", reduction)
		}
	}
}

func TestPlanIdentityUnchangedWithoutLineEndingDrift(t *testing.T) {
	// SourceLineEndingOnly uses omitempty: a plan without drift serializes exactly as before the field was added,
	// plan_id is unchanged, and an upgrade does not void an in-flight approval. A plan with drift could not be built at all before the fix, so
	// there is nothing to void.
	root, candidates := buildChangeFixture(t)
	preview, err := Build(root, authorizationTestTime, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Plan.SourceLineEndingOnly) != 0 {
		t.Fatalf("clean fixture must carry no line-ending report: %+v", preview.Plan.SourceLineEndingOnly)
	}
	recomputed, err := planIdentity(preview.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != preview.Plan.PlanID {
		t.Fatalf("plan identity is not reproducible: %s vs %s", recomputed, preview.Plan.PlanID)
	}
	if bytes.Contains(mustMarshalPlan(t, preview.Plan), []byte("source_line_ending_only")) {
		t.Fatal("an empty additive field must not enter the serialized plan, or every existing " +
			"plan_id changes and in-flight approvals are stranded")
	}
}

// A formal Volume goes by the same ruler: when a Windows checkout rewrites aoci.code.txt,
// volumegovernance judging equivalent while the Scope Change judges drift is the same hole 313a3ab fixed.
func TestFormalVolumeLineEndingRewriteIsNotBaselineDrift(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n"+
		"#Project: line ending fixture\n#Global-Invariants: -\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n"+
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n"+
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n"+
		"#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n"+
		"#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:run the deterministic fixture | R:- | A:main | S:Execution remains deterministic\n")

	active := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		active.Files[rel] = fingerprint
	}

	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err != nil {
		t.Fatalf("aligned fixture must produce guards: %v", err)
	}

	rewriteFixtureLineEndings(t, root, "aoci.code.txt")
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err != nil {
		t.Fatalf("a line-ending rewrite of a Volume must not read as Baseline drift: %v", err)
	}
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, false); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_formal_volume_baseline_drift") {
		t.Fatalf("tolerance off must keep the strict Volume judgement: %v", err)
	}

	// A real content change is still drift, tolerance or not.
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7T]: F:a genuinely different responsibility | R:- | A:main | S:-\n")
	if _, err := FormalCognitionBaselineGuards(root, "aoci.txt", active, true); err == nil ||
		!strings.Contains(err.Error(), "managed_scope_formal_volume_baseline_drift") {
		t.Fatalf("a genuinely rewritten Volume must still fail closed: %v", err)
	}
}
