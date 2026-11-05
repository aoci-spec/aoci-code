package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
)

func scopeActivationFixture(t *testing.T) string {
	t.Helper()
	return scopeActivationFixtureWith(t, nil)
}

// scopeActivationFixtureWith adds files to the fixture before its Baseline is
// established, so a *_test.go lands in the Observe role the production
// profile gives it.
func scopeActivationFixtureWith(t *testing.T, extra map[string]string) string {
	t.Helper()
	root, cfg := alignedVolumeCLIFixture(t, true, false)
	for rel, content := range extra {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.LedgerEnabled = true
	if err := cfg.SetNewProjectGovernance(machinecontract.ScopeProfileProduction); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, cfg.EffectiveManagedScope(), managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	state := baseline.NewBaseline(files)
	budgetID, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	state.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, BudgetPolicyIdentity: budgetID, BudgetPolicy: cfg.CognitionBudget,
		ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired, ApplyAuthorizationMode: config.AutomationModeAuto}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root
}

func activateJSON(t *testing.T, root string) (int, []byte) {
	t.Helper()
	var out, errs bytes.Buffer
	code := executeCLI([]string{"--repo", root, "--json", "scope", "activate"}, &out, &errs)
	if !json.Valid(out.Bytes()) {
		t.Fatalf("expected one JSON object: code=%d stdout=%s stderr=%s", code, &out, &errs)
	}
	return code, out.Bytes()
}

func activationFiles(t *testing.T, root string, paths ...string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		result[path] = data
	}
	return result
}

func assertActivationFiles(t *testing.T, root string, before map[string][]byte) {
	t.Helper()
	for path, want := range before {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("activation changed %s: %v", path, err)
		}
	}
}

func TestScopeActivateAppliesPolicyAndRetainsVolumeSourceDrift(t *testing.T) {
	root := scopeActivationFixture(t)
	if _, err := runScopeCLI(t, root, "rule", "add", "future", "--action", "exclude", "--pattern", "future.txt", "--pattern-kind", "file", "--reason", "future artifact"); err != nil {
		t.Fatal(err)
	}
	before := activationFiles(t, root, ".aoci/config.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
	oldBaseline, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, data := activateJSON(t, root)
	var result scopechange.Result
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if code != 0 || result.Status != "applied" || result.Version != machinecontract.ManagedScopeChangeResultV2 || result.AuthorizationMechanism != machinecontract.ApprovalMechanismPolicyBoundAuto {
		t.Fatalf("activation: code=%d %s", code, data)
	}
	assertActivationFiles(t, root, before)
	current, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if current.ManagedScope.PolicyIdentity == oldBaseline.ManagedScope.PolicyIdentity || current.Files["main.go"].SHA256 != oldBaseline.Files["main.go"].SHA256 {
		t.Fatal("policy was not activated or source drift was silently baselined")
	}
}

func TestScopeActivateApprovalStopsAndSavedPreviewCanBeApplied(t *testing.T) {
	for _, change := range []string{"review", "budget-relaxation"} {
		t.Run(change, func(t *testing.T) {
			root := scopeActivationFixture(t)
			args := []string{"approval-mode", "review"}
			if change == "budget-relaxation" {
				args = []string{"budget", "set", "--max-tokens", "500000"}
			}
			if _, err := runScopeCLI(t, root, args...); err != nil {
				t.Fatal(err)
			}
			before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
			prompt := capturePrompt(t)
			code, data := activateJSON(t, root)
			var response struct {
				Version   int                     `json:"version"`
				OK        bool                    `json:"ok"`
				ExitCode  int                     `json:"exit_code"`
				ErrorCode string                  `json:"error_code"`
				Details   scopeActivationApproval `json:"details"`
			}
			if err := json.Unmarshal(data, &response); err != nil {
				t.Fatal(err)
			}
			details := response.Details
			if code != 2 || response.Version != 1 || response.OK || response.ExitCode != 2 || response.ErrorCode != "managed_scope_human_approval_required" || !details.InteractionRequired || details.FormalWritesStarted || prompt.Len() != 0 {
				t.Fatalf("approval boundary: code=%d %s prompt=%s", code, data, prompt)
			}
			assertActivationFiles(t, root, before)
			if !strings.HasPrefix(details.PreviewFile, filepath.Join(root, ".aoci", "scope-change")+string(os.PathSeparator)) {
				t.Fatalf("preview outside runtime area: %s", details.PreviewFile)
			}
			raw, err := os.ReadFile(details.PreviewFile)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scopechange.DecodePreview(raw); err != nil {
				t.Fatal(err)
			}
			approvalFile := filepath.Join(filepath.Dir(details.PreviewFile), "approval.json")
			// The commands run as printed: no placeholder a person has to edit.
			if strings.Contains(details.ApproveCommand, "<id>") || strings.Contains(details.ApplyCommand, "<id>") {
				t.Fatalf("a printed command still carries a placeholder: %+v", details)
			}
			if details.ApproveCommand != hostInteractionCommand("--repo", root, "scope", "approve", "--preview-file", details.PreviewFile, "--out-file", approvalFile) || details.ApplyCommand != hostInteractionCommand("--repo", root, "scope", "apply", "--preview-file", details.PreviewFile, "--approval-file", approvalFile) {
				t.Fatalf("unbound continuation: %+v", details)
			}
			// A second attempt must not overwrite the artifact already under review.
			_, repeated := activateJSON(t, root)
			var next struct {
				Details scopeActivationApproval `json:"details"`
			}
			if err := json.Unmarshal(repeated, &next); err != nil {
				t.Fatal(err)
			}
			if next.Details.PreviewFile == details.PreviewFile {
				t.Fatal("reused pending preview")
			}
			saved, err := os.ReadFile(details.PreviewFile)
			if err != nil || !bytes.Equal(saved, raw) {
				t.Fatalf("pending preview was changed: %v", err)
			}
			grantTTY(t)
			if _, _, err := runScopeSplit(t, root, strings.NewReader("yes\n"), true, "approve", "--preview-file", details.PreviewFile, "--actor", "tester", "--out-file", approvalFile); err != nil {
				t.Fatal(err)
			}
			out, _, err := runScopeSplit(t, root, nil, true, "apply", "--preview-file", details.PreviewFile, "--approval-file", approvalFile)
			if err != nil {
				t.Fatalf("saved preview cannot be applied: %v %s", err, out)
			}
		})
	}
}

func TestScopeActivatePreservesExistingRefusals(t *testing.T) {
	for _, kind := range []string{"legacy-stale", "off"} {
		t.Run(kind, func(t *testing.T) {
			root := buildScopeCLIRepo(t)
			want := "managed_scope_index_source_stale"
			if kind == "legacy-stale" {
				if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package sample\nfunc Changed() {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg, err := config.LoadBase(root)
				if err != nil {
					t.Fatal(err)
				}
				if err := cfg.SetAutomationMode(config.AutomationModeOff); err != nil {
					t.Fatal(err)
				}
				if err := config.Save(root, cfg); err != nil {
					t.Fatal(err)
				}
				want = "automation_off"
			}
			before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt")
			code, data := activateJSON(t, root)
			if code != 2 || !bytes.Contains(data, []byte(want)) {
				t.Fatalf("expected %s: code=%d %s", want, code, data)
			}
			assertActivationFiles(t, root, before)
			matches, err := filepath.Glob(filepath.Join(root, ".aoci", "scope-change", "activate-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("refusal wrote approval artifacts: %v %v", matches, err)
			}
		})
	}
}

func TestScopeActivateHelpIsLocalized(t *testing.T) {
	for locale, phrase := range map[string]string{"en-US": "empty Candidate Set", "zh-CN": "空Candidate Set"} {
		t.Run(locale, func(t *testing.T) {
			root := scopeActivationFixture(t)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Locale = locale
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			var out, errs bytes.Buffer
			code := executeCLI([]string{"--repo", root, "scope", "activate", "--help"}, &out, &errs)
			if code != 0 || !strings.Contains(out.String(), phrase) || !strings.Contains(out.String(), "--json") {
				t.Fatalf("help code=%d %s %s", code, &out, &errs)
			}
		})
	}
}

// Locks (#113): when the desired policy, budget, and posture already equal the
// active receipt, activation reports already_aligned with exit 0 and records
// no transaction; it used to run Apply and file a zero-change transaction
// that read as a plain "applied".
func TestScopeActivateReportsAlreadyAlignedWithoutATransaction(t *testing.T) {
	root := scopeActivationFixture(t)
	history := filepath.Join(root, ".aoci", "transactions", "history")
	countHistory := func() int {
		entries, err := os.ReadDir(history)
		if err != nil {
			return 0
		}
		return len(entries)
	}
	before := countHistory()
	files := activationFiles(t, root, ".aoci/baseline.json", ".aoci/config.json")
	code, out := activateJSON(t, root)
	if code != ExitOK || !strings.Contains(string(out), `"status": "already_aligned"`) || !strings.Contains(string(out), `"formal_writes_started": false`) {
		t.Fatalf("aligned activation must report already_aligned: code=%d out=%s", code, out)
	}
	if countHistory() != before {
		t.Fatalf("aligned activation recorded a transaction: %d -> %d", before, countHistory())
	}
	assertActivationFiles(t, root, files)
	var outText, errs bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "scope", "activate"}, &outText, &errs); code != ExitOK || !strings.Contains(outText.String(), "already_aligned") {
		t.Fatalf("human output must name already_aligned: code=%d out=%s err=%s", code, &outText, &errs)
	}
	var status map[string]any
	outText.Reset()
	errs.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "scope", "status"}, &outText, &errs); code != ExitOK {
		t.Fatalf("scope status: code=%d err=%s", code, &errs)
	}
	if err := json.Unmarshal(outText.Bytes(), &status); err != nil || status["repository_root"] != root {
		t.Fatalf("scope status must carry repository_root: err=%v out=%s", err, &outText)
	}
}

// After a merge that changed both the rules and an Observe file, activate
// refuses on the unreviewed evidence and names --reviewed-by; with the flag
// it records the review over the planner's own paths and applies the policy
// in one transaction, leaving nothing pending.
func TestScopeActivateRecordsTheObserveReviewItWouldRefuse(t *testing.T) {
	root := scopeActivationFixtureWith(t, map[string]string{"main_test.go": "package main\n"})
	if _, err := runScopeCLI(t, root, "rule", "add", "future", "--action", "exclude", "--pattern", "future.txt", "--pattern-kind", "file", "--reason", "future artifact"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte("package main\n// changed by the merge\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := activationFiles(t, root, ".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.code.txt")
	var out, errs bytes.Buffer
	code := executeCLI([]string{"--repo", root, "scope", "activate"}, &out, &errs)
	if code != ExitInvalid || !strings.Contains(errs.String(), "observed_evidence_review_required") || !strings.Contains(errs.String(), "--reviewed-by") {
		t.Fatalf("activate without a reviewer must refuse and name the flag: code=%d\n%s%s", code, out.String(), errs.String())
	}
	assertActivationFiles(t, root, before)
	out.Reset()
	errs.Reset()
	code = executeCLI([]string{"--repo", root, "--json", "scope", "activate", "--reviewed-by", "merge-reviewer"}, &out, &errs)
	var result scopechange.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 0 || result.Status != "applied" {
		t.Fatalf("activate --reviewed-by must apply: code=%d err=%v\n%s%s", code, err, out.String(), errs.String())
	}
	status, err := runScopeCLI(t, root, "status")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Stage                 string `json:"stage"`
		PolicyIdentityAligned bool   `json:"policy_identity_aligned"`
		ObservedPendingReview int    `json:"observed_pending_review"`
	}
	if err := json.Unmarshal(status, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.PolicyIdentityAligned || parsed.ObservedPendingReview != 0 || parsed.Stage == "observed_evidence_review_required" || parsed.Stage == "scope_change_required" {
		t.Fatalf("one activation must leave the policy active and the review recorded: %+v", parsed)
	}
}
