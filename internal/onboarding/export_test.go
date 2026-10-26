package onboarding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/migrationapply"
)

// PreparedBootstrapReviewForTest builds a review-mode Fresh Bootstrap whose
// session has stopped at its human approval, for tests that drive it from
// outside the package. configure edits the team configuration before the
// session starts, since a later edit would drift the plan's guards.
func PreparedBootstrapReviewForTest(t *testing.T, configure func(*config.Config)) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := cfg.SetAutomationMode(config.AutomationModeReview); err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(cfg)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 8, 6, 5, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := Next(root, 100, 8*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTasks(root, hostAuthoredCompletion(session, batch)); err != nil {
		t.Fatal(err)
	}
	candidateData, _ := json.Marshal(onboardingCandidate(t, root, session.Plan))
	if _, err := Preview(root, candidateData, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// PreparedLegacyMigrationForTest builds a Legacy repository whose migration
// session has stopped at its human approval. The team configuration is
// written before the session starts, as init leaves it in a real repository:
// a server started on a repository without one materializes it, and that
// write would drift the prepared envelope. configure edits it first.
func PreparedLegacyMigrationForTest(t *testing.T, configure func(*config.Config)) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := "#AOCI-CLI Complete Index\n#Project: Onboarding Legacy\n#[Tag dictionary]\n#A Layer: C Code\n" +
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" +
		"main.go[CD9S]: F:Run the legacy fixture | R:- | A:main | S:Preserve approved startup behavior\n" +
		"===Self " + filepath.ToSlash(root) + "/===\n" +
		"aoci.txt[CD9S]: F:Describe the repository cognition contract | R:code:src/main.go | A:FRAS | S:Preserve model ownership\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]baseline.Fingerprint{}
	for _, relative := range []string{"aoci.txt", "src/main.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		files[relative] = fingerprint
	}
	baselineValue, err := baseline.NewBaselineAt(files, "2026-07-31T03:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	baselineData, _ := baseline.MarshalExact(baselineValue)
	if err := os.MkdirAll(filepath.Join(root, ".aoci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), baselineData, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if configure != nil {
		configure(cfg)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	session, err := Start(root, "en-US", time.Date(2026, 7, 31, 3, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	allTasks := append([]string{}, session.PendingAuthoringTargets...)
	if _, err := CompleteTasks(root, Completion{Version: "cognition-onboarding-completion/v1", SessionID: session.OnboardingSessionID, CompletedTasks: allTasks}); err != nil {
		t.Fatal(err)
	}
	candidate := legacyOnboardingCandidate(root, session.Plan, legacy)
	candidateData, _ := json.Marshal(candidate)
	snapshot, err := loadMigrationSnapshot(root, session)
	if err != nil {
		t.Fatal(err)
	}
	template, err := migrationapply.BuildMappingTemplate(root, snapshot, session.Plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	mappingData, _ := json.Marshal(authorOnboardingMapping(t, template))
	if _, err := Preview(root, candidateData, mappingData); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(root); err != nil {
		t.Fatal(err)
	}
	return root
}
