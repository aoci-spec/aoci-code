// 严格JSON重复字段检测测试。
package jsonstrict

import (
	"strings"
	"testing"
)

func TestRejectDuplicateKeys(
	t *testing.T,
) {
	tests := []struct {
		name     string
		input    string
		wantPath string
	}{
		{
			name:     "top_level",
			input:    `{"plan_id":"a","plan_id":"b"}`,
			wantPath: "plan_id",
		},
		{
			name:     "nested_object",
			input:    `{"decision":{"role":"a","role":"b"}}`,
			wantPath: "decision.role",
		},
		{
			name:     "array_object",
			input:    `{"decisions":[{"decision":"include","decision":"exclude"}]}`,
			wantPath: "decisions[0].decision",
		},
		{
			name:  "distinct_objects_may_share_key",
			input: `{"items":[{"path":"a"},{"path":"b"}]}`,
		},
		{
			name:  "valid_nested",
			input: `{"a":{"b":1},"items":[{"c":2},{"c":3}]}`,
		},
		{
			name:  "malformed_is_left_to_real_decoder",
			input: `{"a":`,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				err := RejectDuplicateKeys(
					[]byte(current.input),
				)

				if current.wantPath == "" {
					if err != nil {
						t.Fatalf(
							"不应报告重复字段: %v",
							err,
						)
					}
					return
				}

				if err == nil ||
					!strings.Contains(
						err.Error(),
						current.wantPath,
					) {
					t.Fatalf(
						"应报告重复字段%s: %v",
						current.wantPath,
						err,
					)
				}
			},
		)
	}
}
