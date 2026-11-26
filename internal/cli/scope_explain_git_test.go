package cli

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func TestScopeExplainGitExcludedDescendantAndFuturePath(t *testing.T) {
	root := testkit.GitRepo(t)
	mustCLI(t, root, "--json", "init")
	testkit.WriteFile(t, root, ".gitignore", "dist/\nfuture/cache*.js\n!future/cache-keep.js\n")
	testkit.WriteFile(t, root, "packages/web/dist/app.js", "fixture\n")
	explain := func(path string) managedscope.PathEvaluation {
		t.Helper()
		raw := mustCLI(t, root, "--json", "scope", "explain", path)
		var item managedscope.PathEvaluation
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	folder := explain("packages/web/dist/")
	if folder.Role != machinecontract.ScopeRoleExclude || folder.Reason != "exclude_root_dirs:dist:configured" {
		t.Fatalf("fixture did not collapse the ignored directory: %+v", folder)
	}
	t.Run("collapsed_descendant", func(t *testing.T) {
		const path = "packages/web/dist/app.js"
		item := explain(path)
		folder.Path = path
		if item != folder {
			t.Fatalf("descendant must inherit the folded exclusion: %+v", item)
		}
	})
	t.Run("future_ignored", func(t *testing.T) {
		const path = "future/cache-future.js"
		item := explain(path)
		if item.Role != machinecontract.ScopeRoleExclude || item.GitStatus != "ignored" || item.ReadsContent ||
			item.EntersWholeIndex || item.EntersObserveFingerprint {
			t.Fatalf("future ignored path must be excluded: %+v", item)
		}
		testkit.WriteFile(t, root, path, "fixture\n")
		if existing := explain(path); existing != item {
			t.Fatalf("explanation changed after the ignored file appeared: %+v vs %+v", existing, item)
		}
	})
	t.Run("future_negated", func(t *testing.T) {
		item := explain("future/cache-keep.js")
		if item.Role != machinecontract.ScopeRoleIndex || item.GitStatus != "future_or_absent" {
			t.Fatalf("Git negation was ignored: %+v", item)
		}
	})
}

func TestScopeExplainFutureGitPathsMatchScan(t *testing.T) {
	root := testkit.GitRepo(t)
	mustCLI(t, root, "--json", "init")
	testkit.WriteFile(t, root, ".gitignore", "packages/web/dist/*.js\n!packages/web/dist/keep.js\n*.data.js\n")
	mustCLI(t, root, "scope", "rule", "add", "dist-index", "--action", "index", "--pattern",
		"packages/web/dist/*.js", "--reason", "fixture explicit index rule")

	const dist = "packages/web/dist/new.js"
	const keep = "packages/web/dist/keep.js"
	const literal = ":(literal)future.data.js"
	paths := []string{dist, keep}
	if runtime.GOOS != "windows" {
		paths = append(paths, literal)
	}
	explain := func(path string) managedscope.PathEvaluation {
		t.Helper()
		var item managedscope.PathEvaluation
		if err := json.Unmarshal([]byte(mustCLI(t, root, "--json", "scope", "explain", path)), &item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	check := func(path string, item managedscope.PathEvaluation) {
		t.Helper()
		switch path {
		case dist:
			if item.Role != machinecontract.ScopeRoleExclude || item.ReadsContent || item.Reason != "exclude_root_dirs:dist:configured" {
				t.Errorf("nested ignored artifact did not receive its scan exclusion: %+v", item)
			}
		case literal:
			if item.Role != machinecontract.ScopeRoleExclude || item.ReadsContent || item.GitStatus != "ignored" {
				t.Errorf("literal filename did not use Git ignore: %+v", item)
			}
		case keep:
			if item.Role != machinecontract.ScopeRoleIndex {
				t.Errorf("Git negation did not keep the file: %+v", item)
			}
		}
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			check(path, explain(path))
			testkit.WriteFile(t, root, path, "fixture\n")
			check(path, explain(path))
		})
	}
	mustCLI(t, root, "--json", "scan")
	bl, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load scan Baseline: exists=%t err=%v", exists, err)
	}
	for _, path := range paths {
		if _, inBaseline := bl.Files[path]; inBaseline != (path == keep) {
			t.Errorf("unexpected Baseline membership for %s: %t", path, inBaseline)
		}
	}
}
