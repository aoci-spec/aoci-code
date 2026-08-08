package cognition

import "testing"

func TestExpectedOwnerPartitionsVolumeObjects(t *testing.T) {
	tests := map[string]string{
		"aoci.txt":                        OwnerRoot,
		"code:aoci.txt":                   OwnerRoot,
		"aoci.meta.txt":                   OwnerMeta,
		"code:aoci.meta.txt":              OwnerMeta,
		"aoci.code.txt":                   OwnerCode,
		"src/main.go":                     OwnerCode,
		"code:src/main.go":                OwnerCode,
		"aoci.database.txt":               OwnerDatabase,
		"database://primary/public/users": OwnerDatabase,
	}
	for identity, want := range tests {
		if got := ExpectedOwner(identity); got != want {
			t.Fatalf("ExpectedOwner(%q)=%q, want %q", identity, got, want)
		}
	}
	if owner, formal := FormalAssetOwner("aoci.code.txt"); !formal || owner != OwnerCode {
		t.Fatalf("Code Volume asset ownership mismatch: owner=%q formal=%t", owner, formal)
	}
	if _, formal := FormalAssetOwner("src/main.go"); formal {
		t.Fatal("ordinary Code source was classified as a formal asset")
	}
}

func TestOwnershipConflictsRejectsRootAssetInCodeVolume(t *testing.T) {
	set := &Set{LayoutMode: LayoutVolumesV1, Volumes: map[string]*Asset{
		OwnerCode: {Objects: []Object{
			{VolumeID: OwnerCode, CanonicalRef: "code:src/main.go"},
			{VolumeID: OwnerCode, CanonicalRef: "code:aoci.txt"},
		}},
	}}
	conflicts := OwnershipConflicts(set)
	if len(conflicts) != 1 || conflicts[0].Path != "aoci.txt" ||
		conflicts[0].ExpectedOwner != OwnerRoot || conflicts[0].ActualOwner != OwnerCode {
		t.Fatalf("unexpected ownership conflicts: %#v", conflicts)
	}
}
