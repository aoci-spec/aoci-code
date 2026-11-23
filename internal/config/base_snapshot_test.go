package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// The CAS snapshot reads a pre-Locale file as the zh-CN project it is and
// refuses the values Load refuses; a team-file write validates them too
// (audit G4-03).
func TestBaseSnapshotKeepsLegacyLocaleAndValidates(t *testing.T) {
	root := t.TempDir()
	write := func(text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(FilePath(root)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(FilePath(root), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("{\"version\":1,\"index_path\":\"aoci.txt\"}\n")
	cfg, _, err := loadBaseSnapshot(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("pre-Locale snapshot: locale=%v err=%v", cfg, err)
	}
	write("{\"version\":2,\"locale\":\"en-US\",\"sensitive_name_rules\":\"v9\"}\n")
	if _, _, err := loadBaseSnapshot(root); err == nil {
		t.Fatal("an invalid sensitive_name_rules passed the snapshot")
	}
	write("{\"version\":2,\"locale\":\"en-US\",\"human_approval_channel\":\"carrier-pigeon\"}\n")
	if _, _, err := loadBaseSnapshot(root); err == nil {
		t.Fatal("an invalid human_approval_channel passed the snapshot")
	}
	bad := DefaultConfig()
	bad.SensitiveNameRules = "v9"
	if err := Save(t.TempDir(), bad); err == nil {
		t.Fatal("Save wrote an invalid sensitive_name_rules")
	}
}
