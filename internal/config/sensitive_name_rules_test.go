package config

import (
	"os"
	"strings"
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// #148: the rule set is team-owned and validated. Legacy defaults carry no
// value, so an old repository keeps the v1 identity; the local layer cannot
// change it; an unknown name fails closed instead of reading as v1.
func TestSensitiveNameRulesAreTeamOwnedAndValidated(t *testing.T) {
	if DefaultConfig().SensitiveNameRules != "" {
		t.Fatalf("legacy defaults gained a rule set: %q", DefaultConfig().SensitiveNameRules)
	}
	root := t.TempDir()
	team := DefaultConfig()
	team.SensitiveNameRules = afs.SensitiveNameRulesV2
	if err := Save(root, team); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalFilePath(root), []byte(`{"sensitive_name_rules":"v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if merged.SensitiveNameRules != afs.SensitiveNameRulesV2 || merged.WalkOptions().SensitiveNameRules != afs.SensitiveNameRulesV2 {
		t.Fatalf("local configuration overrode the team rule set: %q", merged.SensitiveNameRules)
	}
	if err := SaveLocal(root, merged); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sensitive_name_rules") {
		t.Fatalf("SaveLocal retained the team-only rule set: %s", raw)
	}

	if err := os.WriteFile(FilePath(root), []byte(`{"sensitive_name_rules":"v3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, load := range map[string]func(string) (*Config, error){"Load": Load, "LoadBase": LoadBase} {
		if _, err := load(root); err == nil || !strings.Contains(err.Error(), "sensitive_name_rules_invalid") {
			t.Fatalf("%s accepted an unknown rule set: %v", name, err)
		}
	}
}
