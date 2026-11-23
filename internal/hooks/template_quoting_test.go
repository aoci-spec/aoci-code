package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// A repository path is data in every rendered host file: the hook script
// passes it as one shell word, unexpanded, and the Codex TOML keeps it one
// string (audit G8-01).
func TestRenderedHostFilesKeepHostilePathsLiteral(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook script runs under a POSIX shell")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "expanded")
	root := dir + `/repo "q" $(touch ` + marker + `) ` + "`touch " + marker + "`" + ` it's`
	stub := filepath.Join(dir, "aoci stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if built := NewTplData(root); built.RepoRootShell != quotePOSIXShellArgument(root) || built.BinPathShell != quotePOSIXShellArgument(built.BinPath) {
		t.Fatalf("NewTplData does not supply the quoted shell words: %+v", built)
	}
	data := TplData{BinPath: stub, RepoRoot: root, BinPathShell: quotePOSIXShellArgument(stub), RepoRootShell: quotePOSIXShellArgument(root)}

	script, err := renderLocaleTemplate("pretool", textassets.TemplateClaudePretool, data)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", scriptPath).Output()
	if err != nil {
		t.Fatalf("hook script: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(args) < 2 || args[0] != "--repo" || args[1] != root {
		t.Fatalf("hook passed %q, want --repo %q", args, root)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the hook script expanded a command substitution in the repository path")
	}

	toml, err := renderLocaleTemplate("codex", textassets.TemplateCodexMCPConfig, data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(toml, `"`+root+`"`) {
		t.Fatalf("Codex TOML embeds the path unescaped:\n%s", toml)
	}
	if !strings.Contains(toml, `"--repo", "`+strings.ReplaceAll(root, `"`, `\"`)+`"`) {
		t.Fatalf("Codex TOML does not carry the escaped path:\n%s", toml)
	}
}
