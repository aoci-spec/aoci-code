// 关系闭包批次装箱 —— 让"替代批次"真正自闭合
// 索引条目: relations.go[CG9M]
//
// 背景: 一条 R 只有在目标对象已经存在于卷里、或与来源同批写入时才能解析。
// Fresh Bootstrap 的卷是空的,于是首批的关系目标必须同批。
//
// 关键约束: 计划阶段拿不到关系图 —— R 只存在于模型尚未创作的条目文本里,
// 因此"先算 SCC 再作者化"在原理上做不到。AOCI 只能在每次提交时观察到那一批
// 的真实关系边,把它们累积起来,用累积到的图去挑下一批。
//
// 装箱: 在累积图上求强连通分量,在凝聚 DAG 上按逆拓扑序取前缀 —— 前缀内任何
// 对象的出边只会指向更靠前(已包含)的成分,因此前缀天然自闭合。最小不可拆成分
// 本身超过单批上限时显式失败,绝不丢弃 R、不持久化悬空关系、不改写模型语义。
package codebatch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ObservedRelation 是一次提交里观察到的模型创作关系边。
// AOCI 不生成也不改写 R;这里只记录已经看到的事实,用于挑选自闭合批次。
type ObservedRelation struct {
	SourceObjectRef  string   `json:"source_object_ref"`
	TargetObjectRefs []string `json:"target_object_refs"`
}

// ClosureDiagnostic 在闭包无法装进单批时携带精确机器事实,供宿主定位成分。
type ClosureDiagnostic struct {
	LargestComponent int      `json:"largest_component"`
	BatchLimit       int      `json:"batch_limit"`
	ComponentSample  []string `json:"component_sample"`
}

// ClosureSampleLimit 是诊断里最多点名的成分成员数量。
const ClosureSampleLimit = 10

// ErrRelationClosureExceedsBatchLimit 表示存在一个不可拆的关系成分大于单批上限。
var ErrRelationClosureExceedsBatchLimit = fmt.Errorf("code_candidate_relation_closure_exceeds_batch_limit")

// MergeObservedRelations 按来源合并两组观察结果并规范化,结果确定性。
// 同一来源的后一次观察覆盖前一次 —— 模型可能在重排后改写了该条目的 R。
func MergeObservedRelations(existing, observed []ObservedRelation) []ObservedRelation {
	merged := map[string][]string{}
	for _, group := range [][]ObservedRelation{existing, observed} {
		for _, relation := range group {
			source := strings.TrimSpace(relation.SourceObjectRef)
			if source == "" {
				continue
			}
			targets := map[string]bool{}
			for _, target := range relation.TargetObjectRefs {
				if value := strings.TrimSpace(target); value != "" {
					targets[value] = true
				}
			}
			ordered := make([]string, 0, len(targets))
			for target := range targets {
				ordered = append(ordered, target)
			}
			sort.Strings(ordered)
			merged[source] = ordered
		}
	}
	sources := make([]string, 0, len(merged))
	for source := range merged {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	result := make([]ObservedRelation, 0, len(sources))
	for _, source := range sources {
		result = append(result, ObservedRelation{SourceObjectRef: source, TargetObjectRefs: merged[source]})
	}
	return result
}

// SelectRelationClosedBatch 在已知关系图上挑下一批。
//
//	all:      本计划的全部目标(入参顺序即规范顺序)
//	known:    已累积的关系边 source -> targets
//	resolved: 已能解析的对象(已写入卷、或 change!=create),其关系不要求同批
//	observed: 关系已被观察过的对象 —— 只有它们不会再带来意外的跨批边
//	limit:    单批上限
//
// 三段策略,每一段都确定性:
//  1. 可保证批次: 在"已观察且闭包完整"的子图上按 SCC 逆拓扑前缀装箱。这样的
//     批次里每个对象的关系都已知且目标同批,提交必然通过。
//  2. 学习批次: 还有对象没被观察过时,优先把它们排进来。计划阶段拿不到关系图,
//     只能这样逐批把事实换回来;⌈N/limit⌉ 轮后全图已知。
//  3. 显式失败: 全部观察完仍挑不出可保证批次,说明最小汇点成分本身就超过上限,
//     带出精确事实,绝不丢弃 R 或改写模型语义。
func SelectRelationClosedBatch(all []Target, known map[string][]string,
	resolved, observed map[string]bool, limit int) ([]Target, ClosureDiagnostic, error) {
	if limit < 1 {
		return nil, ClosureDiagnostic{}, fmt.Errorf("code_candidate_batch_limit_invalid")
	}
	position := make(map[string]int, len(all))
	for index, target := range all {
		position[target.ObjectRef] = index
	}
	// 只保留"必须同批"的边: 目标属于本计划且尚不能解析。
	edges := make(map[string][]string, len(known))
	for source, targets := range known {
		if _, planned := position[source]; !planned {
			continue
		}
		kept := make([]string, 0, len(targets))
		for _, target := range targets {
			if _, planned := position[target]; !planned || resolved[target] || target == source {
				continue
			}
			kept = append(kept, target)
		}
		if len(kept) > 0 {
			sort.Strings(kept)
			edges[source] = kept
		}
	}
	if len(edges) == 0 {
		return canonicalPrefix(all, nil, limit), ClosureDiagnostic{BatchLimit: limit}, nil
	}

	// 还没看全关系时先学习: 此时任何"看起来闭合"的批次都可能被未观察对象的
	// 未知边推翻,与其赌一把不如把事实换回来。⌈N/limit⌉ 轮后全图已知。
	if !allObserved(all, resolved, observed) {
		return learningBatch(all, edges, resolved, observed, limit), ClosureDiagnostic{BatchLimit: limit}, nil
	}
	// 全图已知: 所有待写对象构成的图上按 SCC 逆拓扑前缀装箱,结果必然自闭合。
	return packClosedPrefix(all, edges, allPending(all, resolved), position, limit)
}

// packClosedPrefix 在给定候选集合上按 SCC 逆拓扑前缀装箱。
func packClosedPrefix(all []Target, edges map[string][]string, candidates map[string]bool,
	position map[string]int, limit int) ([]Target, ClosureDiagnostic, error) {
	scoped := make([]Target, 0, len(candidates))
	for _, target := range all {
		if candidates[target.ObjectRef] {
			scoped = append(scoped, target)
		}
	}
	if len(scoped) == 0 {
		return nil, ClosureDiagnostic{BatchLimit: limit}, nil
	}
	scopedEdges := make(map[string][]string, len(edges))
	for source, targets := range edges {
		if !candidates[source] {
			continue
		}
		kept := make([]string, 0, len(targets))
		for _, target := range targets {
			if candidates[target] {
				kept = append(kept, target)
			}
		}
		if len(kept) > 0 {
			scopedEdges[source] = kept
		}
	}
	components := stronglyConnectedComponents(scoped, scopedEdges)
	componentOf := make(map[string]int, len(scoped))
	for index, component := range components {
		for _, ref := range component {
			componentOf[ref] = index
		}
	}
	successors := make([]map[int]bool, len(components))
	for index := range successors {
		successors[index] = map[int]bool{}
	}
	for source, targets := range scopedEdges {
		from := componentOf[source]
		for _, target := range targets {
			if to := componentOf[target]; to != from {
				successors[from][to] = true
			}
		}
	}
	order := reverseTopologicalOrder(components, successors)
	if size := len(components[order[0]]); size > limit {
		component := append([]string{}, components[order[0]]...)
		sort.Slice(component, func(i, j int) bool { return position[component[i]] < position[component[j]] })
		sample := component
		if len(sample) > ClosureSampleLimit {
			sample = sample[:ClosureSampleLimit]
		}
		return nil, ClosureDiagnostic{LargestComponent: size, BatchLimit: limit,
			ComponentSample: append([]string{}, sample...)}, ErrRelationClosureExceedsBatchLimit
	}
	selectedRefs := map[string]bool{}
	selectedComponents := map[int]bool{}
	count, stopped := 0, 0
	for cursor, index := range order {
		if count+len(components[index]) > limit {
			stopped = cursor
			break
		}
		selectedComponents[index] = true
		for _, ref := range components[index] {
			selectedRefs[ref] = true
		}
		count += len(components[index])
		stopped = cursor + 1
	}
	// 补充遍历: 后继已全部包含的成分也可纳入,提高填充率而不破坏闭包。
	for _, index := range order[stopped:] {
		if count+len(components[index]) > limit {
			continue
		}
		complete := true
		for successor := range successors[index] {
			if !selectedComponents[successor] {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		selectedComponents[index] = true
		for _, ref := range components[index] {
			selectedRefs[ref] = true
		}
		count += len(components[index])
	}
	// 填充只允许"没有待写出边"的对象 —— 它们不可能破坏闭包。带约束的对象
	// 一旦被填进来, 其目标未必同批, 批次就不再自闭合(这正是旧实现的错法)。
	selected := make([]Target, 0, limit)
	for _, target := range all {
		if selectedRefs[target.ObjectRef] {
			selected = append(selected, target)
		}
	}
	for _, target := range all {
		if len(selected) >= limit {
			break
		}
		if selectedRefs[target.ObjectRef] || resolvedOrConstrained(target.ObjectRef, candidates, edges) {
			continue
		}
		selectedRefs[target.ObjectRef] = true
		selected = append(selected, target)
	}
	sort.Slice(selected, func(i, j int) bool {
		return position[selected[i].ObjectRef] < position[selected[j].ObjectRef]
	})
	return selected, ClosureDiagnostic{BatchLimit: limit}, nil
}

// resolvedOrConstrained 判断该对象是否不宜作为填充项: 不在候选集合内, 或仍有
// 待写出边。
func resolvedOrConstrained(ref string, candidates map[string]bool, edges map[string][]string) bool {
	if !candidates[ref] {
		return true
	}
	return len(edges[ref]) > 0
}

// learningBatch 在还没看全关系时挑下一批。
//
// 先放"已提交的关系来源及其待写目标"—— 规范承诺替代批次必须包含它们;再优先
// 排入尚未被观察的对象,把关系事实尽快换回来(⌈N/limit⌉ 轮后全图已知);最后按
// 规范顺序填满。
func learningBatch(all []Target, edges map[string][]string, resolved, observed map[string]bool, limit int) []Target {
	chosen := map[string]bool{}
	required := map[string]bool{}
	for source, targets := range edges {
		required[source] = true
		for _, target := range targets {
			required[target] = true
		}
	}
	if len(required) <= limit {
		for ref := range required {
			chosen[ref] = true
		}
	}
	count := len(chosen)
	for _, target := range all {
		if count >= limit {
			break
		}
		if chosen[target.ObjectRef] || observed[target.ObjectRef] || resolved[target.ObjectRef] {
			continue
		}
		chosen[target.ObjectRef] = true
		count++
	}
	return canonicalPrefix(all, chosen, limit)
}

// canonicalPrefix 先放已选中的对象,再按规范顺序填满剩余名额。
func canonicalPrefix(all []Target, chosen map[string]bool, limit int) []Target {
	selected := make([]Target, 0, limit)
	taken := map[string]bool{}
	for _, target := range all {
		if chosen[target.ObjectRef] {
			selected = append(selected, target)
			taken[target.ObjectRef] = true
		}
	}
	for _, target := range all {
		if len(selected) >= limit {
			break
		}
		if taken[target.ObjectRef] {
			continue
		}
		selected = append(selected, target)
		taken[target.ObjectRef] = true
	}
	if len(selected) > limit {
		selected = selected[:limit]
	}
	position := make(map[string]int, len(all))
	for index, target := range all {
		position[target.ObjectRef] = index
	}
	sort.Slice(selected, func(i, j int) bool {
		return position[selected[i].ObjectRef] < position[selected[j].ObjectRef]
	})
	return selected
}

func allObserved(all []Target, resolved, observed map[string]bool) bool {
	for _, target := range all {
		if !resolved[target.ObjectRef] && !observed[target.ObjectRef] {
			return false
		}
	}
	return true
}

func allPending(all []Target, resolved map[string]bool) map[string]bool {
	pending := map[string]bool{}
	for _, target := range all {
		if !resolved[target.ObjectRef] {
			pending[target.ObjectRef] = true
		}
	}
	return pending
}

// stronglyConnectedComponents 用迭代式 Tarjan 求强连通分量。
// 成分内部与成分之间都按规范顺序排序,保证同输入产出完全相同的划分。
func stronglyConnectedComponents(all []Target, edges map[string][]string) [][]string {
	position := make(map[string]int, len(all))
	nodes := make([]string, 0, len(all))
	for index, target := range all {
		position[target.ObjectRef] = index
		nodes = append(nodes, target.ObjectRef)
	}
	const undefined = -1
	indexOf := make(map[string]int, len(nodes))
	lowLink := make(map[string]int, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	stack := make([]string, 0, len(nodes))
	counter := 0
	components := [][]string{}

	type frame struct {
		node  string
		child int
	}
	for _, root := range nodes {
		if _, visited := indexOf[root]; visited {
			continue
		}
		frames := []frame{{node: root}}
		indexOf[root], lowLink[root] = counter, counter
		counter++
		stack = append(stack, root)
		onStack[root] = true
		for len(frames) > 0 {
			current := &frames[len(frames)-1]
			targets := edges[current.node]
			if current.child < len(targets) {
				target := targets[current.child]
				current.child++
				if _, visited := indexOf[target]; !visited {
					indexOf[target], lowLink[target] = counter, counter
					counter++
					stack = append(stack, target)
					onStack[target] = true
					frames = append(frames, frame{node: target})
				} else if onStack[target] {
					if indexOf[target] < lowLink[current.node] {
						lowLink[current.node] = indexOf[target]
					}
				}
				continue
			}
			node := current.node
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				parent := frames[len(frames)-1].node
				if lowLink[node] < lowLink[parent] {
					lowLink[parent] = lowLink[node]
				}
			}
			if lowLink[node] == indexOf[node] {
				component := []string{}
				for {
					top := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[top] = false
					component = append(component, top)
					if top == node {
						break
					}
				}
				sort.Slice(component, func(i, j int) bool { return position[component[i]] < position[component[j]] })
				components = append(components, component)
			}
		}
		_ = undefined
	}
	sort.Slice(components, func(i, j int) bool {
		return position[components[i][0]] < position[components[j][0]]
	})
	return components
}

// reverseTopologicalOrder 返回凝聚 DAG 的逆拓扑序(汇点优先)。
// 同层按成分的规范顺序排列,保证结果确定性。
func reverseTopologicalOrder(components [][]string, successors []map[int]bool) []int {
	state := make([]int, len(components)) // 0 未访问 1 访问中 2 已完成
	order := make([]int, 0, len(components))
	for index := range components {
		if state[index] != 0 {
			continue
		}
		stack := []int{index}
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			if state[node] == 2 {
				stack = stack[:len(stack)-1]
				continue
			}
			if state[node] == 1 {
				state[node] = 2
				order = append(order, node)
				stack = stack[:len(stack)-1]
				continue
			}
			state[node] = 1
			children := make([]int, 0, len(successors[node]))
			for successor := range successors[node] {
				children = append(children, successor)
			}
			sort.Sort(sort.Reverse(sort.IntSlice(children)))
			for _, child := range children {
				if state[child] == 0 {
					stack = append(stack, child)
				}
			}
		}
	}
	return order
}

// IssuedBatchIdentities 列出同一计划谱系里已经签发过的批次身份。
// 收据按批次身份落在同一目录,PlanID 由全集派生,因此同谱系可以直接筛出来。
// 用于重排的不收敛兜底检测;读取失败按"未签发"处理,绝不因审计读取阻断写入。
func IssuedBatchIdentities(root, planID string) []string {
	entries, err := os.ReadDir(filepath.Join(root, ".aoci", "drafts", "code-cognition"))
	if err != nil {
		return nil
	}
	issued := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "candidate-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		batchID := strings.TrimSuffix(strings.TrimPrefix(name, "candidate-"), ".json")
		receipt, loadErr := LoadReceipt(root, batchID)
		if loadErr != nil || receipt.PlanID != planID {
			continue
		}
		issued = append(issued, receipt.BatchID)
	}
	sort.Strings(issued)
	return issued
}
