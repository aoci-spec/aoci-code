package mcptools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func decodeToolJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%.400s", err, text)
	}
	return decoded
}

func rootInvite(t *testing.T, session *mcp.ClientSession) map[string]any {
	t.Helper()
	maintained := decodeToolJSON(t, callVolumeTool(t, session, "aoci_maintain", map[string]any{}))
	if maintained["aligned"] != true {
		t.Fatalf("the fixture must be aligned: %v", maintained["status"])
	}
	invite, _ := maintained["root_overview"].(map[string]any)
	return invite
}

func submitRootOverview(t *testing.T, session *mcp.ClientSession, project, invariants, sha string) map[string]any {
	t.Helper()
	return decodeToolJSON(t, callVolumeTool(t, session, "aoci_update_entry", map[string]any{"root_overview": map[string]any{
		"project": project, "global_invariants": invariants, "root_sha256": sha}}))
}

func rootReceiptPending(root string) bool {
	_, err := os.Stat(rootOverviewReceiptPath(root))
	return err == nil
}

func assertRootAligned(t *testing.T, root string) {
	t.Helper()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil || !facts.GovernanceAligned {
		t.Fatalf("the repository must stay aligned: %v %+v", err, facts.Findings)
	}
	state, _, err := baseline.Load(root)
	if err != nil || state.Files["aoci.txt"].SHA256 != loaded.set.Root.SHA256 {
		t.Fatalf("the Baseline must record the Root as written: %v", err)
	}
}

// An aligned index whose Root carries no overview invites the model to author
// it; the submission writes the two lines and the Baseline record together,
// keeps the repository aligned, and ends the invitation.
func TestRootOverviewIsInvitedOnceAlignedAndWritten(t *testing.T) {
	root, session := deltaVolumesFixture(t, 4)
	invite := rootInvite(t, session)
	if invite == nil || invite["global_invariants"] != "-" || len(invite["instructions"].([]any)) != 1 ||
		int(invite["global_invariants_max_chars"].(float64)) != machinecontract.RootOverviewInvariantsMaxChars {
		t.Fatalf("an aligned index with an unauthored Root must invite the overview: %v", invite)
	}
	before, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sha := invite["root_sha256"].(string)
	written := submitRootOverview(t, session, "A fixture system that returns constants", "Every unit stays independent", sha)
	result, _ := written["root_overview_result"].(map[string]any)
	proof, _ := written["terminal_proof"].(map[string]any)
	if written["status"] != autoStatusApplied || written["aligned"] != true || result == nil || result["baseline_bound"] != true ||
		proof == nil || proof["aligned"] != true || rootReceiptPending(root) {
		t.Fatalf("the overview must be written and leave the repository aligned: %v", written)
	}
	after, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "#Project: A fixture system that returns constants\n") ||
		!strings.Contains(string(after), "#Global-Invariants: Every unit stays independent\n") {
		t.Fatalf("the two lines must be written:\n%s", after)
	}
	kept := string(after)
	for _, line := range strings.Split(string(before), "\n") {
		if strings.HasPrefix(line, rootProjectPrefix) || strings.HasPrefix(line, rootInvariantsPrefix) {
			continue
		}
		index := strings.Index(kept, line+"\n")
		if line != "" && index < 0 {
			t.Fatalf("every other line must stay, in order; lost %q:\n%s", line, after)
		}
		if index >= 0 {
			kept = kept[index+len(line):]
		}
	}
	assertRootAligned(t, root)
	if again := rootInvite(t, session); again != nil {
		t.Fatalf("an authored overview ends the invitation: %v", again)
	}
	// The same submission again writes nothing, even with the stale digest.
	repeat := submitRootOverview(t, session, "A fixture system that returns constants", "Every unit stays independent", sha)
	if result, _ := repeat["root_overview_result"].(map[string]any); repeat["status"] != autoStatusApplied || result["already_applied"] != true || repeat["applied"].(float64) != 0 {
		t.Fatalf("a repeated submission must be answered as already applied: %v", repeat)
	}
	// A different overview over a Root the caller has not seen is refused.
	if stale := submitRootOverview(t, session, "Another description", "Other", sha); stale["status"] != autoStatusRepairRequired ||
		!strings.Contains(stale["next_action"].(string), "root_overview_root_changed") {
		t.Fatalf("a stale root_sha256 must be a repair finding: %v", stale)
	}
	overview := callVolumeTool(t, session, "aoci_overview", map[string]any{})
	if !strings.Contains(overview, "#Global-Invariants: Every unit stays independent") {
		t.Fatal("the Overview must deliver the authored overview")
	}
}

// Values that are not one authored line of the allowed length are refused
// before anything is written, as is an overview beside other work or over an
// index that is not aligned.
func TestRootOverviewRefusals(t *testing.T) {
	root, session := deltaVolumesFixture(t, 4)
	sha := rootInvite(t, session)["root_sha256"].(string)
	before, _ := os.ReadFile(filepath.Join(root, "aoci.txt"))
	for name, values := range map[string][2]string{
		"root_overview_project_not_one_line":           {"two\nlines", "fine"},
		"root_overview_global_invariants_unauthored":   {"fine", "-"},
		"root_overview_project_unauthored":             {"  ", "fine"},
		"root_overview_global_invariants_too_long":     {"fine", strings.Repeat("x", machinecontract.RootOverviewInvariantsMaxChars+1)},
		"root_overview_global_invariants_not_one_line": {"fine", "a\rb"},
	} {
		refused := submitRootOverview(t, session, values[0], values[1], sha)
		if refused["status"] != autoStatusRepairRequired || !strings.Contains(refused["next_action"].(string), name) || refused["formal_writes_started"] != false {
			t.Fatalf("%s: %v", name, refused)
		}
	}
	mixed := callVolumeTool(t, session, "aoci_update_entry", map[string]any{
		"root_overview": map[string]any{"project": "p", "global_invariants": "g", "root_sha256": sha},
		"removals":      []any{map[string]any{"path": "pkg/mod000/file000.go"}}})
	if !strings.Contains(mixed, "root_overview_stands_alone") {
		t.Fatalf("an overview beside other work must be refused: %s", mixed)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "mod000", "file000.go"), []byte("package mod000\n\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty := submitRootOverview(t, session, "p", "g", sha); dirty["status"] != autoStatusStopped ||
		!strings.Contains(dirty["next_action"].(string), "root_overview_requires_aligned_index") {
		t.Fatalf("an overview over an index that is not aligned must be refused: %v", dirty)
	}
	if maintained := decodeToolJSON(t, callVolumeTool(t, session, "aoci_maintain", map[string]any{})); maintained["root_overview"] != nil {
		t.Fatal("no invitation while authoring work is pending")
	}
	if after, _ := os.ReadFile(filepath.Join(root, "aoci.txt")); string(after) != string(before) || rootReceiptPending(root) {
		t.Fatal("a refused overview must write nothing")
	}
}

// A write cut between the Root and its Baseline record leaves the receipt;
// the next Maintain rolls it forward. Bytes the receipt does not know stop
// the roll-forward and keep the receipt.
func TestRootOverviewReceiptRollsForward(t *testing.T) {
	root, session := deltaVolumesFixture(t, 4)
	sha := rootInvite(t, session)["root_sha256"].(string)
	original := rootOverviewWriteCAS
	t.Cleanup(func() { rootOverviewWriteCAS = original })
	writes := 0
	rootOverviewWriteCAS = func(path string, data []byte, expected string) error {
		writes++
		if writes == 2 {
			return errors.New("cut")
		}
		return original(path, data, expected)
	}
	cut := submitRootOverview(t, session, "A fixture system", "Units stay independent", sha)
	if cut["status"] != autoStatusStopped || cut["formal_writes_started"] != true || !rootReceiptPending(root) {
		t.Fatalf("the cut write must stop with its receipt in place: %v", cut)
	}
	rootOverviewWriteCAS = original
	if maintained := decodeToolJSON(t, callVolumeTool(t, session, "aoci_maintain", map[string]any{})); maintained["aligned"] != true ||
		maintained["root_overview"] != nil || rootReceiptPending(root) {
		t.Fatalf("the next Maintain must finish the write: %v", maintained["status"])
	}
	assertRootAligned(t, root)

	// A receipt beside a Root that holds neither of its images stays.
	root, session = deltaVolumesFixture(t, 4)
	sha = rootInvite(t, session)["root_sha256"].(string)
	writes = 0
	rootOverviewWriteCAS = func(path string, data []byte, expected string) error {
		writes++
		if writes == 2 {
			return errors.New("cut")
		}
		return original(path, data, expected)
	}
	submitRootOverview(t, session, "A fixture system", "Units stay independent", sha)
	rootOverviewWriteCAS = original
	rootPath := filepath.Join(root, "aoci.txt")
	raw, _ := os.ReadFile(rootPath)
	if err := os.WriteFile(rootPath, []byte(strings.Replace(string(raw), "Units stay independent", "edited by hand", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "aoci_maintain", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if text := resText(t, result); !result.IsError || !strings.Contains(text, "root_overview_root_third_party_conflict") || !rootReceiptPending(root) {
		t.Fatalf("bytes the receipt does not know must stop the roll-forward: %s", text)
	}
}

func TestRewriteRootOverviewKeepsEveryOtherByte(t *testing.T) {
	root := "#AOCI-ROOT-MANIFEST: 1\r\n#Format-Version: cognition-volumes/v1\r\n#Locale: en-US\r\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\r\n"
	got := string(rewriteRootOverview([]byte(root), "P", "G"))
	want := "#AOCI-ROOT-MANIFEST: 1\r\n#Format-Version: cognition-volumes/v1\r\n#Locale: en-US\r\n#Project: P\r\n#Global-Invariants: G\r\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\r\n"
	if got != want {
		t.Fatalf("missing lines must be inserted after the Locale, CRLF kept:\n%q", got)
	}
	if project, invariants := rootOverviewFields([]byte(got)); project != "P" || invariants != "G" {
		t.Fatalf("fields read back wrong: %q %q", project, invariants)
	}
	if again := string(rewriteRootOverview([]byte(got), "P", "G")); again != got {
		t.Fatal("rewriting the same values must change nothing")
	}
}

// The final Apply is where a first build ends, with the whole system just
// read: its result carries the invitation, since no Maintain follows it.
func TestFinalApplyCarriesTheRootOverviewInvitation(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 3)
	session := connectMCPClient(t, root)
	var applied map[string]any
	for round := 0; round < 5; round++ {
		response := maintainJSON(t, session, map[string]any{})
		if response["status"] != autoStatusRepairRequired {
			break
		}
		applied = applyMaintainBatch(t, session, response)
	}
	invite, _ := applied["root_overview"].(map[string]any)
	if applied["aligned"] != true || invite == nil || invite["root_sha256"] == "" {
		t.Fatalf("the aligning Apply must invite the overview: %v", applied)
	}
	written := submitRootOverview(t, session, "A fixture system", "none", invite["root_sha256"].(string))
	if written["status"] != autoStatusApplied || written["aligned"] != true {
		t.Fatalf("the invitation's digest must be current: %v", written)
	}
	assertRootAligned(t, root)
}

// The invitation is for a finished index only: none while governance is not
// aligned, and none for an index that holds no Entry yet.
func TestRootOverviewInvitationNeedsAFinishedIndex(t *testing.T) {
	root, _ := deltaVolumesFixture(t, 2)
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	facts, err := volumegovernance.Assess(root, loaded.cfg, loaded.set)
	if err != nil || rootOverviewInvitation(loaded, facts) == nil {
		t.Fatalf("the aligned fixture must be invited: %v", err)
	}
	unaligned := *facts
	unaligned.GovernanceAligned = false
	empty := *facts
	empty.CodeEntryCount = 0
	if rootOverviewInvitation(loaded, &unaligned) != nil || rootOverviewInvitation(loaded, &empty) != nil || rootOverviewInvitation(loaded, nil) != nil {
		t.Fatal("no invitation for an index that is not aligned or holds no Entry")
	}
}
