// 头部Prompt编译器测试: 纪律锚点、事实注入、空头部分支、确定性和必填校验。
package prompt

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// sampleInput 构造一份带全量画像的输入。
func sampleInput() HeaderInput {
	return HeaderInput{
		ProjectName:   "demo-repo",
		RepoRootSlash: "/opt/demo-repo",
		CurrentHeader: "#【系统】demo — 演示项目\n#三分法: 略",
		TotalFiles:    42,
		Dirs: []DirCount{
			{Dir: ".", Count: 5},
			{Dir: "internal/cli", Count: 12},
		},
		Exts: []ExtCount{
			{Ext: ".go", Count: 30},
			{Ext: ".md", Count: 3},
		},
		SampleFiles: []string{
			"cmd/aoci/main.go",
			"internal/cli/root.go",
		},
	}
}

func TestSystemContainsDisciplineAnchors(t *testing.T) {
	system, _, err := BuildHeaderMessages(sampleInput())
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, anchor := range []string{
		"以 # 开头",
		"===",
		"禁止生成任何文件条目",
		"通常控制在10个字以内",
		"语义完整优先",
		"双重自问",
		"禁演进叙事",
		machinecontract.NumericText().SQuotaDefaultCompact,
		"禁用数字",
		"禁止虚构",
		"9核心 8高频 7业务",
		"只列跨文件强依赖",
		"只列本文件对外提供的API",
		"语义不重叠",
		"【正确索引示例】",
		"F:进程入口",
		"F:原子写",
		"实际标签必须依据当前项目字典判断",
	} {
		if !strings.Contains(system, anchor) {
			t.Fatalf("system 缺少纪律锚点: %q", anchor)
		}
	}

	for _, forbidden := range []string{
		"负空间",
		"F必须小于10个字",
		"F 小于10个字",
	} {
		if strings.Contains(system, forbidden) {
			t.Fatalf("Header Prompt含已废止的严格合同: %q", forbidden)
		}
	}
}

func TestUserEmbedsFacts(t *testing.T) {
	in := sampleInput()
	_, user, err := BuildHeaderMessages(in)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, fact := range []string{
		"demo-repo",
		"/opt/demo-repo",
		"#【系统】demo — 演示项目",
		"在其基础上完善",
		"人工校准示例必须保留",
		"文件总数: 42",
		"internal/cli: 12",
		".go: 30",
		"cmd/aoci/main.go",
	} {
		if !strings.Contains(user, fact) {
			t.Fatalf("user 缺少事实: %q", fact)
		}
	}

	if strings.Contains(user, "从零起草") {
		t.Fatal("有现有头部时不应出现从零起草指示")
	}
	if strings.Contains(user, "负空间") {
		t.Fatal("Header用户消息不得继续要求负空间")
	}
}

func TestUserEmptyHeaderBranch(t *testing.T) {
	in := sampleInput()
	in.CurrentHeader = "   "

	_, user, err := BuildHeaderMessages(in)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(user, "从零起草") {
		t.Fatal("空头部应指示从零起草")
	}
	if strings.Contains(user, "现有头部原文 开始") {
		t.Fatal("空头部不应出现原文包裹标记")
	}
}

func TestDeterministic(t *testing.T) {
	in := sampleInput()
	systemOne, userOne, errOne := BuildHeaderMessages(in)
	systemTwo, userTwo, errTwo := BuildHeaderMessages(in)

	if errOne != nil || errTwo != nil {
		t.Fatalf("编译失败: %v %v", errOne, errTwo)
	}
	if systemOne != systemTwo || userOne != userTwo {
		t.Fatal("同输入必得同输出")
	}
}

func TestRequiresRepoRoot(t *testing.T) {
	in := sampleInput()
	in.RepoRootSlash = "  "

	if _, _, err := BuildHeaderMessages(in); err == nil {
		t.Fatal("RepoRootSlash为空应报错")
	}
}

func TestProjectNameFallbackAndNegativeTotal(t *testing.T) {
	in := sampleInput()
	in.ProjectName = ""
	in.TotalFiles = -3

	_, user, err := BuildHeaderMessages(in)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(user, "未命名项目") {
		t.Fatal("空项目名应回退为未命名项目")
	}
	if !strings.Contains(user, "文件总数: 0") {
		t.Fatal("负文件总数应按0渲染")
	}
}
