// S 字段 C 配额检查表驱动测试(默认配额 + P-22 头部自定义声明 + R51 三态)
// 索引条目: quota_test.go(待补录)
package index

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// mkEntry 构造指定 S 字数(全中文字符,每字 1 rune 3 字节,兼验 rune 计数)的条目行
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
	// 点分形态: 首个数字仍是 C(B/D 禁数字锚点两形态同样成立)
	if v := CheckSQuota(mkEntry("I.IX.3.T", machinecontract.SQuotaLowRunes+1)); v == nil {
		t.Fatal("点分标签 C3 超配额应告警")
	}
	if v := CheckSQuota(mkEntry("I.IX.9.T", machinecontract.SQuotaHighRunes+1)); v == nil {
		t.Fatal("点分标签 C9 超配额应告警")
	}
}

func TestCheckSQuotaSVariantsSummed(t *testing.T) {
	// S1+S2 变体字数累加: 各 30 字合计 60 超 C3 配额 50
	line := "f.go[XGI3T]: F:x | R:- | A:- | S1:" + strings.Repeat("甲", 30) + " | S2:" + strings.Repeat("乙", 30)
	if v := CheckSQuota(line); v == nil {
		t.Fatal("S 变体合计超配额应告警")
	}
}

func TestCheckSQuotaUnjudgeable(t *testing.T) {
	// 无标签结构/标签无数字(非标降级)/占位 S 均不告警
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
	// F/R/A 超长不计入 S 配额
	line := "f.go[XGI3T]: F:" + strings.Repeat("功", 300) + " | R:- | A:- | S:短"
	if v := CheckSQuota(line); v != nil {
		t.Fatalf("F 段字数不应计入 S 配额: %v", v.Msg)
	}
}

// TestSQuotaExtractForms P-22 声明形态矩阵: 标准形态与五种实弹级变体
// (全角≤/单档/双C区间/字后缀/维名夹注)均须可提取且判定语义正确。
// quotas 非导出,经 CheckSQuotaWith 行为黑盒断言。
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
		// C9 声明上限 100: 恰 100 放行,101 告警且文案注明声明来源
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
		// C3 声明上限 30: 31 告警(默认为 50,声明收紧生效的判决断言)
		if v := CheckSQuotaWith(mkEntry("XGI3T", 31), th); v == nil {
			t.Fatalf("%s: C3 超声明上限 30 应告警(默认 50 下不会告警,声明未生效)", f.name)
		}
	}
}

// TestSQuotaStrictLess <N 语义为 ≤N-1: 声明 C3<50 时 49 放行 50 告警。
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

// TestSQuotaGapFillDefault 缺口补默认(操作者裁决): 只声明 C9-8 时,
// 未声明的 C3 档走机器合同默认值且文案不带声明来源。
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

// TestSQuotaSawLineTriState R51 跳过可见三态: 无行 / 有行无声明 / 有行有声明。
// "声明在场但机器不可解析"与"根本没声明"必须可区分 —— 上层分型文案
// (D97 同款)的判据基础,静默合并两态即假绿家族病灶复活。
func TestSQuotaSawLineTriState(t *testing.T) {
	// 态一: 头部无 S配额 行
	th := ExtractSQuotaThresholds("#【系统】x\n#A层级: I索引核心\n")
	if th.SawSQuotaLine() {
		t.Fatal("无 S配额 行不应置 sawLine")
	}
	if th.HasQuotas() {
		t.Fatal("无 S配额 行不应产出声明")
	}
	// 态二: 行在场但字段全部不可解析(自然语言形态)
	th = ExtractSQuotaThresholds("#S配额: 高档六百字 低档五十字\n")
	if !th.SawSQuotaLine() {
		t.Fatal("不可解析形态应置 sawLine(行在场)")
	}
	if th.HasQuotas() {
		t.Fatal("不可解析形态不应产出声明")
	}
	// 此态下判定回退默认(兜底不静默失效)。
	if v := CheckSQuotaWith(
		mkEntry("IC9T", machinecontract.SQuotaHighRunes+1),
		th,
	); v == nil {
		t.Fatal("声明不可解析时应回退默认配额兜底")
	}
	// 态三: 行在场且可提取
	th = ExtractSQuotaThresholds(
		"#S配额: " + machinecontract.NumericText().SQuotaDefaultCompact + "\n",
	)
	if !th.SawSQuotaLine() || !th.HasQuotas() {
		t.Fatal("标准形态应同时具备 sawLine 与声明")
	}
	// nil 接收者安全 + nil 与默认等价
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

// TestSQuotaOverrideOrder 同档重复声明后者覆盖前者(逐C写入语义)。
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
	for _, want := range []string{"C9≤321", "C8≤600", "C7-6≤500", "C5≤123", "C4≤500", "C3-1≤50"} {
		if !strings.Contains(got, want) {
			t.Fatalf("effective quota contract missing %q: %s", want, got)
		}
	}
}
