package volumegovernance

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
)

const pngHead = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00"

// Held sources are index-role files without an Entry that no model can author
// from their bytes. Until v0.1.0-rc14 Verify counted every raw Missing file
// while Maintain withheld these behind a curation decision Volumes v1 has no
// path to make, so a repository holding one image never reached aligned in
// auto mode. This pins the one computation every consumer now reads.
func TestHeldSourcesAreReportedNeverPlannedAndNeverBlock(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	writeFixtureFile(t, root, "assets/logo.png", pngHead)
	writeFixtureFile(t, root, "data/empty.txt", "")
	writeFixtureFile(t, root, "data/dump.txt", strings.Repeat("row\n", (1<<20)/4+1))
	stampFingerprints(t, root, "assets/logo.png", "data/empty.txt", "data/dump.txt")

	facts := assessHeldFixture(t, root, cfg)
	if !facts.GovernanceAligned || facts.Result != ResultAligned {
		t.Fatalf("held sources must not be authoring debt: result=%s findings=%+v", facts.Result, facts.Findings)
	}
	if got, want := strings.Join(facts.CodeDrift.Skipped, ","), "assets/logo.png,data/dump.txt,data/empty.txt"; got != want || len(facts.CodeDrift.Missing) != 0 {
		t.Fatalf("skipped=%q missing=%v", got, facts.CodeDrift.Missing)
	}
	causes := map[string]string{}
	for _, finding := range facts.Findings {
		if finding.Code == FindingCodeSkipped {
			causes[finding.Target] = finding.Cause
		}
	}
	if causes["assets/logo.png"] != "binary" || causes["data/empty.txt"] != "empty" || causes["data/dump.txt"] != "oversize" {
		t.Fatalf("skipped findings must name their cause: %v", causes)
	}
	set := loadFixtureSet(t, root, cfg)
	if work := CodeAuthoringWorkFor(set, facts.CodeDrift); len(work.Targets) != 0 {
		t.Fatalf("a held source must never be planned: %v", work.Targets)
	}

	// A held source whose bytes move binds nothing: it is neither stale nor
	// unbaselined, and a new one without a fingerprint is simply held.
	writeFixtureFile(t, root, "assets/logo.png", pngHead+"moved")
	writeFixtureFile(t, root, "assets/new.png", "\x00binary")
	facts = assessHeldFixture(t, root, cfg)
	if !facts.GovernanceAligned || len(facts.CodeDrift.Stale) != 0 || len(facts.CodeDrift.Unbaselined) != 0 || len(facts.CodeDrift.Skipped) != 4 {
		t.Fatalf("moved or new held sources must stay informational: %+v", facts.CodeDrift)
	}

	// An ordinary new file is still authoring debt, planned exactly once.
	writeFixtureFile(t, root, "pkg/util.go", "package pkg\n")
	facts = assessHeldFixture(t, root, cfg)
	work := CodeAuthoringWorkFor(set, facts.CodeDrift)
	if facts.GovernanceAligned || facts.Result != ResultAuthoringRequired || strings.Join(work.Targets, ",") != "pkg/util.go" {
		t.Fatalf("actionable file lost: result=%s targets=%v", facts.Result, work.Targets)
	}

	// A team curation exclusion is the other held kind, with its source named.
	cfg.CurationExclude = []string{"data/empty.txt"}
	facts = assessHeldFixture(t, root, cfg)
	if strings.Join(facts.CodeDrift.CurationExcluded, ",") != "data/empty.txt" || len(facts.CodeDrift.Skipped) != 3 {
		t.Fatalf("curation exclusion must move the file out of skipped: %+v", facts.CodeDrift)
	}
	excludedCause := ""
	for _, finding := range facts.Findings {
		if finding.Code == FindingCodeCurationExcluded && finding.Target == "data/empty.txt" {
			excludedCause = finding.Cause
		}
	}
	if excludedCause != "config" {
		t.Fatalf("curation exclusion must name its source, got %q", excludedCause)
	}
}

func TestSkippedAndCurationExcludedFindingsAreInformational(t *testing.T) {
	facts := &Facts{StructureValid: true, Findings: []Finding{
		{Code: FindingCodeSkipped, Domain: cognition.ScopeCode, Target: "assets/logo.png", Cause: "binary"},
		{Code: FindingCodeCurationExcluded, Domain: cognition.ScopeCode, Target: "data/empty.txt", Cause: "config"},
	}}
	finalize(facts)
	if facts.Result != ResultAligned || !facts.GovernanceAligned {
		t.Fatalf("held-source findings must not change the result, got %q", facts.Result)
	}
	if !Informational(FindingCodeSkipped) || !Informational(FindingCodeCurationExcluded) || Informational("code_missing") {
		t.Fatal("Informational must name exactly the codes finalize ignores")
	}
}

func assessHeldFixture(t *testing.T, root string, cfg *config.Config) *Facts {
	t.Helper()
	facts, err := Assess(root, cfg, loadFixtureSet(t, root, cfg))
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func loadFixtureSet(t *testing.T, root string, cfg *config.Config) *cognition.Set {
	t.Helper()
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// stampFingerprints records the files in the Baseline the way scan does, so
// the fixture is at the state a first scan leaves a repository in.
func stampFingerprints(t *testing.T, root string, rels ...string) {
	t.Helper()
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range rels {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = fingerprint
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
}

// A valid exclude decision in curation.json on an index-role file is the held
// kind a Managed Scope repository can actually reach besides skipped bytes;
// the finding names its source.
func TestCurationDecisionIsHeldAndNamesItsSource(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	writeFixtureFile(t, root, "data/empty.txt", "")
	stampFingerprints(t, root, "data/empty.txt")
	profiles := curation.BuildProfiles(root, []string{"data/empty.txt"})
	document := curation.NewDocument()
	document.Decisions = append(document.Decisions, curation.Decision{Path: "data/empty.txt", Decision: curation.DecisionExclude,
		Role: "placeholder", Reason: "empty placeholder kept for layout", Confidence: 90,
		SourceSHA256: profiles["data/empty.txt"].SourceSHA256, Agent: "test", UpdatedAt: "2026-09-25T00:00:00Z"})
	if err := curation.Save(root, document); err != nil {
		t.Fatal(err)
	}
	facts := assessHeldFixture(t, root, cfg)
	if !facts.GovernanceAligned || strings.Join(facts.CodeDrift.CurationExcluded, ",") != "data/empty.txt" || len(facts.CodeDrift.Skipped) != 0 {
		t.Fatalf("curation exclusion must be held and reported: aligned=%v drift=%+v", facts.GovernanceAligned, facts.CodeDrift)
	}
	for _, finding := range facts.Findings {
		if finding.Code == FindingCodeCurationExcluded && finding.Target == "data/empty.txt" && finding.Cause == "curation.json" {
			return
		}
	}
	t.Fatalf("curation exclusion must name its source: %+v", facts.Findings)
}
