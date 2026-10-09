package baseline

import (
	"strings"
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/index"
)

// The Baseline reads an index Entry's path through the same configured
// exclusion lists as the disk snapshot. Before rc20 this file carried its own
// copy of the exclude_dirs match and never learned exclude_root_dirs, so an
// Entry under a root-excluded backup/ was an Orphan in a repository
// initialized by rc20 and a silent skip in an older one (PR #105 review).
func TestIsExcludedRelMatchesEveryConfiguredList(t *testing.T) {
	options := afs.WalkOptions{
		ExcludeDirs:     []string{"backups"},
		ExcludeRootDirs: []string{"backup"},
		ExcludeFiles:    []string{"*.bak"},
	}
	cases := map[string]bool{
		"backup/a.txt":       true,
		"backup/":            true,
		"backup/deep/b.txt":  true,
		"src/backup/b.go":    false,
		"src/backup/":        false,
		"backup":             false,
		"backups/e.txt":      true,
		"src/backups/d.go":   true,
		"src/backups/":       true,
		"notes.bak":          true,
		"old.bak/":           true,
		".aoci/config.json":  true,
		"tools/.aoci/x.json": true,
		"main.go":            false,
		"src/main.go":        false,
	}
	for rel, want := range cases {
		if got := isExcludedRel(rel, options); got != want {
			t.Errorf("isExcludedRel(%q) = %v, want %v", rel, got, want)
		}
		if strings.HasSuffix(rel, "/") || strings.Contains(rel, ".aoci") {
			continue
		}
		if got, want := isExcludedRel(rel, options), afs.PathExcludedByConfig(rel, options); got != want {
			t.Errorf("isExcludedRel(%q) = %v but the Safe Inventory says %v", rel, got, want)
		}
	}
}

func TestDetectSkipsEntriesUnderRootExcludedDirectories(t *testing.T) {
	root, write := buildRepo(t)
	write("main.go", "M1")
	write("backup/a.txt", "stale dump")
	options := afs.WalkOptions{ExcludeRootDirs: []string{"backup"}}

	snapshot, _, err := Snapshot(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, listed := snapshot["backup/a.txt"]; listed {
		t.Fatalf("the snapshot must not list a root-excluded file: %v", snapshot)
	}
	baselineValue := NewBaseline(map[string]Fingerprint{"main.go": snapshot["main.go"]})

	document := buildSingleEntryDocument(t, root, "backup/a.txt")
	result := Detect(root, document, baselineValue, snapshot, options)
	if len(result.Orphan) != 0 {
		t.Fatalf("an Entry under a root-excluded directory is skipped, not an Orphan: %v", result.Orphan)
	}

	nested := buildSingleEntryDocument(t, root, "src/backup/b.go")
	result = Detect(root, nested, baselineValue, snapshot, options)
	if len(result.Orphan) != 1 || result.Orphan[0] != "src/backup/b.go" {
		t.Fatalf("a nested tracked path under that name is source, so its missing file is an Orphan: %v", result.Orphan)
	}
	_ = index.Document{}
}
