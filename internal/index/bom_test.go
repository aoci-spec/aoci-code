// UTF-8 BOM lenient-read/heal tests (P-18)
// Index entry: bom_test.go (to be recorded, entering the index with this batch)
//
// The defect: with a \ufeff on the first line the ^=== anchored regular expression matched nothing; an index whose first line is a section header collapsed entirely into
// the header and lost every entry. The fix strips the BOM at the three entries Parse/ExtractHeader/ReplaceHeader.
//
// AbsPath expectation discipline (the first version's red-light lesson): Parse stores AbsPath as the TrimSpace form of the regular expression's
// raw capture; the header ===代码/repo/=== captures "/repo/" with its trailing slash (the path token stops
// at =, and the trailing slash is inside the capture); stripping the trailing slash is the parsing-side job of normalizeRootPath, which
// the storage layer does not do. Assertions must align with the storage layer's real semantics rather than writing the normalized form by intuition.
package index

import (
	"strings"
	"testing"
)

// bomMark is the UTF-8 BOM character (test only, with the bom prefix against same-package collisions)
const bomMark = "\ufeff"

// TestParseBOMFirstLineSection: the first line is a directory section header and carries a BOM; the section and its entries must not be lost
// (the P-18 defect itself: with the BOM unstripped this index parsed to zero directory sections)
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

// TestParseBOMHeaderLine: the first line is a # comment carrying a BOM; the header line is kept as it is once the BOM is stripped
func TestParseBOMHeaderLine(t *testing.T) {
	text := bomMark + "#头部说明\n===代码/repo/===\n"
	doc, _ := Parse(text)
	if len(doc.HeaderLines) != 1 || doc.HeaderLines[0] != "#头部说明" {
		t.Fatalf("头部行应剥净BOM且内容不变: %q", doc.HeaderLines)
	}
}

// TestParseNoBOMByteIdentical: LF input without a BOM changes nothing (idempotence against collateral damage)
func TestParseNoBOMByteIdentical(t *testing.T) {
	text := "#头部\n===代码/repo/===\na.go[X.Y.5.T]: F:功 | R:- | A:- | S:-\n"
	doc, _ := Parse(text)
	if doc.RawText != text {
		t.Fatalf("无BOM输入 RawText 应逐字节保持: %q", doc.RawText)
	}
}

// TestExtractHeaderBOM: ExtractHeader shares Parse's reading; a BOM-carrying first-line section header
// is not filed under the header by mistake (the header is empty and hasSections=true)
func TestExtractHeaderBOM(t *testing.T) {
	text := bomMark + "===代码/repo/===\na.go[X.Y.5.T]: F:- | R:- | A:- | S:-\n"
	header, hasSections := ExtractHeader(text)
	if header != "" || !hasSections {
		t.Fatalf("带BOM首行段头: 期望头部空且hasSections=true,得 header=%q hasSections=%v", header, hasSections)
	}
}

// TestReplaceHeaderHealsBOM, the write-side heal: after the header is replaced the output carries no BOM,
// and the first directory section is not swallowed (with a BOM present the old boundary judgement filed the section header under the header and replaced it wholesale)
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
