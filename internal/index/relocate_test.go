// ResolveRelPaths 路径可移植性(重定位)测试
// 索引条目: relocate_test.go[TPS7TS]
//
// 立测背景(2026-07-10 httpx 实弹协议级缺陷): 索引段头写创建时的绝对路径,
// 仓库迁移后全部段路径失配,全体条目 RelPath 为空 → verify 全量 Missing。
// 修法为解析侧重定位: 最长公共目录前缀视为"创建时根",剥离后拼当前根。
//
// 规则精化(同日第二协议级发现,Codex 真机现场): 初版"全部失配才重定位"的
// 保守规则,在迁移仓库内由 agent 插入新段(段头写当前根路径)后自我瓦解 ——
// 新段直接命中即构成"混合命中",存量段全体放弃重定位再度失明。真实世界的
// 混合根几乎唯一成因就是"迁移后插新段"。精化为逐段解析: 每段先试直接命中,
// 命中即用;全部失配的段落自成集合,公共前缀只在该集合内计算并重定位。
//
// 本文件锁定的行为矩阵:
//  1. 原路径场景零变化;
//  2. 单段索引迁移(httpx 形态);
//  3. 多段索引迁移(公共前缀为创建时根);
//  4. 失配段无公共前缀 → 保守放弃;
//  5. 混合根: 命中段直取,失配段子集重定位(初版"混合即全弃"已改判);
//  6. Windows 盘符根之间的迁移;
//  7. FindSectionForPath 与 ResolveRelPaths 共享解析(防孪生);
//  8. Codex 真机现场复现: Linux 前缀存量段 + Windows 当前根新段共存,两界各自成立。
package index

import "testing"

// buildDoc 从文本解析并返回文档(测试辅助)
func buildDoc(t *testing.T, text string) *Document {
	t.Helper()
	doc, _ := Parse(text)
	return doc
}

// relOf 取指定段序号/条目序号的 RelPath(测试辅助)
func relOf(t *testing.T, doc *Document, sec, ent int) string {
	t.Helper()
	if sec >= len(doc.Sections) || ent >= len(doc.Sections[sec].Entries) {
		t.Fatalf("段/条目下标越界: sec=%d ent=%d", sec, ent)
	}
	return doc.Sections[sec].Entries[ent].RelPath
}

// TestResolveOriginalPathUnchanged 原路径场景: 直接命中,行为与重定位引入前一致
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

// TestResolveSingleSectionRelocated 单段索引迁移(httpx 实弹形态)
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

// TestResolveMultiSectionRelocated 多段索引迁移(本仓形态)
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

// TestResolveNoCommonPrefixConservative 失配段之间无公共前缀: 保守放弃,RelPath 留空
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

// TestResolveMixedHitSubsetRelocation 混合根(规则精化改判):
// 命中段直取;失配段自成集合并在集合内做公共前缀重定位。
// 初版行为(混合即全弃)已被 Codex 真机现场证伪 —— 迁移仓插新段后存量段全失明。
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

// TestResolveWindowsDriveRelocated Windows 盘符根之间迁移
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

// TestFindSectionAfterRelocation 迁移后段定位(共享解析防孪生)
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

// TestResolveCodexMixedRootScene Codex 真机现场复现(2026-07-10):
// Linux 前缀存量段(/opt/httpx 系)迁到 Windows 仓库(C:/work/httpx)后,agent
// 插入当前根路径的新段(C:/work/httpx/examples)。精化规则下: 新段直接命中,
// 存量段子集按公共前缀 /opt/httpx 重定位 —— 两个世界各自成立,存量索引不失明。
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
