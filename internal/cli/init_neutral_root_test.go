package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func TestInitCreatesNeutralCodeRootAndPreservesExistingCode(t *testing.T) {
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	codePath := filepath.Join(root, "aoci.code.txt")
	raw, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "#AOCI-CODE-VOLUME: 1\n===project/.code/===\n" {
		t.Fatalf("fresh Code skeleton must have a machine-independent root: %s", raw)
	}
	line := "a.go[EG7T]: F:Provides a source fixture | R:- | A:- | S:-"
	written, err := index.InsertEntry(string(raw), "src/a.go", line, root)
	if err != nil || strings.Contains(written, filepath.ToSlash(root)) {
		t.Fatalf("first Entry must not record the machine path: %v\n%s", err, written)
	}
	for _, original := range []string{written, "#AOCI-CODE-VOLUME: 1\n===/old/machine/repo/===\n" + line + "\n"} {
		if err := os.WriteFile(codePath, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(codePath)
		if err != nil || string(got) != original {
			t.Fatalf("init must preserve an established Code Volume: %v\n%s", err, got)
		}
	}
}

func TestInitResumesOlderEmptyCodeSkeleton(t *testing.T) {
	root := t.TempDir()
	legacy := []byte("#AOCI-CODE-VOLUME: 1\n")
	codePath := filepath.Join(root, "aoci.code.txt")
	if err := os.WriteFile(codePath, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	assets, err := renderInitialVolumeAssets(root)
	if err != nil {
		t.Fatal(err)
	}
	postimages, err := initializeVolumeFirst(root, "aoci.txt", assets)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(codePath)
	if err != nil || string(got) != string(legacy) {
		t.Fatalf("older partial init must keep its exact Code postimage: %v\n%s", err, got)
	}
	if postimages["aoci.code.txt"] != baseline.HashBytes("aoci.code.txt", legacy) {
		t.Fatal("Baseline must bind the preserved older Code skeleton")
	}
}
