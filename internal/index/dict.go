// 标签字典提取与校验: 从索引头部提取 ABCDE 字典,校验条目标签字典合规性
// 索引条目: dict.go[IVL8M]
//
// 定位(D40 CLI 侧参考实现): 臆造标签不破坏解析(ParseTags 照样切分)但语义
// 层作废 —— agent 属性查询不知臆造符号何意。平台两轮审计证明缺此闸是臆造
// 标签病反复发作的根因;第二次实弹(2026-07-09)在 CLI 侧实锤同一缺口
// (E 位非法的 DC7Meta 被放进正式索引)。本文件即该闸的参考实现,平台照抄。
//
// 切分同源纪律: 校验用 ParseTags(types.go)做标签切分,绝不自写第二套
// 启发式 —— 切分规则只有一份,字典校验只判"切出来的符号在不在字典"。
//
// 级别 Warning(与配额闸同哲学): 存量臆造标签的仓库不能被堵死回写;
// AI 批量生成场景(增量无存量包袱)由 CLI 层升为硬拒 —— D40"字典外直接拒"
// 的原始语义在 entries apply 兑现。
//
// 跨维混淆点破(P-14,2026-07-12 httpx 实弹): AI 起草的字典曾埋 A/B 维语义
// 重叠坑,冷启动 10 发 dict 拦截的病根是符号放错维(A 维语义写进 B 位)而非
// 真臆造 —— 旧文案只报"不在字典",agent 会误判为该扩典或换符号。修法:
// 违规符号恰好存在于另一维字典时,文案点名"是X维符号,疑似维度错位",把
// 病根从"符号不存在"纠正为"符号放错位";起草端预防归 prompt 资产
// header-dict-rules.txt 第 5 条(A/B 语义不重叠),两端合围。
//
// 字典行识别(P-21 修法,2026-07-12 httpx-rerun 真机实弹): 旧实现对剥 # 后的
// 行做 HasPrefix(t,"A层级:") 硬前缀匹配 —— AI 起草的实物形态
// "#A层级(python,架构角色): I-接口(...)" 在维名与冒号间插了括号夹注,前缀
// 匹配失败,A/B 双维零提取 → HasDict false → maintain 判"字典未立约"暂停
// 派发,且 CheckTagsAgainstDict 对全部条目静默跳过(字典闸假绿,与 P-15 的
// 静默免检同族,这次轮到字典闸自己)。本仓头部为紧凑形态两路皆过,故本仓
// 测试全绿真机才炸。修法 parseDictLine 三形态容错: 剥 # 后再 TrimSpace
// (容忍"# A层级"),行内首个冒号半角/全角均认,冒号前文本剥括号夹注后精确
// 等于维名才命中 —— 精确相等保住不误伤(头部其余"xxx:"行维名不等即不命中)。
// 内容侧同样剥夹注后再收符号: 夹注内的示例文件路径词(如 httpx/_api.py)
// 曾被逐词取首字母误收为符号(httpx 入 A 字典 = 符号污染,字典被动变宽松)。
//
// 维名白名单含跨闸门共享维(P-22 批次): E规模 由 escale.go 消费(阈值提取),
// S配额 由 quota.go 消费(配额提取)—— parseDictLine 是全部字典行识别的
// 单点实现,新增可识别维名只改本函数白名单,消费逻辑归各闸自有文件;
// 本文件的 ExtractTagDict 消费 A层级/B模块/C重要度/D特征/E规模(S配额 非标签维,
// 其 switch 无分支自然跳过)。
//
// 字典行形态(宽容提取): 头部 A/B/C/D/E 行(容忍上述变体),
// 内容按空白分词；字母轴取开头连续 ASCII 字母，C轴取一位数字 —— 兼容 "X-Script(说明)"
// (连字符分隔)、"I索引核心"(符号中文连写)、"AI-AI增强"(多字母符号)三形态。
// D 特征是可选轴；字典未声明 D 时，Entry 只能省略 D。
package index

import (
	"fmt"
	"sort"
	"strings"
)

// TagDict 头部提取的标签字典(键=合法符号)
type TagDict struct {
	A map[string]bool // 层级符号
	B map[string]bool // 模块符号
	C map[string]bool // 重要度符号
	D map[string]bool // 可选特征符号
	E map[string]bool // 规模符号

	declared    map[string]bool
	malformed   []string
	definitions map[string]map[string]string
	conflicts   []tagDictionaryConflict
}

// tagDictionaryConflict records one symbol that is assigned more than one
// meaning inside the same axis. A conflicting dictionary is never widened by
// unioning the declarations.
type tagDictionaryConflict struct {
	Axis   string
	Symbol string
	First  string
	Second string
}

// TagViolation is a deterministic axis-level decision produced after the tag
// has been split by ParseTags. It is shared by Legacy warnings and Volumes
// candidate/projected validation; it does not introduce another tag parser.
type TagViolation struct {
	Axis     string
	Value    string
	Expected string
	Actual   string
	Cause    string
}

// HasDict 判断字典是否可用于校验(A 与 B 至少各有一个符号)。
// 头部无字典行/提取为空时返回 false,调用方应跳过校验(不可判定不误报)。
func (d *TagDict) HasDict() bool {
	return d != nil && len(d.A) > 0 && len(d.B) > 0
}

// HasObjectContract reports whether the dictionary is a complete authority for
// a Volumes v1 object. D remains optional; when it is absent the only legal D
// representation is omission.
func (d *TagDict) HasObjectContract() bool {
	return d != nil && len(d.ObjectContractProblems()) == 0
}

// ObjectContractProblems returns deterministic machine facts for a missing,
// malformed, or internally conflicting Volumes object dictionary.
func (d *TagDict) ObjectContractProblems() []string {
	if d == nil {
		return []string{"dictionary=missing"}
	}
	var problems []string
	for _, axis := range []string{"A", "B", "C", "E"} {
		if !d.declared[axis] || len(d.axis(axis)) == 0 {
			problems = append(problems, "axis="+axis+";state=missing_or_unparseable")
		}
	}
	if d.declared["D"] && len(d.D) == 0 {
		problems = append(problems, "axis=D;state=unparseable")
	}
	problems = append(problems, d.malformed...)
	for _, conflict := range d.conflicts {
		problems = append(problems, fmt.Sprintf(
			"axis=%s;symbol=%s;state=conflict;definitions=%s,%s",
			conflict.Axis, conflict.Symbol, conflict.First, conflict.Second,
		))
	}
	sort.Strings(problems)
	return uniqueStrings(problems)
}

// Contract describes the exact current dictionary facts in a stable order.
func (d *TagDict) Contract() string {
	if d == nil {
		return "dictionary=missing"
	}
	return strings.Join([]string{
		"format=compact(A+B+C+[D]+E)",
		"A=" + joinedSymbols(d.A),
		"B=" + joinedSymbols(d.B),
		"C=" + joinedSymbols(d.C),
		"D=" + optionalSymbols(d.D),
		"E=" + joinedSymbols(d.E),
	}, ";")
}

// Definition returns the exact parsed meaning bound to one axis symbol. It is
// used only when a consumer must distinguish an official semantic calibration
// from a custom dictionary that happens to reuse the same letters.
func (d *TagDict) Definition(axis, symbol string) (string, bool) {
	if d == nil || d.definitions[axis] == nil {
		return "", false
	}
	definition, ok := d.definitions[axis][symbol]
	return definition, ok
}

func (d *TagDict) axis(axis string) map[string]bool {
	switch axis {
	case "A":
		return d.A
	case "B":
		return d.B
	case "C":
		return d.C
	case "D":
		return d.D
	case "E":
		return d.E
	default:
		return nil
	}
}

// stripParens 剥除文本中的括号段(半角()与全角（）,支持嵌套按深度计)。
// 双重用途(P-21): 维名侧剥夹注令 "A层级(python,架构角色)" 可与 "A层级"
// 精确比对;内容侧剥夹注防夹注内含空格的示例路径被误收为符号。
// 不配对的右括号按原样保留深度不减到负(防畸形输入吞掉后续全部文本)。
func stripParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// parseDictLine 尝试把一行识别为字典行(P-21 单点实现)。
// 命中返回(维名,冒号后内容,true);容错三形态 —— # 后空格 / 全角冒号 /
// 维名与冒号间括号夹注;维名剥夹注并 TrimSpace 后须精确等于
// A层级/B模块/C重要度/D特征/E规模/S配额(精确相等防误伤头部其余含冒号的说明行)。
// 消费方分工: A/B/C/D/E 归 ExtractTagDict，E阈值另由 escale.go 消费,
// S配额 归 quota.go 的 ExtractSQuotaThresholds(P-22 批次新增维)。
func parseDictLine(line string) (dim, content string, ok bool) {
	t := strings.TrimSpace(line)
	t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
	// 行内首个冒号: 半角与全角谁先出现认谁
	ih := strings.Index(t, ":")
	iw := strings.Index(t, "：")
	idx, clen := -1, 0
	switch {
	case ih >= 0 && (iw < 0 || ih < iw):
		idx, clen = ih, len(":")
	case iw >= 0:
		idx, clen = iw, len("：")
	}
	if idx < 0 {
		return "", "", false
	}
	name := strings.TrimSpace(stripParens(t[:idx]))
	switch name {
	case "A层级", "A Layer":
		return "A层级", t[idx+clen:], true
	case "B模块", "B Module":
		return "B模块", t[idx+clen:], true
	case "C重要度", "C Importance":
		return "C重要度", t[idx+clen:], true
	case "D特征", "D Trait":
		return "D特征", t[idx+clen:], true
	case "E规模", "E Scale":
		return "E规模", t[idx+clen:], true
	case "S配额", "S quota", "S Quota":
		return "S配额", t[idx+clen:], true
	}
	return "", "", false
}

// ExtractTagDict 从头部文本提取字典。
// 行识别经 parseDictLine(三形态容错,见 P-21 注释);
// E 行缺失时回退默认 LMST(spec 既定四值)。
func ExtractTagDict(headerText string) *TagDict {
	d := &TagDict{
		A:           map[string]bool{},
		B:           map[string]bool{},
		C:           map[string]bool{},
		D:           map[string]bool{},
		E:           map[string]bool{},
		declared:    map[string]bool{},
		definitions: map[string]map[string]string{},
	}
	for _, line := range strings.Split(headerText, "\n") {
		dim, content, ok := parseDictLine(line)
		if !ok {
			continue
		}
		// 内容侧剥夹注后收符号(P-21: 防夹注内示例路径词污染字典);
		// S配额 维无分支自然跳过(消费归 quota.go)
		switch dim {
		case "A层级":
			d.collectDimension("A", stripParens(content))
		case "B模块":
			d.collectDimension("B", stripParens(content))
		case "C重要度":
			d.collectDimension("C", stripParens(content))
		case "D特征":
			d.collectDimension("D", stripParens(content))
		case "E规模":
			d.collectDimension("E", stripParens(content))
		}
	}
	if len(d.E) == 0 {
		for _, s := range []string{"L", "M", "S", "T"} {
			d.E[s] = true
		}
	}
	return d
}

// ExtractScopedTagDict selects exactly one named dictionary from a Volumes
// Meta asset and delegates every axis parse to ExtractTagDict. A missing or
// duplicate section is unusable instead of being silently merged.
func ExtractScopedTagDict(metaText, profile string) *TagDict {
	startMarker := "#[Tag dictionary: " + profile + "]"
	sectionCount := 0
	collect := false
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(metaText, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#[Tag dictionary:") {
			collect = trimmed == startMarker
			if collect {
				sectionCount++
			}
			continue
		}
		if collect && sectionCount == 1 {
			lines = append(lines, line)
		}
	}
	if sectionCount == 0 {
		return nil
	}
	dictionary := ExtractTagDict(strings.Join(lines, "\n"))
	if sectionCount != 1 {
		dictionary.malformed = append(dictionary.malformed,
			fmt.Sprintf("profile=%s;state=duplicate;count=%d", profile, sectionCount))
	}
	return dictionary
}

func (d *TagDict) collectDimension(axis, content string) {
	d.declared[axis] = true
	if d.definitions[axis] == nil {
		d.definitions[axis] = map[string]string{}
	}
	words := strings.Fields(content)
	if len(words) == 0 {
		d.malformed = append(d.malformed, "axis="+axis+";state=unparseable")
		return
	}
	accepted := 0
	for _, word := range words {
		symbol := leadingASCIILetters(word)
		if axis == "C" {
			symbol = leadingASCIIDigits(word)
		}
		if !validDictionarySymbol(axis, symbol) {
			continue
		}
		accepted++
		d.axis(axis)[symbol] = true
		definition := strings.TrimSpace(strings.TrimPrefix(word, symbol))
		if previous, ok := d.definitions[axis][symbol]; ok && previous != definition {
			d.conflicts = append(d.conflicts, tagDictionaryConflict{
				Axis: axis, Symbol: symbol, First: previous, Second: definition,
			})
		} else {
			d.definitions[axis][symbol] = definition
		}
	}
	if accepted == 0 {
		d.malformed = append(d.malformed, "axis="+axis+";state=unparseable")
	}
}

func validDictionarySymbol(axis, symbol string) bool {
	if symbol == "" {
		return false
	}
	if axis == "C" {
		return len(symbol) == 1 && symbol[0] >= '1' && symbol[0] <= '9'
	}
	if axis == "A" || axis == "E" {
		return len(symbol) == 1
	}
	return true
}

// leadingASCIILetters 取词开头连续的 ASCII 字母段
func leadingASCIILetters(word string) string {
	end := 0
	for end < len(word) {
		c := word[end]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			end++
			continue
		}
		break
	}
	return word[:end]
}

func leadingASCIIDigits(word string) string {
	end := 0
	for end < len(word) && word[end] >= '0' && word[end] <= '9' {
		end++
	}
	return word[:end]
}

// crossDimHint 跨维错位提示(P-14): 违规符号恰好存在于另一维字典时,点名疑似
// 维度错位;不存在于任何维时返回空(维持"不在字典"原语义)。
func crossDimHint(sym string, dict *TagDict, selfDim string) string {
	var others []string
	if selfDim != "A" && dict.A[sym] {
		others = append(others, "A层级")
	}
	if selfDim != "B" && dict.B[sym] {
		others = append(others, "B模块")
	}
	if selfDim != "C" && dict.C[sym] {
		others = append(others, "C重要度")
	}
	if selfDim != "D" && dict.D[sym] {
		others = append(others, "D特征")
	}
	if selfDim != "E" && dict.E[sym] {
		others = append(others, "E规模")
	}
	if len(others) == 0 {
		return ""
	}
	return "(该符号是" + strings.Join(others, "/") + "字典的既有符号,疑似维度错位——五维各司其职,请核对标签内符号顺序而非扩典)"
}

// CheckTagsAgainstDict 校验条目行标签的字典合规性(Warning 级)。
// 切分经 ParseTags 同源;非标标签(空 map)不判定 —— 结构问题归 ValidateEntryLine。
// 返回 nil = 合规或不可判定;违规返回含逐维说明的 Warning(符号存在于另一维
// 字典时点名疑似维度错位,P-14)。
func CheckTagsAgainstDict(line string, dict *TagDict) *Violation {
	if !dict.HasDict() {
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
		return nil // 非标标签降级,不做字典判定
	}
	violations := ValidateTagAgainstDict(tags, "", dict)
	var probs []string
	for _, violation := range violations {
		// Legacy headers predate C/D dictionary declarations. Preserve their
		// warning contract while validating the extra axes whenever declared.
		if (violation.Axis == "C" || violation.Axis == "D") && !dict.declared[violation.Axis] {
			continue
		}
		legacyCause := violation.Cause
		switch violation.Axis {
		case "A":
			legacyCause = "A层级符号" + violation.Value + "不在字典"
		case "B":
			legacyCause = "B模块符号" + violation.Value + "不在字典"
		case "E":
			legacyCause = "E规模符号" + violation.Value + "非法(合法值见头部E规模行)"
		}
		probs = append(probs, legacyCause+crossDimHint(violation.Value, dict, violation.Axis))
	}
	if len(probs) == 0 {
		return nil
	}
	return &Violation{
		Level: LevelWarning,
		Msg:   "标签字典违规: " + strings.Join(probs, ";") + " —— 臆造符号不破坏解析但语义作废,请用头部字典既有符号或先扩典",
	}
}

// ValidateTagAgainstDict validates one ParseTags result against one current
// dictionary. tagRaw is included only in the returned machine facts.
func ValidateTagAgainstDict(tags map[string]string, tagRaw string, dict *TagDict) []TagViolation {
	if dict == nil || len(tags) == 0 {
		return nil
	}
	var violations []TagViolation
	labels := map[string]string{"A": "A Layer", "B": "B Module", "C": "C Importance", "D": "D Trait", "E": "E Scale"}
	for _, axis := range []string{"A", "B", "C", "D", "E"} {
		value := tags[axis]
		allowed := dict.axis(axis)
		legal := value != "" && allowed[value]
		if axis == "D" && value == "" && len(allowed) == 0 {
			legal = true
		}
		if legal {
			continue
		}
		if axis == "D" && value == "" {
			continue
		}
		actualValue := value
		if actualValue == "" {
			actualValue = "-"
		}
		violations = append(violations, TagViolation{
			Axis: axis, Value: value, Expected: dict.Contract(),
			Actual: tagActual(tagRaw, tags),
			Cause:  fmt.Sprintf("%s value %s is not declared by the current Meta dictionary", labels[axis], actualValue),
		})
	}
	return violations
}

func tagActual(raw string, tags map[string]string) string {
	value := func(axis string) string {
		if tags[axis] == "" {
			return "-"
		}
		return tags[axis]
	}
	return strings.Join([]string{
		"tag=" + raw, "A=" + value("A"), "B=" + value("B"),
		"C=" + value("C"), "D=" + value("D"), "E=" + value("E"),
	}, ";")
}

func joinedSymbols(values map[string]bool) string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func optionalSymbols(values map[string]bool) string {
	if len(values) == 0 {
		return "-"
	}
	return joinedSymbols(values)
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
