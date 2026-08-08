// config.curation_exclude静态路径策略测试。
//
// 本文件只验证团队配置中的路径本体与目录子树匹配；正式文件级语义决策、
// source_sha256有效性和curation.json持久化由internal/curation测试覆盖。
package config

import "testing"

func TestCurationExcludedBoundaries(t *testing.T) {
	cfg := &Config{CurationExclude: []string{
		"docs",
		"tests/",
		"scripts\\",
		".github",
		"README.rst",
		"./examples//",
	}}

	cases := []struct {
		rel  string
		want bool
	}{
		{"docs/index.md", true},
		{"docs", true},
		{"docs2/x.md", false},
		{"tests/models/test_a.py", true},
		{"scripts/check.sh", true},
		{".github/workflows/ci.yml", true},
		{"README.rst", true},
		{"README.rst.bak", false},
		{"httpx/_api.py", false},
		{"docs\\guide.md", true},
		{"examples/demo.txt", true},
		{"../docs/escape.md", false},
		{"/absolute/docs/index.md", false},
		{"", false},
	}

	for _, testCase := range cases {
		if got := cfg.CurationExcluded(
			testCase.rel,
		); got != testCase.want {
			t.Fatalf(
				"CurationExcluded(%q)=%v, want %v",
				testCase.rel,
				got,
				testCase.want,
			)
		}
	}
}

func TestCurationExcludedInvalidPatternsIgnored(t *testing.T) {
	cfg := &Config{
		CurationExclude: []string{
			"",
			"  ",
			"../outside",
			"/absolute",
		},
	}

	for _, rel := range []string{
		"docs/x.md",
		"outside/x.md",
		"absolute/x.md",
	} {
		if cfg.CurationExcluded(rel) {
			t.Fatalf(
				"无效静态路径选择器不应命中%q",
				rel,
			)
		}
	}
}

func TestCurationExcludedEmptyAndNil(t *testing.T) {
	var nilConfig *Config

	if nilConfig.CurationExcluded(
		"docs/x.md",
	) {
		t.Fatal("nil配置应恒不命中")
	}

	empty := &Config{}

	if empty.CurationExcluded(
		"docs/x.md",
	) {
		t.Fatal("空清单应恒不命中")
	}
}

func TestCountCurationExcluded(t *testing.T) {
	cfg := &Config{
		CurationExclude: []string{
			"docs",
			"README.md",
		},
	}

	rels := []string{
		"docs/a.md",
		"docs/b.md",
		"README.md",
		"httpx/_api.py",
	}

	if count := cfg.CountCurationExcluded(
		rels,
	); count != 3 {
		t.Fatalf(
			"计数应为3，实得%d",
			count,
		)
	}
}
