// 扩展读法测试 (#58, #60): 目录名含空白/=/(, 文件名含 [。
//
// 立测背景: 三正则的目录路径在首个空白、"="、"(" 或 "（" 处截断, 条目文件名不能含 "["。
// 后果有两类: (1) 写入器把解析回来的截断路径当作历史根, 之后的段头全部写成截断根;
// 子目录名含空格时条目被解析成另一个路径, 同时报 orphan 与 missing, 仓库永远无法对齐;
// (2) Next.js / Nuxt / SvelteKit 的动态路由文件名 ([...id].jsx) 根本写不进去, 而批次是
// 原子的, 一条写不进去整批永远失败。
//
// 本文件锁定: 旧读法仍是第一读法(今天能解析的行读法不变); 机器写出的段头取到最后一个
// "/"; 人写的带注释或多路径段头不变; 已经带着截断根的索引原地与克隆后都继续解析;
// 写入端对无法无歧义表示的目录失败关闭。
package index

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestDirHeaderExtendedReadingKeepsLegacyFirst(t *testing.T) {
	for _, test := range []struct {
		line, abs, legacy string
		directory         bool
	}{
		{"===/tmp/example project/repo/===", "/tmp/example project/repo/", "/tmp/example", true},
		{"===/repo/pkg/validations/filetests/max=/===", "/repo/pkg/validations/filetests/max=/", "/repo/pkg/validations/filetests/max", true},
		{"===/repo/app/(home)/===", "/repo/app/(home)/", "/repo/app/", true},
		{"===/repo/docs/（全角）/===", "/repo/docs/（全角）/", "/repo/docs/", true},
		{"===C:/Users/John Doe/repo/src/===", "C:/Users/John Doe/repo/src/", "C:/Users/John", true},
		{"===Go sources/tmp/x/example project/repo/===", "/tmp/x/example project/repo/", "/tmp/x/example", true},
		// 今天就能正确解析的形态: 两种读法一致
		{"===配置索引/opt/aoci-code/===", "/opt/aoci-code/", "/opt/aoci-code/", true},
		{"===/opt/app/[locale]/===", "/opt/app/[locale]/", "/opt/app/[locale]/", true},
		// 人写的段头保持旧读法: 尾注不以 "/" 结尾、完整目录后接空白的注释、第二个绝对路径
		{"===/opt/aoci-code/ (备注)===", "/opt/aoci-code/", "/opt/aoci-code/", true},
		{"===/opt/x/ see docs/===", "/opt/x/", "/opt/x/", true},
		{"===/opt/a/ 与 /opt/b/===", "/opt/a/", "/opt/a/", true},
		{"===/opt/a/ and C:/b/===", "/opt/a/", "/opt/a/", true},
		// 相对多路径段头今天不是目录段, 以后也不是
		{"===internal/ 与 testdata/===", "", "", false},
		{"==========", "", "", false},
	} {
		doc, _ := Parse(test.line + "\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n")
		if len(doc.Sections) != 1 {
			t.Fatalf("%q: sections=%d", test.line, len(doc.Sections))
		}
		sec := doc.Sections[0]
		if (sec.AbsPath != "") != test.directory || sec.AbsPath != test.abs || sec.LegacyAbsPath != test.legacy {
			t.Fatalf("%q: AbsPath=%q LegacyAbsPath=%q, want %q / %q", test.line, sec.AbsPath, sec.LegacyAbsPath, test.abs, test.legacy)
		}
	}
}

func TestEntryNamesMayContainBrackets(t *testing.T) {
	for _, test := range []struct{ line, name, tags string }{
		{"[...id].jsx[CG5T]: F:Renders one document page | R:- | A:- | S:-", "[...id].jsx", "CG5T"},
		{"[id].vue[CG5T]: F:Renders one item | R:- | A:- | S:-", "[id].vue", "CG5T"},
		{"a[b].go[CG7S]: F:Holds a bracketed name | R:- | A:- | S:-", "a[b].go", "CG7S"},
		{"x[1][CG5T]: F:Ends with a bracket pair | R:- | A:- | S:-", "x[1]", "CG5T"},
		// 旧读法仍然先行
		{"main.go[CG9L]: F:Runs the program | R:- | A:main | S:-", "main.go", "CG9L"},
		{"b]r.go[CG5T]: F:Holds a closing bracket | R:- | A:- | S:-", "b]r.go", "CG5T"},
		{"inner space.go[CG5T]: F:Holds a space | R:- | A:- | S:-", "inner space.go", "CG5T"},
	} {
		entry, ok := ParseEntryLine(test.line, 1)
		if !ok || entry.Filename != test.name || entry.TagsRaw != test.tags || entry.FullLine != test.line {
			t.Fatalf("%q: ok=%t entry=%+v", test.line, ok, entry)
		}
		if entry.F == "" {
			t.Fatalf("%q: F was not decomposed: %+v", test.line, entry)
		}
	}
	line := "[...id].jsx[CG5T]: F:Renders one document page | R:- | A:- | S:-"
	for _, violation := range ValidateEntryLine("pages/docs/[...id].jsx", line) {
		if violation.Level == LevelError {
			t.Fatalf("a bracketed file name must validate against its own path: %+v", violation)
		}
	}
	mismatch := false
	for _, violation := range ValidateEntryLine("pages/docs/[slug].jsx", line) {
		mismatch = mismatch || violation.Level == LevelError
	}
	if !mismatch {
		t.Fatal("the file-name gate must still refuse an Entry written for a different path")
	}
	// 不是条目的行仍然不是条目
	for _, line := range []string{"[CG5T]: F:no name", " lead.go[CG5T]: F:leading space", "see [x] for details"} {
		if _, ok := ParseEntryLine(line, 1); ok {
			t.Fatalf("%q must not parse as an Entry", line)
		}
	}
}

func TestSpecialDirectoriesResolveAndInsertUnderCleanRoot(t *testing.T) {
	root := "/repo"
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for _, target := range []struct{ rel, line string }{
		{"dir space/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		{"pkg/max=/b.go", "b.go[CG5T]: F:b | R:- | A:- | S:-"},
		{"app/(home)/page.tsx", "page.tsx[CG5T]: F:page | R:- | A:- | S:-"},
		{"pages/docs/[...id].jsx", "[...id].jsx[CG5T]: F:doc page | R:- | A:- | S:-"},
		{"dir space/deeper dir/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-"},
	} {
		var err error
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
		doc, warnings := Parse(text)
		if len(warnings) != 0 {
			t.Fatalf("insert %s left parse warnings: %+v", target.rel, warnings)
		}
		ResolveRelPaths(doc, root)
		if FindEntry(doc, target.rel) == nil {
			t.Fatalf("%s does not resolve after its own insert:\n%s", target.rel, text)
		}
	}
	for _, header := range []string{"===/repo/dir space/===", "===/repo/pkg/max=/===", "===/repo/app/(home)/===", "===/repo/dir space/deeper dir/==="} {
		if !strings.Contains(text, header+"\n") {
			t.Fatalf("missing natural header %s:\n%s", header, text)
		}
	}
	// 替换与删除沿同一解析
	replaced, err := ReplaceEntryForPath(text, root, "dir space/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-", "a.go[CG5T]: F:a revised | R:- | A:- | S:-")
	if err != nil || !strings.Contains(replaced, "F:a revised") {
		t.Fatalf("replace under a spaced directory: %v", err)
	}
	removed, err := RemoveEntryForPath(replaced, root, "pages/docs/[...id].jsx", "[...id].jsx[CG5T]: F:doc page | R:- | A:- | S:-")
	if err != nil || strings.Contains(removed, "[...id].jsx[") {
		t.Fatalf("remove a bracketed file name: %v", err)
	}
}

// rc13 及更早版本在含空格的根下写出的索引: 首段是完整根, 之后的段挂在截断根下。
const truncatedRootIndex = "#head\n" +
	"===/tmp/example project/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"

func TestIndexWrittenWithTruncatedRootHealsInPlace(t *testing.T) {
	root := "/tmp/example project/repo"
	doc := buildDoc(t, truncatedRootIndex)
	ResolveRelPaths(doc, root)
	for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
		if FindEntry(doc, rel) == nil {
			t.Fatalf("%s must resolve in place; got %q %q %q", rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
		}
	}
	// 新段延续该索引族已经在用的截断根: 克隆到别处的副本没有仓库根可比对, 它靠"恰好一个
	// 完整根段"认出旧写入器, 再多一个完整根段就会失去重定位(评审实测: CI 检出全体 orphan)。
	text, err := InsertEntry(truncatedRootIndex, "lib/new dir/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatalf("a root spelled two ways is one root, so a new section must be accepted: %v", err)
	}
	if !strings.Contains(text, "===/tmp/example/lib/new dir/===\n") || strings.Contains(text, "===/tmp/example project/repo/lib/") {
		t.Fatalf("new sections must continue the truncated spelling the family already uses:\n%s", text)
	}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		doc = buildDoc(t, text)
		ResolveRelPaths(doc, at)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go", "lib/new dir/c.go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("at %q: %s lost after inserting a new section", at, rel)
			}
		}
	}
}

// 新写入器在含特殊字符的根下从零建立的索引, 与此前每个版本写出的形状完全相同: 根段以完整根
// 拼写, 之后的段挂在旧读法读回来的那个根下。世上因此只有一种索引形状, 旧读者仍能解析其中
// 名字普通的段, 而每一段在原地与克隆里解析一致。(四轮评审: "每段都写完整根"的新形状与旧写入器
// 的字节在根路径以截断字符开头的段上无法区分, 克隆只能猜。)
func TestFreshIndexUnderSpecialRootStaysConsistentAcrossInserts(t *testing.T) {
	root := "/work/my project (v2)/repo"
	text := "#head\n"
	var err error
	for _, target := range []struct{ rel, line string }{
		{"main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-"},
		{"src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		{"src/deep dir/b.go", "b.go[CG5T]: F:b | R:- | A:- | S:-"},
		{"pages/[id]/[...slug].go", "[...slug].go[CG5T]: F:s | R:- | A:- | S:-"},
	} {
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
	}
	if strings.Count(text, "===/work/my project (v2)/repo/") != 1 || !strings.HasPrefix(text, "#head\n\n===/work/my project (v2)/repo/===\n") {
		t.Fatalf("the root section, and only it, carries the full root, and it comes first:\n%s", text)
	}
	for _, header := range []string{"===/work/my/src/===\n", "===/work/my/src/deep dir/===\n", "===/work/my/pages/[id]/===\n"} {
		if !strings.Contains(text, header) {
			t.Fatalf("later sections continue the root as the original reading reads it back; missing %q:\n%s", header, text)
		}
	}
	for _, at := range []string{root, "/srv/checkout"} {
		doc := buildDoc(t, text)
		ResolveRelPaths(doc, at)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go", "pages/[id]/[...slug].go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("at %q: %s does not resolve", at, rel)
			}
		}
	}
}

// 标签定位只有一处(EntryTagSpan): 字典、E 规模、S 配额读到的必须是真正的标签, 而不是
// 文件名里的第一个方括号。评审实测: [id].page.test.ts[XCR7T] 被字典硬闸永久拒绝,
// [v1].ts[CG9T] 被按 C1 计配额, 401 行的 [big].ts 不报 E 规模。
func TestTagConsumersReadTheTagNotTheFileName(t *testing.T) {
	for _, test := range []struct {
		line    string
		nameEnd int
		tag     string
	}{
		{"[id].page.test.ts[XCR7T]: F:x | R:- | A:- | S:-", 17, "XCR7T"},
		{"[v1].ts[CG9T]: F:x | R:- | A:- | S:-", 7, "CG9T"},
		{"pages/docs/[...id].go[CD5S]: F:x | R:- | A:- | S:-", 21, "CD5S"},
		{"main.go[CG7T]: F:x | R:- | A:- | S:-", 7, "CG7T"},
		{"loose[T1]: no fras here", 5, "T1"},
	} {
		nameEnd, tagStart, tagEnd, ok := EntryTagSpan(test.line)
		if !ok || nameEnd != test.nameEnd || test.line[tagStart:tagEnd] != test.tag {
			t.Fatalf("%q: nameEnd=%d tag=%q ok=%t", test.line, nameEnd, test.line[tagStart:tagEnd], ok)
		}
	}
	long := strings.Repeat("s", 120)
	if v := CheckSQuotaWith("[id9].vue[CG3T]: F:x | R:- | A:- | S:"+long, nil); v == nil {
		t.Fatal("a C3 Entry with a 120-rune S must be measured against the C3 quota, not against the 9 in its file name")
	}
	if v := CheckSQuotaWith("[v1].ts[CG9T]: F:x | R:- | A:- | S:"+long, nil); v != nil {
		t.Fatalf("a C9 Entry must not be measured against C1 because its file name holds a 1: %+v", v)
	}
	thresholds := ExtractEScaleThresholds("#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100")
	if mismatch := CheckEScaleDetail("[big].ts[CG5T]: F:x | R:- | A:- | S:-", 401, thresholds); mismatch == nil || mismatch.Actual != "T" {
		t.Fatalf("the E tag of a bracketed name must be checked: %+v", mismatch)
	}
}

// resolvedRelPaths 返回索引在给定仓库根下解析出的全部条目路径(排序)。
func resolvedRelPaths(t *testing.T, text, root string) []string {
	t.Helper()
	doc := buildDoc(t, text)
	ResolveRelPaths(doc, root)
	var rels []string
	for _, sec := range doc.Sections {
		for _, entry := range sec.Entries {
			rels = append(rels, entry.RelPath)
		}
	}
	sort.Strings(rels)
	return rels
}

// rc13 及更早版本: 点目录(.circleci/ .claude/ .cursor/ .devcontainer/)排在 .gitattributes
// 之前时, 以完整根拼写写下的首段并不是根段。旧读法把它读成根段, 旧写入器随后把根文件登记
// 进了这一段, 再把其余段挂到截断根下。二轮评审实测: 若原地按真实目录读这一段而克隆按旧读法
// 读它, 同一份字节在两处解析出不同的路径集, 原地修好克隆就坏, 两边永远无法同时对齐 ——
// 比旧版本(两处错得一样)更糟。锁定: 一旦后续段确认了旧写入器, 这一段在原地也沿用旧读法。
const dotDirFirstIndex = "#head\n" +
	"===/tmp/example project/repo/.circleci/===\n" +
	"config.yml[CG5T]: F:c | R:- | A:- | S:-\n" +
	".gitattributes[CG5T]: F:g | R:- | A:- | S:-\n" +
	"AGENTS.md[CG5T]: F:a | R:- | A:- | S:-\n\n" +
	"===/tmp/example/src/===\na.go[CG5T]: F:s | R:- | A:- | S:-\n"

func TestOldWriterIndexReadsTheSameInPlaceAndInAClone(t *testing.T) {
	root := "/tmp/example project/repo"
	clones := []string{"/srv/checkout", "C:/ci/work", "/home/u/other place/repo"}
	want := []string{".gitattributes", "AGENTS.md", "config.yml", "src/a.go"}
	if got := resolvedRelPaths(t, dotDirFirstIndex, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("in place the old writer's section keeps the old reader's view: got %q want %q", got, want)
	}
	for _, at := range clones {
		if got := resolvedRelPaths(t, dotDirFirstIndex, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("clone at %q resolves %q, the origin resolves %q", at, got, want)
		}
	}

	// 孤儿流程要求的修复在各处落点一致: 误登记为根文件的 config.yml 被移除, 真正的
	// .circleci/config.yml 在截断根下得到自己的段。
	text, err := RemoveEntryForPath(dotDirFirstIndex, root, "config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-")
	if err != nil {
		t.Fatalf("remove the misfiled Entry: %v", err)
	}
	text, err = InsertEntry(text, ".circleci/config.yml", "config.yml[CG5T]: F:ci | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatalf("author the real file: %v", err)
	}
	if !strings.Contains(text, "===/tmp/example/.circleci/===\n") {
		t.Fatalf("the new section continues the truncated root:\n%s", text)
	}
	healed := []string{".circleci/config.yml", ".gitattributes", "AGENTS.md", "src/a.go"}
	for _, at := range append([]string{root}, clones...) {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, healed) {
			t.Fatalf("after the repair, %q resolves %q, want %q", at, got, healed)
		}
	}
}

// 新条目绝不落进旧写入器那一个完整根拼写的段: 它在原地会解析成 lib/x.go, 在克隆里解析成
// x.go(二轮评审实测)。沿用旧读法后该段是根段, lib/ 得到截断根下自己的段, 两处一致。
func TestInsertNeverLandsInTheOldWritersFullSpelledSection(t *testing.T) {
	root := "/tmp/example project/repo"
	text := "#head\n===/tmp/example project/repo/lib/===\nl.go[CG5T]: F:l | R:- | A:- | S:-\n\n" +
		"===/tmp/example/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "lib/x.go", "x.go[CG5T]: F:x | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "===/tmp/example/lib/===\nx.go[CG5T]") {
		t.Fatalf("lib/x.go must get its own section under the truncated root:\n%s", out)
	}
	origin := resolvedRelPaths(t, out, root)
	if !reflect.DeepEqual(origin, []string{"l.go", "lib/x.go", "src/a.go"}) {
		t.Fatalf("in place: %q", origin)
	}
	for _, at := range []string{"/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, origin) {
			t.Fatalf("clone at %q resolves %q, the origin resolves %q", at, got, origin)
		}
	}
}

// 首段完整拼写的非根段在没有任何截断根段佐证时, 就是它字面上的目录: 新写入器在特殊根下
// 从零建索引时, 第一条恰好落在点目录里, 第二条(根文件)必须得到自己的根段, 而不是被塞进去。
func TestFreshIndexWhoseFirstSectionIsADotDirectoryKeepsItsOwnRootSection(t *testing.T) {
	root := "/tmp/example project/repo"
	text := "#head\n"
	var err error
	for _, target := range []struct{ rel, line string }{
		{".circleci/config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-"},
		{".gitattributes", ".gitattributes[CG5T]: F:g | R:- | A:- | S:-"},
		{"src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
	} {
		if text, err = InsertEntry(text, target.rel, target.line, root); err != nil {
			t.Fatalf("insert %s: %v", target.rel, err)
		}
	}
	want := []string{".circleci/config.yml", ".gitattributes", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("at %q: %q, want %q\n%s", at, got, want, text)
		}
	}
}

// 三轮评审实测(真 rc13 二进制): 截断字符恰好是某个路径段的首字符时(/w/(x)/repo、
// C:/w/=e/repo、/w/（旧）/repo), 截断根 /w 与目录对齐, 旧写入器那个完整拼写的首段落在
// 截断根"之下", 克隆里的旧读法回退不触发, 根文件被解析成 (x)/repo/main.go —— rc13 两处
// 都对齐, 这是回归。锁定: 首个目录段在旧读法下等于公共前缀、扩展读法下不等, 就是旧写入器
// 的那一段, 不论它的扩展路径在前缀之外还是之下。
func TestOldWriterIndexUnderASlashCutRootRelocates(t *testing.T) {
	for _, shape := range []struct{ root, truncated string }{
		{"/w/(x)/repo", "/w"},
		{"C:/w/=e/repo", "C:/w"},
		{"/w/（旧）/repo", "/w"},
	} {
		text := "#head\n===" + shape.root + "/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
			"===" + shape.truncated + "/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
			"===" + shape.truncated + "/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
		want := []string{"main.go", "src/a.go", "src/deep dir/b.go"}
		for _, at := range []string{shape.root, "/srv/checkout", "D:/ci/work", "/home/u/other (place)/repo"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("index written at %q, read at %q: %q, want %q", shape.root, at, got, want)
			}
		}
		// 新段延续截断根, 插入后各处仍一致。
		grown, err := InsertEntry(text, "lib/c.go", "c.go[CG5T]: F:c | R:- | A:- | S:-", shape.root)
		if err != nil || !strings.Contains(grown, "==="+shape.truncated+"/lib/===\n") {
			t.Fatalf("root %q: %v\n%s", shape.root, err, grown)
		}
		want = []string{"lib/c.go", "main.go", "src/a.go", "src/deep dir/b.go"}
		for _, at := range []string{shape.root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, grown, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("after growth, written at %q, read at %q: %q, want %q", shape.root, at, got, want)
			}
		}
	}
}

// 旧写入器索引里的顶层目录名以截断字符开头(路由分组 (home)/): 它的段在旧读法下同样等于
// 截断根, 但它不是首段, 必须按自己的扩展路径解析, 原地与克隆一致。
func TestOldWriterIndexWithATopLevelCutDirectoryReadsTheSameEverywhere(t *testing.T) {
	root := "/tmp/example project/repo"
	grown, err := InsertEntry(truncatedRootIndex, "(home)/page.go", "page.go[CG5T]: F:p | R:- | A:- | S:-", root)
	if err != nil || !strings.Contains(grown, "===/tmp/example/(home)/===\n") {
		t.Fatalf("%v\n%s", err, grown)
	}
	want := []string{"(home)/page.go", "main.go", "src/a.go", "src/deep dir/b.go"}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, grown, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want %q", at, got, want)
		}
	}
}

// 没有根段、兄弟目录共用一个截断字符前缀(app/ 与 "app (old)/"): 这不是旧写入器, 克隆不得
// 把两段都压扁到根上(三轮评审 NIT 8)。旧写入器从不在首段旁边再写一个落在截断根上的普通段。
func TestSiblingsSharingACutPrefixAreNotAnOldWriterIndex(t *testing.T) {
	for _, text := range []string{
		"#head\n===/repo/app/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/repo/app (old)/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n",
		"#head\n===/repo/app (old)/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n\n===/repo/app/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n",
	} {
		want := []string{"app (old)/b.go", "app/a.go"}
		for _, at := range []string{"/repo", "/srv/checkout"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("read at %q: %q, want %q\n%s", at, got, want, text)
			}
		}
	}
}

// 截断根加目录恰好拼出仓库根本身的那个目录(/w 加 (x)/repo 等于 /w/(x)/repo): 旧写入器判据是
// 纯文本的, 非首段一律按扩展路径相对截断根解析, 所以这个段头虽与首段同文, 解析到的仍是它自己的
// 目录, 且原地与克隆一致。
func TestADirectoryRepeatingTheRootsTailResolvesTheSameEverywhere(t *testing.T) {
	root := "/w/(x)/repo"
	text := "#head\n===/w/(x)/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/w/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "(x)/repo/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"(x)/repo/f.go", "main.go", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want %q", at, got, want)
		}
	}
}

// 段头读得回来还不够, 新段还必须解析到它要去的目录。克隆被放在索引所记录的根"之内"时
// (记录根 /srv, 运行根 /srv/work), 目录 work/x 的段头 /srv/work/x 会被直接前缀比对解析成 x,
// 条目就登记到了别的路径上。写入端失败关闭。
func TestInsertRefusesASectionThatWouldResolveSomewhereElse(t *testing.T) {
	text := "#head\n===/srv/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/srv/lib/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "work/x/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", "/srv/work")
	if !errors.Is(err, ErrDirectoryUnspellable) || out != "" {
		t.Fatalf("work/x would resolve as x under this root and must be refused: %v\n%s", err, out)
	}
	if err := CheckInsertable(text, "work/x/f.go", "/srv/work"); !errors.Is(err, ErrDirectoryUnspellable) {
		t.Fatalf("the probe must answer what the writer answers: %v", err)
	}
	if out, err := InsertEntry(text, "docs/f.go", "f.go[CG5T]: F:f | R:- | A:- | S:-", "/srv/work"); err != nil ||
		!strings.Contains(out, "===/srv/docs/===\n") {
		t.Fatalf("a directory that does not collide is accepted: %v", err)
	}
}

// 仓库根本身写不成读得回来的段头(首段以截断字符开头, 或某段首尾是空白): rc13 靠旧读法
// "碰巧"能治理这些根, 三轮评审实测新写入器把每一批都拒掉了。锁定: 新索引记录段头读回来的
// 那个根(段根是坐标不是运行时路径), 首条目落在子目录时先写一个空根段给重定位当锚, 原地
// 与克隆解析一致; 连读回来的根都没有时才拒绝, 并点名是仓库根。
func TestFreshIndexUnderARootNoHeaderCanSpell(t *testing.T) {
	for _, shape := range []struct{ root, recorded string }{
		{"/(x)/repo", "/repo"},
		{"C:/(backup)/repo", "/repo"},
		{"/home/u/s /repo", "/home/u/s"},
		{"/home/u/ s/repo", "/home/u"},
	} {
		text := "#head\n"
		var err error
		for _, target := range []struct{ rel, line string }{
			{".circleci/config.yml", "config.yml[CG5T]: F:c | R:- | A:- | S:-"},
			{".gitattributes", ".gitattributes[CG5T]: F:g | R:- | A:- | S:-"},
			{"src/deep dir/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"},
		} {
			if text, err = InsertEntry(text, target.rel, target.line, shape.root); err != nil {
				t.Fatalf("root %q insert %s: %v", shape.root, target.rel, err)
			}
		}
		if !strings.Contains(text, "==="+shape.recorded+"/===\n") || strings.Contains(text, shape.root) {
			t.Fatalf("root %q must be recorded as %q:\n%s", shape.root, shape.recorded, text)
		}
		want := []string{".circleci/config.yml", ".gitattributes", "src/deep dir/a.go"}
		for _, at := range []string{shape.root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("root %q read at %q: %q, want %q\n%s", shape.root, at, got, want, text)
			}
		}
	}
	var unspellable *DirectoryUnspellableError
	if _, err := InsertEntry("#head\n", "main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-", "/(x)"); !errors.As(err, &unspellable) || !unspellable.Root {
		t.Fatalf("a root with no spellable reading at all is refused as the root: %v", err)
	}
}

// 规划端探针只判目录: 文件名再怪也不影响结论。
func TestCheckInsertableJudgesTheDirectoryOnly(t *testing.T) {
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for rel, refused := range map[string]bool{
		"src/[id].go": false, "app/(home)/ odd.go": false, "main2.go": false, "trail /x.go": true, " lead/x.go": true,
	} {
		err := CheckInsertable(text, rel, "/repo")
		if refused != errors.Is(err, ErrDirectoryUnspellable) || (!refused && err != nil) {
			t.Fatalf("%q: refused=%t err=%v", rel, refused, err)
		}
	}
}

// authoredIndex inserts one plain Entry per path into an empty index, in order.
func authoredIndex(t *testing.T, root string, rels ...string) string {
	t.Helper()
	text := "#head\n"
	for _, rel := range rels {
		out, err := InsertEntry(text, rel, rel[strings.LastIndex(rel, "/")+1:]+"[CG5T]: F:x | R:- | A:- | S:-", root)
		if err != nil {
			t.Fatalf("insert %s at %s: %v", rel, root, err)
		}
		text = out
	}
	return text
}

// 四轮评审(真二进制实测)的两个阻断, 同一个歧义: 段头字节 ===<P>/(x)…/=== 加 ===<P>/src/===
// 既可能是"根路径某段以截断字符开头"的旧写入器索引, 也可能是干净根下"顶层目录以截断字符开头且
// 排在最前"的索引, 纯文本无法区分, 克隆只能按旧写入器读。锁定两件事:
// 其一, 本写入器永不产出这组字节: 新索引总以根段开头(首条目落在目录里时先写空根段), 于是它
// 建立的索引在没有任何根文件条目时, 原地与克隆也解析一致。其二, 这组字节只可能出自 rc13 及
// 更早版本, 那时首段就是被当作根段用的(根文件登记在里面), 原地也按同一条纯文本判据读, 与克隆、
// 与 rc13 自己一致, 普通修复两处同步收敛。
func TestFreshIndexWithoutRootEntriesReadsTheSameEverywhere(t *testing.T) {
	for _, shape := range []struct {
		root string
		rels []string
	}{
		{"/w/(x)/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/=x/repo", []string{"a/f.go", "b/g.go"}},
		{"C:/w/(x)/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/x y/repo", []string{"a/f.go", "b/g.go"}},
		{"/w/x/repo", []string{"a/f.go", "b/g.go"}},
		{"/clean/repo", []string{"(home)/p.go", "src/q.go"}},
		{"/clean/repo", []string{"=x/p.go", "src/deep dir/q.go"}},
		{"/tmp/example project/repo", []string{".circleci/config.yml", "src/q.go"}},
	} {
		text := authoredIndex(t, shape.root, shape.rels...)
		want := append([]string{}, shape.rels...)
		sort.Strings(want)
		for _, at := range []string{shape.root, "/srv/checkout", "D:/ci/work", "/home/u/other (place)/repo"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("index created at %q, read at %q: %q, want %q\n%s", shape.root, at, got, want, text)
			}
		}
		if first := firstDirectorySection(buildDoc(t, text)); first == nil || len(first.Entries) != 0 ||
			normalizeRootPath(first.AbsPath) != normalizeRootPath(shape.root) {
			t.Fatalf("a new index whose first Entry lives in a directory begins with its empty root section, spelled in full:\n%s", text)
		}
		// Only the root section carries the full root; the rest hang from it as the
		// original reading reads it back, the single shape every release has written.
		if legacyRoot := normalizeRootPath(firstDirectorySection(buildDoc(t, text)).LegacyAbsPath); legacyRoot != normalizeRootPath(shape.root) &&
			strings.Count(text, "==="+shape.root+"/") != 1 {
			t.Fatalf("under a root holding a cut character only the root section is spelled in full:\n%s", text)
		}
	}
}

// 五轮评审(真二进制实测)的阻断: 根路径某段以截断字符开头(/w/(x)/repo), 仓库里又恰好有一个
// 与该段同名的目录 (x)/ 时, 除首段外的所有段都落在 /w/(x) 之下。当时的判据额外要求"扩展路径的
// 公共前缀等于截断根", 于是判据不成立: 原地退回运行时根读对了, 克隆只能按公共前缀 /w/(x) 重定位,
// 把 (x) 段读成根段、把根段读成 repo/ —— 原地对齐, 每个克隆 5 missing + 5 orphan, 两边来回打架。
// 删除 src 的最后一条也会剪出同样的字节(F1b), 所以光在写入端拦不住。判据不再有那一条。
func TestADirectoryNamedLikeTheRootsCutSegmentReadsTheSameEverywhere(t *testing.T) {
	for _, root := range []string{"/w/(x)/repo", "/home/u/(work)/proj", "C:/Users/(me)/repo", "/w/=x/repo", "/w/（全角）/repo", "/w/(x)/(y)/repo"} {
		segments := strings.Split(root, "/")
		cut := ""
		for _, segment := range segments {
			if strings.ContainsAny(segment, "=(（") {
				cut = segment
				break
			}
		}
		rels := []string{"AGENTS.md", cut + "/main.go", cut + "/lib/x.go"}
		text := authoredIndex(t, root, rels...)
		want := append([]string{}, rels...)
		sort.Strings(want)
		for _, at := range []string{root, "/srv/checkout", "D:/ci/work"} {
			if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
				t.Fatalf("created at %q, read at %q: %q, want %q\n%s", root, at, got, want, text)
			}
		}
	}
	// F1b: 根段 + (x)/ + src/, 删掉 src 唯一的条目后剩下同样的字节, 仍须两处一致。
	root := "/w/(x)/repo"
	text := authoredIndex(t, root, "AGENTS.md", "(x)/main.go", "src/a.go")
	pruned, err := RemoveEntryForPath(text, root, "src/a.go", "a.go[CG5T]: F:x | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, pruned, at); !reflect.DeepEqual(got, []string{"(x)/main.go", "AGENTS.md"}) {
			t.Fatalf("after the removal, read at %q: %q\n%s", at, got, pruned)
		}
	}
}

// "每一段都带完整根、且没有根段"的索引不是任何已发布写入器的产物(本写入器总以根段开头, 后续段
// 挂在旧读法读回来的根下)。根路径某段以截断字符开头时, 这组字节与 rc13 在干净根 /w 下写出的字节
// 逐字相同, 纯文本无法区分; 锁定的是两处读法一致, 而不是某一种含义。
func TestBytesSharedWithTheOldWriterReadTheSameEverywhere(t *testing.T) {
	text := "#head\n===/w/(x)/repo/a/===\nf.go[CG5T]: F:f | R:- | A:- | S:-\n\n===/w/(x)/repo/b/===\ng.go[CG5T]: F:g | R:- | A:- | S:-\n"
	origin := resolvedRelPaths(t, text, "/w/(x)/repo")
	for _, at := range []string{"/w", "/srv/checkout", "C:/ci/work"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, origin) {
			t.Fatalf("read at %q: %q, the origin reads %q", at, got, origin)
		}
	}
}

// 同一个段头字节可以出现两次而指向两个目录: 旧写入器的首段按旧读法落在根上, 为它字面上那个目录
// 补登记的段按扩展路径落在目录上。两段里同名的文件是两个不同的路径; Parse 曾按"段头路径+文件名"
// 报重复条目, 而 code_parse_warning 会拒绝此后对这份索引的每一次写入, 仓库永远停在
// authoring_required(五轮评审实测, rc13 也卡在这里)。首段在两种读法不同时单独记键; 真正解析到
// 同一路径的两条仍由对象加载处按路径报告, 普通的同头重复段照旧在这里报警。
func TestTheSameNameUnderTwoReadingsOfOneHeaderIsNotADuplicate(t *testing.T) {
	// rc13, 干净根, 顶层 (home)/ 排在最前, 根上也有 main.go。
	root := "/clean/repo"
	text := "#head\n===/clean/repo/(home)/===\nmain.go[CG5T]: F:root main | R:- | A:- | S:-\n\n" +
		"===/clean/repo/src/===\nq.go[CG5T]: F:q | R:- | A:- | S:-\n"
	out, err := InsertEntry(text, "(home)/main.go", "main.go[CG5T]: F:home main | R:- | A:- | S:-", root)
	if err != nil {
		t.Fatal(err)
	}
	if _, warnings := Parse(out); len(warnings) != 0 {
		t.Fatalf("two paths, not one duplicate: %+v\n%s", warnings, out)
	}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, []string{"(home)/main.go", "main.go", "src/q.go"}) {
			t.Fatalf("read at %q: %q", at, got)
		}
	}
	// 本写入器自己的索引: /w/(x)/repo 下的目录 (x)/repo 与根段同头。
	own := authoredIndex(t, "/w/(x)/repo", "main.go", "src/a.go", "(x)/repo/other.go", "(x)/repo/main.go")
	if _, warnings := Parse(own); len(warnings) != 0 {
		t.Fatalf("this writer's own index must not warn: %+v\n%s", warnings, own)
	}
	// 首段两种读法不同、但旧写入器判据不成立时(旁边有落在根上的普通根段), 首段按扩展路径读,
	// 与后面同头的段就是同一个目录: 同名条目是真重复, 必须报警。单文件(Legacy)布局没有别处会报它
	// (六轮评审: 单独记键后这一形状在 Legacy 下悄无声息, remove-entry 会删掉其中一条)。
	sameDirectory := "#head\n===/clean/repo/app (old)/===\na.go[CG5T]: F:first | R:- | A:- | S:-\n\n" +
		"===/clean/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/clean/repo/app (old)/===\na.go[CG5T]: F:second | R:- | A:- | S:-\n"
	if _, warnings := Parse(sameDirectory); len(warnings) != 1 || !strings.Contains(warnings[0].Msg, "重复条目") {
		t.Fatalf("one directory spelled twice holds a true duplicate: %+v", warnings)
	}
	// 普通的同头重复段里的同名条目仍然报警。
	plain := "#head\n===/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/repo/===\nm.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/repo/src/===\na.go[CG5T]: F:again | R:- | A:- | S:-\n"
	if _, warnings := Parse(plain); len(warnings) != 1 || !strings.Contains(warnings[0].Msg, "重复条目") {
		t.Fatalf("a true duplicate must still be reported: %+v", warnings)
	}
}

func TestOldWriterIndexUnderACleanRootReadsAsTheOldReaderDidEverywhere(t *testing.T) {
	// rc13 在干净根下、默认 scope: "(" 排在 ".gitattributes" 之前, 首段写成 ===<根>/(home)/===,
	// 读回来是根, 于是根文件都登记在这一段里, (home)/p.go 自己被解析成 p.go(orphan + missing)。
	root := "/clean/repo"
	text := "#head\n===/clean/repo/(home)/===\np.go[CG5T]: F:p | R:- | A:- | S:-\n" +
		".gitattributes[CG5T]: F:g | R:- | A:- | S:-\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/clean/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	want := []string{".gitattributes", "main.go", "p.go", "src/a.go"}
	for _, at := range []string{root, "C:/clean/repo", "/srv/checkout"} {
		if got := resolvedRelPaths(t, text, at); !reflect.DeepEqual(got, want) {
			t.Fatalf("read at %q: %q, want the old reader's view %q", at, got, want)
		}
	}
	// 普通修复: 移除误解析出来的 p.go, 登记真正的 (home)/p.go; 两处同步收敛。
	repaired, err := RemoveEntryForPath(text, root, "p.go", "p.go[CG5T]: F:p | R:- | A:- | S:-")
	if err != nil {
		t.Fatal(err)
	}
	if repaired, err = InsertEntry(repaired, "(home)/p.go", "p.go[CG5T]: F:page | R:- | A:- | S:-", root); err != nil {
		t.Fatal(err)
	}
	healed := []string{"(home)/p.go", ".gitattributes", "main.go", "src/a.go"}
	for _, at := range []string{root, "/srv/checkout"} {
		if got := resolvedRelPaths(t, repaired, at); !reflect.DeepEqual(got, healed) {
			t.Fatalf("after the repair, read at %q: %q, want %q\n%s", at, got, healed, repaired)
		}
	}
}

// 根段是整份索引坐标的锚: 重定位按各段的公共前缀求根, 只有某个段正好落在根上时这个前缀才是根。
// 四轮评审实测: 根写不成段头的仓库里, 空根段被下一次删除顺手剪掉, 原地随即把活着的条目报成
// orphan(a.go、b.go), 只有全部删掉重写才能恢复。只要还有别的目录段, 根段即使空了也不剪。
func TestTheRootSectionIsNeverPrunedWhileOtherSectionsRemain(t *testing.T) {
	for _, root := range []string{"/home/u/s /repo", "/clean/repo", "/tmp/example project/repo", "/w/(x)/repo"} {
		text := authoredIndex(t, root, "src/a.go", "src/b.go", "src/c.go")
		out, err := RemoveEntryForPath(text, root, "src/c.go", "c.go[CG5T]: F:x | R:- | A:- | S:-")
		if err != nil {
			t.Fatal(err)
		}
		for _, at := range []string{root, "/srv/checkout"} {
			if got := resolvedRelPaths(t, out, at); !reflect.DeepEqual(got, []string{"src/a.go", "src/b.go"}) {
				t.Fatalf("root %q read at %q after a removal: %q\n%s", root, at, got, out)
			}
		}
	}
	// 旧写入器的首段(完整拼写)同样是锚: 根文件删光后不剪, 截断根下的段族仍然解析。
	out, err := RemoveEntryForPath(truncatedRootIndex, "/tmp/example project/repo", "main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-")
	if err != nil || !strings.Contains(out, "===/tmp/example project/repo/===\n") {
		t.Fatalf("the old writer's first section anchors its family and must stay: %v\n%s", err, out)
	}
	// 不是锚的空段照常剪掉; 最后一个目录段空了也照常剪掉。
	plain := "#head\n===/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n===/repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n"
	if out, err := RemoveEntryForPath(plain, "/repo", "src/a.go", "a.go[CG5T]: F:a | R:- | A:- | S:-"); err != nil || strings.Contains(out, "/repo/src/") {
		t.Fatalf("an empty section that anchors nothing is pruned: %v\n%s", err, out)
	}
	only := "#head\n===/repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n"
	if out, err := RemoveEntryForPath(only, "/repo", "main.go", "main.go[CG5T]: F:m | R:- | A:- | S:-"); err != nil || strings.Contains(out, "===") {
		t.Fatalf("the last section is pruned when it empties: %v\n%s", err, out)
	}
}

func TestIndexWrittenWithTruncatedRootStillRelocates(t *testing.T) {
	// 同一份索引被克隆到别处: 今天靠"全部截断成同一个根"碰巧成立, 扩展读法下首段变成完整根,
	// 必须仍然成立。
	doc := buildDoc(t, truncatedRootIndex)
	ResolveRelPaths(doc, "/srv/checkout")
	for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
		if FindEntry(doc, rel) == nil {
			t.Fatalf("%s must survive relocation; got %q %q %q", rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
		}
	}
}

func TestNewStyleIndexUnderSpecialRootRelocates(t *testing.T) {
	text := "#head\n" +
		"===/home/a/my repo/===\nmain.go[CG5T]: F:m | R:- | A:- | S:-\n\n" +
		"===/home/a/my repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n" +
		"===/home/a/my repo/src/deep dir/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
	for _, root := range []string{"/home/a/my repo", "/srv/checkout", "C:/Users/John Doe/work"} {
		doc := buildDoc(t, text)
		ResolveRelPaths(doc, root)
		for _, rel := range []string{"main.go", "src/a.go", "src/deep dir/b.go"} {
			if FindEntry(doc, rel) == nil {
				t.Fatalf("root %q: %s must resolve; got %q %q %q", root, rel, relOf(t, doc, 0, 0), relOf(t, doc, 1, 0), relOf(t, doc, 2, 0))
			}
		}
	}
	// 没有根段的新式索引不能被旧读法压扁到同一个目录
	noRoot := "#head\n===/home/a/my repo/src/===\na.go[CG5T]: F:a | R:- | A:- | S:-\n\n===/home/a/my repo/lib/===\nb.go[CG5T]: F:b | R:- | A:- | S:-\n"
	doc := buildDoc(t, noRoot)
	ResolveRelPaths(doc, "/srv/checkout")
	if FindEntry(doc, "src/a.go") == nil || FindEntry(doc, "lib/b.go") == nil {
		t.Fatalf("sections without a root section must keep their own directories: %q %q", relOf(t, doc, 0, 0), relOf(t, doc, 1, 0))
	}
}

func TestInsertEntryRefusesADirectoryTheHeaderCannotSpell(t *testing.T) {
	text := "#head\n===/repo/===\nmain.go[CG5T]: F:x | R:- | A:- | S:-\n"
	for _, rel := range []string{" lead/a.go", "trail /a.go"} {
		out, err := InsertEntry(text, rel, "a.go[CG5T]: F:a | R:- | A:- | S:-", "/repo")
		if err == nil {
			t.Fatalf("%q would be written as a header the next read resolves elsewhere:\n%s", rel, out)
		}
	}
}
