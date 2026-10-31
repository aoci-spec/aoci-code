// Header-area primitive tests: extraction boundary / replacement fidelity / structural hard refusal (two rules) / newline style preservation / LCS diff
// Index entry: header_test.go[THD8TS]
package index

import (
	"strings"
	"testing"
)

// An ordinary document: two header lines plus one directory section
const hdrDocLF = "#头部规范第一行\n#标签字典第二行\n===测试段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-\n"

func TestExtractHeaderNormal(t *testing.T) {
	h, has := ExtractHeader(hdrDocLF)
	if !has {
		t.Fatal("常规文档应报告 hasSections=true")
	}
	if h != "#头部规范第一行\n#标签字典第二行" {
		t.Fatalf("头部提取不符: %q", h)
	}
}

func TestExtractHeaderZeroSection(t *testing.T) {
	text := "#只有头部\n#没有任何段\n"
	h, has := ExtractHeader(text)
	if has {
		t.Fatal("零段文档应报告 hasSections=false")
	}
	if h != "#只有头部\n#没有任何段" {
		t.Fatalf("零段文档头部应为全文: %q", h)
	}
}

func TestExtractHeaderSeparatorAlsoBounds(t *testing.T) {
	// A separator (=== without a path) ends the header area too, the same source as Parse's inHeader flip
	text := "#头部\n===分隔符===\n#分隔符后的行不属于头部\n"
	h, has := ExtractHeader(text)
	if !has {
		t.Fatal("含分隔符的文档应报告 hasSections=true")
	}
	if h != "#头部" {
		t.Fatalf("分隔符应终结头部: %q", h)
	}
}

func TestExtractHeaderToleratesBareLegacyLines(t *testing.T) {
	// A historical index header may hold bare text lines (the spec compatibility form): extraction tolerates them as they are, and validation binds only a new header
	text := "裸的历史头部行\n#正常行\n===段/opt/x/===\n"
	h, _ := ExtractHeader(text)
	if h != "裸的历史头部行\n#正常行" {
		t.Fatalf("历史裸头部行应原样提取: %q", h)
	}
}

func TestReplaceHeaderBodyUntouched(t *testing.T) {
	out, err := ReplaceHeader(hdrDocLF, "#新头部A\n#新头部B\n#新头部C")
	if err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	want := "#新头部A\n#新头部B\n#新头部C\n===测试段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-\n"
	if out != want {
		t.Fatalf("替换结果不符:\n得到: %q\n期望: %q", out, want)
	}
	if !strings.HasSuffix(out, "===测试段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-\n") {
		t.Fatal("条目区被改动")
	}
}

func TestReplaceHeaderIdempotent(t *testing.T) {
	h, _ := ExtractHeader(hdrDocLF)
	out, err := ReplaceHeader(hdrDocLF, h)
	if err != nil {
		t.Fatalf("同头部替换失败: %v", err)
	}
	if out != hdrDocLF {
		t.Fatalf("同头部替换应逐字节幂等:\n得到: %q", out)
	}
}

func TestReplaceHeaderPreservesCRLF(t *testing.T) {
	crlfDoc := "#旧头\r\n===段/opt/x/===\r\nf.go[XC1T]: F:x | R:- | A:- | S:-\r\n"
	out, err := ReplaceHeader(crlfDoc, "#新头1\n#新头2")
	if err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	want := "#新头1\r\n#新头2\r\n===段/opt/x/===\r\nf.go[XC1T]: F:x | R:- | A:- | S:-\r\n"
	if out != want {
		t.Fatalf("CRLF 保留失败:\n得到: %q\n期望: %q", out, want)
	}
}

func TestReplaceHeaderPreservesNoTrailingNewline(t *testing.T) {
	doc := "#旧头\n===段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-" // 无末尾换行
	out, err := ReplaceHeader(doc, "#新头")
	if err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatal("原文无末尾换行,输出不得追加")
	}
	if out != "#新头\n===段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-" {
		t.Fatalf("结果不符: %q", out)
	}
}

func TestReplaceHeaderRejectsSectionLines(t *testing.T) {
	// Both the directory-header shape and the separator shape must be hard-refused (rule one)
	for _, bad := range []string{
		"#合法行\n===偷渡目录段/opt/evil/===",
		"#合法行\n======",
	} {
		if _, err := ReplaceHeader(hdrDocLF, bad); err == nil {
			t.Fatalf("含 === 行的新头部应被硬拒: %q", bad)
		}
	}
}

func TestReplaceHeaderRejectsBareLines(t *testing.T) {
	// Rule two: a non-empty line must start with #, catching prose lines and entry-shaped lines alike
	for _, bad := range []string{
		"#合法行\n这是模型输出的解释性散文",
		"#合法行\nf.go[XC1T]: F:偷渡条目 | R:- | A:- | S:-",
	} {
		if _, err := ReplaceHeader(hdrDocLF, bad); err == nil {
			t.Fatalf("含非#非空行的新头部应被硬拒: %q", bad)
		}
	}
}

func TestReplaceHeaderZeroSectionDoc(t *testing.T) {
	text := "#旧头全文\n"
	out, err := ReplaceHeader(text, "#新头全文")
	if err != nil {
		t.Fatalf("零段文档替换失败: %v", err)
	}
	if out != "#新头全文\n" {
		t.Fatalf("零段文档应整体替换并保留末尾换行: %q", out)
	}
}

func TestReplaceHeaderEmptyNewHeader(t *testing.T) {
	out, err := ReplaceHeader(hdrDocLF, "")
	if err != nil {
		t.Fatalf("空头部结构上合法,不应报错: %v", err)
	}
	if out != "===测试段/opt/x/===\nf.go[XC1T]: F:x | R:- | A:- | S:-\n" {
		t.Fatalf("空头部替换后应只剩条目区: %q", out)
	}
}

func TestValidateHeaderTextLineNumber(t *testing.T) {
	ln, msg := ValidateHeaderText("#第一行\n#第二行\n===x/opt/y/===\n#第四行")
	if ln != 3 || msg == "" {
		t.Fatalf("应定位到第 3 行违规,得到 ln=%d msg=%q", ln, msg)
	}
	if ln, _ := ValidateHeaderText("#全部合法\n#普通行"); ln != 0 {
		t.Fatalf("合法头部不应报违规,得到 ln=%d", ln)
	}
}

func TestValidateHeaderTextBareLineAndBlank(t *testing.T) {
	// A bare text line is located by line number with a hint to start with #; a blank line is fully legal
	ln, msg := ValidateHeaderText("#合法\n裸文本行\n#又合法")
	if ln != 2 || !strings.Contains(msg, "#开头") {
		t.Fatalf("裸文本行应定位第2行并提示#规则,得到 ln=%d msg=%q", ln, msg)
	}
	if ln, _ := ValidateHeaderText("#a\n\n#b\n   \n#c"); ln != 0 {
		t.Fatalf("空行与纯空白行应合法,得到 ln=%d", ln)
	}
}
