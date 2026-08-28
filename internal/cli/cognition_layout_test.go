package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/spf13/cobra"
)

func TestVolumeCLIWriteGuardPrecedesLayoutCompleteness(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(cognition.RootManifestMarker+"\n#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireLegacyWriteLayout(root, cfg, false); err == nil || !strings.Contains(err.Error(), "Volumes v1") {
		t.Fatalf("damaged Volume layout was allowed into a write path: %v", err)
	}
	before := cfg.Locale
	if err := prepareLocaleChange(root, cfg, "zh-CN"); err == nil || !strings.Contains(err.Error(), "Volumes v1") {
		t.Fatalf("locale governance did not fail closed: %v", err)
	}
	if cfg.Locale != before || cfg.LocaleMigration != nil {
		t.Fatal("failed Volume locale change mutated configuration state")
	}
}

func TestLegacyIncompleteIndexRemainsInitializable(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("# incomplete legacy skeleton\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireLegacyWriteLayout(root, cfg, false); err != nil {
		t.Fatalf("Legacy initialization compatibility regressed: %v", err)
	}
}

func TestVolumeCommonIndexCommandsFailWithStableReadOnlyResult(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(rootText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.meta.txt"), []byte(metaText), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := loadIndexForCLI(&cobra.Command{}, root, cfg)
	if err == nil || !strings.Contains(err.Error(), "Volumes v1") {
		t.Fatalf("common CLI write/read path did not fail closed: %v", err)
	}
}

func TestStatusFailsClosedForInvalidDeclaredVolume(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(rootText), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "status"}, &stdout, &stderr); code != ExitInvalid {
		t.Fatalf("invalid Volume status did not fail closed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if combined := stdout.String() + stderr.String(); !strings.Contains(combined, "declared_volume_missing") {
		t.Fatalf("status did not expose the structural finding: %s", combined)
	}
}

// status --deep is documented Legacy-only. It used to run against a Volumes
// repository and report a drift set the Volumes governance path owns, so the
// public documents and the binary disagreed about the same command.
func TestStatusDeepIsRefusedOnVolumesLayout(t *testing.T) {
	root := t.TempDir()
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(rootText), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := requireLegacyLayout(root, cfg, false); err == nil {
		t.Fatal("a read-only Legacy-only path must still diagnose a Volumes layout")
	}

	// The neutral name and the write-path name are one precondition, not two:
	// a second copy is how a guard silently stops covering one of its callers.
	writeErr := requireLegacyWriteLayout(root, cfg, false)
	readErr := requireLegacyLayout(root, cfg, false)
	if writeErr == nil || readErr == nil || writeErr.Error() != readErr.Error() {
		t.Fatalf("write and read guards must resolve one verdict: %v vs %v", writeErr, readErr)
	}
}
