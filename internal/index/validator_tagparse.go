// 标签不可解析警告的分类辅助。
//
// 背景(P-15/R51): ValidateEntryLineWith 已负责识别“[标签]结构在场但无法按
// ABCDE 规则切分”的 Warning。score 需要把该 Warning 独立汇总为 tagparse
// 维度,但不得复制 ParseTags 或重新发明标签判据,否则会形成第二套隐性规则。
//
// 本文件只做“对既有 Violation 分类”,不参与标签解析、不改变校验级别。
package index

import "strings"

const tagParseWarningPrefix = "标签不可解析("

// HasTagParseWarning 判断违规列表中是否包含标签不可解析 Warning。
//
// 该函数是 tagparse 报表维度识别此类 Warning 的唯一入口。判断同时要求
// LevelWarning 与稳定文案前缀,避免把格式 Error、配额 Warning 或演进叙事
// Warning 混入。标签是否可解析仍完全由 ValidateEntryLineWith→ParseTags 决定。
func HasTagParseWarning(vs []Violation) bool {
	for _, v := range vs {
		if v.Level == LevelWarning && strings.HasPrefix(v.Msg, tagParseWarningPrefix) {
			return true
		}
	}
	return false
}
