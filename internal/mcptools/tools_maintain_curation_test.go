// maintain 包对策展匹配单一事实源的边界测试。
// 索引条目待补: tools_maintain_curation_test.go
//
// 本文件不再测试 mcptools 私有匹配函数 —— 该函数已删除。maintain 统一依赖
// config.Config.CurationExcluded(v1.1),本测试从消费包侧锁定裸目录、目录边界
// 与单文件三项关键语义,防后续有人在 mcptools 恢复本地副本。
package mcptools

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestMaintainCurationMatcherContract(t *testing.T) {
	cfg := &config.Config{CurationExclude: []string{"docs", "README.md"}}

	cases := []struct {
		rel  string
		want bool
	}{
		{"docs/api.md", true},
		{"docs2/api.md", false},
		{"README.md", true},
		{"README.md.bak", false},
		{"src/main.go", false},
	}

	for _, tc := range cases {
		if got := cfg.CurationExcluded(tc.rel); got != tc.want {
			t.Fatalf("Config.CurationExcluded(%q)=%v, want %v", tc.rel, got, tc.want)
		}
	}
}
