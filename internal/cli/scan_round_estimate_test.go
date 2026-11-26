package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/engine"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/testkit"
)

func TestFirstBuildEstimateMatchesMaintain(t *testing.T) {
	pinDefaultLocale(t)
	// Newly written files may be inside the fingerprint cache's racy window.
	// Keeping the cache cold makes the wire-size boundary deterministic.
	t.Setenv(baseline.FingerprintCacheEnv, "off")
	for _, fixture := range []struct {
		name      string
		directory string
		short     int
	}{
		{name: "78_file_boundary", directory: "module0000"},
		// The first batch samples short paths, while later batches sample
		// the deeply nested sources that remain after that batch applies.
		{name: "long_paths", directory: strings.Repeat("module", 30) + "/" + strings.Repeat("service_", 29) + "x", short: 6},
	} {
		for _, mode := range []struct {
			name            string
			parallel        int
			deliverSiblings bool
		}{
			{name: "single_batch", parallel: 1},
			{name: "default_solo", parallel: 3},
			{name: "default_parallel", parallel: 3, deliverSiblings: true},
		} {
			t.Run(fixture.name+"/"+mode.name, func(t *testing.T) {
				root := t.TempDir()
				testkit.WriteFile(t, root, ".gitattributes", "* text=auto\n")
				testkit.WriteFile(t, root, ".gitignore", "# Fixture\n")
				testkit.WriteFile(t, root, "AGENTS.md", "# Fixture\n")
				const targets = 78
				for i := 0; i < targets-3; i++ {
					directory := "src/" + fixture.directory
					if i < fixture.short {
						directory = "src/a"
					}
					testkit.WriteFile(t, root, fmt.Sprintf("%s/file%03d.go", directory, i), "package fixture\n")
				}
				printed, _, stderr, code := runCLIPrinted(t, root, "init", "--locale", "en-US", "--agent=", "--hooks=false", "--here=false")
				if code != ExitOK {
					t.Fatalf("init: exit %d\n%s", code, stderr)
				}
				if mode.parallel == 1 {
					mustCLI(t, root, "config", "set", "code_cognition_parallel_batches", "1")
				}
				var scan struct {
					Estimate scanAuthoringEstimate `json:"authoring_estimate"`
				}
				raw := mustCLI(t, root, "scan", "--json")
				if err := json.Unmarshal([]byte(raw), &scan); err != nil {
					t.Fatal(err)
				}
				estimate := scan.Estimate
				if estimate.Targets != targets || estimate.ParallelBatches != mode.parallel {
					t.Fatalf("unexpected fixture estimate: %+v", estimate)
				}
				sizes := authorFirstBuild(t, root, mode.parallel, targets, mode.deliverSiblings)
				actual := len(sizes)
				predicted := estimate.Rounds
				if mode.deliverSiblings {
					predicted = estimate.ParallelRounds
					if !strings.Contains(printed, scanAuthoringEstimateLine(estimate)) ||
						!strings.Contains(printed, scanParallelEstimateLine(estimate)) {
						t.Errorf("init and scan disagree on the estimate: %+v\n%s", estimate, printed)
					}
				}
				t.Logf("estimate=%d actual=%d round sizes=%v", predicted, actual, sizes)
				if predicted != actual {
					t.Errorf("estimated %d Maintain rounds, reached alignment in %d", predicted, actual)
				}
			})
		}
	}
}

func authorFirstBuild(t *testing.T, root string, parallel, targets int, deliverSiblings bool) []int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	repo, err := engine.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	connect := func() *engine.Session {
		session, err := repo.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	leader := connect()
	call := func(session *engine.Session, tool string, args map[string]any, response any) {
		t.Helper()
		result, err := session.Call(ctx, tool, args)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("%s: %s", tool, result.Text)
		}
		if err := json.Unmarshal([]byte(result.Text), response); err != nil {
			t.Fatalf("%s: %v\n%s", tool, err, result.Text)
		}
	}
	type batch struct {
		Candidates []struct {
			Path string `json:"path"`
		} `json:"candidates"`
		CodePlan struct {
			ID       string `json:"batch_id"`
			Siblings []struct {
				ID string `json:"batch_id"`
			} `json:"siblings"`
		} `json:"code_plan"`
	}
	apply := func(session *engine.Session, current batch) bool {
		t.Helper()
		if current.CodePlan.ID == "" || len(current.Candidates) == 0 {
			t.Fatalf("Maintain failed to issue Code candidates: %+v", current)
		}
		entries := make([]map[string]string, 0, len(current.Candidates))
		for _, item := range current.Candidates {
			entries = append(entries, map[string]string{"path": item.Path,
				"new_entry": filepath.Base(item.Path) + "[CG5T]: F:Defines the fixture source | R:- | A:- | S:-"})
		}
		var result struct {
			Status        string `json:"status"`
			Applied       int    `json:"applied"`
			TerminalProof *struct {
				Aligned bool `json:"aligned"`
			} `json:"terminal_proof"`
		}
		call(session, "aoci_update_entry", map[string]any{"code_batch_id": current.CodePlan.ID, "entries": entries}, &result)
		if result.Status != "applied" || result.Applied != len(entries) {
			t.Fatalf("Apply did not write the whole batch: %+v", result)
		}
		return result.TerminalProof != nil && result.TerminalProof.Aligned
	}
	var sizes []int
	for authored := 0; authored < targets; {
		var first batch
		call(leader, "aoci_maintain", nil, &first)
		if len(first.CodePlan.Siblings) >= parallel {
			t.Fatalf("too many sibling batches: %+v", first.CodePlan)
		}
		if !deliverSiblings {
			first.CodePlan.Siblings = nil
		}
		workers := make([]*engine.Session, len(first.CodePlan.Siblings))
		siblings := make([]batch, len(workers))
		for i, sibling := range first.CodePlan.Siblings {
			workers[i] = connect()
			call(workers[i], "aoci_maintain", map[string]any{"batch_id": sibling.ID}, &siblings[i])
		}
		aligned := apply(leader, first)
		size := len(first.Candidates)
		for i, sibling := range siblings {
			aligned = apply(workers[i], sibling)
			size += len(sibling.Candidates)
		}
		sizes = append(sizes, size)
		authored += size
		if aligned != (authored == targets) {
			t.Fatalf("terminal proof aligned=%v after authoring %d of %d targets", aligned, authored, targets)
		}
	}
	return sizes
}
