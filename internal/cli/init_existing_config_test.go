package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

// A custom index_path cannot hold a Volumes Root, so that configuration keeps
// the Legacy skeleton and its migration route.
func TestInitUnderACustomIndexPathKeepsTheLegacySkeleton(t *testing.T) {
	root := t.TempDir()
	cfg := legacyTestConfig()
	cfg.IndexPath = "docs/aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatalf("init under a custom index_path failed: %v", err)
	}
	set, err := cognition.Load(root, "docs/aoci.txt")
	if err != nil || set.LayoutMode != cognition.LayoutLegacyMonolithic {
		t.Fatalf("a custom index_path did not keep the Legacy skeleton: %#v %v", set, err)
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.meta.txt")); !os.IsNotExist(err) {
		t.Fatalf("a custom index_path also received Volumes: %v", err)
	}
}
