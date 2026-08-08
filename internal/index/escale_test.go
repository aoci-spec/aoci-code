// E 档位闸测试: 阈值提取形态 + 判定矩阵(错配/边界重叠/空隙/降级)
//   - P-22 行识别容错与 R51 跳过可见三态。
//
// 索引条目待补: escale_test.go
//
// 命名纪律: 条目构造辅助函数取 mkEScaleEntry 而非 mkEntry —— 同包
// quota_test.go 已有 mkEntry(tags,sRunes) 且签名不兼容,同包测试辅助
// 函数命名须带域前缀防撞名(本文件首版即因此 vet 红)。
package index

import (
	"strings"
	"testing"
)

// escaleHeader 本仓实况形态的 E规模行(>N / N-M / <N 三形态齐)
const escaleHeader = "#【系统】x\n#E规模: L大>400 M中200-400 S小100-200 T微<100\n"

// mkEScaleEntry 造合规结构的条目行(标签紧凑形态: A=X,B=CR,C=7,E=末字符)
func mkEScaleEntry(e string) string {
	return "f.go[XCR7" + e + "]: F:x | R:- | A:- | S:-"
}

// TestEScaleNoThresholds 骨架占位形态(无数字)不可判定: 一律 nil。
func TestEScaleNoThresholds(t *testing.T) {
	th := ExtractEScaleThresholds("#E规模: L大 M中 S小 T微\n")
	if th.HasThresholds() {
		t.Fatal("无数字声明的 E规模行不得产出阈值")
	}
	if v := CheckEScale(mkEScaleEntry("L"), 10, th); v != nil {
		t.Fatalf("不可判定时应返回 nil: %+v", v)
	}
}

// TestEScaleMatrix 判定矩阵: 错配告警/命中放行/边界重叠宽容/非标降级。
func TestEScaleMatrix(t *testing.T) {
	th := ExtractEScaleThresholds(escaleHeader)
	if !th.HasThresholds() {
		t.Fatal("本仓形态 E规模行应产出阈值")
	}
	cases := []struct {
		name     string
		line     string
		lines    int
		wantWarn bool
		wantIn   string // 告警消息应含的期望档位串
	}{
		{"306行标L应为M", mkEScaleEntry("L"), 306, true, "应为M"},
		{"306行标M合规", mkEScaleEntry("M"), 306, false, ""},
		{"边界200行标S合规(S含200)", mkEScaleEntry("S"), 200, false, ""},
		{"边界200行标M合规(M含200)", mkEScaleEntry("M"), 200, false, ""},
		{"边界200行标L告警且列双档", mkEScaleEntry("L"), 200, true, "M/S"},
		{"401行标L合规(>400起于401)", mkEScaleEntry("L"), 401, false, ""},
		{"400行标L应为M", mkEScaleEntry("L"), 400, true, "应为M"},
		{"50行标T合规", mkEScaleEntry("T"), 50, false, ""},
		{"50行标L应为T", mkEScaleEntry("L"), 50, true, "应为T"},
		{"非标标签降级不判", "f.go[!!!]: F:x | R:- | A:- | S:-", 50, false, ""},
		{"无标签结构不判", "散文一行", 50, false, ""},
	}
	for _, c := range cases {
		v := CheckEScale(c.line, c.lines, th)
		if c.wantWarn {
			if v == nil {
				t.Fatalf("%s: 应告警", c.name)
			}
			if v.Level != LevelWarning {
				t.Fatalf("%s: 应为 Warning 级: %+v", c.name, v)
			}
			if c.wantIn != "" && !strings.Contains(v.Msg, c.wantIn) {
				t.Fatalf("%s: 消息应含 %q: %s", c.name, c.wantIn, v.Msg)
			}
		} else if v != nil {
			t.Fatalf("%s: 不应告警: %+v", c.name, v)
		}
	}
}

// TestEScaleGapAndInclusive 空隙保守 + >=/<= 含界形态。
func TestEScaleGapAndInclusive(t *testing.T) {
	th := ExtractEScaleThresholds("#E规模: L>=400 T<=100\n")
	if !th.HasThresholds() {
		t.Fatal(">=/<= 形态应产出阈值")
	}
	// 400 含于 L(>=),100 含于 T(<=)
	if v := CheckEScale(mkEScaleEntry("L"), 400, th); v != nil {
		t.Fatalf(">=400 应含 400 行: %+v", v)
	}
	if v := CheckEScale(mkEScaleEntry("T"), 100, th); v != nil {
		t.Fatalf("<=100 应含 100 行: %+v", v)
	}
	// 250 行落入 101-399 空隙: 任何标注都不告警(不越权裁决字典完备性)
	if v := CheckEScale(mkEScaleEntry("T"), 250, th); v != nil {
		t.Fatalf("字典空隙应保守跳过: %+v", v)
	}
}

// TestEScaleLineFormsTolerance P-22 行识别容错矩阵: 行识别改走 parseDictLine
// 单点后,#后空格/全角冒号/维名夹注/全角比较符/括号内阈值五种实弹级变体
// 形态均须可提取 —— 任一回归即行识别第二份逻辑复活或单点被削弱。
// 判定语义经 CheckEScale 行为断言(ranges 非导出,黑盒验证)。
func TestEScaleLineFormsTolerance(t *testing.T) {
	forms := []struct {
		name   string
		header string
	}{
		{"井号后空格", "# E规模: L大>400 T微<100\n"},
		{"全角冒号", "#E规模： L大>400 T微<100\n"},
		{"维名括号夹注加行字后缀", "#E规模(行数): L大>400行 T微<100行\n"},
		{"全角比较符", "#E规模: L大＞400行 T微＜100行\n"},
		{"阈值写在括号内", "#E规模: L大(>400行) T微(<100行)\n"},
	}
	for _, f := range forms {
		th := ExtractEScaleThresholds(f.header)
		if !th.SawEScaleLine() {
			t.Fatalf("%s: 应识别到 E规模 行", f.name)
		}
		if !th.HasThresholds() {
			t.Fatalf("%s: 应产出阈值", f.name)
		}
		// 500 行标 L 合规(>400 含 500)
		if v := CheckEScale(mkEScaleEntry("L"), 500, th); v != nil {
			t.Fatalf("%s: 500行标L应合规: %+v", f.name, v)
		}
		// 50 行标 L 告警且期望档含 T(<100 含 50)
		v := CheckEScale(mkEScaleEntry("L"), 50, th)
		if v == nil {
			t.Fatalf("%s: 50行标L应告警", f.name)
		}
		if !strings.Contains(v.Msg, "应为T") {
			t.Fatalf("%s: 告警消息应含 应为T: %s", f.name, v.Msg)
		}
	}
}

// TestEScaleSawLineTriState R51 跳过可见三态: 无行 / 有行无阈值 / 有行有阈值。
// "行在场但不可解析"与"行不存在"必须可区分 —— 上层分型文案(D97 同款)的
// 判据基础,静默合并两态即假绿家族病灶复活。
func TestEScaleSawLineTriState(t *testing.T) {
	// 态一: 头部无 E规模 行
	th := ExtractEScaleThresholds("#【系统】x\n#A层级: I索引核心\n")
	if th.SawEScaleLine() {
		t.Fatal("无 E规模 行不应置 sawLine")
	}
	if th.HasThresholds() {
		t.Fatal("无 E规模 行不应产出阈值")
	}
	// 态二: 行在场但无数字阈值(骨架占位)
	th = ExtractEScaleThresholds("#E规模: L大 M中 S小 T微\n")
	if !th.SawEScaleLine() {
		t.Fatal("骨架占位形态应置 sawLine(行在场)")
	}
	if th.HasThresholds() {
		t.Fatal("骨架占位形态不应产出阈值")
	}
	// 态三: 行在场且阈值可提取
	th = ExtractEScaleThresholds(escaleHeader)
	if !th.SawEScaleLine() || !th.HasThresholds() {
		t.Fatal("本仓形态应同时具备 sawLine 与阈值")
	}
	// nil 接收者安全
	var nilTh *EScaleThresholds
	if nilTh.SawEScaleLine() || nilTh.HasThresholds() {
		t.Fatal("nil 阈值表两判据均应为 false")
	}
}
