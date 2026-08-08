package dbevidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotStoreAndExplicitBaselineAcceptance(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	loadedManifest, loaded, exists, err := LoadSnapshot(root, manifest.SourceID)
	if err != nil || !exists || loadedManifest.SourceID != manifest.SourceID || loaded.SourceSnapshotSHA256 != snapshot.SourceSnapshotSHA256 {
		t.Fatalf("snapshot round trip failed: exists=%t err=%v manifest=%+v snapshot=%+v", exists, err, loadedManifest, loaded)
	}
	if _, baselineExists, err := LoadBaseline(root); err != nil || baselineExists {
		t.Fatalf("snapshot silently created Baseline: exists=%t err=%v", baselineExists, err)
	}
	if err := AcceptSnapshot(root, loaded, "wrong"); err == nil {
		t.Fatal("Baseline accepted without the exact snapshot binding")
	}
	if _, err := os.Stat(BaselinePath(root)); !os.IsNotExist(err) {
		t.Fatalf("failed acceptance created a formal Baseline: %v", err)
	}
	if err := AcceptSnapshot(root, loaded, loaded.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	baseline, baselineExists, err := LoadBaseline(root)
	if err != nil || !baselineExists || len(baseline.Sources) != 1 || baseline.Sources[0].SourceSnapshotSHA256 != loaded.SourceSnapshotSHA256 {
		t.Fatalf("accepted Baseline mismatch: exists=%t err=%v baseline=%+v", baselineExists, err, baseline)
	}
}

func TestSnapshotStoreRejectsTampering(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(RuntimeEvidenceRoot(root), filepath.FromSlash(snapshot.Tables[0].EvidenceRef))
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadSnapshot(root, manifest.SourceID); err == nil {
		t.Fatal("tampered evidence was accepted")
	}
}

func TestSnapshotStoreRejectsUnsafeEvidenceReferenceBeforeRead(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Tables[0].EvidenceRef = "../../outside.json"
	identity, err := snapshotIdentityBytes(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SourceSnapshotSHA256 = sha256Hex(identity)
	if err := WriteSnapshot(root, manifest, snapshot, files); err == nil {
		t.Fatal("unsafe evidence reference was accepted")
	}
}

func TestSnapshotStoreRejectsEvidenceSymlinkEscape(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, evidencePath, externalDir string)
	}{
		{"table_file", func(t *testing.T, evidencePath, externalDir string) {
			outside := filepath.Join(externalDir, filepath.Base(evidencePath))
			data, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outside, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(evidencePath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, evidencePath); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
		{"tables_directory", func(t *testing.T, evidencePath, externalDir string) {
			outside := filepath.Join(externalDir, filepath.Base(evidencePath))
			data, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outside, data, 0o644); err != nil {
				t.Fatal(err)
			}
			tablesDir := filepath.Dir(evidencePath)
			if err := os.RemoveAll(tablesDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(externalDir, tablesDir); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifest := fixtureManifest()
			snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteSnapshot(root, manifest, snapshot, files); err != nil {
				t.Fatal(err)
			}
			record := snapshot.Tables[0]
			evidencePath := filepath.Join(RuntimeEvidenceRoot(root), filepath.FromSlash(record.EvidenceRef))
			test.mutate(t, evidencePath, t.TempDir())
			if _, _, _, err := LoadSnapshot(root, manifest.SourceID); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("symlinked Evidence escaped the runtime root: %v", err)
			}
			if _, err := LoadTableEvidence(root, record); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("direct table load accepted symlinked Evidence: %v", err)
			}
		})
	}
}

func TestSnapshotWriteRejectsEvidenceDirectorySymlinkEscape(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(RuntimeEvidenceRoot(root), manifest.SourceID)
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(sourceDir, "tables")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := WriteSnapshot(root, manifest, snapshot, files); err == nil || !strings.Contains(err.Error(), "unsafe database evidence directory") {
		t.Fatalf("snapshot followed a symlinked Evidence directory: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected snapshot wrote outside the Evidence root: %v", entries)
	}
}

func TestBaselineRejectsNonCanonicalInventory(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	snapshot, files, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users"), fixtureTable("accounts")})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	path := BaselinePath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var baseline Baseline
	if err := decodeStrict(data, &baseline); err != nil {
		t.Fatal(err)
	}
	baseline.Sources[0].Tables[0], baseline.Sources[0].Tables[1] = baseline.Sources[0].Tables[1], baseline.Sources[0].Tables[0]
	data, err = CanonicalJSON(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadBaseline(root); err == nil {
		t.Fatal("non-canonical Baseline inventory was accepted")
	}
}

func TestBaselineAcceptanceRejectsSnapshotThatIsNoLongerCurrent(t *testing.T) {
	root := t.TempDir()
	manifest := fixtureManifest()
	oldSnapshot, oldFiles, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users")})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(root, manifest, oldSnapshot, oldFiles); err != nil {
		t.Fatal(err)
	}
	newSnapshot, newFiles, err := BuildSnapshot(manifest, []TableEvidence{fixtureTable("users"), fixtureTable("orders")})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(root, manifest, newSnapshot, newFiles); err != nil {
		t.Fatal(err)
	}
	if err := AcceptSnapshot(root, oldSnapshot, oldSnapshot.SourceSnapshotSHA256); err == nil {
		t.Fatal("a stale saved snapshot was accepted")
	}
	if _, exists, err := LoadBaseline(root); err != nil || exists {
		t.Fatalf("stale acceptance wrote a Baseline: exists=%t err=%v", exists, err)
	}
}
