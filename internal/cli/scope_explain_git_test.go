package cli

import (
	"encoding/json"
	"testing"

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
