package cognition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/index"
)

func buildImpactGraph(current, projected impactRegistry) impactGraph {
	graph := impactGraph{
		out: map[string][]impactEdge{}, in: map[string][]impactEdge{},
		issuesBySource: map[string][]relationIssue{}, issuesByCandidate: map[string][]relationIssue{},
	}
	edgeSeen := map[string]bool{}
	for _, stage := range []struct {
		name     string
		registry impactRegistry
	}{{"pre", current}, {"post", projected}} {
		nameIndex, namespaceIndex := buildImpactNameIndexes(stage.registry)
		refs := sortedObjectRefs(stage.registry)
		for _, sourceRef := range refs {
			source := stage.registry[sourceRef]
			tokens, invalidTokens := splitImpactRelations(source.Entry)
			for _, token := range invalidTokens {
				graph.issuesBySource[sourceRef] = append(graph.issuesBySource[sourceRef], relationIssue{code: "impact_relation_invalid", source: sourceRef, token: token})
			}
			for _, token := range tokens {
				targets, issueCode := resolveImpactRelation(source, token, stage.registry, nameIndex, namespaceIndex)
				if issueCode != "" {
					issue := relationIssue{code: issueCode, source: sourceRef, token: token, candidates: append([]string(nil), targets...)}
					graph.issuesBySource[sourceRef] = append(graph.issuesBySource[sourceRef], issue)
					for _, target := range targets {
						graph.issuesByCandidate[target] = append(graph.issuesByCandidate[target], issue)
					}
					continue
				}
				target := targets[0]
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

func resolveImpactClosure(graph impactGraph, direct map[string]map[string]bool) (map[string]map[string]bool, []ImpactFinding) {
	review := map[string]map[string]bool{}
	queue := sortedKeys(direct)
	for ref, reasons := range direct {
		review[ref] = copyStringSet(reasons)
	}
	findingsByKey := map[string]ImpactFinding{}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		for _, issue := range append(append([]relationIssue(nil), graph.issuesBySource[ref]...), graph.issuesByCandidate[ref]...) {
			key := issue.code + "\x00" + issue.source + "\x00" + issue.token
			findingsByKey[key] = ImpactFinding{
				Code: issue.code, ObjectRef: issue.source, Relation: issue.token, Candidates: append([]string(nil), issue.candidates...),
				Message: impactRelationMessage(issue),
			}
		}
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
	findings := make([]ImpactFinding, 0, len(findingsByKey))
	for _, finding := range findingsByKey {
		findings = append(findings, finding)
	}
	return review, findings
}

func resolveImpactRelation(source Object, token string, registry impactRegistry, nameIndex map[string][]string, namespaceIndex map[string]string) ([]string, string) {
	if strings.HasPrefix(token, "code:") || strings.HasPrefix(token, "database://") {
		if _, ok := registry[token]; ok {
			return []string{token}, ""
		}
		return nil, "impact_relation_unresolved"
	}
	if source.VolumeID == "database" && source.Namespace != "" {
		if local := namespaceIndex[source.Namespace+"\x00"+token]; local != "" {
			return []string{local}, ""
		}
	}
	matches := append([]string(nil), nameIndex[token]...)
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return nil, "impact_relation_unresolved"
	case 1:
		return matches, ""
	default:
		return matches, "impact_relation_ambiguous"
	}
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

func splitImpactRelations(entry *index.Entry) ([]string, []string) {
	if entry == nil {
		return nil, nil
	}
	value := strings.TrimSpace(entry.R)
	if value == "-" {
		return nil, nil
	}
	if value == "" {
		return nil, []string{"<empty>"}
	}
	if strings.Contains(value, "，") {
		return nil, []string{value}
	}
	parts := strings.Split(value, ",")
	relations := make([]string, 0, len(parts))
	var invalid []string
	for _, part := range parts {
		relation := strings.TrimSpace(part)
		if relation == "" || relation == "-" {
			invalid = append(invalid, relation)
			continue
		}
		relations = append(relations, relation)
	}
	sort.Strings(relations)
	sort.Strings(invalid)
	return relations, invalid
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

func impactRelationMessage(issue relationIssue) string {
	if issue.code == "impact_relation_ambiguous" {
		return fmt.Sprintf("relation %q from %s matches multiple canonical cognition objects", issue.token, issue.source)
	}
	if issue.code == "impact_relation_invalid" {
		return fmt.Sprintf("relation %q from %s does not use the canonical comma-delimited relation grammar", issue.token, issue.source)
	}
	return fmt.Sprintf("relation %q from %s does not resolve to a managed canonical cognition object", issue.token, issue.source)
}
