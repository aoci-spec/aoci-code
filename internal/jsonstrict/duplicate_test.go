// Tests of strict JSON duplicate field detection.
package jsonstrict

import (
	"errors"
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

// Locks: the byte scanner keeps treating keys that differ only by case as
// distinct, because a map target decodes them as two entries; the fold
// scanner reports the same documents, naming both spellings.
func TestRejectDuplicateKeysKeepsCaseDistinctKeys(
	t *testing.T,
) {
	for _, input := range []string{
		`{"README.md":1,"readme.md":2}`,
		`{"mcp":{"aoci":{},"AOCI":{}}}`,
		`{"servers":[{"a":1,"A":2}]}`,
		`{"ſ":1,"S":2}`,
	} {
		if err := RejectDuplicateKeys(
			[]byte(input),
		); err != nil {
			t.Fatalf(
				"逐字节扫描不应把仅大小写不同的键报告为重复 %s: %v",
				input,
				err,
			)
		}

		err := RejectDuplicateKeysFold(
			[]byte(input),
		)
		var duplicate *DuplicateKeyError
		if !errors.As(err, &duplicate) || duplicate.Conflicts == "" {
			t.Fatalf(
				"折叠扫描必须带两种拼写地报告 %s: %v",
				input,
				err,
			)
		}
	}
}

func TestRejectDuplicateKeysFold(
	t *testing.T,
) {
	tests := []struct {
		name          string
		input         string
		wantPath      string
		wantConflicts string
		wantMessage   string
	}{
		{
			name:        "exact_top_level_keeps_old_message",
			input:       `{"plan_id":"a","plan_id":"b"}`,
			wantPath:    "plan_id",
			wantMessage: "JSON字段重复: plan_id",
		},
		{
			name:        "exact_nested_keeps_old_message",
			input:       `{"decision":{"role":"a","role":"b"}}`,
			wantPath:    "decision.role",
			wantMessage: "JSON字段重复: decision.role",
		},
		{
			name:        "exact_array_object_keeps_old_message",
			input:       `{"decisions":[{"decision":"include","decision":"exclude"}]}`,
			wantPath:    "decisions[0].decision",
			wantMessage: "JSON字段重复: decisions[0].decision",
		},
		{
			name:          "case_only_top_level",
			input:         `{"path":"a","Path":"b"}`,
			wantPath:      "Path",
			wantConflicts: "path",
			wantMessage:   "JSON字段重复: Path (与 path 仅大小写不同)",
		},
		{
			name:          "case_only_reports_the_later_spelling",
			input:         `{"PATH":1,"path":2}`,
			wantPath:      "path",
			wantConflicts: "PATH",
			wantMessage:   "JSON字段重复: path (与 PATH 仅大小写不同)",
		},
		{
			name:          "case_only_nested_object",
			input:         `{"foo":{"path":1,"PATH":2}}`,
			wantPath:      "foo.PATH",
			wantConflicts: "foo.path",
			wantMessage:   "JSON字段重复: foo.PATH (与 foo.path 仅大小写不同)",
		},
		{
			name:          "case_only_array_object",
			input:         `{"targets":[{"sha256":"x"},{"sha256":"y","SHA256":"z"}]}`,
			wantPath:      "targets[1].SHA256",
			wantConflicts: "targets[1].sha256",
		},
		{
			name:          "case_only_deeply_nested_path",
			input:         `{"a":{"b":[{"c":{"version":1,"Version":2}}]}}`,
			wantPath:      "a.b[0].c.Version",
			wantConflicts: "a.b[0].c.version",
			wantMessage:   "JSON字段重复: a.b[0].c.Version (与 a.b[0].c.version 仅大小写不同)",
		},
		{
			name:          "case_only_after_an_unrelated_key",
			input:         `{"version":1,"targets":[],"VERSION":2}`,
			wantPath:      "VERSION",
			wantConflicts: "version",
		},
		{
			name:        "unicode_exact_duplicate",
			input:       `{"路径":1,"路径":2}`,
			wantPath:    "路径",
			wantMessage: "JSON字段重复: 路径",
		},
		{
			name:          "unicode_long_s_folds_to_s",
			input:         `{"ſ":1,"S":2}`,
			wantPath:      "S",
			wantConflicts: "ſ",
		},
		{
			name:          "unicode_kelvin_sign_folds_to_k",
			input:         "{\"K\":1,\"k\":2}",
			wantPath:      "k",
			wantConflicts: "K",
		},
		{
			name:          "unicode_sigma_variants_fold",
			input:         `{"σ":1,"ς":2}`,
			wantPath:      "ς",
			wantConflicts: "σ",
		},
		{
			name:          "unicode_escaped_spelling_is_folded_after_unquoting",
			input:         `{"path":1,"PATH":2}`,
			wantPath:      "PATH",
			wantConflicts: "path",
		},
		{
			name:  "unicode_sharp_s_does_not_fold_to_ss",
			input: `{"straße":1,"STRASSE":2}`,
		},
		{
			name:  "unicode_dotted_capital_i_does_not_fold",
			input: `{"İ":1,"i":2}`,
		},
		{
			name:  "unicode_distinct_ideographs",
			input: `{"路径":1,"路徑":2}`,
		},
		{
			name:  "distinct_objects_may_share_case_variants",
			input: `{"items":[{"path":"a"},{"Path":"b"}]}`,
		},
		{
			name:  "valid_nested",
			input: `{"a":{"b":1},"items":[{"c":2},{"c":3}]}`,
		},
		{
			name:  "malformed_is_left_to_real_decoder",
			input: `{"a":`,
		},
		{
			name:  "malformed_before_the_collision_is_left_to_real_decoder",
			input: `{"a":,"A":1}`,
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				err := RejectDuplicateKeysFold(
					[]byte(current.input),
				)

				if current.wantPath == "" && current.wantConflicts == "" {
					if err != nil {
						t.Fatalf(
							"不应报告重复字段: %v",
							err,
						)
					}
					return
				}

				var duplicate *DuplicateKeyError
				if !errors.As(err, &duplicate) {
					t.Fatalf(
						"应报告重复字段%s: %v",
						current.wantPath,
						err,
					)
				}
				if duplicate.Path != current.wantPath ||
					duplicate.Conflicts != current.wantConflicts {
					t.Fatalf(
						"路径应为 %q/%q，实际 %q/%q",
						current.wantPath,
						current.wantConflicts,
						duplicate.Path,
						duplicate.Conflicts,
					)
				}
				if current.wantMessage != "" &&
					err.Error() != current.wantMessage {
					t.Fatalf(
						"错误文本应为 %q，实际 %q",
						current.wantMessage,
						err.Error(),
					)
				}
				if current.wantConflicts == "" &&
					strings.Contains(
						err.Error(),
						"仅大小写不同",
					) {
					t.Fatalf(
						"逐字节重复不应带大小写说明: %q",
						err.Error(),
					)
				}
			},
		)
	}
}

// Locks: foldKey agrees with strings.EqualFold on every pair, including the
// pairs where ASCII lowering and Unicode simple folding disagree, so a single
// map lookup is an exact substitute for comparing against every earlier key.
func TestFoldKeyMatchesEqualFold(
	t *testing.T,
) {
	keys := []string{
		"", "path", "Path", "PATH", "plan_id", "PLAN_ID", "Plan_Id",
		"k", "K", "K", "s", "S", "ſ", "σ", "ς", "Σ", "ß", "ẞ", "straße", "STRASSE",
		"İ", "I", "i", "ı", "ϑ", "θ", "Θ", "ǅ", "ǆ", "Ǆ", "路径", "路徑", "a", "A", "ab", "aB",
		"\xff", "\xfe", "�",
	}
	for _, left := range keys {
		for _, right := range keys {
			want := strings.EqualFold(
				left,
				right,
			)
			got := foldKey(left) == foldKey(right)
			if got != want {
				t.Errorf(
					"foldKey(%q)==foldKey(%q) 为 %v，strings.EqualFold 为 %v",
					left,
					right,
					got,
					want,
				)
			}
		}
	}
	for _, key := range []string{"", "PATH", "PLAN_ID", "A1_B2", "路径"} {
		if foldKey(key) != key {
			t.Errorf(
				"已是规范形式的键不应改变: %q -> %q",
				key,
				foldKey(key),
			)
		}
	}
}
