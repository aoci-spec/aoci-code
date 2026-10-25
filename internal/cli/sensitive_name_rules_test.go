package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// #148: init writes the v2 rule set only for a repository it creates. An
// existing team configuration without the key keeps it absent, which is the
// rc22 identity, so the binary upgrade alone changes no scope.
func TestInitLeavesAnExistingRepositorysSensitiveNameRulesAbsent(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SensitiveNameRules != "" {
		t.Fatalf("init rewrote an existing repository's rule set: %q", cfg.SensitiveNameRules)
	}
	raw, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sensitive_name_rules") {
		t.Fatalf("the key must stay absent in an old team configuration: %s", raw)
	}
}

// config set accepts a rule-set name only: a third value is refused with the
// value in the message, and the stored choice is untouched.
func TestConfigSetRefusesAnUnknownSensitiveNameRuleSet(t *testing.T) {
	root := t.TempDir()
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "config", "set", "sensitive_name_rules", "v3"}, &stdout, &stderr)
	if code != ExitConfig {
		t.Fatalf("config set sensitive_name_rules v3: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if blob := stdout.String() + stderr.String(); !strings.Contains(blob, "v3") {
		t.Fatalf("the refusal must name the value: %s", blob)
	}
	stored, _ := runConfigJSON(t, root, "get", "sensitive_name_rules")
	assertJSONValue(t, "config get sensitive_name_rules after a refused set", stored, "v1")
}

// doctor lists the Baseline index paths the v2 rule set would exclude while
// the repository is still on v1, reads nothing else, and prints nothing once
// v2 is active. Observe paths hold no cognition and are not listed; an
// omitted role is the legacy index role.
func TestDoctorListsTheSensitiveNameUpgradeCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	zeros := strings.Repeat("0", 64)
	if err := baseline.Save(root, baseline.NewBaseline(map[string]baseline.Fingerprint{
		"conf/prod.env":   {Role: "index", SHA256: zeros},
		"src/main.ts":     {Role: "index", SHA256: zeros},
		"notes/token.txt": {Role: "observe", SHA256: zeros},
		"legacy/passwd":   {SHA256: zeros},
	})); err != nil {
		t.Fatal(err)
	}
	label := cliMessage("doctor.label.sensitive_name_rules")

	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	out, _ := runDoctor(t, root)
	if !strings.Contains(out, label) || !strings.Contains(out, "conf/prod.env") || !strings.Contains(out, "legacy/passwd") {
		t.Fatalf("doctor must list the index paths v2 would exclude:\n%s", out)
	}
	if strings.Contains(out, "src/main.ts") || strings.Contains(out, "notes/token.txt") {
		t.Fatalf("doctor listed a path v2 keeps or an observe path:\n%s", out)
	}

	cfg.SensitiveNameRules = afs.SensitiveNameRulesV2
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	out, _ = runDoctor(t, root)
	if strings.Contains(out, label) {
		t.Fatalf("doctor must stay silent once v2 is active:\n%s", out)
	}
}
