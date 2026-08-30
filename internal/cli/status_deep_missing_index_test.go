package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --deep on a repository with no index has a curated answer: say the index does
// not exist and name the command that creates one. The Legacy-layout guard was
// then placed ahead of that branch and shadowed it, so the operator received the
// raw filesystem error instead — losing the instruction and leaking an absolute
// host path into the machine-readable message.
func TestStatusDeepWithoutAnIndexKeepsItsActionableAnswer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "config.json"),
		[]byte(`{"version":1,"index_path":"aoci.txt","locale":"en-US"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := executeCLI([]string{"status", "--deep", "--json", "--repo", root}, &out, &errb)
	if code == 0 {
		t.Fatalf("expected a refusal, got exit 0: %s", out.String())
	}

	payload := map[string]any{}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("refusal was not machine-readable JSON: %v\n%s", err, out.String())
	}
	message, _ := payload["message"].(string)
	if strings.Contains(message, root) || strings.Contains(message, "no such file or directory") {
		t.Fatalf("the refusal leaks a raw filesystem error and an absolute host path: %q", message)
	}
	if !strings.Contains(strings.ToLower(message), "init") {
		t.Fatalf("the refusal no longer names the command that resolves it: %q", message)
	}
}
