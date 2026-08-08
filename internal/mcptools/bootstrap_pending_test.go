package mcptools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapPendingRecoveryBlocksRepositoryCognitionLoad(t *testing.T) {
	for _, operation := range []string{"bootstrap", "migration", "reversal"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".aoci", "transactions"), 0o755); err != nil {
				t.Fatal(err)
			}
			filename := operation + "-deadbeef.json"
			if err := os.WriteFile(filepath.Join(root, ".aoci", "transactions", filename), []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("#AOCI-CLI Complete Index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, fail := loadCognitionCtx(root); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
				t.Fatalf("repository cognition crossed pending %s: %#v", operation, fail)
			}
		})
	}
}
