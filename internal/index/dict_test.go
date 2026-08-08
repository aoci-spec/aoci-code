// 字典提取与校验测试
// 索引条目: dict_test.go[TVL8TT]
package index

import (
	"strings"
	"testing"
)

const dictHeader = "#A层级: X-Script(脚本) M-Model(存储) D-Doc(文档) C-Config(配置)\n" +
	"#B模块: Srv-服务入口 Store-存储层 Doc-文档 Meta-项目元信息\n" +
	"#E规模: L大(>300行) M中(100-300行) S小(30-100行) T微(<30行)\n"

func TestExtractTagDictForms(t *testing.T) {
	// 三种既存形态: 连字符分隔 / 符号中文连写 / 多字母符号
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
	// E 行缺失回退 LMST
	if !d.E["L"] || !d.E["T"] || len(d.E) != 4 {
		t.Fatalf("E 缺失应回退 LMST: %+v", d.E)
	}
}

// TestExtractTagDictVariantForms 字典行形态容错(P-21,httpx-rerun 真机实弹):
// AI 起草的头部 "#A层级(python,架构角色): I-接口(...)" 在维名与冒号间插夹注,
// 旧硬前缀匹配零提取 → HasDict false → maintain 判未立约且字典闸静默假绿。
// 三变体全锁: 维名夹注(实物形态) / #后空格 / 全角冒号。
func TestExtractTagDictVariantForms(t *testing.T) {
	// 实物形态(取自 httpx-rerun 头部字节取证,夹注内含空格与示例路径)
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
	// 符号污染防线: 夹注内示例路径词(httpx/...)绝不入字典
	if d.A["httpx"] {
		t.Fatalf("夹注内路径词不得污染字典: %+v", d.A)
	}

	// #后空格变体
	d2 := ExtractTagDict("# A层级: X-x\n# B模块: Y-y\n")
	if !d2.A["X"] || !d2.B["Y"] {
		t.Fatalf("#后空格形态应可提取: A=%+v B=%+v", d2.A, d2.B)
	}

	// 全角冒号变体
	d3 := ExtractTagDict("#A层级：X-x\n#B模块：Y-y\n")
	if !d3.A["X"] || !d3.B["Y"] {
		t.Fatalf("全角冒号形态应可提取: A=%+v B=%+v", d3.A, d3.B)
	}

	// 精确相等防误伤: 维名不等的含冒号说明行不得命中
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
		warn string // 空=应合规;非空=违规消息须含此子串
	}{
		{"合规紧凑", "main.go[XSrv8T]: F:x | R:- | A:- | S:-", ""},
		{"合规多字母B", "store.go[MStore9S]: F:x | R:- | A:- | S:-", ""},
		{"E位非法_实弹DC7Meta", "AGENTS.md[DC7Meta]: F:x | R:- | A:- | S:-", "E规模符号a非法"},
		{"B不在字典", "x.go[XBogus5T]: F:x | R:- | A:- | S:-", "B模块符号Bogus不在字典"},
		{"A不在字典", "x.go[QSrv5T]: F:x | R:- | A:- | S:-", "A层级符号Q不在字典"},
		{"非标降级不判", "x.go[无效标签]: F:x | R:- | A:- | S:-", ""},
		{"点分合规", "x.go[X.Srv.9.T]: F:x | R:- | A:- | S:-", ""},
		// 跨维错位点破(P-14,httpx实弹病根: 10发dict拦截多为符号放错维而非真臆造):
		// 符号M在A字典(M-Model)与E字典(M中)而不在B字典,用在B位应点名疑似维度错位
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

// TestCrossDimHintAbsentForTrueBogus 真臆造负例(P-14 修法边界):
// 符号不存在于任何维字典时,文案维持"不在字典"原语义,绝不出现维度错位提示
// (错位提示只在符号确实存在于另一维时触发,防止对真臆造给出误导性归因)。
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
