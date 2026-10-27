package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A Host that runs what each onboarding result names gets from a Legacy
// repository to Preview: start names the batch command, each batch names the
// receipt command and hands out the receipt that completes it, and once
// nothing is pending a Migration names its mapping template over the
// session's own plan and snapshot, then Preview with the mapping. The drill
// in scripts/blackbox/legacy-migration follows these commands to an aligned
// Volumes repository.
func TestLegacyOnboardingNamesEveryNextCommand(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	command := onboardingTestCommand(t)
	var session onboardingSessionView
	decodeOnboardingTestJSON(t, mustCLI(t, root, "--json", "cognition", "onboard", "start"), &session)
	if !reflect.DeepEqual(session.NextCommands, []string{command(machinecontract.OnboardingCommandNext)}) {
		t.Fatalf("start does not name the batch command: %#v", session.NextCommands)
	}
	receipt := filepath.Join(t.TempDir(), "completion.json")
	args := []string{"--json", "cognition", "onboard", "next"}
	for round := 0; round < 10; round++ {
		var batch struct {
			Tasks              []json.RawMessage `json:"tasks"`
			CompletionTemplate json.RawMessage   `json:"completion_template"`
			NextCommands       []string          `json:"next_commands"`
		}
		decodeOnboardingTestJSON(t, mustCLI(t, root, args...), &batch)
		if len(batch.Tasks) == 0 {
			decodeOnboardingTestJSON(t, mustCLI(t, root, "--json", "cognition", "onboard", "status"), &session)
			artifacts := strings.NewReplacer(
				"{plan_file}", onboardingTestQuote(filepath.Join(root, filepath.FromSlash(session.PlanArtifact))),
				"{snapshot_file}", onboardingTestQuote(filepath.Join(root, filepath.FromSlash(session.SnapshotArtifact))))
			want := []string{command(artifacts.Replace(machinecontract.OnboardingCommandMappingTemplate)),
				command(machinecontract.OnboardingCommandPreviewMapping)}
			if batch.CompletionTemplate != nil || !reflect.DeepEqual(batch.NextCommands, want) || !reflect.DeepEqual(session.NextCommands, want) {
				t.Fatalf("finished authoring does not route a Migration through its mapping template:\nbatch   %#v\nsession %#v", batch.NextCommands, session.NextCommands)
			}
			return
		}
		if len(batch.CompletionTemplate) == 0 || !reflect.DeepEqual(batch.NextCommands, []string{command(machinecontract.OnboardingCommandComplete)}) {
			t.Fatalf("batch %d does not name and hand out its receipt: %#v", round, batch)
		}
		if err := os.WriteFile(receipt, batch.CompletionTemplate, 0o600); err != nil {
			t.Fatal(err)
		}
		args = []string{"--json", "cognition", "onboard", "next", "--completion-file", receipt}
	}
	t.Fatal("authoring did not finish in ten batches")
}

type onboardingSessionView struct {
	NextAction       string   `json:"next_action"`
	NextCommands     []string `json:"next_commands"`
	PlanArtifact     string   `json:"plan_artifact"`
	SnapshotArtifact string   `json:"snapshot_artifact"`
}

// onboardingTestCommand spells a suffix the way this test binary binds it.
func onboardingTestCommand(t *testing.T) func(string) string {
	t.Helper()
	executable, err := currentAgentExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := agentCommandPrefixFor(runtime.GOOS, executable)
	if err != nil {
		t.Fatal(err)
	}
	return func(suffix string) string { return prefix + " " + suffix }
}

func onboardingTestQuote(path string) string {
	if runtime.GOOS == "windows" {
		return powerShellQuote(path)
	}
	return posixQuote(path)
}

func decodeOnboardingTestJSON(t *testing.T, data string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), target); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, data)
	}
}
