// A new production repository is born with the starter observe rules for
// vendored static assets, they decide roles at the first scan, and a
// repository that already has a config never receives them.
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// The RuoYi shape: third-party bundles, fonts and a source map under static/
// and assets/, with the project's own code beside them. src/lib is ordinary
// code in umami and hoppscotch and must stay indexed: only lib under static/
// is a starter pattern.
var starterRuleFixtureRoles = map[string]string{
	"static/ajax/libs/jquery.min.js": machinecontract.ScopeRoleObserve,
	"static/js/plugins/x.js":         machinecontract.ScopeRoleObserve,
	"assets/fonts/a.woff2":           machinecontract.ScopeRoleObserve,
	"static/js/app.js.map":           machinecontract.ScopeRoleObserve,
	"static/ruoyi/js/ry-ui.js":       machinecontract.ScopeRoleIndex,
	"src/lib/util.ts":                machinecontract.ScopeRoleIndex,
	"src/app.go":                     machinecontract.ScopeRoleIndex,
}

func starterRuleFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		"static/ajax/libs/jquery.min.js": "/*! jQuery */!function(e){}();\n",
		"static/js/plugins/x.js":         "(function(){})();\n",
		"assets/fonts/a.woff2":           "not a real font\n",
		"static/js/app.js.map":           "{\"version\":3}\n",
		"static/ruoyi/js/ry-ui.js":       "window.ry = {};\n",
		"src/lib/util.ts":                "export const util = 1;\n",
		"src/app.go":                     "package src\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "fixture"},
	} {
		command := exec.Command("git", args...)
		command.Dir = root
		if out, err := command.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	return root
}

func starterRuleIDs(rules []managedscope.Rule) []string {
	ids := []string{}
	for _, rule := range rules {
		if strings.HasPrefix(rule.RuleID, managedscope.StarterRuleIDPrefix) {
			ids = append(ids, rule.RuleID)
		}
	}
	return ids
}

func scanBaseline(t *testing.T, root string) *baseline.Baseline {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &out, &errOut); code != ExitOK {
		t.Fatalf("scan failed: code=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	value, found, err := baseline.Load(root)
	if err != nil || !found {
		t.Fatalf("scan left no Baseline: found=%v err=%v", found, err)
	}
	return value
}

func TestInitPersistsStarterRulesAndScanObservesVendoredStatic(t *testing.T) {
	root := starterRuleFixture(t)
	if _, err := runInit(t, root, "--agent=claude", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManagedScope == nil || len(starterRuleIDs(cfg.ManagedScope.Rules)) != 7 {
		t.Fatalf("init did not persist the seven starter rules: %+v", cfg.ManagedScope)
	}
	value := scanBaseline(t, root)
	for rel, role := range starterRuleFixtureRoles {
		fingerprint, ok := value.Files[rel]
		if !ok || fingerprint.Role != role {
			t.Fatalf("%s: Baseline role=%q found=%v, want %q", rel, fingerprint.Role, ok, role)
		}
	}
}

func TestInitLeavesAnExistingConfigWithoutStarterRules(t *testing.T) {
	for name, prepare := range map[string]func(*config.Config){
		"no managed_scope block": func(*config.Config) {},
		"production policy written before the starter rules existed": func(cfg *config.Config) {
			policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
			if err != nil {
				t.Fatal(err)
			}
			cfg.ManagedScope = &policy
		},
	} {
		prepare := prepare
		t.Run(name, func(t *testing.T) {
			root := starterRuleFixture(t)
			cfg := config.DefaultConfig()
			prepare(cfg)
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			hadBlock := cfg.ManagedScope != nil
			before, err := managedscope.Identity(cfg.EffectiveManagedScope())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runInit(t, root, "--agent=claude", "--hooks=false"); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if (loaded.ManagedScope != nil) != hadBlock {
				t.Fatalf("init changed the managed_scope block of an existing config: %+v", loaded.ManagedScope)
			}
			if ids := starterRuleIDs(loaded.EffectiveManagedScope().Rules); len(ids) != 0 {
				t.Fatalf("init added starter rules to an existing config: %v", ids)
			}
			after, err := managedscope.Identity(loaded.EffectiveManagedScope())
			if err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatalf("init moved the policy identity of an existing repository: %s -> %s", before, after)
			}
			// The old repository keeps the roles it always had: the bundle stays
			// indexed. A Baseline written under the legacy policy omits the role,
			// and baseline.Fingerprint documents an omitted role as legacy index.
			value := scanBaseline(t, root)
			fingerprint, ok := value.Files["static/ajax/libs/jquery.min.js"]
			if !ok || (fingerprint.Role != "" && fingerprint.Role != machinecontract.ScopeRoleIndex) {
				t.Fatalf("an existing repository's roles moved: found=%v %+v", ok, fingerprint)
			}
		})
	}
}
