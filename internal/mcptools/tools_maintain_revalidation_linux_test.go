package mcptools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestMaintainRevalidatesRestoreAfterConcurrentApply(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 28)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "--all"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	enableProductionManagedScope(t, root)
	writer, follower := connectMCPClient(t, root), connectMCPClient(t, root)
	buildToAligned(t, writer)
	setBatchEntries(t, root, 3)
	rebinds := writeMaintainRebindWave(t, root, 28)

	// Like rv54-B, pause during inventory, after the assessment read the
	// Baseline but before it compares the live Code Volume to that record.
	// Writer commands bypass the hook while the reader is at a gate.
	gate := t.TempDir()
	script := filepath.Join(gate, "git")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
case "$*" in
  *"ls-files -z --cached --stage"*)
    if mkdir "$AOCI_TEST_GATE/active" 2>/dev/null; then
      for step in 1 2; do
        if (set -C; : > "$AOCI_TEST_GATE/ready-$step") 2>/dev/null; then
          tries=0
          until test -e "$AOCI_TEST_GATE/go-$step"; do
            tries=$((tries + 1))
            if test "$tries" -ge 500; then exit 97; fi
            sleep 0.02
          done
          break
        fi
      done
      rmdir "$AOCI_TEST_GATE/active"
    fi
esac
exec "$AOCI_TEST_REAL_GIT" "$@"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOCI_TEST_GATE", gate)
	t.Setenv("AOCI_TEST_REAL_GIT", git)
	t.Setenv("AOCI_PROCESS_MEMO", "off")
	t.Setenv("PATH", gate+string(os.PathListSeparator)+os.Getenv("PATH"))
	var planChecks atomic.Int32
	previousHook := beforePlanRecordsCheck
	beforePlanRecordsCheck = func(string) { planChecks.Add(1) }
	defer func() { beforePlanRecordsCheck = previousHook }()
	goFiles := []string{filepath.Join(gate, "go-1"), filepath.Join(gate, "go-2")}
	finished := make(chan struct{})
	answer := make(chan string, 1)
	go func() {
		defer close(finished)
		answer <- callVolumeToolQuiet(follower, "aoci_maintain", nil)
	}()
	defer func() {
		for _, path := range goFiles {
			_ = os.WriteFile(path, nil, 0o600)
		}
		<-finished
	}()
	for i, path := range goFiles {
		ready := filepath.Join(gate, fmt.Sprintf("ready-%d", i+1))
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			select {
			case <-finished:
				t.Fatalf("Maintain answered before gate %d: %s", i+1, <-answer)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("Maintain never reached inventory gate %d", i+1)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if planChecks.Load() != int32(i) {
			t.Fatalf("inventory gate %d must occur within assessment %d, got %d prior plan checks", i+1, i+1, planChecks.Load())
		}
		beforeCode := volumeFileText(t, root, "aoci.code.txt")
		beforeBaseline := volumeFileText(t, root, ".aoci/baseline.json")
		if i == 1 {
			rebinds[i]["new_entry"] = strings.Replace(rebinds[i]["new_entry"].(string), "implement fixture behavior", "implement revised fixture behavior", 1)
		}
		applied := applyVolumeBatch(t, writer, rebinds[i])
		if applied.Status != autoStatusApplied || (volumeFileText(t, root, "aoci.code.txt") != beforeCode) != (i == 1) ||
			volumeFileText(t, root, ".aoci/baseline.json") == beforeBaseline {
			t.Fatalf("unexpected competing Apply %d: %+v", i+1, applied)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw := <-answer
	var result volumeMaintainResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result.NextAction == machinecontract.MaintainActionRestoreFormalAssets || result.Status != autoStatusRepairRequired ||
		result.CodePlan == nil || len(result.Candidates) == 0 || strings.Contains(raw, machinecontract.RemediationGitRestoreFormalAssets) {
		t.Fatalf("Maintain must reload after both successful Applies and issue work, got status=%s next_action=%s commands=%v", result.Status, result.NextAction, result.NextCommands)
	}
	for _, candidate := range result.Candidates {
		if candidate.Path == rebinds[0]["path"] || candidate.Path == rebinds[1]["path"] {
			t.Fatalf("Maintain repeated a target settled by the other session: %s", candidate.Path)
		}
	}
	if applied := applyVolumeBatch(t, follower, codeBatchArguments(wireCodePlan(result))); applied.Status != autoStatusApplied {
		t.Fatalf("the fresh batch must Apply: %+v", applied)
	}
	t.Logf("after Baseline-only Apply and Code Volume Apply, Maintain issued and applied %d fresh candidates", len(result.Candidates))
}
