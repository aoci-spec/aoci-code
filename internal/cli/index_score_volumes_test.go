// index score on a Cognition Volumes repository: the nine dimensions are
// projected from the governance facts, so every number equals verify's.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/indexgen"
)

func scoreJSON(t *testing.T, root string, args ...string) indexgen.Score {
	t.Helper()
	stdout, stderr, code := runCLI(t, root, append([]string{"--json", "index", "score"}, args...)...)
	if code != ExitOK {
		t.Fatalf("index score exit %d\n%s%s", code, stdout, stderr)
	}
	var score indexgen.Score
	if err := json.Unmarshal([]byte(stdout), &score); err != nil {
		t.Fatalf("score JSON: %v\n%s", err, stdout)
	}
	return score
}

// volumeVerifyJSON decodes verify's report whatever its exit code: a drifting
// fixture exits 1 and still prints the facts the score must agree with.
func volumeVerifyJSON(t *testing.T, root string) volumeVerifyReport {
	t.Helper()
	stdout, stderr, _ := runCLI(t, root, "--json", "verify")
	var report volumeVerifyReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || report.Governance == nil {
		t.Fatalf("verify JSON: %v\n%s%s", err, stdout, stderr)
	}
	return report
}

// numericEScaleMeta gives the fixture's code dictionary numeric E bands, the
// form a real Meta carries, so the E scale audit has thresholds to judge by.
func numericEScaleMeta(t *testing.T, root string) {
	t.Helper()
	metaPath := filepath.Join(root, "aoci.meta.txt")
	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(meta), "#E Scale: L M S T\n", "#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n", 1)
	if text == string(meta) {
		t.Fatal("fixture Meta no longer carries the symbolic E scale line")
	}
	if err := os.WriteFile(metaPath, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scoreDimension(t *testing.T, score indexgen.Score, name string) indexgen.Dimension {
	t.Helper()
	for _, dimension := range score.Dimensions {
		if dimension.Name == name {
			return dimension
		}
	}
	t.Fatalf("score lacks the %s dimension: %+v", name, score.Dimensions)
	return indexgen.Dimension{}
}

func TestVolumeIndexScoreReportsNineDimensionsFromGovernance(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	numericEScaleMeta(t, root)
	score := scoreJSON(t, root)
	verify := volumeVerifyJSON(t, root)

	names := []string{}
	for _, dimension := range score.Dimensions {
		names = append(names, dimension.Name)
	}
	if strings.Join(names, ",") != strings.Join(scoreDimNames, ",") {
		t.Fatalf("dimensions %v, want %v", names, scoreDimNames)
	}
	if score.Layout != cognition.LayoutVolumesV1 {
		t.Fatalf("layout %q", score.Layout)
	}
	ids := []string{}
	for _, asset := range score.Volumes {
		ids = append(ids, asset.ID)
		if asset.SHA256 == "" || asset.AssetState != cognition.AssetPresent {
			t.Fatalf("asset summary without identity: %+v", asset)
		}
	}
	if strings.Join(ids, ",") != "root,meta,code" {
		t.Fatalf("volumes %v", ids)
	}
	if score.EntryCount != 1 || score.DiskCount != verify.Governance.CodeSourceCount {
		t.Fatalf("entries %d disk %d, verify counts %d sources", score.EntryCount, score.DiskCount, verify.Governance.CodeSourceCount)
	}
	for _, name := range []string{"format", "coverage", "freshness", "squota", "dict", "agent_ready", "tagparse"} {
		if dimension := scoreDimension(t, score, name); dimension.Bad != 0 {
			t.Fatalf("%s reports %d violations on an aligned fixture: %+v", name, dimension.Bad, dimension)
		}
	}
	if dict := scoreDimension(t, score, "dict"); dict.Total != 1 {
		t.Fatalf("dict judged %d objects under the Meta dictionary, want 1", dict.Total)
	}
	token := scoreDimension(t, score, "token")
	if score.Budget == nil || token.Total != verify.Governance.Budget.WholeIndexTokens ||
		score.IndexTokens != token.Total || score.Budget.WholeIndexTokens != token.Total {
		t.Fatalf("token %d, budget %+v, verify measured %d", token.Total, score.Budget, verify.Governance.Budget.WholeIndexTokens)
	}
	// main.go is one line and its Entry says S: the audit verify runs is the
	// one the score shows.
	escale := scoreDimension(t, score, "escale")
	if verify.EScale == nil || verify.EScale.Mismatched != 1 {
		t.Fatalf("fixture premise: verify must audit one E scale mismatch, got %+v", verify.EScale)
	}
	if escale.Total != 1 || escale.Bad != verify.EScale.Mismatched || strings.Join(escale.Samples, ",") != "main.go" {
		t.Fatalf("escale %+v, verify audited %+v", escale, verify.EScale)
	}

	stdout, stderr, code := runCLI(t, root, "index", "score", "--dim", "escale")
	if code != ExitOK || !strings.Contains(stdout, "main.go") {
		t.Fatalf("--dim escale exit %d\n%s%s", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, root, "index", "score")
	if code != ExitOK || !strings.Contains(stdout, "escale") {
		t.Fatalf("human score exit %d\n%s%s", code, stdout, stderr)
	}
}

func TestVolumeIndexScoreDriftEqualsVerify(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	// A new source (missing), a changed one (stale), and an Entry whose file
	// is gone (orphan); the Code Volume's own fingerprint is rebound so the
	// orphan Entry is drift of the sources, not of the formal asset.
	if err := os.WriteFile(filepath.Join(root, "extra.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(root, "aoci.code.txt")
	codeVolume, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codePath, append(codeVolume, []byte("gone.go[CD5T]: F:a file that left | R:- | A:- | S:-\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("baseline: %v exists=%v", err, exists)
	}
	fingerprint, err := baseline.HashFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	state.Files["aoci.code.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}

	verify := volumeVerifyJSON(t, root)
	drift := verify.Governance.CodeDrift
	if len(drift.Missing) != 1 || len(drift.Stale) != 1 || len(drift.Orphan) != 1 {
		t.Fatalf("fixture premise: one missing, one stale, one orphan, got %+v", drift)
	}
	score := scoreJSON(t, root)

	if coverage := scoreDimension(t, score, "coverage"); coverage.Bad != len(drift.Missing) || strings.Join(coverage.Samples, ",") != strings.Join(drift.Missing, ",") {
		t.Fatalf("coverage %+v, verify missing %v", coverage, drift.Missing)
	}
	unresolved := map[string]bool{}
	for _, group := range [][]string{drift.Missing, drift.Orphan, drift.Stale, drift.Unbaselined} {
		for _, rel := range group {
			unresolved[rel] = true
		}
	}
	if freshness := scoreDimension(t, score, "freshness"); freshness.Bad != len(unresolved) || freshness.Total != verify.Governance.CodeSourceCount {
		t.Fatalf("freshness %+v, verify lists %d unresolved paths over %d sources", freshness, len(unresolved), verify.Governance.CodeSourceCount)
	}
	want := indexgen.DriftSummary{Missing: len(drift.Missing) + len(drift.Skipped) + len(drift.CurationExcluded),
		RawMissing: len(drift.Missing) + len(drift.Skipped) + len(drift.CurationExcluded), ActionableMissing: len(drift.Missing),
		Orphan: len(drift.Orphan), Stale: len(drift.Stale), Unbaselined: len(drift.Unbaselined), LineEndingOnly: len(drift.LineEndingOnly),
		SkippedMissing: len(drift.Skipped), CurationExcludedMissing: len(drift.CurationExcluded),
		ObservedNew: len(drift.ObservedNew), ObservedChanged: len(drift.ObservedChanged), ObservedRemoved: len(drift.ObservedRemoved)}
	if score.Drift != want {
		t.Fatalf("drift summary %+v, want %+v", score.Drift, want)
	}
	if score.ManagedScope.IndexCount != verify.Governance.ManagedScope.IndexCount || score.ManagedScope.ObserveCount != verify.Governance.ManagedScope.ObserveCount {
		t.Fatalf("managed scope %+v, verify %+v", score.ManagedScope, verify.Governance.ManagedScope)
	}
	if escale := scoreDimension(t, score, "escale"); verify.EScale == nil || escale.Bad != verify.EScale.Mismatched {
		t.Fatalf("escale %+v, verify audited %+v", escale, verify.EScale)
	}
}

func TestVolumeIndexScoreJudgesDatabaseObjectsUnderTheirDictionary(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, true)
	numericEScaleMeta(t, root)
	score := scoreJSON(t, root)
	if score.EntryCount != 2 {
		t.Fatalf("entries %d, want the Code and the Database object", score.EntryCount)
	}
	for _, name := range []string{"format", "squota", "dict", "tagparse"} {
		dimension := scoreDimension(t, score, name)
		if dimension.Total != 2 || dimension.Bad != 0 {
			t.Fatalf("%s %+v, want both objects judged and clean", name, dimension)
		}
	}
	if escale := scoreDimension(t, score, "escale"); escale.Total != 1 {
		t.Fatalf("escale judged %d objects, want the Code object only", escale.Total)
	}
	ids := []string{}
	for _, asset := range score.Volumes {
		ids = append(ids, asset.ID)
	}
	if strings.Join(ids, ",") != "root,meta,code,database" {
		t.Fatalf("volumes %v", ids)
	}
}

// A Legacy score keeps its bytes: the three Volumes fields never appear.
func TestLegacyIndexScoreKeepsItsShape(t *testing.T) {
	root := buildUpdateRepo(t)
	stdout, stderr, code := runCLI(t, root, "--json", "index", "score")
	if code != ExitOK {
		t.Fatalf("legacy score exit %d\n%s%s", code, stdout, stderr)
	}
	for _, key := range []string{`"layout"`, `"volumes"`, `"budget"`} {
		if strings.Contains(stdout, key) {
			t.Fatalf("legacy score carries %s:\n%s", key, stdout)
		}
	}
}
