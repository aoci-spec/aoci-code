package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/engine"
	"github.com/aoci-spec/aoci-code/internal/baseline"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
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
				estimate := readScanAuthoringEstimate(t, root)
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
				if fixture.name == "long_paths" && mode.deliverSiblings && actual > 7 {
					t.Errorf("the long-path fixture took %d parallel rounds, above the seven-round baseline", actual)
				}
			})
		}
	}
}

func TestColdFirstBuildEstimateUsesMaintainLineEvidence(t *testing.T) {
	pinDefaultLocale(t)
	t.Setenv(baseline.FingerprintCacheEnv, "off")
	root := t.TempDir()
	initialPaths := []string{".gitattributes", ".gitignore", "AGENTS.md"}
	testkit.WriteFile(t, root, ".gitattributes", "* text=auto\n")
	testkit.WriteFile(t, root, ".gitignore", "# Fixture\n")
	testkit.WriteFile(t, root, "AGENTS.md", "# Fixture\n")
	const shortPaths = 17
	for i := range shortPaths {
		path := fmt.Sprintf("src/a/file%03d.go", i)
		testkit.WriteFile(t, root, path, "package fixture\n")
		initialPaths = append(initialPaths, path)
	}
	// The last five paths must make the candidate payload itself span two
	// rounds even when advisory samples no longer consume most of the budget.
	directory := "src/z" + strings.Repeat("x", 190) + strings.Repeat("/"+strings.Repeat("m", 190), 8)
	for i := range 5 {
		testkit.WriteFile(t, root, fmt.Sprintf("%s/file%03d.go", directory, i), "package fixture\n")
	}
	mustCLI(t, root, "--quiet", "init", "--locale", "en-US", "--agent=", "--hooks=false", "--here=false")
	mustCLI(t, root, "config", "set", "code_cognition_parallel_batches", "1")
	mustCLI(t, root, "config", "set", "code_cognition_batch_entries", "20")

	// Including the tall file exactly fills the reading budget. Only a warm
	// line cache should cut before the next short file and alter the next
	// round's path samples.
	tallLines := machinecontract.CodeCognitionBatchSourceLines
	for _, path := range initialPaths[:len(initialPaths)-2] {
		count, err := afs.CountFileLines(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		tallLines -= count
	}
	testkit.WriteFile(t, root, "src/a/file015.go", "package fixture\n"+strings.Repeat("//x\n", tallLines-1))

	estimate := readScanAuthoringEstimate(t, root)
	if estimate.Targets != 25 || estimate.BatchLimit != 20 || estimate.SourceLines != machinecontract.CodeCognitionBatchSourceLines+6 {
		t.Fatalf("unexpected fixture estimate: %+v", estimate)
	}
	sizes := authorFirstBuild(t, root, 1, 25, false)
	t.Logf("scan estimate=%d actual=%d batch sizes=%v source lines=%d", estimate.Rounds, len(sizes), sizes, estimate.SourceLines)
	if len(sizes) != 3 || sizes[0] != estimate.BatchLimit || estimate.Rounds != len(sizes) {
		t.Fatalf("estimated %d rounds, reached alignment with batches %v; want 3 rounds", estimate.Rounds, sizes)
	}
}

func TestFirstBuildEstimateAcrossCacheTransition(t *testing.T) {
	pinDefaultLocale(t)
	t.Setenv(baseline.FingerprintCacheEnv, "")
	cacheWrites := baseline.FingerprintCacheWritesEnabled()
	baseline.SetFingerprintCacheWrites(true)
	t.Cleanup(func() { baseline.SetFingerprintCacheWrites(cacheWrites) })
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	stable := time.Unix(1234567890, 0)
	const tallLines = machinecontract.CodeCognitionBatchSourceLines + 1
	for _, source := range []struct {
		path  string
		lines int
	}{{"src/a.go", 1}, {"src/b.go", tallLines}, {"src/c.go", 1}} {
		testkit.WriteFile(t, root, source.path, "package fixture\n"+strings.Repeat("//x\n", source.lines-1))
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(source.path)), stable, stable); err != nil {
			t.Fatal(err)
		}
	}
	printed, _, stderr, code := runCLIPrinted(t, root, "init", "--locale", "en-US", "--agent=", "--hooks=false", "--here=false")
	if code != ExitOK {
		t.Fatalf("init: exit %d: %s", code, stderr)
	}
	before := snapshotInitTree(t, root)
	if _, exists := before[".aoci/fingerprints.json"]; exists {
		t.Fatal("init persisted the fingerprint cache")
	}
	if _, exists, err := baseline.Load(root); err != nil || exists {
		t.Fatalf("expected no Baseline after init, got exists=%t err=%v", exists, err)
	}
	dry := readScanAuthoringEstimate(t, root, "--dry-run")
	if after := snapshotInitTree(t, root); !maps.Equal(before, after) {
		t.Error("scan --dry-run changed the repository tree")
	}
	scan := readScanAuthoringEstimate(t, root)
	if lines, known := baseline.OpenFingerprintCache(root).KnownLines(filepath.Join(root, "src", "b.go"), "src/b.go"); !known || lines != tallLines {
		t.Errorf("scan must persist %d trusted lines for src/b.go, got %d (known=%t)", tallLines, lines, known)
	}
	const targets = 5
	sizes := authorFirstBuild(t, root, scan.ParallelBatches, targets, false)
	t.Logf("init=%q dry-run=%+v scan=%+v Maintain batch sizes=%v", firstBuildEstimateOutput(printed), dry, scan, sizes)
	if !slices.Equal(sizes, []int{3, 1, 1}) {
		t.Fatalf("first build batch sizes = %v, want [3 1 1]", sizes)
	}
	if scan.Targets != targets || scan.Rounds != len(sizes) || dry != scan {
		t.Errorf("dry-run and scan must both predict %d targets in %d rounds: dry-run=%+v scan=%+v", targets, len(sizes), dry, scan)
	}
	if got, want := firstBuildEstimateOutput(printed), scanAuthoringEstimateLine(scan); got != want {
		t.Errorf("init disagrees with scan: got %q, want %q", got, want)
	}
}

func firstBuildEstimateOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "index files need an Entry:") {
			return strings.TrimPrefix(strings.TrimSpace(line), "- ")
		}
	}
	return ""
}

func TestScanDryRunLeavesLegacyLocaleUnwritten(t *testing.T) {
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	testkit.WriteFile(t, root, ".aoci/config.json", "{\"version\": 1}\n")
	testkit.WriteFile(t, root, "main.go", "package fixture\n")
	before := snapshotInitTree(t, root)
	mustCLI(t, root, "scan", "--json", "--dry-run")
	if after := snapshotInitTree(t, root); !maps.Equal(before, after) {
		t.Fatal("scan --dry-run changed the pre-Locale repository tree")
	}
}

func TestMaintainDatabaseAndObserveShareTransportBudget(t *testing.T) {
	pinDefaultLocale(t)
	if runtime.GOOS != "linux" {
		t.Skip("fixture needs Linux paths over 1 KiB")
	}
	root := t.TempDir()
	for path, content := range map[string]string{
		"main.go": "package fixture\n", ".gitattributes": "* text=auto\n", "AGENTS.md": "# Fixture\n",
	} {
		testkit.WriteFile(t, root, path, content)
	}
	directory := strings.Repeat(strings.Repeat("&", 227)+"/", 8)
	for i := range 3 {
		testkit.WriteFile(t, root, fmt.Sprintf("%sitem%02d_test.go", directory, i), "package fixture\n")
	}
	mustCLI(t, root, "--quiet", "init", "--locale", "en-US", "--agent=", "--hooks=false", "--here=false")
	scan := readScanAuthoringEstimate(t, root)
	authorFirstBuild(t, root, scan.ParallelBatches, scan.Targets, false)

	dbPath := filepath.Join(t.TempDir(), "wide.sqlite")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for i := range 12 {
		columns := []string{"id INTEGER PRIMARY KEY"}
		for j := range 26 {
			columns = append(columns, fmt.Sprintf("fixture_property_%02d TEXT DEFAULT 'synthetic'", j))
		}
		if _, err := db.Exec(fmt.Sprintf("CREATE TABLE fixture_table_%02d (%s)", i, strings.Join(columns, ","))); err != nil {
			t.Fatal(err)
		}
	}
	mustCLI(t, root, "database", "source", "add", "--source-id", "local", "--engine", "sqlite", "--path", dbPath)
	var snap struct {
		SHA256 string `json:"source_snapshot_sha256"`
	}
	if err := json.Unmarshal([]byte(mustCLI(t, root, "database", "snapshot", "--source", "local", "--json")), &snap); err != nil {
		t.Fatal(err)
	}
	mustCLI(t, root, "database", "baseline", "accept", "--source", "local", "--snapshot-sha", snap.SHA256, "--json")

	repo, err := engine.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.Connect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	read := func() (int, int, int, bool, bool) {
		t.Helper()
		result, err := session.Call(t.Context(), "aoci_maintain", nil)
		if err != nil || result.IsError {
			t.Fatalf("Maintain: %v %s", err, result.Text)
		}
		var response struct {
			Candidates []json.RawMessage `json:"candidates"`
			Batch      struct {
				Exceeded bool `json:"transport_budget_exceeded"`
			} `json:"authoring_batch"`
			Observe struct {
				Evidence []json.RawMessage `json:"evidence"`
				Confirm  *struct{}         `json:"confirm_request"`
			} `json:"observe_review"`
		}
		if err := json.Unmarshal([]byte(result.Text), &response); err != nil {
			t.Fatal(err)
		}
		return len(result.Text), len(response.Candidates), len(response.Observe.Evidence), response.Batch.Exceeded, response.Observe.Confirm != nil
	}
	bytes, candidates, listed, exceeded, confirm := read()
	if bytes <= scan.TransportBudgetBytes || candidates <= 1 || listed != 0 || exceeded || confirm {
		t.Fatalf("pure Database plan should retain its independent Evidence limit, got %d bytes, %d candidates, %d review items, exceeded=%t confirm=%t", bytes, candidates, listed, exceeded, confirm)
	}
	for i := range 3 {
		testkit.WriteFile(t, root, fmt.Sprintf("%sitem%02d_test.go", directory, i), "package fixture\n// observed change\n")
	}
	bytes, candidates, listed, exceeded, confirm = read()
	t.Logf("Database with Observe: %d bytes, %d candidates, %d review items, exceeded=%t confirm=%t", bytes, candidates, listed, exceeded, confirm)
	if bytes <= scan.TransportBudgetBytes || candidates != 1 || listed != 3 || !exceeded || !confirm {
		t.Fatal("oversize Observe evidence must stay complete beside a single marked Database candidate")
	}
}

func readScanAuthoringEstimate(t *testing.T, root string, flags ...string) scanAuthoringEstimate {
	t.Helper()
	var scan struct {
		Estimate scanAuthoringEstimate `json:"authoring_estimate"`
	}
	raw := mustCLI(t, root, append([]string{"scan", "--json"}, flags...)...)
	if err := json.Unmarshal([]byte(raw), &scan); err != nil {
		t.Fatal(err)
	}
	return scan.Estimate
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
