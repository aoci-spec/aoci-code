// 解析器表驱动测试
// 索引条目: parser_test.go[TPS9TM]
package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture 读取仓库 testdata 夹具
func readFixture(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatalf("读取夹具失败 %s: %v", rel, err)
	}
	return string(data)
}

// TestParsePlatformStyle 平台紧凑标签样例: 元协议主张的测试化
func TestParsePlatformStyle(t *testing.T) {
	doc, warns := Parse(readFixture(t, "indexes/platform_style.txt"))
	if len(warns) != 0 {
		t.Fatalf("平台样例不应有警告,得到: %v", warns)
	}
	ResolveRelPaths(doc, "/opt/aoci-platform")

	e := FindEntry(doc, "internal/handler/project_source.go")
	if e == nil {
		t.Fatal("未命中 project_source.go")
	}
	// 紧凑标签 WA9JM → A=W B=A C=9 D=J E=M
	for k, want := range map[string]string{"A": "W", "B": "A", "C": "9", "D": "J", "E": "M"} {
		if got := e.TagsParsed[k]; got != want {
			t.Errorf("紧凑标签维度 %s: 期望 %q 得到 %q", k, want, got)
		}
	}
	// S 字段含 |[]<> 混排且不被 " | " 误切(无空格包围的 | 属正文)
	if !strings.Contains(e.S, "|[]<>") {
		t.Errorf("S 字段混排字符丢失: %q", e.S)
	}
	// S1/S2 变体合并进 S
	e2 := FindEntry(doc, "internal/handler/websocket_term.go")
	if e2 == nil || !strings.Contains(e2.S, "第一段") || !strings.Contains(e2.S, "第二段") {
		t.Errorf("S1/S2 变体未合并: %+v", e2)
	}
}

// TestParseCliStyle 点分标签/无标签容忍/带目录前缀文件名
func TestParseCliStyle(t *testing.T) {
	doc, _ := Parse(readFixture(t, "indexes/cli_style.txt"))
	ResolveRelPaths(doc, "/repo")

	if e := FindEntry(doc, "internal/index/parser.go"); e == nil || e.TagsParsed["D"] != "Rgx" {
		t.Fatalf("点分五段标签解析失败: %+v", e)
	}
	if e := FindEntry(doc, "internal/index/notag.go"); e == nil || len(e.TagsParsed) != 0 {
		t.Fatalf("无标签条目应容忍且 TagsParsed 为空: %+v", e)
	}
	if e := FindEntry(doc, "scripts/tool.sh"); e == nil {
		t.Fatal("带目录前缀的文件名 rel 换算失败")
	}
}

// TestParseCRLFAndHeader CRLF 折叠 + 头部示例行不误采
func TestParseCRLFAndHeader(t *testing.T) {
	text := "====头====\r\n===头部索引===\r\n文件名[标签]: F:头部格式示例不应成为条目\r\n===头部索引完毕===\r\n===段 /r/x/===\r\na.go[T.T.5.T]: F:甲 | R:- | A:- | S:-\r\n====完====\r\n"
	doc, _ := Parse(text)
	ResolveRelPaths(doc, "/r")
	total := 0
	for _, s := range doc.Sections {
		total += len(s.Entries)
	}
	if total != 1 {
		t.Fatalf("应只采集目录段内 1 条(头部示例行不采),得到 %d", total)
	}
	if FindEntry(doc, "x/a.go") == nil {
		t.Fatal("CRLF 输入下条目未命中")
	}
}

// TestParseDuplicateWarning 同段同名重复应出警告
func TestParseDuplicateWarning(t *testing.T) {
	text := "===段 /r/x/===\na.go[T.T.5.T]: F:1 | R:- | A:- | S:-\na.go[T.T.5.T]: F:2 | R:- | A:- | S:-\n"
	_, warns := Parse(text)
	if len(warns) != 1 || !strings.Contains(warns[0].Msg, "重复条目") {
		t.Fatalf("应产生 1 条重复警告,得到: %v", warns)
	}
}

// TestFindSectionExact 精确目录匹配: 根段不吞新目录;根文件命中根段
func TestFindSectionExact(t *testing.T) {
	text := "===根 /r/===\nroot.txt[T.T.5.T]: F:- | R:- | A:- | S:-\n===子 /r/sub/===\ns.go[T.T.5.T]: F:- | R:- | A:- | S:-\n"
	doc, _ := Parse(text)
	ResolveRelPaths(doc, "/r")
	if sec := FindSectionForPath(doc, "/r", "newdir/x.go"); sec != nil {
		t.Fatalf("新目录不应命中任何段(根段不得通配),命中了 %q", sec.AbsPath)
	}
	if sec := FindSectionForPath(doc, "/r", "NOTICE"); sec == nil || sec.AbsPath != "/r/" {
		t.Fatal("根文件应命中根段")
	}
	if sec := FindSectionForPath(doc, "/r", "sub/s.go"); sec == nil || sec.AbsPath != "/r/sub/" {
		t.Fatal("子目录文件应命中子段")
	}
}

// TestParseWindowsDrivePaths Windows 盘符形态段头(真机教训: C:/repo 不以 / 开头,
// 原正则零目录段致骨架不可用;修复后段头/换算/定位/插入全链路须认盘符形态)
func TestParseWindowsDrivePaths(t *testing.T) {
	text := "#===头部===\n===C:/aoci-test/===\nmain.go[XC9T]: F:入口 | R:- | A:- | S:-\n===子 C:/aoci-test/src/===\nutil.go[XC5T]: F:工具 | R:- | A:- | S:-\n"
	doc, warns := Parse(text)
	if len(warns) != 0 {
		t.Fatalf("盘符样例不应有警告: %v", warns)
	}
	// 目录段应识别为 2 个
	dirs := 0
	for _, s := range doc.Sections {
		if s.AbsPath != "" {
			dirs++
		}
	}
	if dirs != 2 {
		t.Fatalf("应识别 2 个盘符目录段,得到 %d", dirs)
	}
	// rel 换算: root 用反斜杠+小写盘符形态,验证归一互认
	ResolveRelPaths(doc, `c:\aoci-test`)
	if FindEntry(doc, "main.go") == nil {
		t.Fatal("盘符根段条目 rel 换算失败(大小写/斜杠归一未生效)")
	}
	if FindEntry(doc, "src/util.go") == nil {
		t.Fatal("盘符子段条目 rel 换算失败")
	}
	// 段定位: 根与子段均可命中
	if sec := FindSectionForPath(doc, `C:\aoci-test`, "main.go"); sec == nil {
		t.Fatal("盘符根段定位失败")
	}
	if sec := FindSectionForPath(doc, "C:/aoci-test", "src/x.go"); sec == nil || sec.AbsPath != "C:/aoci-test/src/" {
		t.Fatal("盘符子段定位失败")
	}
	// 插入: 盘符形态下段内插入可用(骨架首条补录的真实场景)
	out, err := InsertEntry(text, "src/new.go", "new.go[XC5T]: F:新 | R:- | A:- | S:-", "C:/aoci-test")
	if err != nil || !strings.Contains(out, "new.go[XC5T]") {
		t.Fatalf("盘符形态插入失败: %v", err)
	}
	// POSIX 行为零变化交叉确认
	if !consistencyDirRe.MatchString("===段 /opt/x/===") {
		t.Fatal("POSIX 段头匹配被破坏")
	}
}
