// Single-entry form hint tests for the R field.
//
// The shared premise of these cases: the machine does not check who R points at. This place once checked whether the target existed, was a
// symbolic link, was duplicated, or was a self-reference; all of that was the program building relations on the model's behalf, and it has been torn out.
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

// As long as the line reads, whatever the target says produces no hint: a file that does not exist, an object not yet authored,
// an external system, something ambiguous by name, all of it is the model's semantic freedom.
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

// The only hint kept is "this line itself does not read", and it is always just a Warning.
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

// One entry can produce only so many hints, so an abnormal candidate does not flood the output.
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
