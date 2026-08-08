// E规模参与路径判据边界测试。
package index

import "testing"

// TestShouldCheckEScalePathBoundary锁定AOCI治理资产排除与相似路径不误伤。
func TestShouldCheckEScalePathBoundary(
	t *testing.T,
) {
	cases := []struct {
		name      string
		rel       string
		wantCheck bool
	}{
		{
			name:      "空路径不判",
			rel:       "",
			wantCheck: false,
		},
		{
			name:      "当前目录不判",
			rel:       ".",
			wantCheck: false,
		},
		{
			name:      "根AOCI目录不判",
			rel:       ".aoci",
			wantCheck: false,
		},
		{
			name:      "根AOCI尾斜杠不判",
			rel:       ".aoci/",
			wantCheck: false,
		},
		{
			name:      "根AOCI子资产不判",
			rel:       ".aoci/ledger.jsonl",
			wantCheck: false,
		},
		{
			name:      "单层点前缀运行时资产不判",
			rel:       "./.aoci/drafts/manifest.json",
			wantCheck: false,
		},
		{
			name:      "多层点前缀运行时资产不判",
			rel:       "././.aoci/config.json",
			wantCheck: false,
		},
		{
			name:      "Windows反斜杠运行时资产不判",
			rel:       `.\.aoci\drafts\manifest.json`,
			wantCheck: false,
		},
		{
			name:      "根索引本体不判",
			rel:       "aoci.txt",
			wantCheck: false,
		},
		{
			name:      "点前缀根索引本体不判",
			rel:       "./aoci.txt",
			wantCheck: false,
		},
		{
			name:      "Windows形态根索引本体不判",
			rel:       `.\aoci.txt`,
			wantCheck: false,
		},
		{
			name:      "同名前缀目录仍判",
			rel:       ".aoci2/file.txt",
			wantCheck: true,
		},
		{
			name:      "嵌套同名目录仍判",
			rel:       "nested/.aoci/file.txt",
			wantCheck: true,
		},
		{
			name:      "嵌套同名索引文件仍判",
			rel:       "nested/aoci.txt",
			wantCheck: true,
		},
		{
			name:      "索引备份相似名仍判",
			rel:       "aoci.txt.backup",
			wantCheck: true,
		},
		{
			name:      "普通源码仍判",
			rel:       "internal/index/escale.go",
			wantCheck: true,
		},
		{
			name:      "普通Windows形态仍判",
			rel:       `internal\index\escale.go`,
			wantCheck: true,
		},
		{
			name:      "上级路径不被静默排除",
			rel:       "../.aoci/file.txt",
			wantCheck: true,
		},
	}

	for _, testCase := range cases {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				got := ShouldCheckEScalePath(
					testCase.rel,
				)

				if got != testCase.wantCheck {
					t.Fatalf(
						"ShouldCheckEScalePath(%q)=%t，期望%t",
						testCase.rel,
						got,
						testCase.wantCheck,
					)
				}
			},
		)
	}
}
