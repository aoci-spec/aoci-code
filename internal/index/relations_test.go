// R字段单条形式提示测试。
//
// 这些用例的共同前提: 机器不核对R指向谁。曾经这里检查过目标是否存在、是否
// 符号链接、是否重复、是否自引用 —— 那些都是程序在替模型建立关系, 已经拆除。
package index

import (
	"strings"
	"testing"
)

func relationEntryLine(relation string) string {
	return "a.go[X.Y.5.T]: F:关系测试 | R:" +
		relation +
		" | A:- | S:-"
}

func requireRelationWarningContaining(
	t *testing.T,
	violations []Violation,
	anchor string,
) {
	t.Helper()

	for _, violation := range violations {
		if violation.Level == LevelWarning &&
			strings.Contains(
				violation.Msg,
				anchor,
			) {
			return
		}
	}

	t.Fatalf(
		"未找到包含%q的R形式Warning: %+v",
		anchor,
		violations,
	)
}

// 只要这一行读得通, 目标写什么都不产生提示: 不存在的文件、尚未创作的对象、
// 外部系统、同名多义的东西, 全是模型的语义自由。
func TestValidateEntryRelationsNeverJudgesWhatATargetPointsAt(
	t *testing.T,
) {
	root := t.TempDir()

	for _, test := range []struct {
		name     string
		relation string
	}{
		{"无关系占位", "-"},
		{"规范身份", "code:internal/service/store.go"},
		{"尚未创作的对象", "code:internal/service/planned_next_batch.go"},
		{"磁盘上根本不存在", "src/ghost.go"},
		{"指向自身", "a.go"},
		{"同一目标重复出现", "src/b.go,src/b.go"},
		{"等价表达的重复", "src/b.go,./src/b.go"},
		{"数据库身份", "database://primary/public/users"},
		{"外部URI", "https://example.invalid/contracts/users"},
		{"外部模块名", "github.com/example/module"},
		{"绝对路径", "/etc/passwd"},
		{"逃逸路径", "../../outside.go"},
		{"多个目标", "code:src/a.go,code:src/b.go,database://primary/public/users"},
	} {
		t.Run(test.name, func(t *testing.T) {
			violations := ValidateEntryRelations(
				root,
				"a.go",
				relationEntryLine(test.relation),
			)
			if len(violations) != 0 {
				t.Fatalf(
					"机器不应评判R指向: %+v",
					violations,
				)
			}
		})
	}
}

// 保留的只有"这一行本身读不通"的提示, 而且永远只是Warning。
func TestValidateEntryRelationsWarnsOnSingleLineFormWithoutBlocking(
	t *testing.T,
) {
	root := t.TempDir()

	for _, test := range []struct {
		name     string
		relation string
		anchor   string
	}{
		{"空R字段", "", "R字段为空"},
		{"全角逗号", "code:src/a.go，code:src/b.go", "全角逗号"},
		{"空片段", "code:src/a.go,,code:src/b.go", "第2项为空"},
		{"占位符与真实目标混用", "-,code:src/a.go", "占位符与真实目标混用"},
	} {
		t.Run(test.name, func(t *testing.T) {
			violations := ValidateEntryRelations(
				root,
				"a.go",
				relationEntryLine(test.relation),
			)
			requireRelationWarningContaining(t, violations, test.anchor)
			for _, violation := range violations {
				if violation.Level != LevelWarning {
					t.Fatalf(
						"R形式提示不得产生硬拒: %+v",
						violations,
					)
				}
			}
		})
	}
}

// 单条能产生的提示有上限, 异常候选不会刷屏。
func TestValidateEntryRelationsBoundsReportedTargets(
	t *testing.T,
) {
	targets := make([]string, 0, maxRelationTargetsToReport+10)
	for index := 0; index < maxRelationTargetsToReport+10; index++ {
		targets = append(targets, "")
	}
	violations := ValidateEntryRelations(
		t.TempDir(),
		"a.go",
		relationEntryLine(strings.Join(targets, ",")),
	)
	requireRelationWarningContaining(t, violations, "超过单条提示上限")
	if len(violations) > maxRelationTargetsToReport+1 {
		t.Fatalf("提示数量未被限制: %d", len(violations))
	}
}

func TestValidateEntryRelationsLeavesBrokenStructureToFormatGate(
	t *testing.T,
) {
	violations := ValidateEntryRelations(
		t.TempDir(),
		"src/a.go",
		"这不是条目",
	)

	if len(violations) != 0 {
		t.Fatalf(
			"结构错误应由格式闸负责，R形式提示不得重复报告: %+v",
			violations,
		)
	}
}
