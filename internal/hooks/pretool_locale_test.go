// The hook kernel speaks the repository's locale: an en-US project must not be
// handed Chinese prose before every write. The package TestMain pins zh-CN for
// the legacy assertions, so this test pins en-US for its own duration through
// testkit.PinLocale (restored at cleanup) and must never run in parallel.
package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/aoci-spec/aoci-code/textassets"
)

const (
	pretoolEntryLineA = "a.go[X.Y.5.T]: F:甲 | R:- | A:- | S:约束"
	pretoolEntryLineB = "b.go[X.Y.5.T]: F:乙 | R:- | A:- | S:乙约束"

	pretoolEnglishEntryHeader = "aoci: the index Entry for this file (follow its constraints before changing it):\n"
	pretoolEnglishStaleHeader = "aoci ⚠ STALE: this file changed after its Entry was written; the Entry below may be stale, check the source first:\n"
	pretoolEnglishUnbaselined = "aoci: this file is absent from the Baseline (drift is unknown); its Entry follows:\n"
	pretoolEnglishStrictBlock = "\naoci strict: the Entry is stale, so this write is blocked — check the source and update the Entry with aoci_update_entry, then retry (aoci scan cannot move a Managed Scope Baseline forward)."
	pretoolEnglishNotIndexed  = "aoci: this file has no index Entry (src/n.go). After the change, add one with aoci_update_entry."
)

// buildUnbaselinedRepo indexes src/b.go but creates it only after the Baseline
// snapshot, which is the "Entry present, drift unknown" shape.
func buildUnbaselinedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src"), 0o755))
	must(os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A1"), 0o644))
	idx := "===段 " + filepath.ToSlash(root) + "/src/===\n" + pretoolEntryLineA + "\n" + pretoolEntryLineB + "\n"
	must(os.MkdirAll(filepath.Join(root, ".aoci"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".aoci", "index.txt"), []byte(idx), 0o644))
	cfg := config.DefaultConfig()
	cfg.IndexPath = ".aoci/index.txt"
	must(config.Save(root, cfg))
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	must(err)
	must(baseline.Save(root, baseline.NewBaseline(snap)))
	must(os.WriteFile(filepath.Join(root, "src", "b.go"), []byte("B1"), 0o644))
	return root
}

func TestPretoolEnglishLocaleInjectsEnglishText(t *testing.T) {
	testkit.PinLocale(t, textassets.DefaultLocale)

	expect := func(name string, res PretoolResult, block bool, text string) {
		t.Helper()
		if res.Block != block {
			t.Fatalf("%s: Block=%v, want %v: %+v", name, res.Block, block, res)
		}
		if res.Text != text {
			t.Fatalf("%s: text differs\n got: %q\nwant: %q", name, res.Text, text)
		}
	}

	// Default mode: a fresh Entry is injected under the English header.
	root := buildRepo(t, false)
	expect("fresh", HandlePreTool(root, "Edit", "src/a.go"),
		false, pretoolEnglishEntryHeader+pretoolEntryLineA)

	// A target without an Entry gets the English enrolment reminder.
	if err := os.WriteFile(filepath.Join(root, "src", "n.go"), []byte("N"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("not indexed", HandlePreTool(root, "Write", "src/n.go"),
		false, pretoolEnglishNotIndexed)

	// Stale without strict: English warning, still no block.
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("A2"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("stale default", HandlePreTool(root, "Edit", "src/a.go"),
		false, pretoolEnglishStaleHeader+pretoolEntryLineA)

	// Strict + stale: the only blocking shape, with the English strict suffix.
	strict := buildRepo(t, true)
	if err := os.WriteFile(filepath.Join(strict, "src", "a.go"), []byte("A2"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect("stale strict", HandlePreTool(strict, "Edit", "src/a.go"),
		true, pretoolEnglishStaleHeader+pretoolEntryLineA+pretoolEnglishStrictBlock)

	// Strict + fresh: strict changes nothing about a current Entry.
	expect("fresh strict", HandlePreTool(buildRepo(t, true), "Edit", "src/a.go"),
		false, pretoolEnglishEntryHeader+pretoolEntryLineA)

	// Indexed but absent from the Baseline: drift unknown, Entry still injected.
	expect("unbaselined", HandlePreTool(buildUnbaselinedRepo(t), "Edit", "src/b.go"),
		false, pretoolEnglishUnbaselined+pretoolEntryLineB)

	// An unresolvable path is explained in English and never blocks.
	res := HandlePreTool(root, "Edit", "../etc/passwd")
	if res.Block ||
		!strings.HasPrefix(res.Text, "aoci hook: the path could not be resolved (") ||
		!strings.HasSuffix(res.Text, "); nothing is injected this time") {
		t.Fatalf("escaping path must be explained in English without blocking: %+v", res)
	}
}

// A catalog failure is a hook failure, and the hook is fail-open: the helper
// reports it instead of panicking or returning a placeholder the host would
// print, so HandlePreTool injects nothing and blocks nothing.
func TestPretoolMessageReportsCatalogFailures(t *testing.T) {
	for _, current := range []struct {
		name string
		key  string
		args []any
	}{
		{"unknown key", "hook.pretool.no_such_key", nil},
		{"missing argument", "hook.pretool.not_indexed", nil},
		{"surplus argument", "hook.pretool.entry_header", []any{"extra"}},
	} {
		text, ok := pretoolMessage(current.key, current.args...)
		if ok || text != "" {
			t.Fatalf("%s: pretoolMessage must report the failure with empty text: ok=%v text=%q", current.name, ok, text)
		}
	}
	if text, ok := pretoolMessage("hook.pretool.entry_header"); !ok || text == "" {
		t.Fatalf("a declared key must render: ok=%v text=%q", ok, text)
	}
}

// Locks: the unresolved-path line renders the refusal reason from the UI
// catalog, so an en-US host sees English and a zh-CN host keeps the exact
// text it had when the reason was a hardcoded Chinese error.
func TestPretoolUnresolvedPathReasonIsLocalized(t *testing.T) {
	root := buildRepo(t, false)
	testkit.PinLocale(t, "zh-CN")
	if got := HandlePreTool(root, "Edit", "/etc/passwd").Text; got != "aoci hook: 路径未解析(拒绝绝对路径: 只接受仓库相对路径),本次不注入" {
		t.Fatalf("zh-CN unresolved text = %q", got)
	}
	testkit.PinLocale(t, "en-US")
	got := HandlePreTool(root, "Edit", "/etc/passwd").Text
	if !strings.HasPrefix(got, "aoci hook: ") || !strings.Contains(got, "an absolute path is refused") {
		t.Fatalf("en-US unresolved text must carry the English reason: %q", got)
	}
	for _, r := range got {
		if r > 0x7f && r != '⚠' {
			t.Fatalf("en-US unresolved text must not carry Chinese: %q", got)
		}
	}
}
