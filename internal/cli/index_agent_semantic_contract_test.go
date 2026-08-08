// R63 Host-Agent纯模型语义生成合同测试。
package cli

import (
	"strings"
	"testing"
)

func TestHeaderGuideCarriesCorrectExamplesWithoutNegativeSpace(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageHeaderRequired,
	)
	plan.HeaderState = agentPlanHeaderMissing

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	instructions := strings.Join(
		guide.Instructions,
		"\n",
	)

	for _, anchor := range []string{
		"当前模型",
		"禁止使用路径",
		"批量脚本生成语义",
		"通常控制在10个字以内",
		"语义完整优先",
		"A只列本文件对外API",
		"【正确索引示例】",
		"#main.go[CRT9OT]",
		"#atomic.go[FAT9AT]",
		"实际标签必须依据当前项目字典判断",
		"不要创建“负空间”板块",
	} {
		if !strings.Contains(instructions, anchor) {
			t.Fatalf(
				"Header Guide缺少R63合同%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	if strings.Contains(
		instructions,
		"F必须小于10个字",
	) {
		t.Fatal("Header Guide不得把F长度升级为阻断性硬要求")
	}
}

func TestEntriesGuideRequiresPerFileModelGeneration(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	plan.Targets = []agentPlanTarget{
		{
			Path:         "internal/example.go",
			Kind:         "create",
			SourceSHA256: strings.Repeat("a", 64),
		},
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	instructions := strings.Join(
		guide.Instructions,
		"\n",
	)

	for _, anchor := range []string{
		"当前模型阅读目标文件后独立生成",
		"AST",
		"符号提取",
		"import扫描",
		"路径",
		"文件名",
		"正则",
		"模板",
		"规则引擎",
		"批量脚本",
		"不得先生成语义草稿",
		"逐个完整读取",
		"通常控制在10个字以内",
		"语义完整优先",
		"R只列跨文件强关联",
		"A只列本文件对外API",
		"同一请求中的每条entry都必须由当前模型逐项生成",
		"不得由脚本循环拼接",
	} {
		if !strings.Contains(instructions, anchor) {
			t.Fatalf(
				"Entries Guide缺少R63合同%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	if strings.Contains(
		instructions,
		"F必须小于10个字",
	) {
		t.Fatal("Entries Guide不得把F长度升级为阻断性硬要求")
	}
}

func TestCurationGuideRequiresIndividualModelJudgment(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageCurationRequired,
	)
	plan.CurationTargets = []agentPlanCurationTarget{
		{
			Path:         "marker.empty",
			SourceSHA256: strings.Repeat("b", 64),
		},
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	instructions := strings.Join(
		guide.Instructions,
		"\n",
	)

	for _, anchor := range []string{
		"当前模型独立判断",
		"禁止按路径",
		"文件名",
		"扩展名",
		"物理画像",
		"正则",
		"模板",
		"规则引擎",
		"批量脚本",
		"confidence=-1",
		"逐项替换",
		"可审计排除决策",
	} {
		if !strings.Contains(instructions, anchor) {
			t.Fatalf(
				"Curation Guide缺少R63合同%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	if strings.Contains(
		instructions,
		"可审计负空间",
	) {
		t.Fatal("Curation Guide不得继续使用负空间概念")
	}
}
