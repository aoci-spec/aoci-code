// R字段的单条形式提示。
//
// AOCI条目的R字段是模型创作的语义标注，不是CLI执行路径，也不会直接控制文件
// 写入。条目之间的关系由模型读完整索引时用注意力机制建立，程序不参与：本检查
// 器不解析R目标指向谁，不查询文件系统，不判断目标是否存在、是否同一实体、是否
// 重复，也不建立任何关系图。R指向尚未创作的对象、已经移除的对象、外部系统或
// 同名多义的东西，都是模型的语义自由。
//
// 这里只看这一行本身能不能读：分隔符是否规范、有没有空片段、占位符有没有和真实
// 目标混用。所有结果都是Warning，永远不阻断Check、人工Apply、MCP回写或Auto
// Apply。真正的写入目标path仍由fs.NormalizeRelPath执行安全硬闸。
package index

import (
	"fmt"
	"strings"
)

// maxRelationTargetsToReport 限制单条能产生的形式提示数量，避免异常候选刷屏。
const maxRelationTargetsToReport = 64

// ValidateEntryRelations对候选条目的R字段执行单条形式检查。
//
// repoRoot保留在签名里以维持既有调用点不变；本函数不再访问文件系统。
// relPath是当前条目对应的仓库相对路径；line必须是已经StripFences处理后的
// 完整单行条目。
//
// 条目结构、R段缺失或重复仍由ValidateEntryLineWith负责。本函数在条目结构
// 可解析时检查R值，并且只返回LevelWarning。
func ValidateEntryRelations(
	repoRoot,
	relPath,
	line string,
) []Violation {
	match := consistencyEntryRe.FindStringSubmatch(line)
	if match == nil {
		// 条目结构错误由格式闸负责，避免同一问题重复报告。
		return nil
	}

	_, relationText, _, _ := splitFRAS(match[3])
	relationText = strings.TrimSpace(relationText)

	if relationText == "" {
		return []Violation{relationWarning(
			"R字段为空: 无明确跨文件强依赖时建议使用 R:- 占位",
		)}
	}
	if relationText == "-" {
		return nil
	}

	violations := []Violation{}

	if strings.Contains(relationText, "，") {
		violations = append(
			violations,
			relationWarning(
				"R字段使用了全角逗号，建议改用半角逗号分隔目标",
			),
		)
		relationText = strings.ReplaceAll(
			relationText,
			"，",
			",",
		)
	}

	rawTargets := strings.Split(
		relationText,
		",",
	)
	checkTargets := rawTargets

	if len(rawTargets) > maxRelationTargetsToReport {
		violations = append(
			violations,
			relationWarning(
				fmt.Sprintf(
					"R目标共%d项，超过单条提示上限%d；仅提示前%d项，"+
						"Auto流程不会因此停止",
					len(rawTargets),
					maxRelationTargetsToReport,
					maxRelationTargetsToReport,
				),
			),
		)
		checkTargets = rawTargets[:maxRelationTargetsToReport]
	}

	for position, rawTarget := range checkTargets {
		target := strings.TrimSpace(rawTarget)

		if target == "" {
			violations = append(
				violations,
				relationWarning(
					fmt.Sprintf(
						"R目标第%d项为空，建议删除重复分隔符",
						position+1,
					),
				),
			)
			continue
		}

		if target == "-" {
			violations = append(
				violations,
				relationWarning(
					"R占位符与真实目标混用；无关系时写 R:-，"+
						"有关系时只列目标",
				),
			)
		}
	}

	return violations
}

func relationWarning(message string) Violation {
	return Violation{
		Level: LevelWarning,
		Msg:   message,
	}
}
