// 标签不可解析 Warning 分类辅助测试。
package index

import "testing"

func TestHasTagParseWarning(t *testing.T) {
	cases := []struct {
		name string
		vs   []Violation
		want bool
	}{
		{
			name: "真实校验器产出的标签不可解析警告",
			vs:   ValidateEntryLine("a/b.go", "b.go[UAU8]: F:功 | R:- | A:- | S:态"),
			want: true,
		},
		{
			name: "演进叙事警告不误判",
			vs:   ValidateEntryLine("a/b.go", "b.go[X.Y.5.T]: F:功 | R:- | A:- | S:本次修改"),
			want: false,
		},
		{
			name: "格式硬拒不误判",
			vs:   ValidateEntryLine("a/b.go", "不是条目"),
			want: false,
		},
		{
			name: "空列表",
			vs:   nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasTagParseWarning(tc.vs); got != tc.want {
				t.Fatalf("HasTagParseWarning()=%v, want %v, violations=%+v", got, tc.want, tc.vs)
			}
		})
	}
}
