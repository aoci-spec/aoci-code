package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/aoci-spec/aoci-code/textassets"
)

// These tests cover the credential path that had no coverage at all, because
// requireHumanPhrase consulted os.Stdin directly and no test could get past it.

// grantTTY makes the confirmed branch reachable. It is package-private on
// purpose: no flag and no environment variable can reach it, so a model still
// cannot claim a TTY it does not have.
func grantTTY(t *testing.T) {
	t.Helper()
	previous := stdinIsCharDevice
	stdinIsCharDevice = func() bool { return true }
	t.Cleanup(func() { stdinIsCharDevice = previous })
}

func capturePrompt(t *testing.T) *bytes.Buffer {
	t.Helper()
	recorder := &bytes.Buffer{}
	previous := humanPromptWriter
	humanPromptWriter = recorder
	t.Cleanup(func() { humanPromptWriter = previous })
	return recorder
}

// runScopeSplit is runScopeCLI with the two streams kept apart, because "the
// credential must not reach stdout" is exactly what a merged buffer cannot say.
func runScopeSplit(t *testing.T, root string, stdin io.Reader, quiet bool, args ...string) (stdout, stderr *bytes.Buffer, err error) {
	t.Helper()
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, quiet
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	command := newScopeCmd()
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	if stdin != nil {
		command.SetIn(stdin)
	}
	command.SetArgs(args)
	return stdout, stderr, command.Execute()
}

// reviewPreviewFile produces a preview whose plan demands a human, which is the
// only shape that reaches the confirmation prompt at all.
func reviewPreviewFile(t *testing.T, root string) (path string, preview *scopechange.Preview) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeReview); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package sample\n// reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := scopechange.CandidateSet{Version: machinecontract.ManagedScopeCandidateSetV1,
		Entries: []scopechange.EntryCandidate{}, Dispositions: []scopechange.EntryDisposition{},
		ObserveReview: &scopechange.ObserveReview{Paths: []string{"main_test.go"},
			ReviewStatus: scopechange.ReviewStatusReviewed, Reviewer: "model-reviewer"}}
	preview, err = scopechange.Build(root, "2026-08-16T03:30:00Z", candidates)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Plan.InteractionRequired {
		t.Fatal("fixture preview does not demand a human; the prompt path is unreachable")
	}
	encoded, err := scopechange.Encode(preview)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "preview.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, preview
}

func TestApprovalWrittenToFileIsApplyUsableAndNeverReachesStdout(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, preview := reviewPreviewFile(t, root)
	grantTTY(t)
	capturePrompt(t)
	outPath := filepath.Join(t.TempDir(), "approval.json")

	stdout, _, err := runScopeSplit(t, root, strings.NewReader(preview.Plan.ConfirmationPhrase+"\n"), true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err != nil {
		t.Fatalf("approve with --out-file failed: %v", err)
	}
	// The credential is a bearer capability; writing it to a 0600 file while also
	// spraying it into scrollback would make that mode decorative.
	if stdout.Len() != 0 {
		t.Fatalf("credential leaked to stdout: %q", stdout.String())
	}
	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("no artifact at --out-file: %v", err)
	}
	approval, err := scopechange.DecodeApproval(written)
	if err != nil {
		t.Fatalf("artifact is not a decodable approval: %v", err)
	}
	if approval.Actor != "tester" || approval.Mechanism != machinecontract.ApprovalMechanismInteractiveDigestConfirmation {
		t.Fatalf("artifact does not record the interactive human approval: %+v", approval)
	}

	// The point of the flag is that Apply can consume the file directly.
	resultBytes, _, err := runScopeSplit(t, root, nil, true,
		"apply", "--preview-file", previewPath, "--approval-file", outPath)
	if err != nil {
		t.Fatalf("apply refused the written artifact: %v: %s", err, resultBytes)
	}
	var result scopechange.Result
	if err := json.Unmarshal(resultBytes.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	// Binding is proven through the real pipeline rather than by re-deriving a
	// digest in the test: Apply accepted this exact artifact for this exact
	// envelope and echoed both identities back.
	if result.Status != "applied" || result.ApprovalDigest != approval.ApprovalDigest ||
		result.EnvelopeDigest != approval.EnvelopeDigest {
		t.Fatalf("apply did not consume the written approval: %+v", result)
	}
}

func TestApprovalOutFileNeverOverwritesExistingBytes(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, preview := reviewPreviewFile(t, root)
	grantTTY(t)
	capturePrompt(t)
	outPath := filepath.Join(t.TempDir(), "occupied.json")
	original := []byte("do not destroy me\n")
	if err := os.WriteFile(outPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := runScopeSplit(t, root, strings.NewReader(preview.Plan.ConfirmationPhrase+"\n"), true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err == nil {
		t.Fatal("approve silently accepted an occupied --out-file")
	}
	if !strings.Contains(err.Error(), "planner_output_target_exists") {
		t.Fatalf("wrong refusal reason: %v", err)
	}
	after, readErr := os.ReadFile(outPath)
	if readErr != nil || !bytes.Equal(after, original) {
		t.Fatalf("existing bytes were not preserved: %q err=%v", after, readErr)
	}
}

func TestApprovalArtifactIsOperatorReadableOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	root := buildScopeCLIRepo(t)
	previewPath, preview := reviewPreviewFile(t, root)
	grantTTY(t)
	capturePrompt(t)
	outPath := filepath.Join(t.TempDir(), "approval.json")

	if _, _, err := runScopeSplit(t, root, strings.NewReader(preview.Plan.ConfirmationPhrase+"\n"), true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath); err != nil {
		t.Fatalf("approve with --out-file failed: %v", err)
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatal(err)
	}
	// The literal is written out rather than compared against
	// approvalArtifactMode, because comparing the constant to itself would pass
	// no matter what the constant became.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("approval artifact mode is %o, want 600", perm)
	}
}

func TestUnusableOutFileIsRefusedBeforeTheHumanIsAsked(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, _ := reviewPreviewFile(t, root)
	prompt := capturePrompt(t)
	grantTTY(t)
	outPath := filepath.Join(t.TempDir(), "absent-parent", "approval.json")

	_, _, err := runScopeSplit(t, root, strings.NewReader("whatever\n"), true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err == nil || !strings.Contains(err.Error(), "planner_output_unavailable") {
		t.Fatalf("an unusable --out-file must be refused up front, got: %v", err)
	}
	// A confirmation cannot be reused, so it must never be spent on a path that
	// was already known to be unwritable.
	if prompt.Len() != 0 {
		t.Fatalf("the human was asked to confirm before the path was checked: %q", prompt.String())
	}
	if _, statErr := os.Stat(outPath); statErr == nil {
		t.Fatal("a refused approval left a file behind")
	}
}

func TestRefusedConfirmationLeavesNoPartialArtifact(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, _ := reviewPreviewFile(t, root)
	grantTTY(t)
	capturePrompt(t)
	outPath := filepath.Join(t.TempDir(), "approval.json")

	_, _, err := runScopeSplit(t, root, strings.NewReader("WRONG PHRASE\n"), true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err == nil {
		t.Fatal("a wrong phrase was accepted")
	}
	// An empty or partial approval file must never exist: a later reader cannot
	// tell it apart from one a human actually authorized.
	if _, statErr := os.Stat(outPath); statErr == nil {
		t.Fatal("a refused confirmation still produced an artifact")
	}
}

// TestOutFileUsageIsLocalizedOnEveryApproveCommand closes a gap the tree-wide
// locale test leaves open: it spot-checks command Short strings under zh-CN but
// never inspects flag usages, so a flag missing from localizedFlagNames renders
// English with nothing to catch it.
//
// Scope note: several pre-existing flags are already missing from that list.
// This locks the three added here rather than silently widening the batch.
func TestOutFileUsageIsLocalizedOnEveryApproveCommand(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	root := newRootCmd()
	t.Cleanup(func() { root.RemoveCommand(subCommands...) })

	for _, path := range [][]string{
		{"scope", "approve"},
		{"scope", "safety", "approve"},
		{"baseline", "scope", "approve"},
	} {
		command := findCLICommand(root, path...)
		if command == nil {
			t.Fatalf("%v not found", path)
		}
		usages := command.LocalNonPersistentFlags().FlagUsages()
		if !strings.Contains(usages, "out-file") {
			t.Fatalf("%v does not offer --out-file: %q", path, usages)
		}
		for _, line := range strings.Split(usages, "\n") {
			if !strings.Contains(line, "out-file") {
				continue
			}
			// The sentinel appears when two message values collide byte-for-byte;
			// it makes --help exit 10 with no output at all.
			if strings.Contains(line, "text asset flag localization failed") {
				t.Fatalf("%v --out-file usage collided with another message: %q", path, line)
			}
			if !hanTextPattern.MatchString(line) {
				t.Fatalf("%v --out-file usage was not localized: %q", path, line)
			}
		}
	}
}

// promptWatcher records what the operator could actually see at the instant the
// command started waiting for them to type.
type promptWatcher struct {
	recorder *bytes.Buffer
	visible  string
	read     bool
	payload  *strings.Reader
}

func (w *promptWatcher) Read(p []byte) (int, error) {
	if !w.read {
		w.read = true
		w.visible = w.recorder.String()
	}
	return w.payload.Read(p)
}

func TestConfirmationPromptIsVisibleBeforeThePhraseIsRead(t *testing.T) {
	root := buildScopeCLIRepo(t)
	previewPath, preview := reviewPreviewFile(t, root)
	grantTTY(t)
	recorder := capturePrompt(t)
	watcher := &promptWatcher{recorder: recorder,
		payload: strings.NewReader(preview.Plan.ConfirmationPhrase + "\n")}
	outPath := filepath.Join(t.TempDir(), "approval.json")

	stdout, stderr, err := runScopeSplit(t, root, watcher, true,
		"approve", "--preview-file", previewPath, "--actor", "tester", "--out-file", outPath)
	if err != nil {
		t.Fatalf("approve failed: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if !watcher.read {
		t.Fatal("the phrase was never read; this test proves nothing")
	}
	// The prompt carries the exact phrase the operator has to type. Routing it
	// through the buffered cobra writer held it until after the command exited,
	// so the operator typed blind or gave up. Any regression to that lands here.
	if !strings.Contains(watcher.visible, preview.Plan.ConfirmationPhrase) {
		t.Fatalf("the confirmation phrase was not on screen when input was requested: %q", watcher.visible)
	}
	if stderr.Len() != 0 {
		t.Fatalf("the prompt went through the buffered writer after all: %q", stderr.String())
	}
}
