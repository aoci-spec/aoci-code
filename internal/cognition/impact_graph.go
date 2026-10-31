package cognition

import (
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
)

// buildImpactGraph reads R only as a semantic annotation already in the text: when it matches a managed object
// an edge is recorded to widen the Review surface, and when it does not, nothing happens.
//
// R is a clue the model writes for the model; the relations between entries are built by the attention of the model reading the complete index, not
// checked by the program. This place judges neither whether an R target "exists" nor whether it is "unique", and never
// blocks a write because of it: the machine minds only single-entry form and budget, and cross-entry semantics are the model's entirely.
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

// resolveImpactRelation does its best to match one R annotation to a managed object and returns an empty string when it cannot.
// A mismatch is not an error: a relation may point at an object not yet authored, an object already deleted, or something not in the
// Managed Scope at all; that is all the model's semantic freedom, and the machine does not judge. An ambiguous same-named target is likewise
// not guessed: no edge, and no diagnosis.
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

// splitImpactRelations splits R into a readable sequence of annotations. Empty, "-", and an empty fragment are all just "no annotation
// to match" and are skipped; the reading side is always tolerant, and the strict single-entry form judgement belongs to the authoring boundary.
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
