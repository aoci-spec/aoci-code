// S 字段 C 配额检查: 按条目 C 重要度核对 S 内容字数上限(Warning 级)
// 索引条目: quota.go[IVL8S]
//
// 配额来源两层(2026-07-13 操作者裁决,与 E 阈值归属裁决同哲学):
// 仓库头部 "#S配额:" 声明优先(用户自定义),未声明的 C 档回退机器合同默认值;
// 缺口补默认而非跳过。默认数值由 internal/machinecontract 单点提供。
//
// 头部声明形态: #S配额: C9-8≤N C7-4≤N C3-1≤N
// 行识别经 parseDictLine 单点(dict.go,P-21/P-22 同款三形态容错: #后空格/
// 全角冒号/维名括号夹注),绝不自持第二份行识别逻辑;字段词容忍 C9-C8 双C
// 写法/单档 C5≤100/全角≤＜＝归一/数字后"字"等汉字后缀天然截止/<N 视 ≤N-1;
// 同一 C 档重复声明后者覆盖前者(逐C写入);形态之外的词跳过不猜。
//
// 跳过可见(R51/14.6): SawSQuotaLine 区分三态 —— 无声明行(用默认,正常态) /
// 行在场但字段全部不可解析(仍用默认兜底,上层可分型提示"声明在场但机器
// 不可解析",D97 同款) / 行在场且可提取(用声明值)。声明不可解析绝不静默
// 当作无声明 —— 上层分型文案接线随 score.go 等调用方现读后落地。
//
// 设立动机: 超配额 S 是索引腐烂的复现机制之一 —— 策展人(人或模型)在
// "内容都重要"的冲动下超写,无机器闸时只能靠自觉;prompt 层已把配额教给
// 模型,本函数把同一规则变成机器可执行的检查,承诺与闸门对齐。
//
// 级别取 Warning 而非 Error: 配额是策展纪律不是结构约束 —— 硬拒会把
// 既有超配额存量条目的一切回写全部堵死(修别的字段也过不去),警告放行
// 让人看见债务并保留裁决权,与 ValidateEntryLine 演进叙事警告同策略。
//
// C 值提取刻意自包含不调 ParseTags: 只依赖字典的锚点铁律"B 与 D 禁用
// 数字,标签中首个数字即 C"(紧凑与点分两形态同样成立),该铁律比任何
// 解析实现都稳定;C 为单字符 1-9,取首个数字字符即可。
//
// 兼容契约: CheckSQuota(line) 保持原签名原语义(等价于阈值表为 nil 走
// 默认配额),既有调用方(validator.go 等)行为零变化;头部声明的生效依赖
// 调用方迁移到 CheckSQuotaWith 并传入 ExtractSQuotaThresholds 结果,
// 迁移逐调用方现读后进行(R50)。
package index

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// SQuotaThresholds 头部提取的 S 配额表(键=C 重要度 1-9,值=字数上限)
type SQuotaThresholds struct {
	quotas  map[int]int
	sawLine bool // S配额行是否在场(含在场但不可解析形态,R51 跳过可见分型用)
}

// HasQuotas 判断配额表是否含至少一个可用声明。
func (t *SQuotaThresholds) HasQuotas() bool {
	return t != nil && len(t.quotas) > 0
}

// SawSQuotaLine 判断头部是否存在被识别为 S配额 的字典行(无论字段提取是否成功)。
// 与 HasQuotas 组合出三态: 无行(默认,正常) / 有行无声明(在场但机器不可
// 解析,上层应点名而非静默用默认) / 有行有声明(自定义生效)。
func (t *SQuotaThresholds) SawSQuotaLine() bool {
	return t != nil && t.sawLine
}

// sQuotaFullWidth 配额字段符号归一: ≤ 统一为 <=,全角＜＝归半角。
// 全角数字不归一(无实弹样本,不过度猜测形态)。
var sQuotaFullWidth = strings.NewReplacer("≤", "<=", "＜", "<", "＝", "=")

// quotaForC 两层取值: 头部声明含该 C 档即用声明值(custom=true),
// 否则回退 spec 默认(缺口补默认,操作者裁决语义)。
func quotaForC(c int, th *SQuotaThresholds) (quota int, custom bool) {
	if th.HasQuotas() {
		if q, ok := th.quotas[c]; ok {
			return q, true
		}
	}
	return machinecontract.DefaultSQuotaForC(c), false
}

// ExtractSQuotaThresholds 从头部文本提取 S 配额声明。
// 行识别经 parseDictLine 单点(维名 S配额,三形态容错);内容按空白分词逐词
// 解析,不可解析的词跳过(该行仍置 sawLine 供分型)。
func ExtractSQuotaThresholds(headerText string) *SQuotaThresholds {
	t := &SQuotaThresholds{quotas: map[int]int{}}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok || dim != "S配额" {
			continue
		}
		t.sawLine = true
		for _, word := range strings.Fields(content) {
			cmin, cmax, q, ok := parseSQuotaField(word)
			if !ok {
				continue
			}
			for c := cmin; c <= cmax; c++ {
				t.quotas[c] = q
			}
		}
	}
	return t
}

// EffectiveSQuotaContract renders the complete effective S quota authority in
// descending C order. Parsed Meta declarations win per C band and every gap is
// filled from machinecontract, so older Meta assets without a quota line still
// produce the current machine contract without changing their formal bytes.
func EffectiveSQuotaContract(headerText string) string {
	thresholds := ExtractSQuotaThresholds(headerText)
	type band struct {
		minC  int
		maxC  int
		quota int
	}
	bands := []band{}
	for importance := 9; importance >= 1; importance-- {
		quota, _ := quotaForC(importance, thresholds)
		if len(bands) > 0 && bands[len(bands)-1].quota == quota && bands[len(bands)-1].minC == importance+1 {
			bands[len(bands)-1].minC = importance
			continue
		}
		bands = append(bands, band{minC: importance, maxC: importance, quota: quota})
	}
	parts := make([]string, 0, len(bands))
	for _, current := range bands {
		label := fmt.Sprintf("C%d", current.maxC)
		if current.minC != current.maxC {
			label += fmt.Sprintf("-%d", current.minC)
		}
		parts = append(parts, fmt.Sprintf("%s≤%d", label, current.quota))
	}
	return strings.Join(parts, " ")
}

// parseSQuotaField 解析单个配额词: C9-8≤N / C9-C8≤N / C5≤N / C3-1≤N字。
// C 值域 1-9(域外拒);区间两端自动排序(9-8 与 8-9 等价);比较符认 ≤(归一
// 为<=)/<=/=,裸 < 视 ≤N-1;数字后非数字字符(如"字")天然截止;配额须为正。
func parseSQuotaField(word string) (cmin, cmax, quota int, ok bool) {
	w := sQuotaFullWidth.Replace(word)
	if !strings.HasPrefix(w, "C") {
		return 0, 0, 0, false
	}
	c1, rest, okInt := leadingIntRest(w[len("C"):])
	if !okInt || c1 < 1 || c1 > 9 {
		return 0, 0, 0, false
	}
	c2 := c1
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimPrefix(rest[len("-"):], "C")
		c2, rest, okInt = leadingIntRest(rest)
		if !okInt || c2 < 1 || c2 > 9 {
			return 0, 0, 0, false
		}
	}
	strict := false
	switch {
	case strings.HasPrefix(rest, "<="):
		rest = rest[len("<="):]
	case strings.HasPrefix(rest, "="):
		rest = rest[len("="):]
	case strings.HasPrefix(rest, "<"):
		rest = rest[len("<"):]
		strict = true // <N 语义为 ≤N-1
	default:
		return 0, 0, 0, false
	}
	q, okInt := leadingInt(rest)
	if !okInt || q <= 0 {
		return 0, 0, 0, false
	}
	if strict {
		q--
	}
	if q <= 0 {
		return 0, 0, 0, false
	}
	if c1 > c2 {
		c1, c2 = c2, c1
	}
	return c1, c2, q, true
}

// extractCFromTags 从标签串提取 C 重要度: 首个数字字符即 C(字典锚点铁律)。
// 无数字返回 0(非标标签,与 ParseTags 降级策略一致 —— 不做配额判定)。
func extractCFromTags(tags string) int {
	for _, r := range tags {
		if r >= '1' && r <= '9' {
			return int(r - '0')
		}
		if r == '0' {
			return 0 // C 取值域 1-9,出现 0 视非标
		}
	}
	return 0
}

// sContentLength 统计条目行中 S 段内容总字数(rune 计):
// 以 " | " 切段,前缀为 S:/S1:/S2:/S3: 的段计入(与 splitFRAS 的 S 变体
// 合并语义一致);多个 S 变体段字数累加;前缀本身与占位 "-" 均计入内容
// (占位仅 1 字对配额无实际影响,不为它做特判保持函数简单)。
func sContentLength(line string) int {
	total := 0
	for _, seg := range strings.Split(line, " | ") {
		rest, ok := cutSPrefix(seg)
		if ok {
			total += utf8.RuneCountInString(rest)
		}
	}
	return total
}

// cutSPrefix 判断段是否为 S 段(S: 或 S 后跟单个数字再冒号),是则返回内容。
func cutSPrefix(seg string) (string, bool) {
	if strings.HasPrefix(seg, "S:") {
		return seg[len("S:"):], true
	}
	if len(seg) >= 3 && seg[0] == 'S' && seg[1] >= '0' && seg[1] <= '9' && seg[2] == ':' {
		return seg[3:], true
	}
	return "", false
}

// CheckSQuotaWith 对单条条目行按给定配额表做 S 字段配额检查。
// th 为 nil 或无声明时全档走 spec 默认;声明命中该 C 档时用声明值并在
// 文案注明来源(写入者可区分默认纪律与仓库自定裁决)。
// 返回 nil 表示无违规或不可判定(无标签结构/标签无 C —— 结构问题归
// ValidateEntryLine,本函数不重复报);超配额返回 Warning 级 Violation。
func CheckSQuotaWith(line string, th *SQuotaThresholds) *Violation {
	// 提取 [tags]: 结构 —— 与条目正则同锚点(首 [ 与其后首个 ]: )
	lb := strings.Index(line, "[")
	if lb < 0 {
		return nil
	}
	rb := strings.Index(line[lb:], "]:")
	if rb < 0 {
		return nil
	}
	c := extractCFromTags(line[lb+1 : lb+rb])
	if c == 0 {
		return nil // 非标标签降级,不做配额判定
	}
	quota, custom := quotaForC(c, th)
	got := sContentLength(line)
	if got <= quota {
		return nil
	}
	src := ""
	if custom {
		src = ",仓库头部S配额声明"
	}
	return &Violation{
		Level: LevelWarning,
		Msg: fmt.Sprintf("S字段%d字超C%d配额(上限%d字%s,配额是上限不是目标): 请压缩为当前状态高熵约束,或上调C需说明理由",
			got, c, quota, src),
	}
}

// CheckSQuota 对单条条目行按 spec 默认配额做检查(兼容入口,语义与
// CheckSQuotaWith(line, nil) 等价;既有调用方零迁移成本)。
func CheckSQuota(line string) *Violation {
	return CheckSQuotaWith(line, nil)
}
