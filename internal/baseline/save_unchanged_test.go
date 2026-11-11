package baseline

import (
	"os"
	"path/filepath"
	"testing"
)

// A save that changes nothing must not touch the file. updated_at used to be
// re-stamped on every save, so two branches with the same managed set
// conflicted on that single line when merged, and a scan that found nothing
// left a dirty tree (#173). A save that does change a fingerprint still
// stamps the time and keeps the previous bytes as the backup.
func TestSaveLeavesAnUnchangedBaselineByteIdentical(t *testing.T) {
	root := t.TempDir()
	state := &Baseline{Version: 1, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
		Files: map[string]Fingerprint{"a.go": {SHA256: "aa", Size: 1}}}
	if err := Save(root, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".aoci", "baseline.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Make the stored stamp one no later save could reproduce by accident.
	loaded, exists, err := Load(root)
	if err != nil || !exists {
		t.Fatalf("load: exists=%t err=%v", exists, err)
	}
	loaded.UpdatedAt = "2020-02-02T02:02:02Z"
	pinned, err := MarshalExact(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pinned, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = first

	again, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	again.UpdatedAt = "2031-01-01T00:00:00Z" // whatever the caller holds is irrelevant
	if err := Save(root, again); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(pinned) {
		t.Fatalf("an unchanged save rewrote the Baseline:\nbefore %s\nafter  %s", pinned, after)
	}

	again.Files["a.go"] = Fingerprint{SHA256: "bb", Size: 2}
	if err := Save(root, again); err != nil {
		t.Fatal(err)
	}
	changed, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Files["a.go"].SHA256 != "bb" || changed.UpdatedAt == "2020-02-02T02:02:02Z" {
		t.Fatalf("a changed save must write and stamp: %+v", changed)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil || string(backup) != string(pinned) {
		t.Fatalf("the backup must hold the previous bytes: err=%v", err)
	}
}
