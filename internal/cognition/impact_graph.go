package cognition

import (
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
)

// buildImpactGraph 只把 R 当作已经落在文本里的语义标注来读: 能对上一个受管对象
// 就记一条边用于扩大 Review 面, 对不上就什么也不发生。
//
// R 是模型写给模型看的线索, 条目之间的关系由读全量索引的注意力机制建立, 不由
// 程序核对。这里既不判定 R 指向是否"存在", 也不判定其是否"唯一", 更不会因此
// 阻断写入 —— 机器只管单条形式与预算, 跨条目的语义一律交给模型。
func buildImpactGraph(current, projected impactRegistry) impactGraph {
	graph := impactGraph{out: map[string][]impactEdge{}, in: map[string][]impactEdge{}}
	edgeSeen := map[string]bool{}
	for _, stage := range []struct {
		name     string
		registry impactRegistry
	}{{"pre", current}, {"post", projected}} {
		nameIndex, namespaceIndex := buildImpactNameIndexes(stage.registry)
		refs := sortedObjectRefs(stage.registry)
		for _, sourceRef := range refs {
			source := stage.registry[sourceRef]
			for _, token := range splitImpactRelations(source.Entry) {
				target := resolveImpactRelation(source, token, stage.registry, nameIndex, namespaceIndex)
				if target == "" {
					continue
				}
				key := sourceRef + "\x00" + target + "\x00" + stage.name
				if edgeSeen[key] {
					continue
				}
				edgeSeen[key] = true
				edge := impactEdge{from: sourceRef, to: target, stage: stage.name}
				graph.out[sourceRef] = append(graph.out[sourceRef], edge)
				graph.in[target] = append(graph.in[target], edge)
				graph.allRelationReasons = append(graph.allRelationReasons, ImpactReason{Code: "relation_" + stage.name, From: sourceRef, To: target})
			}
		}
	}
	for ref := range graph.out {
		sortImpactEdges(graph.out[ref])
	}
	for ref := range graph.in {
		sortImpactEdges(graph.in[ref])
	}
	return graph
}

func resolveImpactClosure(graph impactGraph, direct map[string]map[string]bool) map[string]map[string]bool {
	review := map[string]map[string]bool{}
	queue := sortedKeys(direct)
	for ref, reasons := range direct {
		review[ref] = copyStringSet(reasons)
	}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		for _, edge := range graph.out[ref] {
			if review[edge.to] == nil {
				review[edge.to] = map[string]bool{}
				queue = append(queue, edge.to)
			}
			review[edge.to]["forward_relation"] = true
		}
		for _, edge := range graph.in[ref] {
			if review[edge.from] == nil {
				review[edge.from] = map[string]bool{}
				queue = append(queue, edge.from)
			}
			review[edge.from]["reverse_relation"] = true
		}
	}
	return review
}

// resolveImpactRelation 尽力把一个 R 标注对上一个受管对象, 对不上就返回空串。
// 对不上不是错误: 关系可以指向尚未创作的对象、已经删除的对象、或者根本不在
// Managed Scope 里的东西 —— 那都是模型的语义自由, 机器不评判。同名多义时同样
// 不猜: 不产生边, 也不产生任何诊断。
func resolveImpactRelation(source Object, token string, registry impactRegistry, nameIndex map[string][]string, namespaceIndex map[string]string) string {
	if strings.HasPrefix(token, "code:") || strings.HasPrefix(token, "database://") {
		if _, ok := registry[token]; ok {
			return token
		}
		return ""
	}
	if source.VolumeID == "database" && source.Namespace != "" {
		if local := namespaceIndex[source.Namespace+"\x00"+token]; local != "" {
			return local
		}
	}
	if matches := nameIndex[token]; len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func buildImpactNameIndexes(registry impactRegistry) (map[string][]string, map[string]string) {
	nameIndex := map[string][]string{}
	namespaceIndex := map[string]string{}
	for ref, object := range registry {
		nameIndex[object.Name] = append(nameIndex[object.Name], ref)
		if object.VolumeID == "database" && object.Namespace != "" {
			namespaceIndex[object.Namespace+"\x00"+object.Name] = ref
		}
	}
	for name := range nameIndex {
		sort.Strings(nameIndex[name])
	}
	return nameIndex, namespaceIndex
}

// splitImpactRelations 把 R 拆成可读的标注序列。空、"-" 与空片段都只是"没有可
// 对上的标注", 直接跳过 —— 读取侧一律宽容, 单条形式的严格判定属于创作边界。
func splitImpactRelations(entry *index.Entry) []string {
	if entry == nil {
		return nil
	}
	value := strings.TrimSpace(entry.R)
	if value == "" || value == "-" {
		return nil
	}
	parts := strings.Split(value, ",")
	relations := make([]string, 0, len(parts))
	for _, part := range parts {
		if relation := strings.TrimSpace(part); relation != "" && relation != "-" {
			relations = append(relations, relation)
		}
	}
	sort.Strings(relations)
	return relations
}

func sortImpactEdges(edges []impactEdge) {
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		if edges[i].to != edges[j].to {
			return edges[i].to < edges[j].to
		}
		return edges[i].stage < edges[j].stage
	})
}
