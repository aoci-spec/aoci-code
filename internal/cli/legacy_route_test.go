package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestLegacyOnlyCommandsRejectVolumesWithExitConfigAndNoFormalWrites(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	before := governedCLIState(t, root)

	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "status"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("compact Volumes status must remain supported: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "status_deep", args: []string{"status", "--deep"}},
		{name: "inventory", args: []string{"index", "inventory"}},
		{name: "score", args: []string{"index", "score"}},
		{name: "agent_plan", args: []string{"index", "agent", "plan"}},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			arguments := append([]string{"--repo", root, "--json"}, current.args...)
			code := executeCLI(arguments, &stdout, &stderr)
			if code != ExitConfig || !strings.Contains(stdout.String()+stderr.String(), "Volumes v1") {
				t.Fatalf("Volumes Legacy-only route mismatch: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if after := governedCLIState(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("unsupported route changed formal state\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func TestLegacyOnlyCommandsRetainLegacySuccessBehavior(t *testing.T) {
	root := buildScopeCLIRepo(t)
	for _, arguments := range [][]string{
		{"status", "--deep"},
		{"index", "inventory"},
		{"index", "score"},
		{"index", "agent", "plan"},
	} {
		var stdout, stderr bytes.Buffer
		fullArguments := append([]string{"--repo", root, "--json"}, arguments...)
		if code := executeCLI(fullArguments, &stdout, &stderr); code != ExitOK {
			t.Fatalf("Legacy command %v changed behavior: code=%d stdout=%s stderr=%s", arguments, code, stdout.String(), stderr.String())
		}
	}
}
