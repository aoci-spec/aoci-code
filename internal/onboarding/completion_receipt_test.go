package onboarding

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
)

// A Migration session takes the v1 receipt. The batch hands out the receipt
// that completes it, batch_id included, and a receipt that does not fit is
// refused with the field it gets wrong and changes nothing.
func TestLegacyCompletionTemplateCompletesItsBatch(t *testing.T) {
	root, session := startLegacyOnboarding(t)
	batch, err := Next(root, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	template := batch.CompletionTemplate
	if len(batch.Tasks) != 1 || template == nil || template.Version != LegacyCompletionVersion ||
		template.SessionID != session.OnboardingSessionID || template.BatchID != batch.BatchID ||
		!reflect.DeepEqual(template.CompletedTasks, []string{batch.Tasks[0].TaskID}) || template.SemanticAuthoringDeclaration != nil {
		t.Fatalf("the batch does not hand out the receipt that completes it: %#v", batch)
	}
	other := ""
	for _, id := range session.PendingAuthoringTargets {
		if id != batch.Tasks[0].TaskID {
			other = id
			break
		}
	}
	before := readOnboardingSession(t, root)
	for field, mutate := range map[string]func(*Completion){
		"version": func(value *Completion) { value.Version = CompletionVersion },
		"semantic_authoring_declaration": func(value *Completion) {
			value.SemanticAuthoringDeclaration = &cognitionplan.SemanticAuthoringDeclaration{}
		},
		"batch_id": func(value *Completion) { value.BatchID = strings.Repeat("0", 64) },
	} {
		attempt := *template
		mutate(&attempt)
		if _, err := CompleteTasks(root, attempt); err == nil || err.Error() != "onboarding_completion_contract_mismatch field="+field {
			t.Fatalf("a receipt with a wrong %s was answered %v", field, err)
		}
		if readOnboardingSession(t, root) != before {
			t.Fatalf("a refused receipt with a wrong %s changed the Session", field)
		}
	}
	borrowed := *template
	borrowed.CompletedTasks = []string{other}
	if _, err := CompleteTasks(root, borrowed); err == nil || err.Error() != "onboarding_completion_contract_mismatch field=batch_id" {
		t.Fatalf("one batch's id completed another task set: %v", err)
	}
	if _, err := CompleteTasks(root, *template); err != nil {
		t.Fatalf("the batch's own receipt was refused: %v", err)
	}
	next, err := Next(root, 1, 0)
	if err != nil || next.CompletionTemplate == nil {
		t.Fatalf("no next batch: %#v %v", next, err)
	}
	unnamed := Completion{Version: LegacyCompletionVersion, SessionID: session.OnboardingSessionID, CompletedTasks: next.CompletionTemplate.CompletedTasks}
	if _, err := CompleteTasks(root, unnamed); err != nil {
		t.Fatalf("a v1 receipt naming no batch no longer completes a pending subset: %v", err)
	}
}

// A Fresh session takes the v2 receipt. Its template is complete except for
// the Host's own semantic_authoring_declaration, which AOCI never writes, and
// each field that does not fit the active batch is named.
func TestFreshCompletionTemplateNeedsOnlyTheHostDeclaration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	template := batch.CompletionTemplate
	hostReceipt := hostAuthoredCompletion(session, batch)
	if template == nil || template.Version != CompletionVersion || template.SessionID != hostReceipt.SessionID ||
		template.BatchID != hostReceipt.BatchID || !sameStringSet(template.CompletedTasks, hostReceipt.CompletedTasks) ||
		template.SemanticAuthoringDeclaration != nil {
		t.Fatalf("the v2 template is not the Host receipt without its declaration: %#v", template)
	}
	before := readOnboardingSession(t, root)
	if _, err := CompleteTasks(root, *template); err == nil || !strings.Contains(err.Error(), "semantic_authoring_declaration_missing") {
		t.Fatalf("a receipt without the Host's declaration was answered %v", err)
	}
	for field, mutate := range map[string]func(*Completion){
		"version":            func(value *Completion) { value.Version = LegacyCompletionVersion },
		"batch_id":           func(value *Completion) { value.BatchID = strings.Repeat("0", 64) },
		"completed_task_ids": func(value *Completion) { value.CompletedTasks = value.CompletedTasks[1:] },
	} {
		attempt := *template
		attempt.SemanticAuthoringDeclaration = hostReceipt.SemanticAuthoringDeclaration
		mutate(&attempt)
		if _, err := CompleteTasks(root, attempt); err == nil || err.Error() != "onboarding_completion_contract_mismatch field="+field {
			t.Fatalf("a v2 receipt with a wrong %s was answered %v", field, err)
		}
	}
	if readOnboardingSession(t, root) != before {
		t.Fatal("a refused v2 receipt changed the Session")
	}
	completed := *template
	completed.SemanticAuthoringDeclaration = hostReceipt.SemanticAuthoringDeclaration
	if _, err := CompleteTasks(root, completed); err != nil {
		t.Fatalf("the template with the Host's declaration was refused: %v", err)
	}
}

// startLegacyOnboarding starts a Migration session over a minimal Legacy
// index with an aligned Baseline.
func startLegacyOnboarding(t *testing.T) (string, *Session) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{"src/main.go": "package main\n", "src/util.go": "package main\n\nfunc util() {}\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	legacy := "#AOCI-CLI Complete Index\n#Project: Receipt fixture\n#[Tag dictionary]\n#A Layer: C Code\n" +
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" +
		"main.go[CD9S]: F:Run the legacy fixture | R:- | A:main | S:-\n" +
		"util.go[CD5S]: F:Help the legacy fixture | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]baseline.Fingerprint{}
	for _, rel := range []string{"aoci.txt", "src/main.go", "src/util.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		files[rel] = fingerprint
	}
	value, err := baseline.NewBaselineAt(files, "2026-07-31T03:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := baseline.MarshalExact(value)
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 7, 31, 3, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if session.Version != LegacySessionVersion || len(session.PendingAuthoringTargets) < 2 {
		t.Fatalf("not a Migration session with several tasks: %#v", session)
	}
	sort.Strings(session.PendingAuthoringTargets)
	return root, session
}

func readOnboardingSession(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(SessionPath(root))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
