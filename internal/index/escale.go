// E 规模档位校验: 按头部字典的行数阈值核对条目 E 档与文件实际行数(Warning 级)
// 索引条目: escale.go[IVF7M]
//
// 设立动机(2026-07-11 自举实弹): 同批人审抓获三处 E 档位错配(index.go 306行
// 标L实为M / index_entries.go 409行标M实为L / init_test.go 约150行标T实为S),
// httpx 实弹九人审 16 条改稿中亦含 2 处同类 —— E 档随文件自然生长而漂移,
// 行数可机读、档位界线在头部字典,完全可判定,此前仅靠人审属机器闸空转。
//
// 级别取 Warning(与 quota 闸同哲学): 档位漂移的常见成因是文件生长而条目
// 语义未错 —— 硬拒会阻塞一切与 E 无关的回写;警告放行让写入者看见并顺手
// 更新,保留人的裁决权。
//
// 阈值字典驱动(与 dict 闸同哲学): 界线不硬编码,从头部 E规模行解析;头部
// 无数字阈值声明的仓不可判定即跳过,宽进不冤枉(HasThresholds 同 HasDict
// 语义)。支持三种数字形态: >N / N-M / <N(亦容 >=N / <=N 与全角＞＜＝);
// 形态之外的符号视为无阈值,不参与判定。
//
// 阈值归属裁决(2026-07-13 操作者裁决,P-22 附带裁决案): E 档位行数界线以
// 仓库头部声明为准 —— 用户可自定义,spec 中数值仅为默认模板参考,本闸不做
// spec 数值硬校验;头部声明 >400 或 >300 均为该仓合法界线。
//
// 行识别单点(P-22 修法,2026-07-13): 旧实现自带 HasPrefix(s,"E规模:") 硬
// 前缀匹配 —— 与 dict 闸 P-21 前的旧病同型:AI 起草的变体形态(#后空格/
// 全角冒号/维名括号夹注如 "E规模(行数):")任一形态识别零命中 → HasThresholds
// false → 档位闸静默跳过(假绿家族第三例,前两例为 P-15 缺位标签与 P-21
// 字典行)。修法: 行识别改走 parseDictLine 单点(dict.go,P-21 三形态容错),
// 本闸绝不再持第二份行识别逻辑(判据单一事实源);内容侧与 dict 闸刻意
// 不对称 —— dict 收符号须剥夹注防示例路径污染,本闸取数字须保留夹注
// (阈值可能写在括号内如 "L大(>400行)"),不对称是设计而非遗漏。
// 数字后缀汉字(如 ">400行")天然容忍(leadingInt 遇非数字即止)。
//
// 跳过可见(R51/14.6): sawLine 记录 E规模行是否在场 —— "行在场但阈值不可
// 提取"与"行根本不存在"是两态,供上层(score/maintain)分型展示(P-21 的
// D97 分型同款),杜绝静默跳过型假绿;上层文案接线随 score.go 下次修改落地。
//
// 边界宽容(刻意设计): 相邻档位共享边界值属常见字典写法(如 M中200-400 与
// S小100-200 在 200 行重叠)—— 行数命中多个档位时,条目标注其中任一即合规;
// 只在"标注档位完全不含该行数,且行数确有所属档位"时告警;行数落入字典
// 空隙(任何档位都不含)同样跳过 —— 字典的完备性归策展者,本闸不越权裁决。
//
// E 符号切分复用 ParseTags(dict 闸同款纪律): 切分规则只有一份,本闸只判
// "切出来的 E 符号所标档位含不含该行数";符号合法性归 dict 闸,不重复报。
//
// 反查单点化(R60-F.9-A4,2026-07-18): "行数→所属档位集合"反查原为
// CheckEScale 内联逻辑,提为导出函数 ExpectedEScaleSymbols —— Guide 层
// 借此为 Entries 目标预填 expected_e(确定性字段不让模型猜,HTTPX 实弹
// 中 agent 反复猜 E 的根因是 expected 只在事后 Warning 里出现,时机太晚);
// CheckEScale 改为调用之,判据仍单点零第二份逻辑。
package index

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// escaleRange 单个 E 符号的行数区间(闭区间)
type escaleRange struct {
	min int
	max int
}

// EScaleThresholds 头部提取的 E 档位阈值表(键=E 符号)
type EScaleThresholds struct {
	ranges  map[string]escaleRange
	sawLine bool // E规模行是否在场(含在场但阈值不可提取的形态,R51 跳过可见分型用)
}

// HasThresholds 判断阈值表是否可用于校验(至少一个符号带合法数字阈值)。
// 头部 E规模行无数字声明(如骨架占位 "L大 M中 S小 T微")时返回 false,
// 调用方应跳过校验(不可判定不误报)。
func (t *EScaleThresholds) HasThresholds() bool {
	return t != nil && len(t.ranges) > 0
}

// SawEScaleLine 判断头部是否存在被识别为 E规模 的字典行(无论阈值提取是否成功)。
// 与 HasThresholds 组合出三态(R51 跳过可见分型):
// 无行(未声明) / 有行无阈值(在场但机器不可解析,上层应点名而非笼统报未声明) /
// 有行有阈值(可判定)。
func (t *EScaleThresholds) SawEScaleLine() bool {
	return t != nil && t.sawLine
}

// escaleFullWidthOps 全角比较符归一(AI 起草与中文输入法场景的常见变体)。
// 全角数字不归一 —— 无实弹样本,过度猜测形态违反宽进不臆造。
var escaleFullWidthOps = strings.NewReplacer("＞", ">", "＜", "<", "＝", "=")

// ExtractEScaleThresholds 从头部文本提取 E 档位阈值。
// 行识别经 parseDictLine 单点(P-22,与 dict 闸共用同一容错: #后空格/全角
// 冒号/维名括号夹注);内容按空白分词,每词取开头连续 ASCII 字母作符号,
// 其余部分解析数字形态。内容侧不剥夹注(与 dict 闸刻意不对称,阈值可能
// 写在括号内);无符号前缀的词(如独立的 "(>400)")跳过不猜归属。
func ExtractEScaleThresholds(headerText string) *EScaleThresholds {
	t := &EScaleThresholds{ranges: map[string]escaleRange{}}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok || dim != "E规模" {
			continue
		}
		t.sawLine = true
		for _, word := range strings.Fields(content) {
			sym := leadingASCIILetters(word)
			if sym == "" {
				continue
			}
			if r, ok := parseEScaleSpec(word[len(sym):]); ok {
				t.ranges[sym] = r
			}
		}
	}
	return t
}

// parseEScaleSpec 解析单个档位词的数字形态: >N / >=N / N-M / <N / <=N。
// rest 可含中文描述前缀(如 "大>400")与括号夹注(如 "大(>400行)"),按 ASCII
// 字节定位比较符与数字 —— UTF-8 多字节序列不含 ASCII 字节,直接字节扫描
// 安全;全角比较符先归一为半角;数字后缀非数字字符(如 "行")天然截止。
// 裸数字(无区间语义)与倒序区间不猜,返回 false。
func parseEScaleSpec(rest string) (escaleRange, bool) {
	rest = escaleFullWidthOps.Replace(rest)
	i := strings.IndexAny(rest, "<>0123456789")
	if i < 0 {
		return escaleRange{}, false
	}
	switch rest[i] {
	case '>':
		j := i + 1
		eq := false
		if j < len(rest) && rest[j] == '=' {
			eq = true
			j++
		}
		n, ok := leadingInt(rest[j:])
		if !ok {
			return escaleRange{}, false
		}
		if eq {
			return escaleRange{min: n, max: math.MaxInt}, true
		}
		return escaleRange{min: n + 1, max: math.MaxInt}, true
	case '<':
		j := i + 1
		eq := false
		if j < len(rest) && rest[j] == '=' {
			eq = true
			j++
		}
		n, ok := leadingInt(rest[j:])
		if !ok {
			return escaleRange{}, false
		}
		if eq {
			return escaleRange{min: 0, max: n}, true
		}
		return escaleRange{min: 0, max: n - 1}, true
	default: // 数字开头: 仅认 N-M 区间形态
		n1, rest2, ok := leadingIntRest(rest[i:])
		if !ok {
			return escaleRange{}, false
		}
		if !strings.HasPrefix(rest2, "-") {
			return escaleRange{}, false
		}
		n2, ok := leadingInt(rest2[1:])
		if !ok || n2 < n1 {
			return escaleRange{}, false
		}
		return escaleRange{min: n1, max: n2}, true
	}
}

// leadingInt 取串开头连续数字并转整;无数字返回 false。
func leadingInt(s string) (int, bool) {
	n, _, ok := leadingIntRest(s)
	return n, ok
}

// leadingIntRest 取串开头连续数字转整并返回剩余部分。
func leadingIntRest(s string) (int, string, bool) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, s, false
	}
	return n, s[end:], true
}

// ExpectedEScaleSymbols 返回给定行数按阈值表所属的全部 E 档符号(排序稳定)。
// 反查逻辑唯一事实源(A4): CheckEScale 与 Guide 预填共用本函数,绝不各持
// 第二份区间判定。阈值表不可用或行数落入字典空隙时返回空切片(不猜)。
func ExpectedEScaleSymbols(fileLines int, th *EScaleThresholds) []string {
	if !th.HasThresholds() {
		return nil
	}
	var expected []string
	for sym, sr := range th.ranges {
		if fileLines >= sr.min && fileLines <= sr.max {
			expected = append(expected, sym)
		}
	}
	sort.Strings(expected)
	return expected
}

// CheckEScale 按文件实际行数核对条目 E 档位(Warning 级)。
// fileLines 由调用方提供(行数事实源为 fs.CountFileLines,语义: 末行无换行
// 计 1 行);目录条目/不在盘文件的跳过判断也归调用方。
// 返回 nil = 合规或不可判定: 阈值表不可用 / 无标签结构 / 标签非标 /
// E 符号无阈值 / 行数落入字典空隙 / 行数在标注档位内(含边界重叠)。
func CheckEScale(line string, fileLines int, th *EScaleThresholds) *Violation {
	if !th.HasThresholds() {
		return nil
	}
	lb := strings.Index(line, "[")
	if lb < 0 {
		return nil
	}
	rb := strings.Index(line[lb:], "]:")
	if rb < 0 {
		return nil
	}
	tags := ParseTags(line[lb+1 : lb+rb])
	if len(tags) == 0 {
		return nil // 非标标签降级,不做档位判定(与 quota/dict 同策略)
	}
	e := tags["E"]
	if e == "" {
		return nil
	}
	r, ok := th.ranges[e]
	if !ok {
		return nil // 该符号无阈值声明,不可判定(符号合法性归 dict 闸)
	}
	if fileLines >= r.min && fileLines <= r.max {
		return nil // 标注档位含该行数即合规(边界重叠宽容)
	}
	// 行数实际所属档位经唯一反查函数取得(A4 单点化)
	expected := ExpectedEScaleSymbols(fileLines, th)
	if len(expected) == 0 {
		return nil // 字典空隙: 行数无所属档位,不越权裁决
	}
	return &Violation{
		Level: LevelWarning,
		Msg: fmt.Sprintf("E规模档位错配: 文件%d行按字典应为%s,条目标注%s —— 文件生长跨档属正常,请顺手更新E位",
			fileLines, strings.Join(expected, "/"), e),
	}
}
