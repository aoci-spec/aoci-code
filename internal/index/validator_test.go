// 校验器表驱动测试: 硬拒/警告分级、F柔性口径与围栏清理。
package index

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// TestValidateEntryLine覆盖格式硬拒、警告和F长度不阻断合同。
func TestValidateEntryLine(t *testing.T) {
	cases := []struct {
		name, path, line  string
		wantErr, wantWarn bool
	}{
		{"合规", "a/b.go", "b.go[X.Y.5.T]: F:功 | R:- | A:- | S:当前态", false, false},
		{"合规目录条目", "a/dir/", "dir/[X.Y.5.T]: F:目录 | R:- | A:- | S:-", false, false},
		{"长F不阻断自动流程", "a/b.go", "b.go[X.Y.5.T]: F:实现依赖求解并提供兼容入口 | R:- | A:- | S:-", false, false},
		{"F占位不因语义质量硬拒", "a/b.go", "b.go[X.Y.5.T]: F:- | R:- | A:- | S:-", false, false},
		{"多行", "a/b.go", "b.go[X.Y.5.T]: F:- | R:- | A:- | S:1\n2", true, false},
		{"空条目", "a/b.go", "   ", true, false},
		{"缺结构", "a/b.go", "这不是条目", true, false},
		{"文件名不符", "a/b.go", "c.go[X.Y.5.T]: F:- | R:- | A:- | S:-", true, false},
		{"缺FRAS", "a/b.go", "b.go[X.Y.5.T]: F:- | R:-", true, false},
		{"演进叙事", "a/b.go", "b.go[X.Y.5.T]: F:- | R:- | A:- | S:本次修改改为新版", false, true},
		{"重复R段", "a/b.go", "b.go[X.Y.5.T]: F:功 | R:x.go | R:y.go | R:z.go | A:- | S:当前态", true, false},
		{"重复F段", "a/b.go", "b.go[X.Y.5.T]: F:一 | F:二 | R:- | A:- | S:当前态", true, false},
		{"S变体多段放行", "a/b.go", "b.go[X.Y.5.T]: F:功 | R:- | A:- | S1:一 | S2:二", false, false},
		{"缺E位紧凑标签警告(UAU8实弹)", "a/b.go", "b.go[UAU8]: F:功 | R:- | A:- | S:当前态", false, true},
		{"缺E位紧凑标签警告(MM7实弹)", "a/b.go", "b.go[MM7]: F:功 | R:- | A:- | S:当前态", false, true},
		{"合规紧凑标签不误报", "a/b.go", "b.go[WA9JM]: F:功 | R:- | A:- | S:当前态", false, false},
		{"点分缺段同样警告", "a/b.go", "b.go[X.Y.5]: F:功 | R:- | A:- | S:当前态", false, true},
	}

	for _, current := range cases {
		t.Run(
			current.name,
			func(t *testing.T) {
				violations := ValidateEntryLine(
					current.path,
					current.line,
				)

				if HasError(violations) !=
					current.wantErr {
					t.Fatalf(
						"硬拒判定不符: 期望%v,违规=%v",
						current.wantErr,
						violations,
					)
				}

				hasWarning := false
				for _, violation := range violations {
					if violation.Level ==
						LevelWarning {
						hasWarning = true
					}
				}

				if hasWarning != current.wantWarn {
					t.Fatalf(
						"警告判定不符: 期望%v,违规=%v",
						current.wantWarn,
						violations,
					)
				}
			},
		)
	}
}

func TestEvolutionNarrativeMachineTermsAllReachValidator(t *testing.T) {
	for _, term := range machinecontract.EvolutionNarrativeTerms() {
		violations := ValidateEntryLine(
			"a/b.go",
			"b.go[X.Y.5.T]: F:功 | R:- | A:- | S:"+term,
		)
		matched := false
		for _, violation := range violations {
			if violation.Level == LevelWarning && strings.Contains(violation.Msg, term) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("机器演进叙事词未到达Validator Warning: %q %+v", term, violations)
		}
		if HasError(violations) {
			t.Errorf("机器演进叙事词不得升级为Error: %q %+v", term, violations)
		}
	}
}

// TestValidateEntryLineDupMessage锁定段重复的可操作拒绝理由。
func TestValidateEntryLineDupMessage(t *testing.T) {
	violations := ValidateEntryLine(
		"a/b.go",
		"b.go[X.Y.5.T]: F:功 | R:x.go | R:y.go | A:- | S:态",
	)

	hit := false
	for _, violation := range violations {
		if violation.Level == LevelError &&
			strings.Contains(violation.Msg, "重复") &&
			strings.Contains(violation.Msg, "R×2") {
			hit = true
		}
	}

	if !hit {
		t.Fatalf(
			"段重复应产出点名R×2的可操作硬拒: %+v",
			violations,
		)
	}
}

// TestValidateEntryLineTagParseWarnMessage锁定不可解析标签警告。
func TestValidateEntryLineTagParseWarnMessage(t *testing.T) {
	violations := ValidateEntryLine(
		"a/b.go",
		"b.go[UAU8]: F:功 | R:- | A:- | S:态",
	)

	hit := false
	for _, violation := range violations {
		if violation.Level == LevelWarning &&
			strings.Contains(violation.Msg, "UAU8") &&
			strings.Contains(violation.Msg, "跳过") {
			hit = true
		}
	}

	if !hit {
		t.Fatalf(
			"不可解析标签应产出点名标签且明示双闸跳过的Warning: %+v",
			violations,
		)
	}
	if HasError(violations) {
		t.Fatal("标签不可解析不得升为Error")
	}
}

// TestStripFences锁定围栏与空白清理。
func TestStripFences(t *testing.T) {
	input :=
		"\n```text\nb.go[X.Y.5.T]: F:- | R:- | A:- | S:-\n```\n\n"

	output := StripFences(input)

	if strings.Contains(output, "```") ||
		strings.Contains(output, "\n") {
		t.Fatalf(
			"围栏或换行未清净: %q",
			output,
		)
	}

	keep :=
		"b.go[X.Y.5.T]: F:含`code`片段 | R:- | A:- | S:-"

	if StripFences(keep) != keep {
		t.Fatal("正文反引号被误伤")
	}
}

// TestValidateEntryLineQuotaWired锁定S配额Warning接线。
func TestValidateEntryLineQuotaWired(t *testing.T) {
	long :=
		"f.go[XGI3T]: F:x | R:- | A:- | S:" +
			strings.Repeat("字", 51)

	violations := ValidateEntryLine(
		"f.go",
		long,
	)

	hit := false
	for _, violation := range violations {
		if violation.Level == LevelWarning &&
			strings.Contains(violation.Msg, "配额") {
			hit = true
		}
	}

	if !hit {
		t.Fatalf(
			"超配额应经ValidateEntryLine产出Warning: %+v",
			violations,
		)
	}
	if HasError(violations) {
		t.Fatal("配额违规不得升为Error")
	}

	valid :=
		"f.go[XGI3T]: F:x | R:- | A:- | S:短约束"

	for _, violation := range ValidateEntryLine(
		"f.go",
		valid,
	) {
		if strings.Contains(
			violation.Msg,
			"配额",
		) {
			t.Fatalf(
				"合规条目不应有配额警告: %v",
				violation.Msg,
			)
		}
	}
}
