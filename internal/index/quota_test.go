// Table-driven tests for the S field C-quota check (default quotas + P-22 header custom declarations + the R51 three states)
// Index entry: quota_test.go (to be recorded)
package index

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// mkEntry builds an entry line with the given S character count (all Chinese characters, one rune of three bytes each, which also verifies rune counting)
func mkEntry(tags string, sRunes int) string {
	return "f.go[" + tags + "]: F:x | R:- | A:- | S:" + strings.Repeat("字", sRunes)
}

func TestCheckSQuotaBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		tags   string
		sRunes int
		warn   bool
	}{
		{"C9恰上限放行", "IC9T", machinecontract.SQuotaHighRunes, false},
		{"C9超限告警", "IC9T", machinecontract.SQuotaHighRunes + 1, true},
		{"C8同高档", "IC8M", machinecontract.SQuotaHighRunes + 1, true},
		{"C7恰上限放行", "CIX7L", machinecontract.SQuotaMidRunes, false},
		{"C7超限告警", "CIX7L", machinecontract.SQuotaMidRunes + 1, true},
		{"C4同中档", "GCF4S", machinecontract.SQuotaMidRunes + 1, true},
		{"C3恰上限放行", "XGI3T", machinecontract.SQuotaLowRunes, false},
		{"C3超限告警", "XGI3T", machinecontract.SQuotaLowRunes + 1, true},
		{"C1同低档", "XLC1T", machinecontract.SQuotaLowRunes + 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := CheckSQuota(mkEntry(c.tags, c.sRunes))
			if c.warn && v == nil {
				t.Fatal("应产生配额告警")
			}
			if !c.warn && v != nil {
				t.Fatalf("不应告警: %v", v.Msg)
			}
			if v != nil && v.Level != LevelWarning {
				t.Fatalf("配额违规须为Warning级,得到: %v", v.Level)
			}
		})
	}
}

func TestCheckSQuotaDotFormTags(t *testing.T) {
	// Dotted form: the first digit is still C (the B/D no-digit anchor holds for both forms)
	if v := CheckSQuota(mkEntry("I.IX.3.T", machinecontract.SQuotaLowRunes+1)); v == nil {
		t.Fatal("点分标签 C3 超配额应告警")
	}
	if v := CheckSQuota(mkEntry("I.IX.9.T", machinecontract.SQuotaHighRunes+1)); v == nil {
		t.Fatal("点分标签 C9 超配额应告警")
	}
}

func TestCheckSQuotaSVariantsSummed(t *testing.T) {
	// S1+S2 variant counts add up: 30 characters each, 60 in total, over the C3 quota of 50
	line := "f.go[XGI3T]: F:x | R:- | A:- | S1:" + strings.Repeat("甲", 30) + " | S2:" + strings.Repeat("乙", 30)
	if v := CheckSQuota(line); v == nil {
		t.Fatal("S 变体合计超配额应告警")
	}
}

func TestCheckSQuotaUnjudgeable(t *testing.T) {
	// No tag structure / a tag without digits (non-standard downgrade) / a placeholder S never warn
	for _, line := range []string{
		"没有标签结构的行",
		"f.go[ABCT]: F:x | R:- | A:- | S:" + strings.Repeat("字", 999),
		"f.go[IC9T]: F:x | R:- | A:- | S:-",
	} {
		if v := CheckSQuota(line); v != nil {
			t.Fatalf("不可判定或占位行不应告警: %q → %v", line, v.Msg)
		}
	}
}

func TestCheckSQuotaFRANotCounted(t *testing.T) {
	// Over-long F/R/A do not count toward the S quota
	line := "f.go[XGI3T]: F:" + strings.Repeat("功", 300) + " | R:- | A:- | S:短"
	if v := CheckSQuota(line); v != nil {
		t.Fatalf("F 段字数不应计入 S 配额: %v", v.Msg)
	}
}

// TestSQuotaExtractForms, the P-22 declaration form matrix: the standard form and five live-grade variants
// (full-width ≤ / a single band / a double-C range / a suffix after the number / a note on the dimension name) must all extract with correct judgement semantics.
// quotas is unexported, so the assertions are black-box through CheckSQuotaWith behaviour.
func TestSQuotaExtractForms(t *testing.T) {
	forms := []struct {
		name   string
		header string
	}{
		{"标准形态", "#S配额: C9-8≤100 C3-1≤30\n"},
		{"半角比较符", "#S配额: C9-8<=100 C3-1<=30\n"},
		{"双C区间写法", "#S配额: C9-C8≤100 C3-C1≤30\n"},
		{"区间倒序等价", "#S配额: C8-9≤100 C1-3≤30\n"},
		{"字后缀与井号后空格", "# S配额: C9-8≤100字 C3-1≤30字\n"},
		{"维名夹注与全角冒号", "#S配额(字数上限)： C9-8≤100 C3-1≤30\n"},
	}
	for _, f := range forms {
		th := ExtractSQuotaThresholds(f.header)
		if !th.SawSQuotaLine() {
			t.Fatalf("%s: 应识别到 S配额 行", f.name)
		}
		if !th.HasQuotas() {
			t.Fatalf("%s: 应产出配额声明", f.name)
		}
		// C9 declared limit 100: exactly 100 passes, 101 warns with wording naming the declaration source
		if v := CheckSQuotaWith(mkEntry("IC9T", 100), th); v != nil {
			t.Fatalf("%s: C9 恰声明上限应放行: %v", f.name, v.Msg)
		}
		v := CheckSQuotaWith(mkEntry("IC9T", 101), th)
		if v == nil {
			t.Fatalf("%s: C9 超声明上限应告警", f.name)
		}
		if !strings.Contains(v.Msg, "S配额声明") {
			t.Fatalf("%s: 自定义配额告警应注明声明来源: %s", f.name, v.Msg)
		}
		// C3 declared limit 30: 31 warns (the default is 50; the assertion proves the tightened declaration takes effect)
		if v := CheckSQuotaWith(mkEntry("XGI3T", 31), th); v == nil {
			t.Fatalf("%s: C3 超声明上限 30 应告警(默认 50 下不会告警,声明未生效)", f.name)
		}
	}
}

// TestSQuotaStrictLess: <N means ≤N-1; with C3<50 declared, 49 passes and 50 warns.
func TestSQuotaStrictLess(t *testing.T) {
	th := ExtractSQuotaThresholds("#S配额: C3<50\n")
	if !th.HasQuotas() {
		t.Fatal("<N 形态应产出配额声明")
	}
	if v := CheckSQuotaWith(mkEntry("XGI3T", 49), th); v != nil {
		t.Fatalf("<50 语义下 49 字应放行: %v", v.Msg)
	}
	if v := CheckSQuotaWith(mkEntry("XGI3T", 50), th); v == nil {
		t.Fatal("<50 语义下 50 字应告警")
	}
}

// TestSQuotaGapFillDefault, gaps filled with defaults (the operator decision): when only C9-8 is declared,
// the undeclared C3 band takes the machine-contract default and the wording carries no declaration source.
func TestSQuotaGapFillDefault(t *testing.T) {
	th := ExtractSQuotaThresholds("#S配额: C9-8≤100\n")
	if !th.HasQuotas() {
		t.Fatal("应产出配额声明")
	}
	v := CheckSQuotaWith(
		mkEntry("XGI3T", machinecontract.SQuotaLowRunes+1),
		th,
	)
	if v == nil {
		t.Fatal("未声明档位应按机器合同默认值判定超限")
	}
	if strings.Contains(v.Msg, "S配额声明") {
		t.Fatalf("默认回退档位的告警不应注明声明来源: %s", v.Msg)
	}
	if v := CheckSQuotaWith(
		mkEntry("XGI3T", machinecontract.SQuotaLowRunes),
		th,
	); v != nil {
		t.Fatalf("未声明档位恰默认上限应放行: %v", v.Msg)
	}
}

// TestSQuotaSawLineTriState, the R51 visible-skip three states: no line / a line without declarations / a line with declarations.
// "A declaration present but unparseable by the machine" and "no declaration at all" must be distinguishable; that is the basis of the upper layer's typed wording
// (the D97 kind), and silently merging the two states revives the false-green family of defects.
func TestSQuotaSawLineTriState(t *testing.T) {
	// State one: no S配额 line in the header
	th := ExtractSQuotaThresholds("#【系统】x\n#A层级: I索引核心\n")
	if th.SawSQuotaLine() {
		t.Fatal("无 S配额 行不应置 sawLine")
	}
	if th.HasQuotas() {
		t.Fatal("无 S配额 行不应产出声明")
	}
	// State two: the line is present but no field parses (natural-language form)
	th = ExtractSQuotaThresholds("#S配额: 高档六百字 低档五十字\n")
	if !th.SawSQuotaLine() {
		t.Fatal("不可解析形态应置 sawLine(行在场)")
	}
	if th.HasQuotas() {
		t.Fatal("不可解析形态不应产出声明")
	}
	// In this state the judgement falls back to the defaults (the fallback never fails silently).
	if v := CheckSQuotaWith(
		mkEntry("IC9T", machinecontract.SQuotaHighRunes+1),
		th,
	); v == nil {
		t.Fatal("声明不可解析时应回退默认配额兜底")
	}
	// State three: the line is present and extracts
	th = ExtractSQuotaThresholds(
		"#S配额: " + machinecontract.NumericText().SQuotaDefaultCompact + "\n",
	)
	if !th.SawSQuotaLine() || !th.HasQuotas() {
		t.Fatal("标准形态应同时具备 sawLine 与声明")
	}
	// nil receiver safety, and nil is equivalent to the defaults
	var nilTh *SQuotaThresholds
	if nilTh.SawSQuotaLine() || nilTh.HasQuotas() {
		t.Fatal("nil 配额表两判据均应为 false")
	}
	if v := CheckSQuotaWith(mkEntry("IC9T", machinecontract.SQuotaHighRunes), nilTh); v != nil {
		t.Fatalf("nil 配额表应等价默认配额: %v", v.Msg)
	}
	if v := CheckSQuotaWith(mkEntry("IC9T", machinecontract.SQuotaHighRunes+1), nilTh); v == nil {
		t.Fatal("nil 配额表超限应按默认告警")
	}
}

// TestSQuotaOverrideOrder: a band declared twice takes the later value (written per C).
func TestSQuotaOverrideOrder(t *testing.T) {
	th := ExtractSQuotaThresholds("#S配额: C9≤100 C9≤300\n")
	if v := CheckSQuotaWith(mkEntry("IC9T", 300), th); v != nil {
		t.Fatalf("后声明 300 应覆盖先声明 100: %v", v.Msg)
	}
	if v := CheckSQuotaWith(mkEntry("IC9T", 301), th); v == nil {
		t.Fatal("超后声明 300 应告警")
	}
}

func TestEffectiveSQuotaContractUsesDefaultsAndFormalOverrides(t *testing.T) {
	if got, want := EffectiveSQuotaContract("# no quota declaration\n"), machinecontract.NumericText().SQuotaDefaultCompact; got != want {
		t.Fatalf("old Meta did not receive the current machine fallback: got=%q want=%q", got, want)
	}
	got := EffectiveSQuotaContract("#S quota: C9≤321 C5≤123\n")
	for _, want := range []string{"C9≤321", "C8≤600", "C7-6≤200", "C5≤123", "C4≤200", "C3-1≤50"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effective quota contract missing %q: %s", want, got)
		}
	}
}
