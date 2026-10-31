// ResolveRelPaths path portability (relocation) tests
// Index entry: relocate_test.go[TPS7TS]
//
// Why these tests exist (the 2026-07-10 httpx live protocol-level defect): section headers wrote the absolute path at creation time,
// after a repository move every section path mismatched, every entry's RelPath was empty, and verify reported everything Missing.
// The fix is relocation on the parsing side: the longest common directory prefix is taken as "the root at creation time", stripped, and the current root prepended.
//
// Rule refinement (a second protocol-level finding the same day, on a real Codex machine): the first version's conservative rule
// "relocate only when everything mismatches" collapsed on itself once an agent inserted a new section (whose header wrote the current root path) into a migrated repository:
// the new section hit directly, forming a "mixed hit", and every existing section gave up relocation and went blind again. In the real world the almost only cause of
// mixed roots is "a new section inserted after a move". Refined to per-section resolution: each section first tries a direct hit and uses it when it hits;
// the mismatching sections form their own set, and the common prefix is computed and relocated within that set only.
//
// The behaviour matrix this file locks:
//  1. the original-path scenario changes nothing;
//  2. a single-section index moved (the httpx shape);
//  3. a multi-section index moved (the common prefix is the root at creation time);
//  4. mismatching sections with no common prefix → conservative give-up;
//  5. mixed roots: hit sections are taken directly, the mismatching subset is relocated (the first version's "mixed means give up everything" is overruled);
//  6. a move between Windows drive-letter roots;
//  7. FindSectionForPath and ResolveRelPaths share one resolution (against twins);
//  8. the real Codex scene reproduced: Linux-prefixed existing sections coexist with a Windows current-root new section, and both worlds hold.
package index

import "testing"

// buildDoc parses text and returns the document (test helper)
func buildDoc(t *testing.T, text string) *Document {
	t.Helper()
	doc, _ := Parse(text)
	return doc
}

// relOf takes the RelPath of the given section and entry index (test helper)
func relOf(t *testing.T, doc *Document, sec, ent int) string {
	t.Helper()
	if sec >= len(doc.Sections) || ent >= len(doc.Sections[sec].Entries) {
		t.Fatalf("段/条目下标越界: sec=%d ent=%d", sec, ent)
	}
	return doc.Sections[sec].Entries[ent].RelPath
}

// TestResolveOriginalPathUnchanged, the original-path scenario: a direct hit, behaving as before relocation existed
func TestResolveOriginalPathUnchanged(t *testing.T) {
	text := "===根段/opt/demo/===\n" +
		"a.go[XC9T]: F:f | R:- | A:- | S:-\n" +
		"===子段/opt/demo/internal/===\n" +
		"b.go[XC9T]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/opt/demo")
	if got := relOf(t, doc, 0, 0); got != "a.go" {
		t.Fatalf("根段条目 RelPath = %q, want a.go", got)
	}
	if got := relOf(t, doc, 1, 0); got != "internal/b.go" {
		t.Fatalf("子段条目 RelPath = %q, want internal/b.go", got)
	}
}

// TestResolveSingleSectionRelocated, a single-section index moved (the httpx live shape)
func TestResolveSingleSectionRelocated(t *testing.T) {
	text := "===/opt/httpx/===\n" +
		"pyproject.toml[PBLD7S]: F:f | R:- | A:- | S:-\n" +
		"httpx/_api.py[CCLT8M]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/tmp/httpx-moved")
	if got := relOf(t, doc, 0, 0); got != "pyproject.toml" {
		t.Fatalf("迁移后 RelPath = %q, want pyproject.toml(单段重定位失败即 httpx 缺陷回归)", got)
	}
	if got := relOf(t, doc, 0, 1); got != "httpx/_api.py" {
		t.Fatalf("迁移后带目录前缀条目 RelPath = %q, want httpx/_api.py", got)
	}
}

// TestResolveMultiSectionRelocated, a multi-section index moved (this repository's shape)
func TestResolveMultiSectionRelocated(t *testing.T) {
	text := "===配置索引/opt/aoci-code/===\n" +
		"go.mod[XMO9T]: F:f | R:- | A:- | S:-\n" +
		"===文件系统/opt/aoci-code/internal/fs/===\n" +
		"walk.go[FWK8WS]: F:f | R:- | A:- | S:-\n" +
		"===模板/opt/aoci-code/templates/===\n" +
		"templates.go[ETP7BT]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/home/user/clone")
	if got := relOf(t, doc, 0, 0); got != "go.mod" {
		t.Fatalf("根段迁移 RelPath = %q, want go.mod", got)
	}
	if got := relOf(t, doc, 1, 0); got != "internal/fs/walk.go" {
		t.Fatalf("子段迁移 RelPath = %q, want internal/fs/walk.go", got)
	}
	if got := relOf(t, doc, 2, 0); got != "templates/templates.go" {
		t.Fatalf("子段迁移 RelPath = %q, want templates/templates.go", got)
	}
}

func TestResolveNestedWorktreeSectionsFromPrimaryCheckout(t *testing.T) {
	text := "===/srv/repo/.worktrees/wt/===\n" +
		"go.mod[XMO9T]: F:f | R:- | A:- | S:-\n" +
		"===/srv/repo/.worktrees/wt/internal/fs/===\n" +
		"walk.go[FWK8WS]: F:f | R:- | A:- | S:-\n"
	for _, root := range []string{
		"/srv/repo/.worktrees/wt", // authored checkout
		"/srv/repo",               // primary checkout, an ancestor of the recorded root
		"/tmp/clone",              // unrelated clone
	} {
		t.Run(root, func(t *testing.T) {
			doc := buildDoc(t, text)
			ResolveRelPaths(doc, root)
			if got := relOf(t, doc, 0, 0); got != "go.mod" {
				t.Fatalf("root Entry = %q, want go.mod", got)
			}
			if got := relOf(t, doc, 1, 0); got != "internal/fs/walk.go" {
				t.Fatalf("nested Entry = %q, want internal/fs/walk.go", got)
			}
			if sec := FindSectionForPath(doc, root, "internal/fs/new.go"); sec != doc.Sections[1] {
				t.Fatalf("nested section lookup = %p, want %p", sec, doc.Sections[1])
			}
		})
	}
}

// TestResolveNoCommonPrefixConservative, no common prefix among the mismatching sections: conservative give-up, RelPath left empty
func TestResolveNoCommonPrefixConservative(t *testing.T) {
	text := "===/opt/projA/===\n" +
		"a.go[XC9T]: F:f | R:- | A:- | S:-\n" +
		"===/home/projB/===\n" +
		"b.go[XC9T]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/tmp/elsewhere")
	if got := relOf(t, doc, 0, 0); got != "" {
		t.Fatalf("无公共前缀应保守留空, got %q", got)
	}
	if got := relOf(t, doc, 1, 0); got != "" {
		t.Fatalf("无公共前缀应保守留空, got %q", got)
	}
}

// TestResolveMixedHitSubsetRelocation, mixed roots (the rule refinement's overruling):
// hit sections are taken directly; the mismatching sections form their own set and relocate by the common prefix within it.
// The first version's behaviour (mixed means give up everything) was falsified on a real Codex machine: after a new section was inserted into a moved repository, every existing section went blind.
func TestResolveMixedHitSubsetRelocation(t *testing.T) {
	text := "===/opt/demo/===\n" +
		"a.go[XC9T]: F:f | R:- | A:- | S:-\n" +
		"===/legacy/place/internal/===\n" +
		"b.go[XC9T]: F:f | R:- | A:- | S:-\n" +
		"===/legacy/place/pkg/===\n" +
		"c.go[XC9T]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/opt/demo")
	if got := relOf(t, doc, 0, 0); got != "a.go" {
		t.Fatalf("命中段应直取, got %q", got)
	}
	if got := relOf(t, doc, 1, 0); got != "internal/b.go" {
		t.Fatalf("失配段子集应重定位(公共前缀/legacy/place), got %q", got)
	}
	if got := relOf(t, doc, 2, 0); got != "pkg/c.go" {
		t.Fatalf("失配段子集应重定位, got %q", got)
	}
}

// TestResolveWindowsDriveRelocated, a move between Windows drive-letter roots
func TestResolveWindowsDriveRelocated(t *testing.T) {
	text := "===c:/old-place/repo/===\n" +
		"main.go[XC9T]: F:f | R:- | A:- | S:-\n" +
		"===c:/old-place/repo/internal/===\n" +
		"x.go[XC9T]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, `D:\new\clone`)
	if got := relOf(t, doc, 0, 0); got != "main.go" {
		t.Fatalf("盘符迁移根段 RelPath = %q, want main.go", got)
	}
	if got := relOf(t, doc, 1, 0); got != "internal/x.go" {
		t.Fatalf("盘符迁移子段 RelPath = %q, want internal/x.go", got)
	}
}

// TestFindSectionAfterRelocation, section lookup after a move (shared resolution against twins)
func TestFindSectionAfterRelocation(t *testing.T) {
	text := "===/opt/aoci-code/===\n" +
		"go.mod[XMO9T]: F:f | R:- | A:- | S:-\n" +
		"===/opt/aoci-code/internal/fs/===\n" +
		"walk.go[FWK8WS]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, "/home/clone")
	sec := FindSectionForPath(doc, "/home/clone", "internal/fs/new_file.go")
	if sec == nil {
		t.Fatalf("迁移后 FindSectionForPath 应命中 internal/fs 段(孪生失配即回归)")
	}
	if sec.AbsPath != "/opt/aoci-code/internal/fs/" && sec.AbsPath != "/opt/aoci-code/internal/fs" {
		t.Fatalf("命中段 AbsPath = %q, 非预期段", sec.AbsPath)
	}
	root := FindSectionForPath(doc, "/home/clone", "go.mod")
	if root == nil || root.StartLine != 1 {
		t.Fatalf("迁移后根段定位失败")
	}
}

// TestResolveCodexMixedRootScene reproduces the real Codex scene (2026-07-10):
// Linux-prefixed existing sections (the /opt/httpx family) moved to a Windows repository (C:/work/httpx), then the agent
// inserted a new section under the current root path (C:/work/httpx/examples). Under the refined rule the new section hits directly,
// and the existing subset relocates by the common prefix /opt/httpx; both worlds hold, and the existing index does not go blind.
func TestResolveCodexMixedRootScene(t *testing.T) {
	text := "===/opt/httpx/===\n" +
		"pyproject.toml[PBLD7S]: F:f | R:- | A:- | S:-\n" +
		"===/opt/httpx/httpx/===\n" +
		"_api.py[CCLT8M]: F:f | R:- | A:- | S:-\n" +
		"===C:/work/httpx/examples/===\n" +
		"hello.py[XCLT3T]: F:f | R:- | A:- | S:-\n"
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, `C:\work\httpx`)
	if got := relOf(t, doc, 0, 0); got != "pyproject.toml" {
		t.Fatalf("存量根段应经子集重定位解析, got %q(现场缺陷回归: 插新段致存量失明)", got)
	}
	if got := relOf(t, doc, 1, 0); got != "httpx/_api.py" {
		t.Fatalf("存量子段应经子集重定位解析, got %q", got)
	}
	if got := relOf(t, doc, 2, 0); got != "examples/hello.py" {
		t.Fatalf("当前根新段应直接命中, got %q", got)
	}
}

// An index with no root-level section, every section under one recorded
// subdirectory, is ambiguous: it reads the same whether the writer meant that
// subdirectory as the repository or as a real directory below it. Such an
// index is only written by rc13 and older, since rc14 always begins with the
// root section. What the reader guarantees is that the origin, an ancestor,
// and an unrelated clone all read it identically, so nothing diverges between
// checkouts; this test pins that invariant rather than either interpretation.
func TestSubdirectoryOnlyIndexReadsTheSameAtOriginAncestorAndClone(t *testing.T) {
	text := "===/srv/repo/src/===\n" +
		"a.go[XMO9T]: F:f | R:- | A:- | S:-\n" +
		"===/srv/repo/src/pkg/===\n" +
		"b.go[FWK8WS]: F:f | R:- | A:- | S:-\n"
	readings := map[string][2]string{}
	for _, root := range []string{"/srv/repo", "/srv", "/tmp/clone"} {
		doc := buildDoc(t, text)
		ResolveRelPaths(doc, root)
		readings[root] = [2]string{relOf(t, doc, 0, 0), relOf(t, doc, 1, 0)}
	}
	for root, got := range readings {
		if got != readings["/tmp/clone"] {
			t.Fatalf("reading at %s = %v, clone reads %v", root, got, readings["/tmp/clone"])
		}
	}
}
