// 公开单条CLI必须复用受源码绑定的原子批次与Baseline完整性终态。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func runBoundUpdateEntry(
	t *testing.T,
	root,
	sourceSHA256 string,
) error {
	t.Helper()
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, true
	t.Cleanup(func() { flagRepo, flagQuiet = oldRepo, oldQuiet })
	command := newUpdateEntryCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	arguments := []string{
		"--path", "a.go",
		"--entry", "a.go[XUT5T]: F:Exercises public single-entry recovery | R:- | A:- | S:Preserves source binding",
	}
	if sourceSHA256 != "" {
		arguments = append(arguments, "--source-sha256", sourceSHA256)
	}
	command.SetArgs(arguments)
	return command.Execute()
}

func TestUpdateEntryCLIProductionOutputFollowsProjectLocale(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	if err := os.WriteFile(filepath.Join(root, "c.go"), []byte("package demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Locale = textassets.DefaultLocale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, false
	t.Cleanup(func() {
		flagRepo, flagQuiet = oldRepo, oldQuiet
		_ = textassets.SetActiveLocale(previousLocale)
	})

	entry := "c.go[XUT5T]: F:Provides an English CLI output fixture | R:- | A:- | S:Preserves source binding"
	command := newUpdateEntryCmd()
	var preview bytes.Buffer
	command.SetOut(&preview)
	command.SetArgs([]string{"--path", "c.go", "--entry", entry, "--preview"})
	if err := command.Execute(); err != nil {
		t.Fatalf("en-US preview failed: %v", err)
	}
	assertNoHanCLIOutput(t, "en-US preview", preview.String())
	if !utf8.Valid(preview.Bytes()) {
		t.Fatal("en-US preview is not valid UTF-8")
	}

	fingerprint, err := baseline.HashFile(filepath.Join(root, "c.go"))
	if err != nil {
		t.Fatal(err)
	}
	command = newUpdateEntryCmd()
	var applied bytes.Buffer
	command.SetOut(&applied)
	command.SetArgs([]string{"--path", "c.go", "--entry", entry, "--source-sha256", fingerprint.SHA256})
	if err := command.Execute(); err != nil {
		t.Fatalf("en-US apply failed: %v", err)
	}
	assertNoHanCLIOutput(t, "en-US apply", applied.String())

	var jsonOutput, jsonError bytes.Buffer
	code := executeCLI(
		[]string{"--repo", root, "--json", "update-entry", "--entry", entry},
		&jsonOutput,
		&jsonError,
	)
	if code != ExitConfig || !strings.Contains(jsonOutput.String(), `"error_code"`) {
		t.Fatalf("en-US JSON rejection mismatch: code=%d stdout=%s stderr=%s", code, jsonOutput.String(), jsonError.String())
	}
	assertNoHanCLIOutput(t, "en-US JSON", jsonOutput.String()+jsonError.String())
}

func TestUpdateEntryCLIDefaultFinalizesCodeTargetWithoutMCP(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	formalPath := filepath.Join(root, "aoci.code.txt")
	formalBefore, err := os.ReadFile(formalPath)
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Replace(
		string(formalBefore),
		"run the deterministic CLI fixture",
		"run the finalized CLI target fixture",
		1,
	)
	if target == string(formalBefore) {
		t.Fatal("target fixture did not change the formal Entry")
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.code.target.txt"), []byte(target), 0o644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.go")
	mainBefore, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, append(mainBefore, []byte("// finalized\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() {
		flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet
	})
	partial := newUpdateEntryCmd()
	partial.SilenceUsage = true
	partial.SilenceErrors = true
	partial.SetArgs([]string{"--entry", ""})
	var exitErr *ExitError
	if err := partial.Execute(); !errors.As(err, &exitErr) || exitErr.Code != ExitConfig {
		t.Fatalf("explicit empty single-Entry input must not enter target mode: %v", err)
	}
	if currentFormal, err := os.ReadFile(formalPath); err != nil || !bytes.Equal(currentFormal, formalBefore) {
		t.Fatal("partial single-Entry input changed formal Code")
	}
	command := newUpdateEntryCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("default target finalization failed: %v\n%s", err, output.String())
	}
	var report struct {
		Status  string `json:"status"`
		Aligned bool   `json:"aligned"`
		Metrics struct {
			AOCIToolCalls  int `json:"aoci_tool_calls"`
			ShellAOCICalls int `json:"shell_aoci_calls"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("default target output is not one JSON result: %v\n%s", err, output.String())
	}
	if report.Status != "applied" || !report.Aligned || report.Metrics.AOCIToolCalls != 0 || report.Metrics.ShellAOCICalls != 1 {
		t.Fatalf("default target result mismatch: %+v", report)
	}
	formalAfter, err := os.ReadFile(formalPath)
	if err != nil {
		t.Fatal(err)
	}
	targetAfter, err := os.ReadFile(filepath.Join(root, "aoci.code.target.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(formalAfter) != target || !bytes.Equal(targetAfter, formalAfter) {
		t.Fatal("formal Code and consumed target were not synchronized")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	current, err := baseline.HashFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Files["main.go"].SHA256 != current.SHA256 {
		t.Fatal("default target finalization did not bind the final source hash")
	}
}

func TestUpdateEntryCLIChinesePreviewRemainsChinese(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	oldRepo, oldQuiet := flagRepo, flagQuiet
	flagRepo, flagQuiet = root, false
	t.Cleanup(func() {
		flagRepo, flagQuiet = oldRepo, oldQuiet
		_ = textassets.SetActiveLocale(previousLocale)
	})
	command := newUpdateEntryCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"--path", "a.go",
		"--entry", "a.go[XUT5T]: F:中文预览 | R:- | A:- | S:保留绑定",
		"--preview",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("zh-CN preview failed: %v", err)
	}
	for _, anchor := range []string{"预览", "正式索引", "零写入"} {
		if !strings.Contains(output.String(), anchor) {
			t.Fatalf("zh-CN preview missing %q: %s", anchor, output.String())
		}
	}
}

func TestUpdateEntryCLIPreviewReturnsSharedRepairFindingJSON(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() {
		flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet
	})

	command := newUpdateEntryCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{
		"--path", "main.go",
		"--entry", "main.go[CD7S]: F:run the deterministic CLI fixture | R:- | A:a,b,c,d,e,f,g | S:-",
		"--source-sha256", fingerprint.SHA256,
		"--preview",
	})
	err = command.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitInvalid || exitErr.Msg != "" {
		t.Fatalf("repair Preview must use the silent machine report: %v", err)
	}
	var report updateEntryRepairReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("repair Preview did not emit one JSON object: %v\n%s", err, output.String())
	}
	if report.Status != entriesAutoStatusRepairRequired || report.Attempted != 1 ||
		report.Applied != 0 || report.Remaining != 1 || report.FormalWritesStarted ||
		report.FindingCount != 1 || !report.PreserveOtherCandidates ||
		len(report.RetryScope) != 1 || report.RetryScope[0] != "code:main.go" {
		t.Fatalf("single Preview repair contract mismatch: %+v", report)
	}
	finding := report.Findings[0]
	if finding.CandidateIndex != 1 || finding.Path != "main.go" ||
		finding.CanonicalObjectIdentity != "code:main.go" || finding.Domain != "code" ||
		finding.Field != "A" || finding.RuleCode != "fras_a_too_many_items" ||
		finding.Expected != "max_items=6" || finding.Actual != "item_count=7" ||
		finding.Cause == "" || finding.SafeRepairAction == "" {
		t.Fatalf("single Preview Finding mismatch: %+v", finding)
	}
}

func assertNoHanCLIOutput(t *testing.T, label, value string) {
	t.Helper()
	for _, character := range value {
		if unicode.Is(unicode.Han, character) {
			t.Fatalf("%s contains Han character %q: %s", label, character, value)
		}
	}
}

func TestUpdateEntryCLIRequiresBindingAndReportsBaselineFailure(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	indexBefore := readManualAtomicIndex(t, root)
	missingErr := runBoundUpdateEntry(t, root, "")
	var exitErr *ExitError
	if !errors.As(missingErr, &exitErr) || exitErr.Code != ExitConfig ||
		!strings.Contains(missingErr.Error(), "source-sha256") {
		t.Fatalf("non-preview public single-entry update must require source binding: %v", missingErr)
	}
	if readManualAtomicIndex(t, root) != indexBefore {
		t.Fatal("missing source binding must not write the formal index")
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	unblockBaseline := blockBaselineBackupReplacement(t, root)
	applyErr := runBoundUpdateEntry(t, root, fingerprint.SHA256)
	if !errors.As(applyErr, &exitErr) || exitErr.Code != ExitInternal ||
		!strings.Contains(applyErr.Error(), "Baseline") {
		t.Fatalf("public single-entry update must not report Baseline failure as success: %v", applyErr)
	}
	if !strings.Contains(readManualAtomicIndex(t, root), "Exercises public single-entry recovery") {
		t.Fatal("the index write before Baseline failure must remain visible")
	}
	unblockBaseline()
	if err := runBoundUpdateEntry(t, root, fingerprint.SHA256); err != nil {
		t.Fatalf("replaying the same binding should recover only Baseline: %v", err)
	}
}
