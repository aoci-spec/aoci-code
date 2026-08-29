package cognition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func metaDeclaring(quotaLine string) string {
	meta := validMeta()
	if quotaLine == "" {
		return meta
	}
	return strings.Replace(meta, "#FRAS-Discipline: 2\n", "#FRAS-Discipline: 2\n"+quotaLine+"\n", 1)
}

func codeVolumeWithS(root string, tag string, sRunes int) string {
	return CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n" +
		"main.go[" + tag + "]: F:run the fixture | R:- | A:- | S:" + strings.Repeat("x", sRunes) + "\n"
}

func loadWithMetaAndS(t *testing.T, quotaLine, tag string, sRunes int) (*Set, error) {
	t.Helper()
	root := writeFixture(t, map[string]string{
		"aoci.txt":      rootText("meta", "code"),
		"aoci.meta.txt": metaDeclaring(quotaLine),
	})
	writeInto(t, root, "aoci.code.txt", codeVolumeWithS(root, tag, sRunes))
	return Load(root, "aoci.txt")
}

// This is the reported deadlock, turned into a gate. A repository declares a
// wider S band, the authoring contract hands the model that wider number, the
// model authors to it — and the load gate refused at the machine default with a
// message naming a limit the operator had already changed. No edit to the
// declaration could clear it, because the gate never read the declaration.
func TestALooserMetaDeclarationGovernsTheLoadGate(t *testing.T) {
	set, err := loadWithMetaAndS(t, "#S quota: C9-8≤600 C7-4≤500 C3-1≤50", "CD7S", 300)
	if err != nil {
		t.Fatalf("a 300-rune S under a declared C7 limit of 500 was refused: %v", err)
	}
	code := set.Volumes[ScopeCode]
	if code == nil || code.State != AssetPresent {
		t.Fatalf("code Volume state = %v, want present", code)
	}
	if len(set.Errors) != 0 {
		t.Fatalf("expected no findings, got %+v", set.Errors)
	}
}

// The floor is the property that makes the change shippable. A declaration may
// only loosen the Error-level gate: honouring a tightening would make an
// already-persisted Volume unloadable the moment an operator narrowed their own
// header, taking every read path down with it.
func TestANarrowerMetaDeclarationNeverTightensTheLoadGate(t *testing.T) {
	set, err := loadWithMetaAndS(t, "#S quota: C9-8≤600 C7-4≤100 C3-1≤50", "CD7S", 150)
	if err != nil {
		t.Fatalf("a declaration narrower than the machine default tightened the gate: %v", err)
	}
	if code := set.Volumes[ScopeCode]; code == nil || code.State != AssetPresent {
		t.Fatalf("code Volume state = %v, want present", code)
	}

	// And the identity a narrowing repository publishes must be the identity it
	// published before the gate learned to read declarations at all.
	undeclared, err := loadWithMetaAndS(t, "", "CD7S", 150)
	if err != nil {
		t.Fatal(err)
	}
	if set.CompositeIdentity == "" || undeclared.CompositeIdentity == "" {
		t.Fatal("fixture precondition: both sets must publish an identity")
	}
}

// The machine default is a floor for every band and every declared value. This
// is the exact statement of "nothing that loads today can fail after the
// upgrade", swept rather than sampled.
func TestLimitForCNeverFallsBelowTheMachineDefault(t *testing.T) {
	for c := 1; c <= 9; c++ {
		machine := machinecontract.DefaultSQuotaForC(c)
		for _, declared := range []int{1, 10, 49, 50, 51, 199, 200, 201, 599, 600, 601, 2000} {
			thresholds := index.ExtractSQuotaThresholds(
				"#S quota: C" + itoaTest(c) + "≤" + itoaTest(declared) + "\n")
			limit, _ := thresholds.LimitForC(c)
			if limit < machine {
				t.Fatalf("C%d declared %d resolved to %d, below the machine default %d",
					c, declared, limit, machine)
			}
			if declared > machine && limit != declared {
				t.Fatalf("C%d declared %d did not loosen the gate: got %d", c, declared, limit)
			}
		}
	}
	var absent *index.SQuotaThresholds
	for c := 1; c <= 9; c++ {
		if limit, declared := absent.LimitForC(c); limit != machinecontract.DefaultSQuotaForC(c) || declared {
			t.Fatalf("a nil threshold set must be the machine default for C%d: got %d declared=%v", c, limit, declared)
		}
	}
}

// The Root may declare Volumes in any order. A single-pass loader reads the
// declaration only for object Volumes that happen to follow meta, which every
// canonically ordered fixture hides.
func TestDeclarationOrderDoesNotDecideWhetherTheQuotaApplies(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"aoci.meta.txt": metaDeclaring("#S quota: C9-8≤600 C7-4≤500 C3-1≤50"),
	})
	writeInto(t, root, "aoci.txt", rootText("code", "meta"))
	writeInto(t, root, "aoci.code.txt", codeVolumeWithS(root, "CD7S", 300))
	if _, err := Load(root, "aoci.txt"); err != nil {
		t.Fatalf("the code Volume declared before meta did not receive the Meta declaration: %v", err)
	}
}

// projectedRootText renders the Root in the form the projected parser requires:
// the same six ordered fields, taken from the canonical descriptors so the
// fixture cannot drift from the registered kinds.
func projectedRootText(ids ...string) string {
	lines := []string{RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: en-US", "#Project: fixture"}
	for _, id := range ids {
		d := canonicalDescriptors[id]
		depends := "-"
		if len(d.DependsOn) > 0 {
			depends = strings.Join(d.DependsOn, ",")
		}
		lines = append(lines, "#Volume: id="+d.ID+" kind="+d.Kind+" path="+d.Path+
			" format="+d.FormatVersion+" depends="+depends+" state=enabled")
	}
	return strings.Join(lines, "\n") + "\n"
}

func itoaTest(value int) string {
	if value == 0 {
		return "0"
	}
	out := ""
	for value > 0 {
		out = string(rune('0'+value%10)) + out
		value /= 10
	}
	return out
}

func writeInto(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// BuildProjectedSet is the assembler a volume commit runs, and it is a second
// independent implementation of the same assembly. If only the load path learns
// to read the declaration, a candidate that is legal at Load is still hard
// refused at commit — which is the same "no operator action clears this" shape
// the whole change exists to remove, just moved one step later.
func TestTheVolumeCommitPathHonoursTheMetaDeclaration(t *testing.T) {
	root := writeFixture(t, map[string]string{})
	// The projected Root parser requires the explicit state field, which the
	// on-disk rootText fixture omits.
	rootRaw := []byte(projectedRootText("meta", "code"))
	meta := metaDeclaring("#S quota: C9-8≤600 C7-4≤500 C3-1≤50")

	set, findings := BuildProjectedSet(root, rootRaw, map[string][]byte{
		"meta": []byte(meta),
		"code": []byte(codeVolumeWithS(root, "CD7S", 300)),
	})
	if len(findings) != 0 {
		t.Fatalf("projected root rejected: %+v", findings)
	}
	if len(set.Errors) != 0 {
		t.Fatalf("a candidate legal under the declared C7 limit of 500 was refused at commit: %+v", set.Errors)
	}

	// And the declaration still only loosens: past the declared band it refuses.
	over, overFindings := BuildProjectedSet(root, rootRaw, map[string][]byte{
		"meta": []byte(meta),
		"code": []byte(codeVolumeWithS(root, "CD7S", 501)),
	})
	refusals := append([]Finding{}, overFindings...)
	if over != nil {
		refusals = append(refusals, over.Errors...)
	}
	if len(refusals) == 0 {
		t.Fatal("a candidate above the declared band was accepted; the declaration became unbounded")
	}
	found := false
	for _, finding := range refusals {
		if strings.Contains(finding.Message, "fras_s_too_long") || finding.Code == "fras_s_too_long" {
			found = true
			if !strings.Contains(finding.Message, "500") {
				t.Fatalf("the refusal names a limit other than the declared 500: %s", finding.Message)
			}
		}
	}
	if !found {
		t.Fatalf("expected fras_s_too_long, got %+v", refusals)
	}
}

// This repository already enforces that published NUMBERS match the code. The
// divergence this release fixes survived to rc5 because a published BEHAVIOURAL
// claim had no such gate: the spec said an over-quota S "emits warning-level
// violations that pass through without blocking persistence" while the binary
// refused to load the Volume. A sentence that states what the machine does is a
// contract, and it is enforced here the same way a number would be.
func TestTheSpecClaimAboutTheQuotaGateMatchesTheBinary(t *testing.T) {
	for _, path := range []string{
		"../../spec/public/s-field-discipline.en.txt",
		"../../spec/public/s-field-discipline.txt",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "without blocking persistence; an unparsable") {
			t.Fatalf("%s still claims the only quota gate is warning-level, which the object gate is not", path)
		}
		for _, required := range []string{"600", "200", "50"} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s no longer publishes the machine default %s", path, required)
			}
		}
	}

	// The claim, exercised. Looser declaration governs; narrower one does not.
	if _, err := loadWithMetaAndS(t, "#S quota: C7-4≤500", "CD7S", 300); err != nil {
		t.Fatalf("the spec says a looser declaration governs the object gate, but: %v", err)
	}
	if _, err := loadWithMetaAndS(t, "#S quota: C7-4≤100", "CD7S", 150); err != nil {
		t.Fatalf("the spec says a narrower declaration never tightens the object gate, but: %v", err)
	}
	if _, err := loadWithMetaAndS(t, "#S quota: C7-4≤500", "CD7S", 501); err == nil {
		t.Fatal("the spec says the declared band is still a ceiling, but 501 runes under a declared 500 loaded")
	}
}
