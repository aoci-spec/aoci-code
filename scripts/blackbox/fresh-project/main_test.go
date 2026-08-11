// Package freshproject exercises the public AOCI executable as an external
// Host. It deliberately imports only the Go standard library: release
// acceptance must not depend on AOCI implementation packages or hash helpers.
package freshproject

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const hostAuthoringRunID = "fresh-release-blackbox-host-run"

func TestFreshProjectReleaseBinary(t *testing.T) {
	binary := requireReleaseBinary(t)
	root := createSubjectRepository(t)

	version := runCLI(t, binary, 0, "--version")
	if !strings.Contains(version.stdout, " version ") || !strings.Contains(version.stdout, "commit ") ||
		strings.Contains(version.stdout, "commit none") || strings.Contains(version.stdout, "built unknown") {
		t.Fatalf("release identity is not traceable: %s", version.stdout)
	}

	capabilities := runJSON(t, binary, 0, "--repo", root, "--json", "capabilities")
	if !jsonContainsString(capabilities, "cognition-onboarding-next-action/v1") {
		t.Fatal("capability manifest does not advertise the Fresh Host action contract")
	}
	if !jsonContainsString(capabilities, "cognition-onboarding-route/v1") {
		t.Fatal("capability manifest does not advertise the Fresh routing contract")
	}

	initResult := runCLI(t, binary, 0, "--repo", root, "init", "--agent", "opencode", "--locale", "en-US", "--cognition", "project")
	if !strings.Contains(initResult.stdout, "Fresh Onboarding started") {
		t.Fatalf("project init did not start Fresh Onboarding:\n%s", initResult.stdout)
	}
	assertFormalAssetsAbsent(t, root)
	assertOpenCodeBinding(t, root, binary)
	assertPreBootstrapMCPRoute(t, binary, root)

	batch := runJSON(t, binary, 0, "--repo", root, "cognition", "onboard", "next",
		"--max-objects", "2", "--max-evidence-bytes", "4096", "--json")
	allTasks := map[string]map[string]any{}
	completionCount := 0
	badCompletionChecked := false
	for {
		contract := requireActionContract(t, batch)
		switch mustString(t, contract, "action") {
		case "submit_authoring_completion":
			if count := mustInt(t, batch, "object_count"); count > 2 {
				t.Fatalf("context budget was not preserved across batches: object_count=%d", count)
			}
			arguments := mustStrings(t, mustObject(t, contract, "command"), "arguments")
			assertArgumentPair(t, arguments, "--max-objects", "2")
			assertArgumentPair(t, arguments, "--max-evidence-bytes", "4096")
			captureTasks(t, batch, allTasks)
			request := cloneObject(t, mustObject(t, batch, "completion_request_template"))
			fillHostDeclaration(t, request)
			if !badCompletionChecked {
				assertBadCompletionIsPreciseAndZeroWrite(t, binary, root, batch, request)
				badCompletionChecked = true
			}
			batch = executeContractJSON(t, binary, root, contract, request, 0)
			completionCount++
			if completionCount > 16 {
				t.Fatal("Fresh authoring did not converge within 16 machine-issued batches")
			}

		case "bind_candidate_payload":
			if completionCount < 2 {
				t.Fatalf("expected a context-budgeted multi-batch run, got %d completion(s)", completionCount)
			}
			candidate := authorCandidate(t, root, batch, allTasks)
			binding := executeContractJSON(t, binary, root, contract, candidate, 0)
			if mustString(t, binding, "version") != "cognition-onboarding-candidate-binding/v1" {
				t.Fatalf("unexpected Candidate binding version: %#v", binding)
			}
			if mustBool(t, binding, "semantic_generated") || !mustBool(t, binding, "host_declaration_echoed") {
				t.Fatalf("Candidate binding crossed the Host semantic boundary: %#v", binding)
			}
			provenance := cloneObject(t, mustObject(t, binding, "semantic_authoring_provenance_template"))
			previewContract := requireActionContract(t, binding)
			preview := executeContractJSON(t, binary, root, previewContract, provenance, 0)
			if mustString(t, preview, "status") != "preview_ready" || !mustBool(t, mustObject(t, preview, "formal_asset_proof"), "formal_assets_unchanged") {
				t.Fatalf("Candidate Preview is not ready and zero-write: %#v", preview)
			}
			goto authoringComplete

		default:
			t.Fatalf("unexpected Fresh action %q: %#v", mustString(t, contract, "action"), contract)
		}
	}

authoringComplete:
	status := runJSON(t, binary, 0, "--repo", root, "cognition", "onboard", "status", "--json")
	resumeContract := requireActionContract(t, status)
	if mustString(t, resumeContract, "action") != "resume" {
		t.Fatalf("Preview did not route to resumable Prepare/Auto Apply: %#v", resumeContract)
	}
	session := executeContractWithoutRequest(t, binary, resumeContract, 0)
	for attempt := 0; mustString(t, session, "status") != "completed" && attempt < 2; attempt++ {
		contract := requireActionContract(t, session)
		session = executeContractWithoutRequest(t, binary, contract, 0)
	}
	assertCompletedSession(t, session)

	verify := runJSON(t, binary, 0, "--repo", root, "verify", "--json")
	assertZeroGovernanceDebt(t, verify)
	check := runJSON(t, binary, 0, "--repo", root, "check", "--json")
	if !mustBool(t, check, "ok") {
		t.Fatalf("Check is not ready: %#v", check)
	}
	guide := runJSON(t, binary, 0, "--repo", root, "index", "agent", "guide", "--agent", "opencode", "--json")
	if mustString(t, guide, "stage") != "aligned" || mustString(t, guide, "next_action") != "none" {
		t.Fatalf("Volumes Guide is not aligned: %#v", guide)
	}

	assertMCP(t, binary, root)
}

func assertArgumentPair(t *testing.T, arguments []string, flag, value string) {
	t.Helper()
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag && arguments[index+1] == value {
			return
		}
	}
	t.Fatalf("contract arguments do not preserve %s %s: %#v", flag, value, arguments)
}

type cliResult struct {
	stdout string
	stderr string
	code   int
}

func requireReleaseBinary(t *testing.T) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("AOCI_BIN"))
	if value == "" {
		t.Skip("set AOCI_BIN to an unpacked release binary to run the Fresh project black-box acceptance")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		t.Fatalf("AOCI_BIN is not a regular executable: %s (%v)", abs, err)
	}
	return abs
}

func createSubjectRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for the Fresh project release fixture")
	}
	root := t.TempDir()
	files := map[string]string{
		"README.md": "# Counter Service\n\nA tiny counter library used only for AOCI release acceptance.\n",
		"go.mod":    "module example.invalid/aoci-fresh-blackbox\n\ngo 1.26\n",
		"counter/counter.go": `package counter

// Counter stores a mutable integer.
type Counter struct{ value int }

// Increment increases the counter by one.
func (c *Counter) Increment() { c.value++ }

// Value returns the current integer.
func (c Counter) Value() int { return c.value }
`,
		"counter/counter_test.go": `package counter

import "testing"

func TestIncrement(t *testing.T) {
	var value Counter
	value.Increment()
	if value.Value() != 1 { t.Fatal("counter did not increment") }
}
`,
	}
	for relative, content := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(arguments ...string) {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", arguments, err, output)
		}
	}
	git("init", "--quiet")
	git("config", "user.name", "AOCI Blackbox")
	git("config", "user.email", "blackbox@example.invalid")
	git("add", ".")
	git("commit", "--quiet", "-m", "fresh fixture")
	return root
}

func runCLI(t *testing.T, binary string, expectedCode int, arguments ...string) cliResult {
	t.Helper()
	ctx, cancel := contextWithTimeout(t, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run %s %v: %v", binary, arguments, err)
		}
	}
	if code != expectedCode {
		t.Fatalf("unexpected exit %d, want %d: %s %v\nstdout:\n%s\nstderr:\n%s", code, expectedCode, binary, arguments, stdout.String(), stderr.String())
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func runJSON(t *testing.T, binary string, expectedCode int, arguments ...string) map[string]any {
	t.Helper()
	result := runCLI(t, binary, expectedCode, arguments...)
	var value map[string]any
	if err := decodeOneJSON([]byte(result.stdout), &value); err != nil {
		t.Fatalf("decode JSON from %s %v: %v\nstdout:\n%s\nstderr:\n%s", binary, arguments, err, result.stdout, result.stderr)
	}
	return value
}

func decodeOneJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing output after JSON document")
	}
	return nil
}

func contextWithTimeout(t *testing.T, duration time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), duration)
}

func requireActionContract(t *testing.T, parent map[string]any) map[string]any {
	t.Helper()
	contract := mustObject(t, parent, "next_action_contract")
	if mustString(t, contract, "version") != "cognition-onboarding-next-action/v1" {
		t.Fatalf("unexpected next_action_contract version: %#v", contract)
	}
	if mustBool(t, contract, "formal_writes_started") || mustBool(t, contract, "tty_required") {
		t.Fatalf("Fresh authoring action crossed a write or TTY boundary: %#v", contract)
	}
	return contract
}

func executeContractJSON(t *testing.T, binary, root string, contract, request map[string]any, expectedCode int) map[string]any {
	t.Helper()
	command := mustObject(t, contract, "command")
	executable := mustString(t, command, "executable")
	if !samePath(executable, binary) {
		t.Fatalf("contract executable %q does not match AOCI_BIN %q", executable, binary)
	}
	requestFile := mustString(t, command, "suggested_request_file")
	if requestFile == "" {
		t.Fatal("contract did not supply an exact suggested_request_file")
	}
	if !filepath.IsAbs(requestFile) {
		requestFile = filepath.Join(root, filepath.FromSlash(requestFile))
	}
	writeJSONFile(t, requestFile, request)
	arguments := mustStrings(t, command, "arguments")
	for _, argument := range arguments {
		if strings.Contains(argument, "{") || strings.Contains(argument, "<HOST_") {
			t.Fatalf("contract argv contains an unresolved placeholder: %#v", arguments)
		}
	}
	return runJSON(t, executable, expectedCode, arguments...)
}

func executeContractWithoutRequest(t *testing.T, binary string, contract map[string]any, expectedCode int) map[string]any {
	t.Helper()
	command := mustObject(t, contract, "command")
	executable := mustString(t, command, "executable")
	if !samePath(executable, binary) {
		t.Fatalf("contract executable %q does not match AOCI_BIN %q", executable, binary)
	}
	if requestFile, _ := command["suggested_request_file"].(string); requestFile != "" {
		t.Fatalf("request-free action unexpectedly requires %q", requestFile)
	}
	return runJSON(t, executable, expectedCode, mustStrings(t, command, "arguments")...)
}

func fillHostDeclaration(t *testing.T, request map[string]any) {
	t.Helper()
	declaration := mustObject(t, request, "semantic_authoring_declaration")
	if current := mustString(t, declaration, "origin"); current == "<HOST_ASSERT_REQUIRED_ORIGIN>" {
		declaration["origin"] = "host_model"
	} else if current != "host_model" {
		t.Fatalf("unexpected origin template %q", current)
	}
	if current := mustString(t, declaration, "authoring_run_id"); current == "<HOST_ISSUED_AUTHORING_RUN_ID>" {
		declaration["authoring_run_id"] = hostAuthoringRunID
	} else if current != hostAuthoringRunID {
		t.Fatalf("authoring run changed between machine batches: %q", current)
	}
}

func captureTasks(t *testing.T, batch map[string]any, target map[string]map[string]any) {
	t.Helper()
	for _, raw := range mustArray(t, batch, "tasks") {
		task, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("task is not an object: %#v", raw)
		}
		id := mustString(t, task, "task_id")
		if _, duplicate := target[id]; duplicate {
			t.Fatalf("machine repeated authoring task %q", id)
		}
		target[id] = task
	}
}

func authorCandidate(t *testing.T, root string, batch map[string]any, tasks map[string]map[string]any) map[string]any {
	t.Helper()
	draft := mustObject(t, batch, "candidate_draft_request")
	if mustString(t, draft, "semantic_authoring_provenance_mode") != "omit_until_candidate_binding" {
		t.Fatalf("unexpected Candidate provenance mode: %#v", draft)
	}
	candidate := cloneObject(t, mustObject(t, draft, "template"))
	if _, exists := candidate["semantic_authoring_provenance"]; exists {
		t.Fatal("machine Candidate draft asserted Host provenance")
	}

	assets := mustArray(t, candidate, "assets")
	byID := map[string]map[string]any{}
	for _, raw := range assets {
		asset, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("Candidate asset is not an object: %#v", raw)
		}
		byID[mustString(t, asset, "asset_id")] = asset
	}
	for _, id := range []string{"root", "meta", "code"} {
		if byID[id] == nil {
			t.Fatalf("Candidate template is missing %s asset", id)
		}
	}
	byID["root"]["content"] = strings.Join([]string{
		"#AOCI-ROOT-MANIFEST: 1",
		"#Format-Version: cognition-volumes/v1",
		"#Locale: en-US",
		"#Project: Counter Service release black-box fixture",
		"#Global-Invariants: Counter increments remain deterministic and Host integration stays project-bound",
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled",
	}, "\n") + "\n"
	byID["meta"]["content"] = strings.Join([]string{
		"#AOCI-META-VOLUME: 1",
		"#Object-Protocol: repository-cognition-object/v2",
		"#FRAS-Discipline: 2",
		"#FRAS-v2-Limits-Authority: machine-contract",
		"#S-Admission: non-inferable-and-error-preventing",
		"#Object-Kinds: code=file database=table",
		"#[Tag dictionary: code]",
		"#A Layer: C Code",
		"#B Module: A Application I Infrastructure G Governance",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
		"#[Tag dictionary: database]",
		"#A Layer: D Database",
		"#B Module: S Schema",
		"#C Importance: 9 8 7 5 3 1",
		"#E Scale: L M S T",
	}, "\n") + "\n"
	byID["code"]["content"] = authorCodeVolume(t, root, tasks)
	return candidate
}

func authorCodeVolume(t *testing.T, root string, tasks map[string]map[string]any) string {
	t.Helper()
	semantics := map[string]string{
		"AGENTS.md":          "AGENTS.md[CG5T]: F:Defines project Agent integration and cognition workflow rules | R:- | A:AOCI managed block | S:The managed block remains machine-governed",
		"README.md":          "README.md[CG3T]: F:Introduces the Counter Service release fixture | R:- | A:- | S:-",
		"go.mod":             "go.mod[CI5T]: F:Declares the independent Counter Service Go module | R:- | A:example.invalid/aoci-fresh-blackbox | S:-",
		"opencode.json":      "opencode.json[CG5T]: F:Binds the project OpenCode MCP server to the tested AOCI binary and repository root | R:- | A:OpenCode V1 mcp.aoci | S:Binary and repository paths remain absolute",
		"counter/counter.go": "counter.go[CA9T]: F:Implements mutable counter increment and value access | R:- | A:Counter,Increment,Value | S:Increment changes the stored value by exactly one",
	}
	paths := make([]string, 0)
	for id := range tasks {
		if strings.HasPrefix(id, "code:") {
			paths = append(paths, strings.TrimPrefix(id, "code:"))
		}
	}
	sort.Strings(paths)
	if len(paths) != len(semantics) {
		t.Fatalf("unexpected Code authoring set: %v", paths)
	}
	sections := map[string][]string{}
	for _, path := range paths {
		entry, ok := semantics[path]
		if !ok {
			t.Fatalf("fixture has no Host-authored semantics for machine task %q", path)
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		sections[directory] = append(sections[directory], entry)
	}
	directories := make([]string, 0, len(sections))
	for directory := range sections {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	parts := []string{"#AOCI-CODE-VOLUME: 1"}
	for _, directory := range directories {
		sort.Strings(sections[directory])
		sectionRoot := root
		if directory != "." {
			sectionRoot = filepath.Join(root, filepath.FromSlash(directory))
		}
		parts = append(parts, "===Code "+filepath.ToSlash(sectionRoot)+"/===", strings.Join(sections[directory], "\n"))
	}
	return strings.Join(parts, "\n") + "\n"
}

func assertBadCompletionIsPreciseAndZeroWrite(t *testing.T, binary, root string, batch, good map[string]any) {
	t.Helper()
	before := runJSON(t, binary, 0, "--repo", root, "cognition", "onboard", "status", "--json")
	bad := cloneObject(t, good)
	bad["batch_id"] = strings.Repeat("0", 64)
	path := filepath.Join(t.TempDir(), "blackbox-bad-completion.json")
	writeJSONFile(t, path, bad)
	result := runCLI(t, binary, 2, "--repo", root, "cognition", "onboard", "next", "--completion-file", path, "--json")
	combined := result.stdout + "\n" + result.stderr
	for _, anchor := range []string{"onboarding_completion_batch_mismatch", "batch_id", "formal_writes_started"} {
		if !strings.Contains(combined, anchor) {
			t.Fatalf("precise zero-write diagnostic is missing %q:\n%s", anchor, combined)
		}
	}
	assertFormalAssetsAbsent(t, root)
	after := runJSON(t, binary, 0, "--repo", root, "cognition", "onboard", "status", "--json")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected Completion changed persisted Session state:\nbefore=%#v\nafter=%#v", before, after)
	}
	retry := runJSON(t, binary, 0, "--repo", root, "cognition", "onboard", "next", "--max-objects", "2", "--json")
	if mustString(t, retry, "batch_id") != mustString(t, batch, "batch_id") {
		t.Fatal("rejected Completion changed the active machine Batch")
	}
}

func assertOpenCodeBinding(t *testing.T, root, binary string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := decodeOneJSON(data, &document); err != nil {
		t.Fatal(err)
	}
	mcp := mustObject(t, document, "mcp")
	aoci := mustObject(t, mcp, "aoci")
	command := mustStrings(t, aoci, "command")
	want := []string{binary, "--repo", root, "mcp"}
	if len(command) != len(want) {
		t.Fatalf("OpenCode command = %#v, want %#v", command, want)
	}
	for index := range want {
		if index == 0 || index == 2 {
			if !samePath(command[index], want[index]) {
				t.Fatalf("OpenCode path %q does not match %q", command[index], want[index])
			}
		} else if command[index] != want[index] {
			t.Fatalf("OpenCode command = %#v, want %#v", command, want)
		}
	}
}

func assertFormalAssetsAbsent(t *testing.T, root string) {
	t.Helper()
	for _, relative := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err == nil || !os.IsNotExist(err) {
			t.Fatalf("formal asset exists before Root-last Apply: %s (err=%v)", relative, err)
		}
	}
}

func assertCompletedSession(t *testing.T, session map[string]any) {
	t.Helper()
	if mustString(t, session, "status") != "completed" || mustString(t, session, "next_action") != "none" {
		t.Fatalf("Fresh Session did not complete: %#v", session)
	}
	for _, field := range []string{"governance_aligned", "check_ok", "structure_valid"} {
		if !mustBool(t, session, field) {
			t.Fatalf("completed Session has %s=false: %#v", field, session)
		}
	}
	if mustInt(t, session, "business_rows_read") != 0 || mustInt(t, session, "ddl_dml_statements") != 0 || mustBool(t, session, "network_accessed") {
		t.Fatalf("Fresh Bootstrap crossed the isolated evidence boundary: %#v", session)
	}
	projection := mustObject(t, session, "authorization_projection")
	if !mustBool(t, projection, "auto_ready") || mustString(t, session, "transaction_state") != "applied" {
		t.Fatalf("Fresh Prepare/Apply did not use eligible policy-bound auto: %#v", session)
	}
}

func assertZeroGovernanceDebt(t *testing.T, verify map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(verify)
	for _, anchor := range []string{`"stale":[]`, `"orphan":[]`, `"unbaselined":[]`} {
		if !bytes.Contains(encoded, []byte(anchor)) {
			t.Fatalf("Verify does not prove zero %s debt: %s", anchor, encoded)
		}
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cloneObject(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := decodeOneJSON(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func mustObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object in %#v", key, parent)
	}
	return value
}

func mustArray(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	value, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%s is not an array in %#v", key, parent)
	}
	return value
}

func mustStrings(t *testing.T, parent map[string]any, key string) []string {
	t.Helper()
	raw := mustArray(t, parent, key)
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		current, ok := value.(string)
		if !ok {
			t.Fatalf("%s includes a non-string value: %#v", key, value)
		}
		values = append(values, current)
	}
	return values
}

func mustString(t *testing.T, parent map[string]any, key string) string {
	t.Helper()
	value, ok := parent[key].(string)
	if !ok {
		t.Fatalf("%s is not a string in %#v", key, parent)
	}
	return value
}

func mustBool(t *testing.T, parent map[string]any, key string) bool {
	t.Helper()
	value, ok := parent[key].(bool)
	if !ok {
		t.Fatalf("%s is not a bool in %#v", key, parent)
	}
	return value
}

func mustInt(t *testing.T, parent map[string]any, key string) int {
	t.Helper()
	value, ok := parent[key].(json.Number)
	if !ok {
		t.Fatalf("%s is not a number in %#v", key, parent)
	}
	integer, err := strconv.Atoi(value.String())
	if err != nil {
		t.Fatal(err)
	}
	return integer
}

func jsonContainsString(value any, target string) bool {
	switch current := value.(type) {
	case string:
		return current == target
	case []any:
		for _, child := range current {
			if jsonContainsString(child, target) {
				return true
			}
		}
	case map[string]any:
		for _, child := range current {
			if jsonContainsString(child, target) {
				return true
			}
		}
	}
	return false
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

type rpcClient struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	nextID  int
}

func assertMCP(t *testing.T, binary, root string) {
	t.Helper()
	client := startMCP(t, binary, root)
	defer client.close()

	initialized := client.call(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name": "aoci-fresh-release-blackbox", "version": "1",
		},
	})
	serverInfo := mustObject(t, initialized, "serverInfo")
	if mustString(t, serverInfo, "name") != "aoci-code" || mustString(t, serverInfo, "version") == "" {
		t.Fatalf("unexpected MCP runtime identity: %#v", serverInfo)
	}
	client.notify(t, "notifications/initialized", map[string]any{})

	listed := client.call(t, "tools/list", map[string]any{})
	tools := mustArray(t, listed, "tools")
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("MCP tool is not an object: %#v", raw)
		}
		names = append(names, mustString(t, tool, "name"))
	}
	sort.Strings(names)
	want := []string{"aoci_get_entries", "aoci_header", "aoci_maintain", "aoci_overview", "aoci_remove_entry", "aoci_report", "aoci_rules", "aoci_search", "aoci_update_entry"}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("MCP tools = %v, want exact nine-tool surface %v", names, want)
	}

	rules := client.toolText(t, "aoci_rules", map[string]any{})
	if !strings.Contains(rules, "AOCI") {
		t.Fatalf("aoci_rules returned no runtime contract: %s", rules)
	}
	overview := client.toolText(t, "aoci_overview", map[string]any{})
	for _, anchor := range []string{"completed: true", "governance_aligned: true", "#AOCI-ROOT-MANIFEST: 1"} {
		if !strings.Contains(overview, anchor) {
			t.Fatalf("aoci_overview is missing %q:\n%s", anchor, overview)
		}
	}
}

func assertPreBootstrapMCPRoute(t *testing.T, binary, root string) {
	t.Helper()
	client := startMCP(t, binary, root)
	defer client.close()
	client.call(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name": "aoci-fresh-route-blackbox", "version": "1",
		},
	})
	client.notify(t, "notifications/initialized", map[string]any{})
	rules := client.toolText(t, "aoci_rules", map[string]any{})
	const marker = "AOCI_ONBOARDING_ROUTE_JSON:\n"
	position := strings.Index(rules, marker)
	if position < 0 {
		t.Fatalf("pre-Bootstrap aoci_rules did not return the active Fresh route:\n%s", rules)
	}
	var route map[string]any
	if err := decodeOneJSON([]byte(strings.TrimSpace(rules[position+len(marker):])), &route); err != nil {
		t.Fatalf("decode pre-Bootstrap MCP route: %v", err)
	}
	if mustString(t, route, "status") != "onboarding_in_progress" || mustBool(t, route, "formal_index_available") || mustBool(t, route, "formal_writes_started") {
		t.Fatalf("pre-Bootstrap route is unsafe or incomplete: %#v", route)
	}
	contract := requireActionContract(t, route)
	if mustString(t, contract, "action") != "authoring_next" || mustString(t, contract, "success_next_action") != "submit_authoring_completion" {
		t.Fatalf("pre-Bootstrap rules route does not continue the active Session: %#v", contract)
	}
}

func startMCP(t *testing.T, binary, root string) *rpcClient {
	t.Helper()
	command := exec.Command(binary, "--repo", root, "mcp")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	client := &rpcClient{command: command, stdin: stdin, nextID: 1}
	client.scanner = bufio.NewScanner(stdout)
	client.scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	command.Stderr = &client.stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return client
}

func (client *rpcClient) call(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	id := client.nextID
	client.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	for client.scanner.Scan() {
		line := append([]byte{}, client.scanner.Bytes()...)
		var envelope map[string]any
		if err := decodeOneJSON(line, &envelope); err != nil {
			t.Fatalf("MCP stdout is not JSON-RPC: %v\n%s", err, line)
		}
		responseID, ok := envelope["id"].(json.Number)
		if !ok || responseID.String() != strconv.Itoa(id) {
			continue
		}
		if failure, exists := envelope["error"]; exists {
			t.Fatalf("MCP %s failed: %#v", method, failure)
		}
		return mustObject(t, envelope, "result")
	}
	if err := client.scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("MCP closed during %s; stderr=%s", method, client.stderr.String())
	return nil
}

func (client *rpcClient) notify(t *testing.T, method string, params any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (client *rpcClient) toolText(t *testing.T, name string, arguments map[string]any) string {
	t.Helper()
	result := client.call(t, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("MCP tool %s returned error: %#v", name, result)
	}
	content := mustArray(t, result, "content")
	if len(content) == 0 {
		t.Fatalf("MCP tool %s returned no content", name)
	}
	first, ok := content[0].(map[string]any)
	if !ok || mustString(t, first, "type") != "text" {
		t.Fatalf("MCP tool %s returned unexpected content: %#v", name, content)
	}
	return mustString(t, first, "text")
}

func (client *rpcClient) close() {
	if client.stdin != nil {
		_ = client.stdin.Close()
	}
	if client.command == nil || client.command.Process == nil {
		return
	}
	done := make(chan error, 1)
	go func() { done <- client.command.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = client.command.Process.Kill()
		<-done
	}
}
