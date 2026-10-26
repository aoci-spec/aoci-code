package migrationapply

import "testing"

// The reference functions below are the rescanning implementations the atom
// index replaced, kept as the oracle it must agree with: for every parent and
// every target object, the index returns what they returned.
func referenceSourceEntryAtoms(parent string, ranges []ByteRange) map[string]string {
	result := map[string]string{}
	for _, item := range ranges {
		if item.ParentID == parent {
			result[item.Kind] = item.SHA256
		}
	}
	return result
}

func referenceTargetEntryAtoms(parent string, ranges []TargetRange) map[string]string {
	result := map[string]string{}
	for _, item := range ranges {
		if referenceTargetParentIdentity(item, ranges) == parent {
			result[item.Kind] = item.SHA256
		}
	}
	return result
}

func referenceTargetParentIdentity(item TargetRange, ranges []TargetRange) string {
	if item.Kind == "entry" {
		return ""
	}
	for _, possible := range ranges {
		if possible.Kind == "entry" && possible.Asset == item.Asset && possible.Object == item.Object &&
			item.ByteStart >= possible.ByteStart && item.ByteEnd <= possible.ByteEnd {
			return possible.Identity
		}
	}
	return ""
}

func referenceTargetObjectCount(ranges []TargetRange, asset, object string) int {
	count := 0
	for _, item := range ranges {
		if item.Kind == "entry" && item.Asset == asset && item.Object == object {
			count++
		}
	}
	return count
}

func sameAtoms(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for kind, sha := range want {
		if got[kind] != sha {
			return false
		}
	}
	return true
}

func assertAtomIndexMatchesReference(t *testing.T, sources []ByteRange, targets []TargetRange) {
	t.Helper()
	atoms := newEntryAtomIndex(sources, targets)
	parents := map[string]bool{"": true}
	for _, item := range sources {
		parents[item.Identity], parents[item.ParentID] = true, true
	}
	for _, item := range targets {
		parents[item.Identity] = true
	}
	for parent := range parents {
		if got, want := atoms.sourceAtoms(parent), referenceSourceEntryAtoms(parent, sources); !sameAtoms(got, want) {
			t.Fatalf("source atoms of %q: got %v, want %v", parent, got, want)
		}
		if got, want := atoms.targetAtoms(parent), referenceTargetEntryAtoms(parent, targets); !sameAtoms(got, want) {
			t.Fatalf("target atoms of %q: got %v, want %v", parent, got, want)
		}
	}
	for _, item := range targets {
		if got, want := atoms.targetObjectCount(item.Asset, item.Object), referenceTargetObjectCount(targets, item.Asset, item.Object); got != want {
			t.Fatalf("target object count of %s %q: got %d, want %d", item.Asset, item.Object, got, want)
		}
	}
}

func TestEntryAtomIndexMatchesTheRescanOnAMigrationFixture(t *testing.T) {
	root := migrationScaleFixture(t, 40)
	snapshot, plan, candidate := migrationScaleInputs(t, root)
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(template.TargetRanges) == 0 || len(snapshot.Ranges) == 0 {
		t.Fatalf("fixture produced no ranges: source=%d target=%d", len(snapshot.Ranges), len(template.TargetRanges))
	}
	assertAtomIndexMatchesReference(t, snapshot.Ranges, template.TargetRanges)
	proposals := 0
	for _, task := range template.AuthoringTasks {
		if task.EntryPreservationProposal != nil {
			proposals++
		}
	}
	if proposals != 40 {
		t.Fatalf("every fixture Entry has one target and must carry a preservation proposal: got %d", proposals)
	}
}

func TestEntryAtomIndexKeepsTheRescanRules(t *testing.T) {
	targets := []TargetRange{
		{Identity: "e1", Asset: "code", Object: "code:a.go", Kind: "entry", ByteStart: 0, ByteEnd: 100, SHA256: "E1"},
		{Identity: "e2", Asset: "code", Object: "code:a.go", Kind: "entry", ByteStart: 0, ByteEnd: 200, SHA256: "E2"},
		{Identity: "f1", Asset: "code", Object: "code:a.go", Kind: "entry_f", ByteStart: 10, ByteEnd: 20, SHA256: "F1"},
		{Identity: "f2", Asset: "code", Object: "code:a.go", Kind: "entry_f", ByteStart: 30, ByteEnd: 40, SHA256: "F2"},
		{Identity: "r1", Asset: "code", Object: "code:a.go", Kind: "entry_r", ByteStart: 150, ByteEnd: 160, SHA256: "R1"},
		{Identity: "s1", Asset: "code", Object: "code:b.go", Kind: "entry_s", ByteStart: 10, ByteEnd: 20, SHA256: "S1"},
		{Identity: "t1", Asset: "database", Object: "code:a.go", Kind: "entry_tag", ByteStart: 10, ByteEnd: 20, SHA256: "T1"},
	}
	sources := []ByteRange{
		{Identity: "le1", Kind: "entry", SHA256: "LE1"},
		{Identity: "lf1", Kind: "entry_f", ParentID: "le1", SHA256: "LF1"},
		{Identity: "lf2", Kind: "entry_f", ParentID: "le1", SHA256: "LF2"},
		{Identity: "ls1", Kind: "entry_s", SHA256: "LS1"},
	}
	assertAtomIndexMatchesReference(t, sources, targets)
	atoms := newEntryAtomIndex(sources, targets)
	for _, check := range []struct {
		name      string
		got, want string
	}{
		{"the first containing Entry owns an atom and a repeated kind keeps the later SHA", atoms.targetAtoms("e1")["entry_f"], "F2"},
		{"an atom only the wider Entry contains belongs to it", atoms.targetAtoms("e2")["entry_r"], "R1"},
		{"an atom no Entry of its asset and object contains has no parent", atoms.targetAtoms("")["entry_s"], "S1"},
		{"a source kind repeated under one parent keeps the later SHA", atoms.sourceAtoms("le1")["entry_f"], "LF2"},
	} {
		if check.got != check.want {
			t.Fatalf("%s: got %q, want %q", check.name, check.got, check.want)
		}
	}
	if got := atoms.targetObjectCount("code", "code:a.go"); got != 2 {
		t.Fatalf("two target Entries name code:a.go: got %d", got)
	}
}
