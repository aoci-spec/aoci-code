package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// curation_exclude names files: Managed Scope matches each entry exactly, so
// a directory or a path outside the repository is refused at config set
// instead of being saved (audit G4-08).
func TestConfigSetCurationExcludeAcceptsFilesOnly(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"docs", "docs/", "gen/a.go,build/", "../outside.go", "/etc/passwd"} {
		if _, stderr, code := runCLI(t, root, "config", "set", "curation_exclude", value); code != ExitConfig {
			t.Fatalf("config set curation_exclude %q: code=%d stderr=%s", value, code, stderr)
		}
	}
	if _, stderr, code := runCLI(t, root, "config", "set", "curation_exclude", "gen/a.go, ./docs/b.md"); code != 0 {
		t.Fatalf("file paths refused: code=%d stderr=%s", code, stderr)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"gen/a.go", "docs/b.md"}; !reflect.DeepEqual(cfg.CurationExclude, want) {
		t.Fatalf("curation_exclude = %v, want %v", cfg.CurationExclude, want)
	}
}
