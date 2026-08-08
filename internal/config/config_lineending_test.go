// 团队级换行宽容配置测试。
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLineEndingConfigFile(
	t *testing.T,
	path string,
	content string,
) {
	t.Helper()

	if err := os.MkdirAll(
		filepath.Dir(path),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		path,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

// TestLineEndingToleranceDefaultAndExplicitFalse锁定默认宽容与显式严格语义。
func TestLineEndingToleranceDefaultAndExplicitFalse(
	t *testing.T,
) {
	defaultConfig := DefaultConfig()

	if !defaultConfig.LineEndingTolerance {
		t.Fatal("默认line_ending_tolerance必须为true")
	}

	root := t.TempDir()

	writeLineEndingConfigFile(
		t,
		FilePath(root),
		`{"version":1,"line_ending_tolerance":false}`+"\n",
	)

	merged, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if merged.LineEndingTolerance {
		t.Fatal("团队显式false必须生效，不能被默认true吞掉")
	}

	base, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}

	if base.LineEndingTolerance {
		t.Fatal("LoadBase必须保留团队显式false")
	}
}

// TestLineEndingToleranceCannotBeOverriddenLocally锁定个人层无权改变治理结论。
func TestLineEndingToleranceCannotBeOverriddenLocally(
	t *testing.T,
) {
	testCases := []struct {
		name       string
		teamValue  bool
		localValue bool
		want       bool
	}{
		{
			name:       "团队严格个人宽容无效",
			teamValue:  false,
			localValue: true,
			want:       false,
		},
		{
			name:       "团队宽容个人严格无效",
			teamValue:  true,
			localValue: false,
			want:       true,
		},
	}

	for _, testCase := range testCases {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				root := t.TempDir()

				writeLineEndingConfigFile(
					t,
					FilePath(root),
					`{"version":1,"line_ending_tolerance":`+
						boolJSON(testCase.teamValue)+
						`}`+
						"\n",
				)

				writeLineEndingConfigFile(
					t,
					LocalFilePath(root),
					`{"version":1,"line_ending_tolerance":`+
						boolJSON(testCase.localValue)+
						`}`+
						"\n",
				)

				got, err := Load(root)
				if err != nil {
					t.Fatal(err)
				}

				if got.LineEndingTolerance !=
					testCase.want {
					t.Fatalf(
						"个人层覆盖了团队治理策略: got=%v want=%v",
						got.LineEndingTolerance,
						testCase.want,
					)
				}
			},
		)
	}
}

// TestSaveLocalRemovesLineEndingTolerance锁定个人配置写回时清除越权键。
func TestSaveLocalRemovesLineEndingTolerance(
	t *testing.T,
) {
	root := t.TempDir()

	writeLineEndingConfigFile(
		t,
		LocalFilePath(root),
		`{"version":1,"line_ending_tolerance":false,"manual_key":"keep"}`+
			"\n",
	)

	configValue := DefaultConfig()
	configValue.AI.BaseURL =
		"http://local.example/v1"

	if err := SaveLocal(
		root,
		configValue,
	); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(
		LocalFilePath(root),
	)
	if err != nil {
		t.Fatal(err)
	}

	text := string(raw)

	if strings.Contains(
		text,
		"line_ending_tolerance",
	) {
		t.Fatalf(
			"SaveLocal必须移除团队治理键: %s",
			text,
		)
	}

	if !strings.Contains(
		text,
		`"manual_key"`,
	) ||
		!strings.Contains(
			text,
			"keep",
		) {
		t.Fatalf(
			"SaveLocal不得破坏其他个人键: %s",
			text,
		)
	}

	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if !loaded.LineEndingTolerance {
		t.Fatal("local越权键被移除后应回到团队缺省true")
	}
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}

	return "false"
}
