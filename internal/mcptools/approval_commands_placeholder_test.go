package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Handed approval commands run as printed: --actor has a default, and a
// "<id>" argument is a shell redirection, not a placeholder (audit G8-11).
func TestHandedApprovalCommandsCarryNoPlaceholder(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"<id>"`) {
			t.Errorf("%s hands out a command with a <id> placeholder", file)
		}
	}
}
