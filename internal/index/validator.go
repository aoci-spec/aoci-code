// 条目格式与 S 字段纪律校验(独立成文承载持续生长的纪律)
// 索引条目: validator.go[Index.Val.9.S]
//
// 分级:
//
//	硬拒(LevelError)  —— 多行 / 无 filename[标签]: 结构 / 文件名与 path 末段不一致
//	                     / F R A S 四段缺失 / F R A 段前缀重复(实弹八人审抓获的逃逸形态)
//	警告(LevelWarning)—— 标签不可解析(疑似缺位,P-15) / 演进叙事词命中(提示改写为
//	                     当前状态陈述,不阻断) / S 超 C 配额(见 quota.go)
//
// 纪律: 校验不合格绝不静默修正 —— 静默修正等于教会 agent 继续给脏数据;
// 一律返回可操作的拒绝理由,由调用方(MCP 工具/CLI)透传给写入者。
//
// 配额两层接线(P-22 批次): ValidateEntryLineWith 接收 *SQuotaThresholds,
// 头部 "#S配额:" 声明生效(未声明档回退 spec 默认,见 quota.go 裁决注释);
// ValidateEntryLine 为兼容薄壳(传 nil = 全默认),既有调用方零迁移成本,
// 逐调用方现读后迁移到 With 变体(R50)。
//
// 段前缀重复硬拒的由来(实弹八,2026-07-10): 模型草稿产出 "R:a | R:b | R:c" 三段 R,
// quota 闸与 dict 闸均不覆盖该形态,靠 diff 人审拦获 —— 段结构重复是格式损坏
// 而非策展欠账(与"缺段"同性质),且拆解字段(Entry.F/R/Api/S)单值语义下多段必致
// 信息静默丢失,故取 Error 级。S 除外: S:/S1:/S2:/S3: 变体多段是既有协议特性
// (splitFRAS 合并进 S),裸 S: 重复在变体语义下同族,S 维持宽容。
//
// 标签不可解析警告的由来(P-15,2026-07-12 httpx 真机实弹 UAU8/MM7 双实证):
// 缺 E 位的四位紧凑标签(如 UAU8——首个数字 8 恰在末位)被 ParseTags 判非标
// 返回空 map,而 dict 闸与 escale 闸的跳过条件都是"解析结果为空"—— "缺一位"
// 与"完全没有标签结构"被混为同一种免检状态,agent 写错标签,双闸静默放行。
// 修法: 本层区分两态 —— [标签] 结构存在但不可解析 = 疑似缺位/形态错误,给
// Warning 显式点名(明示双闸已跳过);Warning 不升 Error —— 存量手写非标标签
// 的仓库(平台旧索引兼容形态)不能被堵死一切回写,与配额闸同哲学: 警告放行,
// 保留人裁决权。ParseTags 本身不动 —— 它是三方同步的切分单一事实源(R6),
// 部分解析属协议级演进,归后续批次经 Spec 裁决。
//
// 演进叙事词表与S字数配额均由internal/machinecontract单点提供；Validator
// 直接消费机器值，Spec与Prompt只解释语义目的，不再复制机器集合。
package index

import (
	"path"
	"strconv"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// 违规级别
const (
	LevelError   = "error"   // 硬拒: 写入必须被拒绝
	LevelWarning = "warning" // 警告: 允许写入但提示整改
)

// Violation 一条校验违规
type Violation struct {
	Level string // LevelError / LevelWarning
	Msg   string // 面向写入者的可操作说明
}

// HasError 判断违规列表中是否含硬拒项
func HasError(vs []Violation) bool {
	for _, v := range vs {
		if v.Level == LevelError {
			return true
		}
	}
	return false
}

// StripFences 清理 agent 回写输入: 剔除 Markdown 代码块围栏与首尾空行/空白。
// agent 常把条目包在 ``` 围栏里返回,围栏不是条目内容;
// 只剥离首尾围栏行,不触碰正文中的反引号。
func StripFences(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	// 掐头去尾的空行
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	lines = lines[start:end]
	// 首尾围栏行(``` 或 ```lang)
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ValidateEntryLine 校验一条待写入的条目(兼容入口: 配额走 spec 默认,
// 等价 ValidateEntryLineWith(relPath, line, nil))。
func ValidateEntryLine(relPath, line string) []Violation {
	return ValidateEntryLineWith(relPath, line, nil)
}

// ValidateEntryLineWith 校验一条待写入的条目(P-22 配额两层入口)。
// relPath: 目标文件相对仓库根路径(正斜杠);line: 经 StripFences 清理后的候选条目;
// th: 头部 S配额 声明表(nil 或无声明档位走 spec 默认,见 quota.go)。
// 返回违规列表(可同时含 error 与 warning);空列表 = 完全合规。
func ValidateEntryLineWith(relPath, line string, th *SQuotaThresholds) []Violation {
	var vs []Violation

	// 1. 单行约束(索引协议基石: 一条目一行,diff 即变更记录)
	if strings.Contains(line, "\n") {
		vs = append(vs, Violation{LevelError,
			"条目必须为单行: 检测到换行符。请将全部内容压入一行,长内容用分号衔接"})
		return vs // 多行时后续行级校验无意义,直接返回
	}
	if strings.TrimSpace(line) == "" {
		vs = append(vs, Violation{LevelError, "条目为空"})
		return vs
	}

	// 2. 结构约束: 必须匹配 文件名[标签]: 正文
	m := consistencyEntryRe.FindStringSubmatch(line)
	if m == nil {
		vs = append(vs, Violation{LevelError,
			"条目结构不合规: 必须为 文件名[标签]: F:... | R:... | A:... | S:... 单行格式"})
		return vs
	}
	filename := strings.TrimSpace(m[1])
	rest := m[3]

	// 3. 文件名一致性: 条目文件名末段必须与 relPath 末段一致(同名误改防线的写侧闸门)
	wantBase := path.Base(strings.TrimSuffix(strings.ReplaceAll(relPath, "\\", "/"), "/"))
	gotBase := path.Base(strings.TrimSuffix(filename, "/"))
	if wantBase != gotBase {
		vs = append(vs, Violation{LevelError,
			"文件名不一致: 条目写的是 " + gotBase + ",目标路径末段是 " + wantBase +
				"。条目文件名必须与 path 末段一致"})
	}

	// 4. 标签可解析性(警告级,P-15): [标签] 结构存在但 ParseTags 返回空 map =
	// 疑似缺位(如四位紧凑标签 UAU8 缺 E 位)或形态错误 —— 此态下 dict 闸与
	// escale 闸都会跳过(它们的跳过条件即"解析结果为空"),必须在此显式点名,
	// 把静默免检变成可见提醒。合法标签与结构缺失(上面已 Error)都不触发本项。
	tagsRaw := strings.TrimSpace(m[2])
	if len(ParseTags(tagsRaw)) == 0 {
		vs = append(vs, Violation{LevelWarning,
			"标签不可解析(\"" + tagsRaw + "\"): 无法按 ABCDE 规则切分 —— 紧凑形态要求" +
				"首字符A层级+B模块+数字C重要度+可选D特征+末位E规模(疑似缺位),点分形态要求4或5段。" +
				"此态下字典与档位核对均被跳过(盲区状态),请按索引头部字典补全标签"})
	}

	// 5. FRAS 四段齐备 + F/R/A 段前缀唯一(值可为 "-";S1/S2 等变体计入 S)。
	// 计数而非布尔: 布尔只能判"有没有",判不出"有几个"—— 实弹八模型草稿产出
	// 三段 R:(R:a | R:b | R:c),布尔实现下完全不可见,靠人审拦获后升级为计数硬拒。
	// S 不计重复: S:/S1:/S2:/S3: 变体多段是协议特性(splitFRAS 合并进 S)。
	cntF, cntR, cntA, hasS := 0, 0, 0, false
	for _, seg := range strings.Split(rest, " | ") {
		seg = strings.TrimSpace(seg)
		switch {
		case strings.HasPrefix(seg, "F:"):
			cntF++
		case strings.HasPrefix(seg, "R:"):
			cntR++
		case strings.HasPrefix(seg, "A:"):
			cntA++
		case strings.HasPrefix(seg, "S:") || regexpSVariant.MatchString(seg):
			hasS = true
		}
	}
	var missing []string
	if cntF == 0 {
		missing = append(missing, "F")
	}
	if cntR == 0 {
		missing = append(missing, "R")
	}
	if cntA == 0 {
		missing = append(missing, "A")
	}
	if !hasS {
		missing = append(missing, "S")
	}
	if len(missing) > 0 {
		vs = append(vs, Violation{LevelError,
			"缺少要素段: " + strings.Join(missing, "/") +
				"。四要素必须齐备(无内容用 - 占位,如 A:-),分隔符为「空格|空格」"})
	}
	var dup []string
	if cntF > 1 {
		dup = append(dup, "F×"+strconv.Itoa(cntF))
	}
	if cntR > 1 {
		dup = append(dup, "R×"+strconv.Itoa(cntR))
	}
	if cntA > 1 {
		dup = append(dup, "A×"+strconv.Itoa(cntA))
	}
	if len(dup) > 0 {
		vs = append(vs, Violation{LevelError,
			"要素段重复: " + strings.Join(dup, "/") +
				"。F/R/A 各只能出现一段,多个值请在段内用逗号或分号衔接(如 R:a.go,b.go)"})
	}

	// 6. 演进叙事检测(警告级): S 必须是当前状态陈述
	for _, w := range machinecontract.EvolutionNarrativeTerms() {
		if strings.Contains(line, w) {
			vs = append(vs, Violation{LevelWarning,
				"疑似演进叙事(\"" + w + "\"): 条目应陈述文件此刻是什么,不是发生过什么;变更历史归 Git"})
			break // 一次警告足够,不逐词刷屏
		}
	}

	// 7. S 超 C 配额检查(警告级,quota.go;与演进叙事警告可叠加不互斥;
	// th 为 nil 时走 spec 默认,声明命中档用声明值,P-22 两层语义)
	if v := CheckSQuotaWith(line, th); v != nil {
		vs = append(vs, *v)
	}
	return vs
}
