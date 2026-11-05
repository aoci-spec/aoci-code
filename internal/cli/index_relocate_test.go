package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/index"
)

func runIndexRelocate(t *testing.T, root string, extra ...string) (int, indexRelocateResult, string) {
	t.Helper()
	args := append([]string{"--repo", root, "--json", "index", "relocate"}, extra...)
	var stdout, stderr bytes.Buffer
	code := executeCLI(args, &stdout, &stderr)
	var result indexRelocateResult
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("relocate output: %v\n%s%s", err, stdout.String(), stderr.String())
		}
	}
	return code, result, stdout.String() + stderr.String()
}

var indexRelocateFormalFiles = []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"}

// The fixture's Code Volume records the temp directory it was created under.
// One run rewrites that header to the neutral root, adds the Meta line, moves
// both Baseline records to the new bytes, leaves the Root alone, archives the
// receipt, and leaves the repository aligned; a second run writes nothing.
func TestIndexRelocateRewritesRootsNamesThemInMetaAndAdvancesBaseline(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	if !strings.Contains(string(before["aoci.code.txt"]), "===Code "+filepath.ToSlash(root)+"/===") {
		t.Fatalf("fixture must record its machine root:\n%s", before["aoci.code.txt"])
	}
	code, result, output := runIndexRelocate(t, root)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, output)
	}
	if result.Outcome != indexRelocateApplied || result.SectionsRewritten != 1 || !result.MetaNoteAdded || !result.BaselineAdvanced || result.TransactionID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	after := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	codeText := string(after["aoci.code.txt"])
	if !strings.Contains(codeText, "===Code /<repository>/===") || strings.Contains(codeText, filepath.ToSlash(root)) ||
		!strings.Contains(codeText, "main.go[CD7S]: F:run the deterministic CLI fixture") {
		t.Fatalf("Code Volume not relocated as expected:\n%s", codeText)
	}
	if !strings.Contains(string(after["aoci.meta.txt"]), index.NeutralRepositoryRoot) {
		t.Fatalf("Meta does not name the neutral root:\n%s", after["aoci.meta.txt"])
	}
	if !bytes.Equal(before["aoci.txt"], after["aoci.txt"]) {
		t.Fatal("the Root is a guard and must not change")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("Baseline: %v %v", exists, err)
	}
	for _, rel := range []string{"aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil || state.Files[rel].SHA256 != fingerprint.SHA256 {
			t.Fatalf("Baseline record of %s does not match the file: %+v %v", rel, state.Files[rel], hashErr)
		}
	}
	if state.Files["aoci.meta.txt"].Role != "index" {
		t.Fatalf("the record's role must survive: %+v", state.Files["aoci.meta.txt"])
	}
	if pending, pendingErr := cognitiontxn.Pending(root); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("receipt still pending: %v %v", pending, pendingErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".aoci", "transactions", "history", "relocate-"+result.TransactionID+".json")); statErr != nil {
		t.Fatalf("receipt not archived: %v", statErr)
	}
	if !verifyAligned(t, root) {
		t.Fatal("repository must stay aligned after the relocation")
	}
	code, result, output = runIndexRelocate(t, root)
	if code != 0 || result.Outcome != indexRelocateUnchanged {
		t.Fatalf("second run must change nothing: exit %d %+v %s", code, result, output)
	}
	again := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	for _, rel := range indexRelocateFormalFiles {
		if !bytes.Equal(after[rel], again[rel]) {
			t.Fatalf("%s changed on the second run", rel)
		}
	}
}

func TestIndexRelocateDryRunWritesNothing(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	code, result, output := runIndexRelocate(t, root, "--dry-run")
	if code != 0 || result.Outcome != indexRelocateDryRun || result.SectionsRewritten != 1 || !result.MetaNoteAdded || !result.BaselineAdvanced {
		t.Fatalf("dry run: exit %d %+v %s", code, result, output)
	}
	after := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	for _, rel := range indexRelocateFormalFiles {
		if !bytes.Equal(before[rel], after[rel]) {
			t.Fatalf("%s changed under --dry-run", rel)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, ".aoci", "transactions")); len(entries) != 0 {
		t.Fatalf("dry run left transaction state: %v", entries)
	}
}

// A Baseline record that no longer matches the rewritten asset is someone
// else's drift: the command exits 3 before any write.
func TestIndexRelocateRefusesStaleBaselineRecordWithoutWrites(t *testing.T) {
	root := localeSwitchFixture(t, true)
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	record := state.Files["aoci.code.txt"]
	record.SHA256 = strings.Repeat("0", 64)
	state.Files["aoci.code.txt"] = record
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	before := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	code, _, output := runIndexRelocate(t, root)
	if code != ExitConfig || !strings.Contains(output, "aoci.code.txt") {
		t.Fatalf("stale record must refuse with exit %d naming the asset: exit %d %s", ExitConfig, code, output)
	}
	after := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	for _, rel := range indexRelocateFormalFiles {
		if !bytes.Equal(before[rel], after[rel]) {
			t.Fatalf("%s changed despite the refusal", rel)
		}
	}
}

// A crash after the Meta is published leaves the receipt pending: every other
// command waits, and running relocate again completes the transaction.
func TestIndexRelocateResumesAfterCrashBetweenPublishes(t *testing.T) {
	root := localeSwitchFixture(t, true)
	indexRelocateFault = func(step string) error {
		if step == "after_publish_aoci.meta.txt" {
			return errors.New("injected crash")
		}
		return nil
	}
	t.Cleanup(func() { indexRelocateFault = func(string) error { return nil } })
	code, _, output := runIndexRelocate(t, root)
	if code == 0 {
		t.Fatalf("the injected crash must fail the first run: %s", output)
	}
	pending, err := cognitiontxn.PendingForOperation(root, indexRelocateOperation)
	if err != nil || len(pending) != 1 {
		t.Fatalf("one relocate receipt must be pending: %v %v", pending, err)
	}
	half := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	if !strings.Contains(string(half["aoci.meta.txt"]), index.NeutralRepositoryRoot) || !strings.Contains(string(half["aoci.code.txt"]), filepath.ToSlash(root)) {
		t.Fatal("the crash point must leave the Meta published and the Code Volume untouched")
	}
	var stdout, stderr bytes.Buffer
	if gate := executeCLI([]string{"--repo", root, "verify"}, &stdout, &stderr); gate != ExitInvalid || !strings.Contains(stderr.String(), "relocate") {
		t.Fatalf("other commands must wait on the receipt: exit %d %s%s", gate, stdout.String(), stderr.String())
	}
	indexRelocateFault = func(string) error { return nil }
	code, result, output := runIndexRelocate(t, root)
	if code != 0 || result.Outcome != indexRelocateApplied || result.TransactionID != pending[0] {
		t.Fatalf("resume must complete the same receipt: exit %d %+v %s", code, result, output)
	}
	if still, _ := cognitiontxn.Pending(root); len(still) != 0 {
		t.Fatalf("receipt not closed: %v", still)
	}
	done := readFixtureFiles(t, root, indexRelocateFormalFiles...)
	if strings.Contains(string(done["aoci.code.txt"]), filepath.ToSlash(root)) {
		t.Fatal("the Code Volume must be relocated after the resume")
	}
	if !verifyAligned(t, root) {
		t.Fatal("repository must be aligned after the resume")
	}
}
