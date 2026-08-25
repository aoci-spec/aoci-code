package cognitionplan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestCompareCodeTargetIndexDerivesStableCreateUpdateDeletePlan(t *testing.T) {
	root := codeTargetDiffFixture(t)
	baseBefore, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	target := codeTargetVolume(root,
		"alpha.go[CD9S]: F:coordinate changed alpha behavior | R:code:new.go | A:Alpha | S:Keep alpha ordering stable",
		"new.go[CD9S]: F:implement the planned module behavior | R:- | A:New | S:Keep target intent explicit",
		"unchanged.go[CD9S]: F:retain unchanged behavior | R:- | A:- | S:-",
	)
	report, err := CompareCodeTargetIndex(root, "aoci.txt", []byte(target))
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != machinecontract.CognitionCodeTargetDiffV1 || report.Status != machinecontract.CognitionCodeTargetDiffReady ||
		!report.Derived || report.Authoritative || report.SourceBound || report.ApplyAllowed || report.FormalWritesStarted ||
		report.NetworkAccessed || report.BusinessDataRead || !report.RawBytesChanged || report.FormalTextOnly || report.DiffSHA256 == "" {
		t.Fatalf("unexpected target Diff contract: %#v", report)
	}
	if report.Summary != (CodeTargetDiffSummary{Created: 1, Updated: 1, Deleted: 1, Unchanged: 1}) {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	wantRefs := []string{"code:alpha.go", "code:beta.go", "code:new.go"}
	wantChanges := []string{cognition.ImpactChangeUpdate, cognition.ImpactChangeDelete, cognition.ImpactChangeCreate}
	for index, change := range report.Changes {
		if change.ObjectRef != wantRefs[index] || change.Change != wantChanges[index] {
			t.Fatalf("change order is not canonical: %#v", report.Changes)
		}
	}
	if !reflect.DeepEqual(report.Changes[0].ChangedFields, []string{"F", "R"}) {
		t.Fatalf("changed fields were not derived: %#v", report.Changes[0])
	}
	if !reflect.DeepEqual(report.AffectedCognition.WriteSet, []string{"code"}) ||
		!reflect.DeepEqual(report.AffectedCognition.GuardSet, []string{"root", "meta", "code"}) {
		t.Fatalf("Impact sets were not reused: %#v", report.AffectedCognition)
	}
	again, err := CompareCodeTargetIndex(root, "aoci.txt", []byte(target))
	if err != nil || again.DiffSHA256 != report.DiffSHA256 || again.ProjectedCompositeIdentity != report.ProjectedCompositeIdentity {
		t.Fatalf("target Diff is not deterministic: again=%#v err=%v", again, err)
	}
	baseAfter, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil || !reflect.DeepEqual(baseBefore, baseAfter) {
		t.Fatalf("read-only target Diff changed the active Code Volume: %v", err)
	}
}

func TestCompareCodeTargetIndexIgnoresVolumePresentationOnlyChanges(t *testing.T) {
	root := codeTargetDiffFixture(t)
	base, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Replace(string(base), cognition.CodeVolumeMarker+"\n", cognition.CodeVolumeMarker+"\n# target planning note\n", 1)
	target = strings.ReplaceAll(target, "\n", "\r\n")
	report, err := CompareCodeTargetIndex(root, "aoci.txt", []byte(target))
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != machinecontract.CognitionCodeTargetDiffNoChange || len(report.Changes) != 0 ||
		!report.RawBytesChanged || !report.FormalTextOnly || report.NextAction != "none" || report.Summary.Unchanged != 3 {
		t.Fatalf("presentation-only target became a semantic plan: %#v", report)
	}
}

func TestCompareCodeTargetIndexRejectsNonAuthorableAndMisownedTargets(t *testing.T) {
	root := codeTargetDiffFixture(t)
	t.Run("new dotted tag", func(t *testing.T) {
		target := codeTargetVolume(root,
			"alpha.go[CD9S]: F:coordinate alpha behavior | R:- | A:Alpha | S:Keep alpha ordering stable",
			"beta.go[CD9S]: F:retain beta behavior | R:- | A:- | S:-",
			"future.go[C.D.9.S]: F:implement future behavior | R:- | A:- | S:-",
			"unchanged.go[CD9S]: F:retain unchanged behavior | R:- | A:- | S:-",
		)
		if _, err := CompareCodeTargetIndex(root, "aoci.txt", []byte(target)); err == nil || !strings.Contains(err.Error(), "impact_candidate_tag_not_compact") {
			t.Fatalf("non-authorable target tag was accepted: %v", err)
		}
	})
	t.Run("formal asset in Code", func(t *testing.T) {
		target := codeTargetVolume(root,
			"alpha.go[CD9S]: F:coordinate alpha behavior | R:- | A:Alpha | S:Keep alpha ordering stable",
			"aoci.txt[CD9S]: F:incorrectly own the Root | R:- | A:- | S:-",
			"beta.go[CD9S]: F:retain beta behavior | R:- | A:- | S:-",
			"unchanged.go[CD9S]: F:retain unchanged behavior | R:- | A:- | S:-",
		)
		if _, err := CompareCodeTargetIndex(root, "aoci.txt", []byte(target)); err == nil || !strings.Contains(err.Error(), "volume_ownership_conflict") {
			t.Fatalf("misowned target object was accepted: %v", err)
		}
	})
}

func codeTargetDiffFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "aoci.txt", validRoot([]string{"code"}, "en-US"))
	writeFile(t, root, "aoci.meta.txt", validMeta())
	writeFile(t, root, "aoci.code.txt", codeTargetVolume(root,
		"alpha.go[CD9S]: F:coordinate alpha behavior | R:- | A:Alpha | S:Keep alpha ordering stable",
		"beta.go[CD9S]: F:retain beta behavior | R:- | A:- | S:-",
		"unchanged.go[CD9S]: F:retain unchanged behavior | R:- | A:- | S:-",
	))
	return root
}

func codeTargetVolume(root string, entries ...string) string {
	return cognition.CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n" + strings.Join(entries, "\n") + "\n"
}
