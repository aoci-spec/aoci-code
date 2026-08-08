// UTF-8 BOM 宽进/治愈测试(P-18)
// 索引条目: bom_test.go(待补录,随本批入册)
//
// 病灶: 首行带 \ufeff 时 ^=== 锚定正则零匹配 —— 首行段头的索引整段沦为
// 头部、条目全灭;修法为 Parse/ExtractHeader/ReplaceHeader 三入口剥 BOM。
//
// AbsPath 期望值纪律(首版红灯教训): Parse 存 AbsPath 为正则捕获原文的
// TrimSpace 形态 —— 段头 ===代码/repo/=== 捕到 "/repo/" 带尾斜杠(路径 token
// 遇 = 即止,尾斜杠在捕获内);剥尾斜杠归 normalizeRootPath 的解析侧职责,
// 存储层不做。断言必须对齐存储层真实语义,不凭直觉写归一后形态。
package index

import (
	"strings"
	"testing"
)

// bomMark UTF-8 BOM 字符(测试专用,带 bom 前缀防同包撞名)
const bomMark = "\ufeff"

// TestParseBOMFirstLineSection 首行即目录段头且带 BOM: 段与条目不得丢失
// (P-18 病灶本体: BOM 未剥离时该索引解析结果为零目录段)
func TestParseBOMFirstLineSection(t *testing.T) {
	text := bomMark + "===代码/repo/===\na.go[X.Y.5.T]: F:功 | R:- | A:- | S:-\n"
	doc, _ := Parse(text)
	if len(doc.Sections) != 1 {
		t.Fatalf("带BOM首行段头应识别出恰好1个目录段: %+v", doc.Sections)
	}
	if got := doc.Sections[0].AbsPath; got != "/repo/" {
		t.Fatalf("AbsPath 应为捕获原文带尾斜杠形态 /repo/,得 %q", got)
	}
	if len(doc.Sections[0].Entries) != 1 || doc.Sections[0].Entries[0].Filename != "a.go" {
		t.Fatalf("段内条目应收集到 a.go: %+v", doc.Sections[0].Entries)
	}
	if strings.Contains(doc.RawText, bomMark) {
		t.Fatal("RawText 不应残留 BOM")
	}
}

// TestParseBOMHeaderLine 首行为#注释且带 BOM: 头部行剥净 BOM 后原样保留
func TestParseBOMHeaderLine(t *testing.T) {
	text := bomMark + "#头部说明\n===代码/repo/===\n"
	doc, _ := Parse(text)
	if len(doc.HeaderLines) != 1 || doc.HeaderLines[0] != "#头部说明" {
		t.Fatalf("头部行应剥净BOM且内容不变: %q", doc.HeaderLines)
	}
}

// TestParseNoBOMByteIdentical 无 BOM 的 LF 输入零行为变化(幂等防误伤)
func TestParseNoBOMByteIdentical(t *testing.T) {
	text := "#头部\n===代码/repo/===\na.go[X.Y.5.T]: F:功 | R:- | A:- | S:-\n"
	doc, _ := Parse(text)
	if doc.RawText != text {
		t.Fatalf("无BOM输入 RawText 应逐字节保持: %q", doc.RawText)
	}
}

// TestExtractHeaderBOM ExtractHeader 与 Parse 同口径: 带 BOM 的首行段头
// 不被误归头部(头部为空且 hasSections=true)
func TestExtractHeaderBOM(t *testing.T) {
	text := bomMark + "===代码/repo/===\na.go[X.Y.5.T]: F:- | R:- | A:- | S:-\n"
	header, hasSections := ExtractHeader(text)
	if header != "" || !hasSections {
		t.Fatalf("带BOM首行段头: 期望头部空且hasSections=true,得 header=%q hasSections=%v", header, hasSections)
	}
}

// TestReplaceHeaderHealsBOM 写侧治愈: 替换头部后输出不携带 BOM,
// 且首个目录段未被吞(BOM 在场时旧边界判定会把段头误归头部整体替换掉)
func TestReplaceHeaderHealsBOM(t *testing.T) {
	text := bomMark + "#旧头\n===代码/repo/===\na.go[X.Y.5.T]: F:- | R:- | A:- | S:-\n"
	out, err := ReplaceHeader(text, "#新头")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, bomMark) {
		t.Fatal("输出不应携带 BOM(写侧治愈)")
	}
	if !strings.HasPrefix(out, "#新头\n===代码/repo/===\n") {
		t.Fatalf("头部替换后条目区应原样保留: %q", out)
	}
	if !strings.Contains(out, "a.go[X.Y.5.T]") {
		t.Fatalf("条目行不得丢失: %q", out)
	}
}
