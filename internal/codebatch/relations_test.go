package codebatch

import (
	"errors"
	"fmt"
	"testing"
)

func planTargets(count int) []Target {
	targets := make([]Target, 0, count)
	for index := 1; index <= count; index++ {
		ref := fmt.Sprintf("code:pkg/f%03d.go", index)
		targets = append(targets, Target{ObjectRef: ref, Path: fmt.Sprintf("pkg/f%03d.go", index),
			Change: "create", SourceSHA256: fmt.Sprintf("%064d", index)})
	}
	return targets
}

func ref(index int) string { return fmt.Sprintf("code:pkg/f%03d.go", index) }

func chainRelations(count int) map[string][]string {
	known := map[string][]string{}
	for index := 1; index < count; index++ {
		known[ref(index)] = []string{ref(index + 1)}
	}
	return known
}

func ringRelations(count int) map[string][]string {
	known := chainRelations(count)
	known[ref(count)] = []string{ref(1)}
	return known
}

// observedAll 表示所有目标的关系都已被观察过 —— 全图已知的状态。
func observedAll(count int) map[string]bool {
	result := map[string]bool{}
	for index := 1; index <= count; index++ {
		result[ref(index)] = true
	}
	return result
}

func selectedRefs(targets []Target) map[string]bool {
	result := map[string]bool{}
	for _, target := range targets {
		result[target.ObjectRef] = true
	}
	return result
}

// 链式图每个成分只有一个节点,逆拓扑前缀必须选出自闭合的尾部集合。
func TestSelectRelationClosedBatchClosesChainTail(t *testing.T) {
	all := planTargets(210)
	selected, diagnostic, err := SelectRelationClosedBatch(all, chainRelations(210), nil, observedAll(210), 200)
	if err != nil {
		t.Fatalf("链式图必须有解: %v (%+v)", err, diagnostic)
	}
	if len(selected) != 200 {
		t.Fatalf("批次未填满: %d", len(selected))
	}
	chosen := selectedRefs(selected)
	for index := 11; index <= 210; index++ {
		if !chosen[ref(index)] {
			t.Fatalf("尾部自闭合集合缺少 %s", ref(index))
		}
	}
	// 闭包自证: 选中项的每条关系目标都必须也在批次内。
	for source, targets := range chainRelations(210) {
		if !chosen[source] {
			continue
		}
		for _, target := range targets {
			if !chosen[target] {
				t.Fatalf("批次不自闭合: %s -> %s 不在批次内", source, target)
			}
		}
	}
}

// 单环的强连通分量等于全图,超过上限时必须显式失败并报出精确事实。
func TestSelectRelationClosedBatchReportsOversizedComponent(t *testing.T) {
	all := planTargets(210)
	selected, diagnostic, err := SelectRelationClosedBatch(all, ringRelations(210), nil, observedAll(210), 200)
	if !errors.Is(err, ErrRelationClosureExceedsBatchLimit) {
		t.Fatalf("SCC 超限必须显式失败,实际: %v", err)
	}
	if selected != nil {
		t.Fatal("失败时不得返回批次")
	}
	if diagnostic.LargestComponent != 210 || diagnostic.BatchLimit != 200 {
		t.Fatalf("诊断事实不精确: %+v", diagnostic)
	}
	if len(diagnostic.ComponentSample) != ClosureSampleLimit || diagnostic.ComponentSample[0] != ref(1) {
		t.Fatalf("成分样本不符合约定: %+v", diagnostic.ComponentSample)
	}
}

// 零知识(首批)退化为规范顺序前 limit 个,与既有行为一致。
func TestSelectRelationClosedBatchWithoutKnowledgeKeepsCanonicalPrefix(t *testing.T) {
	all := planTargets(210)
	selected, _, err := SelectRelationClosedBatch(all, nil, nil, nil, 200)
	if err != nil || len(selected) != 200 {
		t.Fatalf("零知识路径异常: len=%d err=%v", len(selected), err)
	}
	if selected[0].ObjectRef != ref(1) || selected[199].ObjectRef != ref(200) {
		t.Fatalf("零知识路径未取规范顺序前缀: %s..%s", selected[0].ObjectRef, selected[199].ObjectRef)
	}
}

// 已能解析的目标(change!=create 或已写入)不要求同批,不应约束装箱。
func TestSelectRelationClosedBatchIgnoresResolvedTargets(t *testing.T) {
	all := planTargets(210)
	resolved := map[string]bool{}
	for index := 201; index <= 210; index++ {
		resolved[ref(index)] = true
	}
	selected, _, err := SelectRelationClosedBatch(all, chainRelations(210), resolved, observedAll(210), 200)
	if err != nil {
		t.Fatalf("目标可解析时必须有解: %v", err)
	}
	chosen := selectedRefs(selected)
	if !chosen[ref(1)] || !chosen[ref(200)] {
		t.Fatal("目标可解析后应能取规范顺序前 200 个")
	}
}

// 后继已全部包含的成分应被补充纳入,提高填充率且不破坏闭包。
func TestSelectRelationClosedBatchFillsWithSatisfiedComponents(t *testing.T) {
	all := planTargets(10)
	known := map[string][]string{
		ref(1): {ref(10)}, // f001 依赖尾部 f010
		ref(2): {ref(10)},
	}
	selected, _, err := SelectRelationClosedBatch(all, known, nil, observedAll(10), 4)
	if err != nil {
		t.Fatalf("装箱失败: %v", err)
	}
	if len(selected) != 4 {
		t.Fatalf("批次未填满: %d", len(selected))
	}
	chosen := selectedRefs(selected)
	if !chosen[ref(10)] {
		t.Fatal("被依赖的汇点必须在批次内")
	}
	for _, source := range []string{ref(1), ref(2)} {
		if chosen[source] && !chosen[ref(10)] {
			t.Fatalf("%s 的关系目标缺失", source)
		}
	}
}

// 同输入必须产出完全相同的选择 —— 批次身份由选择集合派生,不容许抖动。
func TestSelectRelationClosedBatchIsDeterministic(t *testing.T) {
	all := planTargets(210)
	first, _, err := SelectRelationClosedBatch(all, chainRelations(210), nil, observedAll(210), 200)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := SelectRelationClosedBatch(all, chainRelations(210), nil, observedAll(210), 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("两次选择长度不同: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index].ObjectRef != second[index].ObjectRef {
			t.Fatalf("第 %d 项不同: %s vs %s", index, first[index].ObjectRef, second[index].ObjectRef)
		}
	}
}

// 合并观察结果按来源去重、目标排序,并让后一次观察覆盖前一次。
func TestMergeObservedRelationsIsCanonical(t *testing.T) {
	merged := MergeObservedRelations(
		[]ObservedRelation{{SourceObjectRef: ref(2), TargetObjectRefs: []string{ref(9)}},
			{SourceObjectRef: ref(1), TargetObjectRefs: []string{ref(3), ref(2)}}},
		[]ObservedRelation{{SourceObjectRef: ref(2), TargetObjectRefs: []string{ref(4), ref(4), " "}}})
	if len(merged) != 2 || merged[0].SourceObjectRef != ref(1) || merged[1].SourceObjectRef != ref(2) {
		t.Fatalf("合并结果未按来源排序: %+v", merged)
	}
	if len(merged[0].TargetObjectRefs) != 2 || merged[0].TargetObjectRefs[0] != ref(2) {
		t.Fatalf("目标未排序: %+v", merged[0])
	}
	if len(merged[1].TargetObjectRefs) != 1 || merged[1].TargetObjectRefs[0] != ref(4) {
		t.Fatalf("后一次观察必须覆盖前一次: %+v", merged[1])
	}
}
