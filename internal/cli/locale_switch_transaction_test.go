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
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

// localeSwitchFixture is an aligned Volumes repository whose Baseline also
// records the Root when bindRoot is set, the shape every scanned repository
// has; without it the Root is unbound, which is legal and also exercised.
func localeSwitchFixture(t *testing.T, bindRoot bool) string {
	t.Helper()
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	root, _ := alignedVolumeCLIFixture(t, true, false)
	if bindRoot {
		state, exists, err := baseline.Load(root)
		if err != nil || !exists {
			t.Fatalf("fixture Baseline: %v %v", exists, err)
		}
		for _, rel := range []string{"aoci.txt", "aoci.meta.txt"} {
			fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			fingerprint.Role = "index"
			state.Files[rel] = fingerprint
		}
		if err := baseline.Save(root, state); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readFixtureFiles(t *testing.T, root string, rels ...string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		out[rel] = data
	}
	return out
}

var localeSwitchFormalFiles = []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/config.json", ".aoci/baseline.json"}

func runLocaleSet(t *testing.T, root, target string, jsonMode bool) (int, string, string) {
	t.Helper()
	args := []string{"--repo", root}
	if jsonMode {
		args = append(args, "--json")
	}
	args = append(args, "config", "set", "locale", target)
	var stdout, stderr bytes.Buffer
	code := executeCLI(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// verifyAligned reports whether the repository is aligned, allowing one
// authoring target: the switch re-renders the managed AGENTS block in the new
// Locale, and the fixture never indexed that file, so its Entry is the one
// genuinely new piece of work the switch creates. Any other finding fails.
func verifyAligned(t *testing.T, root string) bool {
	t.Helper()
	var stdout, stderr bytes.Buffer
	executeCLI([]string{"--repo", root, "--json", "verify"}, &stdout, &stderr)
	var result struct {
		GovernanceAligned bool `json:"governance_aligned"`
		Governance        struct {
			Findings []struct {
				Code   string `json:"code"`
				Target string `json:"target"`
			} `json:"findings"`
		} `json:"governance"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("verify output: %v\n%s%s", err, stdout.String(), stderr.String())
	}
	if result.GovernanceAligned {
		return true
	}
	for _, finding := range result.Governance.Findings {
		if finding.Target != "AGENTS.md" || (finding.Code != "code_missing" && finding.Code != "code_unbaselined") {
			t.Logf("unexpected finding after the switch: %+v", finding)
			return false
		}
	}
	return len(result.Governance.Findings) > 0
}

func TestLocaleSwitchRewritesRootConfigAndBaselineTogether(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	if !verifyAligned(t, root) {
		t.Fatal("fixture must be aligned before the switch")
	}

	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, true)
	if code != ExitOK {
		t.Fatalf("switch failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	result := decodeConfigSetResult(t, []byte(stdout))
	assertConfigSetResultKeys(t, result, "ok", "key", "value", "restart_mcp_required")
	assertJSONValue(t, "value", result["value"], textassets.LegacyLocale)
	assertJSONValue(t, "restart_mcp_required", result["restart_mcp_required"], true)

	after := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	wantRoot, err := index.ReplaceLocaleMarker(before["aoci.txt"], textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after["aoci.txt"], wantRoot) {
		t.Fatalf("Root changed beyond its #Locale line:\n%s", after["aoci.txt"])
	}
	for _, rel := range []string{"aoci.meta.txt", "aoci.code.txt"} {
		if !bytes.Equal(after[rel], before[rel]) {
			t.Fatalf("%s was rewritten by a Locale switch", rel)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration != nil {
		t.Fatalf("configuration: %+v %v", cfg, err)
	}
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Files["aoci.txt"].SHA256 != cognitiontxn.SHA256(after["aoci.txt"]) || state.Files["aoci.txt"].Role != "index" {
		t.Fatalf("Baseline record of the Root was not moved with it: %+v", state.Files["aoci.txt"])
	}
	var previous baseline.Baseline
	if err := json.Unmarshal(before[".aoci/baseline.json"], &previous); err != nil {
		t.Fatal(err)
	}
	for rel, fingerprint := range previous.Files {
		if rel != "aoci.txt" && state.Files[rel] != fingerprint {
			t.Fatalf("Baseline record of %s changed in a Locale switch", rel)
		}
	}
	if !verifyAligned(t, root) {
		t.Fatal("the repository must stay aligned after the switch")
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil || len(pending) != 0 {
		t.Fatalf("receipt left pending: %v %v", pending, err)
	}
	history, err := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "history", "locale-*.json"))
	if err != nil || len(history) != 1 {
		t.Fatalf("one archived receipt expected: %v %v", history, err)
	}

	// Back again: the Root returns to its exact original bytes.
	code, stdout, stderr = runLocaleSet(t, root, textassets.DefaultLocale, false)
	if code != ExitOK {
		t.Fatalf("switch back failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Entries created or updated from now on are authored in en-US") {
		t.Fatalf("human output must say the Locale is prospective:\n%s", stdout)
	}
	restored := readFixtureFiles(t, root, "aoci.txt")
	if !bytes.Equal(restored["aoci.txt"], before["aoci.txt"]) {
		t.Fatalf("Root after the round trip differs from the original:\n%s", restored["aoci.txt"])
	}
	if !verifyAligned(t, root) {
		t.Fatal("aligned after the round trip")
	}
}

func TestLocaleSwitchLeavesAnUnboundBaselineUntouched(t *testing.T) {
	root := localeSwitchFixture(t, false)
	before := readFixtureFiles(t, root, ".aoci/baseline.json", "aoci.txt")
	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitOK {
		t.Fatalf("switch failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	after := readFixtureFiles(t, root, ".aoci/baseline.json", "aoci.txt")
	if !bytes.Equal(after[".aoci/baseline.json"], before[".aoci/baseline.json"]) {
		t.Fatal("a Baseline that does not record the Root must not be rewritten")
	}
	if bytes.Equal(after["aoci.txt"], before["aoci.txt"]) {
		t.Fatal("the Root marker did not change")
	}
	if !verifyAligned(t, root) {
		t.Fatal("aligned after the switch")
	}
}

func TestLocaleSwitchSameTargetWritesNothing(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	code, stdout, stderr := runLocaleSet(t, root, textassets.DefaultLocale, false)
	if code != ExitOK {
		t.Fatalf("same-target switch failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Restart every running AOCI MCP process") || strings.Contains(stdout, "from now on are authored") {
		t.Fatalf("same-target output must carry the restart notice only:\n%s", stdout)
	}
	after := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	for rel := range before {
		if !bytes.Equal(after[rel], before[rel]) {
			t.Fatalf("%s changed although the Locale was already %s", rel, textassets.DefaultLocale)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, ".aoci", "transactions")); len(entries) != 0 {
		t.Fatalf("a same-target switch minted a receipt: %v", entries)
	}
}

func TestLocaleSwitchRefusesStaleRootBaselineRecordWithoutWrites(t *testing.T) {
	root := localeSwitchFixture(t, true)
	state, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	record := state.Files["aoci.txt"]
	record.SHA256 = strings.Repeat("0", 64)
	state.Files["aoci.txt"] = record
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitConfig || !strings.Contains(stderr, "Baseline record of aoci.txt") {
		t.Fatalf("a stale Root record must stop the switch: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	after := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	for rel := range before {
		if !bytes.Equal(after[rel], before[rel]) {
			t.Fatalf("%s changed by a refused switch", rel)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, ".aoci", "transactions")); len(entries) != 0 {
		t.Fatalf("a refused switch minted a receipt: %v", entries)
	}
}

func TestLocaleSwitchResumesAfterCrashBetweenPublishes(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	localeSwitchFault = func(step string) error {
		if step == "after_publish_aoci.txt" {
			return errors.New("locale_switch_injected_crash_after_root_publish")
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = func(string) error { return nil } })

	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitConfig || !strings.Contains(stderr, "stopped before completion") {
		t.Fatalf("the crash must surface as a stopped switch: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	half := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	if bytes.Equal(half["aoci.txt"], before["aoci.txt"]) || !bytes.Equal(half[".aoci/config.json"], before[".aoci/config.json"]) ||
		!bytes.Equal(half[".aoci/baseline.json"], before[".aoci/baseline.json"]) {
		t.Fatal("expected the Root published and the configuration and Baseline still at their preimage")
	}
	pending, err := cognitiontxn.Pending(root)
	if err != nil || len(pending) != 1 || pending[0].Operation != localeSwitchOperation || !pending[0].LayoutTransaction() {
		t.Fatalf("one pending locale receipt expected: %v %v", pending, err)
	}

	// Every other command waits; Guide and the gate name the receipt.
	var gatedOut, gatedErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "verify"}, &gatedOut, &gatedErr); code != ExitInvalid || !strings.Contains(gatedErr.String(), "Locale switch is still pending") {
		t.Fatalf("verify must wait behind the pending switch: code=%d stdout=%s stderr=%s", code, gatedOut.String(), gatedErr.String())
	}
	var otherOut, otherErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "config", "set", "ledger_enabled", "true"}, &otherOut, &otherErr); code != ExitInvalid {
		t.Fatalf("another config key must wait behind the pending switch: code=%d stdout=%s stderr=%s", code, otherOut.String(), otherErr.String())
	}
	guide := volumeGuideJSON(t, root)
	if guide.Stage != "blocked" || !strings.Contains(strings.Join(guide.Instructions, "\n"), "aoci config set locale <target>") {
		t.Fatalf("the Guide must name the switch as the closure: stage=%s instructions=%v", guide.Stage, guide.Instructions)
	}

	// A different target is refused; the same target resumes to completion.
	localeSwitchFault = func(string) error { return nil }
	code, stdout, stderr = runLocaleSet(t, root, textassets.DefaultLocale, false)
	if code != ExitConfig || !strings.Contains(stderr, "still pending") {
		t.Fatalf("a different target must be refused while the receipt is pending: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if again := readFixtureFiles(t, root, localeSwitchFormalFiles...); !bytes.Equal(again[".aoci/config.json"], before[".aoci/config.json"]) {
		t.Fatal("the refused target wrote the configuration")
	}
	code, stdout, stderr = runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitOK {
		t.Fatalf("resume failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	after := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	wantRoot, _ := index.ReplaceLocaleMarker(before["aoci.txt"], textassets.LegacyLocale)
	if !bytes.Equal(after["aoci.txt"], wantRoot) {
		t.Fatal("Root is not at its postimage after the resume")
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("configuration after the resume: %+v %v", cfg, err)
	}
	state, _, err := baseline.Load(root)
	if err != nil || state.Files["aoci.txt"].SHA256 != cognitiontxn.SHA256(after["aoci.txt"]) {
		t.Fatalf("Baseline after the resume: %+v %v", state.Files["aoci.txt"], err)
	}
	if pending, err := cognitiontxn.Pending(root); err != nil || len(pending) != 0 {
		t.Fatalf("receipt still pending after the resume: %v %v", pending, err)
	}
	if !verifyAligned(t, root) {
		t.Fatal("aligned after the resumed switch")
	}
}

func TestLocaleSwitchRefusesGuardDriftAndResumesOnceRestored(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	metaPath := filepath.Join(root, "aoci.meta.txt")
	localeSwitchFault = func(step string) error {
		if step == "after_intent" {
			// Another writer touches a Volume the switch only guards, after the
			// receipt exists and before the first publish.
			return os.WriteFile(metaPath, append(append([]byte{}, before["aoci.meta.txt"]...), []byte("# drift\n")...), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = func(string) error { return nil } })
	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitConfig || !strings.Contains(stderr, "guard_drift") {
		t.Fatalf("drift of a guarded Volume must stop the publish: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	after := readFixtureFiles(t, root, "aoci.txt", ".aoci/config.json", ".aoci/baseline.json")
	for rel, data := range after {
		if !bytes.Equal(data, before[rel]) {
			t.Fatalf("%s was published although a guard drifted", rel)
		}
	}
	localeSwitchFault = func(string) error { return nil }
	if err := os.WriteFile(metaPath, before["aoci.meta.txt"], 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitOK {
		t.Fatalf("resume after restoring the guard failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if pending, err := cognitiontxn.Pending(root); err != nil || len(pending) != 0 {
		t.Fatalf("receipt still pending: %v %v", pending, err)
	}
	if !verifyAligned(t, root) {
		t.Fatal("aligned after the resumed switch")
	}
}

func TestLocaleSwitchPreservesCRLFRoot(t *testing.T) {
	root := localeSwitchFixture(t, false)
	rootPath := filepath.Join(root, "aoci.txt")
	original, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	crlf := bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n"))
	if err := os.WriteFile(rootPath, crlf, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitOK {
		t.Fatalf("switch failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	after, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := index.ReplaceLocaleMarker(crlf, textassets.LegacyLocale)
	if !bytes.Equal(after, want) || !bytes.Contains(after, []byte("#Locale: zh-CN\r\n")) {
		t.Fatalf("CRLF Root was not preserved:\n%q", after)
	}
}

func TestLocaleSwitchMaterializesTheTeamFileFromTheRootMarker(t *testing.T) {
	root := localeSwitchFixture(t, false)
	if err := os.Remove(filepath.Join(root, ".aoci", "config.json")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.DefaultLocale {
		t.Fatalf("an absent team file over an en-US Root must resolve to en-US, not the markerless zh-CN rule: %+v %v", cfg, err)
	}
	code, stdout, stderr := runLocaleSet(t, root, textassets.LegacyLocale, false)
	if code != ExitOK {
		t.Fatalf("switch failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	cfg, err = config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("configuration after the switch: %+v %v", cfg, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil || !bytes.Contains(data, []byte("#Locale: zh-CN")) {
		t.Fatalf("Root marker: %s %v", data, err)
	}
}

func TestInitRefusesToChangeTheLocaleOfAVolumesRoot(t *testing.T) {
	root := localeSwitchFixture(t, true)
	before := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "init", "--agent=", "--hooks=false", "--locale", textassets.LegacyLocale}, &stdout, &stderr)
	if code != ExitConfig || !strings.Contains(stderr.String(), "aoci config set locale zh-CN") {
		t.Fatalf("init must send the operator to config set locale: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	after := readFixtureFiles(t, root, localeSwitchFormalFiles...)
	for rel := range before {
		if !bytes.Equal(after[rel], before[rel]) {
			t.Fatalf("%s changed by a refused init", rel)
		}
	}
}
