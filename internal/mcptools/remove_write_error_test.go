package mcptools

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A Volume write that landed but reported an error keeps the removal's
// receipt, so the next call finishes the Baseline; a write that left the
// Volume untouched still drops it (audit G2-02: the receipt was dropped in
// both cases, leaving a landed Volume with an unadvanced Baseline).
func TestVolumeRemoveKeepsItsReceiptWhenTheWriteLanded(t *testing.T) {
	for _, landed := range []bool{true, false} {
		root := buildVolumeRepo(t, true, false)
		if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
			t.Fatal(err)
		}
		previous := writeRemoveIndex
		writeRemoveIndex = func(path string, data []byte, expected string) error {
			if landed {
				if err := previous(path, data, expected); err != nil {
					return err
				}
			}
			return errors.New("reported failure")
		}
		_, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false)
		writeRemoveIndex = previous
		if fail == nil {
			t.Fatalf("landed=%t: the reported failure must surface", landed)
		}
		_, statErr := os.Stat(removeRecoveryPath(root, "code:main.go"))
		if kept := statErr == nil; kept != landed {
			t.Fatalf("landed=%t: receipt kept=%t", landed, kept)
		}
		if landed {
			if _, fail := ApplyRemoveEntry(root, "code:main.go", "agent", true, false); fail != nil {
				t.Fatalf("the kept receipt must let the removal finish: %+v", fail)
			}
			if _, err := os.Stat(removeRecoveryPath(root, "code:main.go")); !os.IsNotExist(err) {
				t.Fatalf("the finished removal must drop its receipt: %v", err)
			}
		}
	}
}
