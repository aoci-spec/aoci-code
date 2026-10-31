// Dictionary extraction and validation tests
// Index entry: dict_test.go[TVL8TT]
package index

import (
	"strings"
	"testing"
)

const dictHeader = "#A层级: X-Script(脚本) M-Model(存储) D-Doc(文档) C-Config(配置)\n" +
	"#B模块: Srv-服务入口 Store-存储层 Doc-文档 Meta-项目元信息\n" +
	"#E规模: L大(>300行) M中(100-300行) S小(30-100行) T微(<30行)\n"

func TestExtractTagDictForms(t *testing.T) {
	// Three existing forms: hyphen-separated / symbol run into Chinese / multi-letter symbol
	d := ExtractTagDict("#A层级: X-Script I索引核心 N-LLM适配\n#B模块: AI-AI增强 Srv-入口\n")
	for _, sym := range []string{"X", "I", "N"} {
		if !d.A[sym] {
			t.Fatalf("A 字典应含 %s: %+v", sym, d.A)
		}
	}
	for _, sym := range []string{"AI", "Srv"} {
		if !d.B[sym] {
			t.Fatalf("B 字典应含 %s: %+v", sym, d.B)
		}
	}
	// A missing E line falls back to LMST
	if !d.E["L"] || !d.E["T"] || len(d.E) != 4 {
		t.Fatalf("E 缺失应回退 LMST: %+v", d.E)
	}
}

// TestExtractTagDictVariantForms, dictionary line form tolerance (P-21, the httpx-rerun live run):
// the AI-drafted header "#A层级(python,架构角色): I-接口(...)" put a note between the dimension name and the colon,
// the old hard prefix match extracted nothing → HasDict false → maintain judged no dictionary established and the dictionary gate went silently false green.
// All three variants locked: a note on the dimension name (the real shape) / a space after # / a full-width colon.
func TestExtractTagDictVariantForms(t *testing.T) {
	// The real shape (taken from the httpx-rerun header bytes; the note holds spaces and an example path)
	real := "#A层级(python,架构角色): I-接口(公开API/命令行入口,如 httpx/__init__.py、httpx/_api.py) C-核心(客户端与数据模型主干实现,如 httpx/_client.py) X-测试(tests/**)\n" +
		"#B模块(业务域): Api-顶层API Cli-客户端 Tra-传输层\n"
	d := ExtractTagDict(real)
	if !d.HasDict() {
		t.Fatalf("实物夹注形态应可提取字典: A=%+v B=%+v", d.A, d.B)
	}
	for _, sym := range []string{"I", "C", "X"} {
		if !d.A[sym] {
			t.Fatalf("A 字典应含 %s: %+v", sym, d.A)
		}
	}
	for _, sym := range []string{"Api", "Cli", "Tra"} {
		if !d.B[sym] {
			t.Fatalf("B 字典应含 %s: %+v", sym, d.B)
		}
	}
	// Symbol pollution defence: example path words inside the note (httpx/...) never enter the dictionary
	if d.A["httpx"] {
		t.Fatalf("夹注内路径词不得污染字典: %+v", d.A)
	}

	// The space-after-# variant
	d2 := ExtractTagDict("# A层级: X-x\n# B模块: Y-y\n")
	if !d2.A["X"] || !d2.B["Y"] {
		t.Fatalf("#后空格形态应可提取: A=%+v B=%+v", d2.A, d2.B)
	}

	// The full-width-colon variant
	d3 := ExtractTagDict("#A层级：X-x\n#B模块：Y-y\n")
	if !d3.A["X"] || !d3.B["Y"] {
		t.Fatalf("全角冒号形态应可提取: A=%+v B=%+v", d3.A, d3.B)
	}

	// Exact equality against collateral damage: an explanatory line with a colon whose dimension name differs must not match
	d4 := ExtractTagDict("#三分法A层级说明: 这不是字典行\n#关于B模块: 也不是\n")
	if len(d4.A) != 0 || len(d4.B) != 0 {
		t.Fatalf("非字典说明行不得误提取: A=%+v B=%+v", d4.A, d4.B)
	}
}

func TestExtractTagDictUnusable(t *testing.T) {
	d := ExtractTagDict("#只有普通头部行\n")
	if d.HasDict() {
		t.Fatal("无字典行应判不可用")
	}
	if CheckTagsAgainstDict("f.go[ZZ9Q]: F:x | R:- | A:- | S:-", d) != nil {
		t.Fatal("字典不可用时不应产生违规")
	}
}

func TestCheckTagsAgainstDict(t *testing.T) {
	d := ExtractTagDict(dictHeader)
	cases := []struct {
		name string
		line string
		warn string // empty = should conform; non-empty = the violation message must hold this substring
	}{
		{"合规紧凑", "main.go[XSrv8T]: F:x | R:- | A:- | S:-", ""},
		{"合规多字母B", "store.go[MStore9S]: F:x | R:- | A:- | S:-", ""},
		{"E位非法_实弹DC7Meta", "AGENTS.md[DC7Meta]: F:x | R:- | A:- | S:-", "E规模符号a非法"},
		{"B不在字典", "x.go[XBogus5T]: F:x | R:- | A:- | S:-", "B模块符号Bogus不在字典"},
		{"A不在字典", "x.go[QSrv5T]: F:x | R:- | A:- | S:-", "A层级符号Q不在字典"},
		{"非标降级不判", "x.go[无效标签]: F:x | R:- | A:- | S:-", ""},
		{"点分合规", "x.go[X.Srv.9.T]: F:x | R:- | A:- | S:-", ""},
		// Cross-dimension misplacement exposed (P-14, the httpx live-run root cause: most of the 10 dict refusals were symbols in the wrong dimension, not real inventions):
		// the symbol M exists in the A dictionary (M-Model) and the E dictionary (M中) but not in B; used in the B slot it should be named as a probable misplacement
		{"跨维错位_A符号用在B位", "x.go[XM5T]: F:x | R:- | A:- | S:-", "疑似维度错位"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := CheckTagsAgainstDict(c.line, d)
			if c.warn == "" {
				if v != nil {
					t.Fatalf("不应违规: %v", v.Msg)
				}
				return
			}
			if v == nil {
				t.Fatal("应产生字典违规")
			}
			if v.Level != LevelWarning {
				t.Fatalf("须为Warning级: %v", v.Level)
			}
			if !strings.Contains(v.Msg, c.warn) {
				t.Fatalf("违规消息应含 %q: %q", c.warn, v.Msg)
			}
		})
	}
}

// TestCrossDimHintAbsentForTrueBogus, the true-invention negative case (the boundary of the P-14 fix):
// when the symbol exists in no dimension's dictionary, the wording keeps its "not in the dictionary" meaning and never shows a misplacement hint
// (the hint fires only when the symbol really exists in another dimension, so a true invention never gets a misleading attribution).
func TestCrossDimHintAbsentForTrueBogus(t *testing.T) {
	d := ExtractTagDict(dictHeader)
	v := CheckTagsAgainstDict("x.go[XBogus5T]: F:x | R:- | A:- | S:-", d)
	if v == nil {
		t.Fatal("应产生字典违规")
	}
	if !strings.Contains(v.Msg, "Bogus不在字典") {
		t.Fatalf("应维持不在字典原语义: %q", v.Msg)
	}
	if strings.Contains(v.Msg, "维度错位") {
		t.Fatalf("真臆造符号不应出现维度错位提示: %q", v.Msg)
	}
}

func TestScopedVolumeTagDictionaryContractCoversEveryAxis(t *testing.T) {
	meta := "#[Tag dictionary: code]\n" +
		"#A Layer: C-Code\n#B Module: G-General\n" +
		"#C Importance: 9-core 8-high 7-business 5-routine 3-support 1-edge\n" +
		"#E Scale: L-large M-medium S-small T-tiny\n" +
		"#[Tag dictionary: database]\n#A Layer: D-Database\n#B Module: S-Schema\n" +
		"#C Importance: 9-core 8-high 7-business 5-routine 3-support 1-edge\n" +
		"#E Scale: L-large M-medium S-small T-tiny\n"
	dictionary := ExtractScopedTagDict(meta, "code")
	if dictionary == nil || !dictionary.HasObjectContract() {
		t.Fatalf("official scoped dictionary is not a complete object contract: %#v", dictionary)
	}
	if got := ValidateTagAgainstDict(ParseTags("CG7T"), "CG7T", dictionary); len(got) != 0 {
		t.Fatalf("canonical compact tag was rejected: %#v", got)
	}
	if definition, ok := dictionary.Definition("A", "C"); !ok || definition != "-Code" {
		t.Fatalf("scoped dictionary lost the exact A definition: definition=%q ok=%t", definition, ok)
	}
	if _, ok := dictionary.Definition("B", "missing"); ok {
		t.Fatal("scoped dictionary fabricated a missing symbol definition")
	}
	for _, test := range []struct {
		tag  string
		axis string
	}{{"XG7T", "A"}, {"CX7T", "B"}, {"CG6T", "C"}, {"CG7XT", "D"}, {"CG7Q", "E"}} {
		violations := ValidateTagAgainstDict(ParseTags(test.tag), test.tag, dictionary)
		if len(violations) != 1 || violations[0].Axis != test.axis ||
			!strings.Contains(violations[0].Expected, "C=1,3,5,7,8,9") {
			t.Fatalf("tag %s did not produce the exact %s-axis decision: %#v", test.tag, test.axis, violations)
		}
	}
}

func TestScopedVolumeTagDictionaryRejectsMissingMalformedAndConflict(t *testing.T) {
	if got := ExtractScopedTagDict("#[Tag dictionary: database]\n#A Layer: D-Database\n", "code"); got != nil {
		t.Fatalf("missing scoped dictionary was fabricated: %#v", got)
	}
	for name, meta := range map[string]string{
		"malformed": "#[Tag dictionary: code]\n#A Layer: C-Code\n#B Module: G-General\n#C Importance: none\n#E Scale: T-tiny\n",
		"conflict":  "#[Tag dictionary: code]\n#A Layer: C-Code C-Config\n#B Module: G-General\n#C Importance: 7-business\n#E Scale: T-tiny\n",
	} {
		t.Run(name, func(t *testing.T) {
			dictionary := ExtractScopedTagDict(meta, "code")
			if dictionary == nil || dictionary.HasObjectContract() || len(dictionary.ObjectContractProblems()) == 0 {
				t.Fatalf("%s dictionary was accepted: %#v", name, dictionary)
			}
		})
	}
}
