package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// A configuration that predates its index used to receive the Legacy minimal
// skeleton, and since the Legacy layout is read-only that left nothing to do
// but migrate an empty index. With the Root at aoci.txt, init now starts the
// Volume-first layout a fresh repository gets and leaves the configuration's
// own choices as they were.
func TestInitUnderAnExistingConfigurationStartsVolumeFirst(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("init under an existing configuration failed: %v", err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.Volumes[cognition.ScopeCode] == nil {
		t.Fatalf("init did not start Volume-first: %#v %v", set, err)
	}
	expected, err := renderInitialVolumeAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string][]byte{"aoci.txt": expected.Root, "aoci.meta.txt": expected.Meta, "aoci.code.txt": expected.Code} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("%s is not the Volume-first skeleton: %v", rel, err)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveAutomationMode() != config.AutomationModeLegacy || cfg.HasDeclaredAutomationMode() {
		t.Fatalf("init applied new-project automation to an existing configuration: %s", cfg.EffectiveAutomationMode())
	}
}

// A custom index_path cannot hold a Volumes Root. It used to get a Legacy
// skeleton, a read-only layout whose only next step is a migration of nothing;
// init now refuses, names the fix, and writes no index of either layout.
func TestInitUnderACustomIndexPathRefusesInsteadOfALegacySkeleton(t *testing.T) {
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "docs/aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := runInit(t, root, "--agent=", "--hooks=false")
	if err == nil || !strings.Contains(err.Error(), "docs/aoci.txt") ||
		!strings.Contains(err.Error(), "config set index_path aoci.txt") {
		t.Fatalf("init under a custom index_path must refuse and name the fix: %v", err)
	}
	for _, name := range []string{"docs/aoci.txt", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(statErr) {
			t.Fatalf("the refusal left %s behind: %v", name, statErr)
		}
	}
}
