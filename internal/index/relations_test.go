// R字段关系目标轻量事实检查测试。
package index

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeRelationFixture(
	t *testing.T,
	root,
	rel,
	content string,
) {
	t.Helper()

	target := filepath.Join(
		root,
		filepath.FromSlash(rel),
	)
	if err := os.MkdirAll(
		filepath.Dir(target),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		target,
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

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
		"未找到包含%q的R关系Warning: %+v",
		anchor,
		violations,
	)
}

func requireOnlyRelationWarnings(
	t *testing.T,
	violations []Violation,
) {
	t.Helper()

	for _, violation := range violations {
		if violation.Level != LevelWarning {
			t.Fatalf(
				"R关系检查不得产生硬拒: %+v",
				violations,
			)
		}
	}
}

func TestValidateEntryRelationsAcceptsPlaceholderAndExistingFiles(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(
		t,
		root,
		"src/a.go",
		"package demo\n",
	)
	writeRelationFixture(
		t,
		root,
		"src/b.go",
		"package demo\n",
	)
	writeRelationFixture(
		t,
		root,
		"README.md",
		"demo\n",
	)
	if err := os.MkdirAll(
		filepath.Join(root, "internal", "service"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	writeRelationFixture(
		t,
		root,
		"internal/service/store.go",
		"package service\n",
	)

	cases := []struct {
		name     string
		relation string
	}{
		{
			name:     "无关系占位",
			relation: "-",
		},
		{
			name:     "单个真实文件",
			relation: "src/b.go",
		},
		{
			name:     "同目录裸文件名",
			relation: "b.go",
		},
		{
			name:     "多个真实文件",
			relation: "src/b.go,README.md",
		},
		{
			name:     "反斜杠路径归一",
			relation: `src\b.go`,
		},
		{
			name:     "仓库内部模块目录",
			relation: "internal/service",
		},
		{
			name:     "外部模块路径",
			relation: "github.com/example/dependency/pkg",
		},
		{
			name:     "带点号的外部模块路径",
			relation: "gopkg.in/example/module.v3",
		},
		{
			name:     "外部模块名称",
			relation: "example.module",
		},
		{
			name:     "与源码扩展名相同的歧义裸名称",
			relation: "logging.go",
		},
		{
			name:     "末段像源码的外部模块路径",
			relation: "github.com/example/dependency.go",
		},
		{
			name:     "无法判定归属的斜杠表达",
			relation: "unknown/module.go",
		},
		{
			name:     "首段疑似拼写但归属仍不确定",
			relation: "interal/service/store.go",
		},
		{
			name:     "无仓内首段的尾斜杠表达",
			relation: "unknown/module/",
		},
		{
			name:     "无仓内首段的隐藏名表达",
			relation: "unknown/.module",
		},
		{
			name:     "归属不明的裸隐藏名",
			relation: ".module",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			violations := ValidateEntryRelations(
				root,
				"src/a.go",
				relationEntryLine(
					testCase.relation,
				),
			)

			if len(violations) != 0 {
				t.Fatalf(
					"正常R关系不应产生Warning: %+v",
					violations,
				)
			}
		})
	}
}

func TestValidateEntryRelationsWarnsWithoutBlocking(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(
		t,
		root,
		"src/a.go",
		"package demo\n",
	)
	writeRelationFixture(
		t,
		root,
		"src/b.go",
		"package demo\n",
	)
	cases := []struct {
		name     string
		relation string
		anchor   string
	}{
		{
			name:     "空R字段",
			relation: "",
			anchor:   "R字段为空",
		},
		{
			name:     "不存在文件",
			relation: "src/missing.go",
			anchor:   "R目标不存在",
		},
		{
			name:     "不存在仓库内部模块",
			relation: "src/missing",
			anchor:   "R目标不存在",
		},
		{
			name:     "已存在仓内首段下的尾斜杠缺失",
			relation: "src/missing/",
			anchor:   "R目标不存在",
		},
		{
			name:     "已存在仓内首段下的隐藏名缺失",
			relation: "src/.missing",
			anchor:   "R目标不存在",
		},
		{
			name:     "显式不存在仓库文件",
			relation: "./missing.md",
			anchor:   "R目标不存在",
		},
		{
			name:     "绝对路径",
			relation: "/tmp/outside.go",
			anchor:   "R目标路径不规范",
		},
		{
			name:     "仓外逃逸",
			relation: "../outside.go",
			anchor:   "R目标路径不规范",
		},
		{
			name:     "自引用",
			relation: "src/a.go",
			anchor:   "指向当前条目自身",
		},
		{
			name:     "同目录裸文件名自引用",
			relation: "a.go",
			anchor:   "指向当前条目自身",
		},
		{
			name:     "尾斜杠文件别名自引用",
			relation: "src/a.go/",
			anchor:   "指向当前条目自身",
		},
		{
			name:     "尾斜杠裸文件名自引用",
			relation: "a.go/",
			anchor:   "指向当前条目自身",
		},
		{
			name:     "占位符混用",
			relation: "-,src/b.go",
			anchor:   "占位符与真实路径混用",
		},
		{
			name:     "空分隔项",
			relation: "src/b.go,,README.md",
			anchor:   "目标第2项为空",
		},
		{
			name:     "全角逗号",
			relation: "src/b.go，README.md",
			anchor:   "全角逗号",
		},
		{
			name:     "重复目标",
			relation: "src/b.go,src/b.go",
			anchor:   "R目标重复",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			violations := ValidateEntryRelations(
				root,
				"src/a.go",
				relationEntryLine(
					testCase.relation,
				),
			)

			requireOnlyRelationWarnings(
				t,
				violations,
			)
			requireRelationWarningContaining(
				t,
				violations,
				testCase.anchor,
			)
		})
	}
}

func TestValidateEntryRelationsLeavesExplicitURIIdentitiesToTheirProtocolLayer(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(t, root, "main.go", "package main\n")

	for _, relation := range []string{
		"database://primary/public/users",
		"https://example.invalid/contracts/users",
	} {
		violations := ValidateEntryRelations(
			root,
			"main.go",
			relationEntryLine(relation),
		)
		if len(violations) != 0 {
			t.Fatalf("explicit URI relation %q received filesystem warnings: %+v", relation, violations)
		}
	}
}

func TestValidateEntryRelationsRecognizesEquivalentDuplicates(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(
		t,
		root,
		"src/a.go",
		"package demo\n",
	)
	writeRelationFixture(
		t,
		root,
		"src/b.go",
		"package demo\n",
	)

	for _, relation := range []string{
		`src\b.go,src/b.go`,
		"src/b.go,b.go",
		"b.go,src/b.go",
		"src/missing.go,missing.go",
		"src/b.go,src/b.go/",
		"b.go,b.go/",
	} {
		violations := ValidateEntryRelations(
			root,
			"src/a.go",
			relationEntryLine(relation),
		)

		requireOnlyRelationWarnings(t, violations)
		requireRelationWarningContaining(
			t,
			violations,
			"R目标重复",
		)
	}
}

func TestValidateEntryRelationsTrailingBareNamePrefersSourceDirThenRoot(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(t, root, "src/a.go", "package source\n")
	writeRelationFixture(t, root, "a.go", "package root\n")
	writeRelationFixture(t, root, "README.md", "root readme\n")

	selfViolations := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("a.go/"),
	)
	requireOnlyRelationWarnings(t, selfViolations)
	requireRelationWarningContaining(t, selfViolations, "指向当前条目自身")

	explicitRoot := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("./a.go"),
	)
	if len(explicitRoot) != 0 {
		t.Fatalf("./显式仓库根目标不得先命中同目录同名文件: %+v", explicitRoot)
	}

	rootFallback := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("README.md/,./README.md"),
	)
	requireOnlyRelationWarnings(t, rootFallback)
	requireRelationWarningContaining(t, rootFallback, "R目标重复")
}

func TestValidateEntryRelationsWarnsForSymlinkAndNonRegularFile(
	t *testing.T,
) {
	root := t.TempDir()
	writeRelationFixture(
		t,
		root,
		"src/a.go",
		"package demo\n",
	)
	writeRelationFixture(
		t,
		root,
		"src/target.go",
		"package demo\n",
	)

	linkPath := filepath.Join(root, "src", "link.go")
	if err := os.Symlink("target.go", linkPath); err != nil {
		t.Skipf("当前平台无法创建符号链接夹具: %v", err)
	}

	linkViolations := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("src/link.go"),
	)
	requireOnlyRelationWarnings(t, linkViolations)
	requireRelationWarningContaining(
		t,
		linkViolations,
		"R目标是符号链接",
	)

	outside := t.TempDir()
	writeRelationFixture(t, outside, "b.go", "package outside\n")
	parentLink := filepath.Join(root, "src", "linkdir")
	if err := os.Symlink(outside, parentLink); err != nil {
		t.Skipf("当前平台无法创建目录符号链接夹具: %v", err)
	}
	parentLinkViolations := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("src/linkdir/b.go"),
	)
	requireOnlyRelationWarnings(t, parentLinkViolations)
	requireRelationWarningContaining(t, parentLinkViolations, "R目标是符号链接")

	if runtime.GOOS == "windows" {
		t.Skip("Windows不支持Unix域套接字夹具")
	}

	socketPath := filepath.Join(root, "src", "service.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("创建非普通文件夹具失败: %v", err)
	}
	defer listener.Close()

	socketViolations := ValidateEntryRelations(
		root,
		"src/a.go",
		relationEntryLine("src/service.sock"),
	)
	requireOnlyRelationWarnings(t, socketViolations)
	requireRelationWarningContaining(
		t,
		socketViolations,
		"R目标不是普通文件",
	)
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
			"结构错误应由格式闸负责，R关系检查不得重复报告: %+v",
			violations,
		)
	}
}
