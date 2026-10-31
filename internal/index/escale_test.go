// E-band gate tests: threshold extraction forms plus the judgement matrix (mismatch/boundary overlap/gap/downgrade)
//   - P-22 line-recognition tolerance and the R51 visible-skip three states.
//
// Index entry pending: escale_test.go
//
// Naming discipline: the entry construction helper is named mkEScaleEntry rather than mkEntry, since quota_test.go in the same
// package already has mkEntry(tags,sRunes) with an incompatible signature; same-package test helper
// names must carry a domain prefix against collisions (this file's first version went red in vet for exactly that).
package index

import (
	"strings"
	"testing"
)

// escaleHeader is the E规模 line in this repository's live shape (the >N / N-M / <N forms all present)
const escaleHeader = "#【系统】x\n#E规模: L大>400 M中200-400 S小100-200 T微<100\n"

// mkEScaleEntry builds an entry line with conforming structure (compact tag form: A=X, B=CR, C=7, E=the last character)
func mkEScaleEntry(e string) string {
	return "f.go[XCR7" + e + "]: F:x | R:- | A:- | S:-"
}

// TestEScaleNoThresholds: the skeleton placeholder form (no numbers) is undecidable, always nil.
func TestEScaleNoThresholds(t *testing.T) {
	th := ExtractEScaleThresholds("#E规模: L大 M中 S小 T微\n")
	if th.HasThresholds() {
		t.Fatal("无数字声明的 E规模行不得产出阈值")
	}
	if v := CheckEScale(mkEScaleEntry("L"), 10, th); v != nil {
		t.Fatalf("不可判定时应返回 nil: %+v", v)
	}
}

// TestEScaleMatrix, the judgement matrix: mismatch warns / hit passes / boundary overlap tolerated / non-standard downgraded.
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
		wantIn   string // the expected band string the warning message must hold
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

// TestEScaleGapAndInclusive: conservative on gaps, plus the >=/<= inclusive forms.
func TestEScaleGapAndInclusive(t *testing.T) {
	th := ExtractEScaleThresholds("#E规模: L>=400 T<=100\n")
	if !th.HasThresholds() {
		t.Fatal(">=/<= 形态应产出阈值")
	}
	// 400 is inside L (>=), 100 is inside T (<=)
	if v := CheckEScale(mkEScaleEntry("L"), 400, th); v != nil {
		t.Fatalf(">=400 应含 400 行: %+v", v)
	}
	if v := CheckEScale(mkEScaleEntry("T"), 100, th); v != nil {
		t.Fatalf("<=100 应含 100 行: %+v", v)
	}
	// 250 lines fall into the 101-399 gap: no tag warns (no overreach in judging dictionary completeness)
	if v := CheckEScale(mkEScaleEntry("T"), 250, th); v != nil {
		t.Fatalf("字典空隙应保守跳过: %+v", v)
	}
}

// TestEScaleLineFormsTolerance, the P-22 line-recognition tolerance matrix: after line recognition moved to the single parseDictLine,
// the five live-grade variants (a space after #, a full-width colon, a note on the dimension name, a full-width comparison operator, a threshold inside
// parentheses) must all extract; any regression means a second copy of line recognition revived or the single point was weakened.
// The judgement semantics are asserted through CheckEScale behaviour (ranges is unexported, so black-box verification).
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
		// 500 lines tagged L conforms (>400 holds 500)
		if v := CheckEScale(mkEScaleEntry("L"), 500, th); v != nil {
			t.Fatalf("%s: 500行标L应合规: %+v", f.name, v)
		}
		// 50 lines tagged L warns and the expected band holds T (<100 holds 50)
		v := CheckEScale(mkEScaleEntry("L"), 50, th)
		if v == nil {
			t.Fatalf("%s: 50行标L应告警", f.name)
		}
		if !strings.Contains(v.Msg, "应为T") {
			t.Fatalf("%s: 告警消息应含 应为T: %s", f.name, v.Msg)
		}
	}
}

// TestEScaleSawLineTriState, the R51 visible-skip three states: no line / a line without thresholds / a line with thresholds.
// "A line present but unparseable" and "no line" must be distinguishable; that is the basis of the upper layer's typed wording (the D97 kind),
// and silently merging the two states revives the false-green family of defects.
func TestEScaleSawLineTriState(t *testing.T) {
	// State one: no E规模 line in the header
	th := ExtractEScaleThresholds("#【系统】x\n#A层级: I索引核心\n")
	if th.SawEScaleLine() {
		t.Fatal("无 E规模 行不应置 sawLine")
	}
	if th.HasThresholds() {
		t.Fatal("无 E规模 行不应产出阈值")
	}
	// State two: the line is present but holds no numeric threshold (skeleton placeholder)
	th = ExtractEScaleThresholds("#E规模: L大 M中 S小 T微\n")
	if !th.SawEScaleLine() {
		t.Fatal("骨架占位形态应置 sawLine(行在场)")
	}
	if th.HasThresholds() {
		t.Fatal("骨架占位形态不应产出阈值")
	}
	// State three: the line is present and the thresholds extract
	th = ExtractEScaleThresholds(escaleHeader)
	if !th.SawEScaleLine() || !th.HasThresholds() {
		t.Fatal("本仓形态应同时具备 sawLine 与阈值")
	}
	// nil receiver safety
	var nilTh *EScaleThresholds
	if nilTh.SawEScaleLine() || nilTh.HasThresholds() {
		t.Fatal("nil 阈值表两判据均应为 false")
	}
}
