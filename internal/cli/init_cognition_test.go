package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

func TestInitProjectCognitionStartsAfterHostIntegrationWithoutFormalAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false", "--cognition=project"); err != nil {
		t.Fatalf("project cognition init failed: %v", err)
	}
	for _, relative := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", "aoci.database.txt", ".aoci/baseline.json"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Fatalf("project init created formal cognition before Bootstrap Apply: %s err=%v", relative, err)
		}
	}
	for _, relative := range []string{"AGENTS.md", "opencode.json", ".aoci/config.json", ".aoci/.gitignore"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("project init did not finish Host integration before planning: %s err=%v", relative, err)
		}
	}
	session, exists, err := onboarding.Load(root)
	if err != nil || !exists {
		t.Fatalf("Fresh Onboarding Session unavailable: exists=%t err=%v", exists, err)
	}
	if session.Operation != cognitionplan.OperationBootstrap || session.CurrentLayout != "uninitialized" ||
		session.TransactionState != "not_started" || session.NextAction != "authoring_next" {
		t.Fatalf("project init did not start a clean Fresh Bootstrap: %#v", session)
	}
	planData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(session.PlanArtifact)))
	if err != nil {
		t.Fatalf("persisted Fresh Plan artifact unavailable: %v", err)
	}
	plan, err := cognitionplan.DecodePlan(planData)
	if err != nil || plan.PlanID != session.PlanID {
		t.Fatalf("persisted Session does not bind the current post-integration Plan: plan=%#v session=%#v err=%v", plan, session, err)
	}
	inventory := map[string]bool{}
	for _, object := range plan.Inventory {
		inventory[object.Path] = true
	}
	for _, required := range []string{"AGENTS.md", "main.go", "opencode.json"} {
		if !inventory[required] {
			t.Fatalf("Fresh Plan was captured before integration file %s existed: inventory=%v", required, inventory)
		}
	}
	cfg, err := config.LoadBase(root)
	if err != nil || !contains(cfg.InstalledAgents, "opencode") || cfg.ManagedScope == nil || cfg.CognitionBudget == nil {
		t.Fatalf("project init did not persist production governance and Host binding: cfg=%#v err=%v", cfg, err)
	}
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false", "--cognition=project"); err != nil {
		t.Fatalf("project cognition init was not retryable before formal Apply: %v", err)
	}
	reloaded, exists, err := onboarding.Load(root)
	if err != nil || !exists || reloaded.OnboardingSessionID != session.OnboardingSessionID || reloaded.PlanID != session.PlanID {
		t.Fatalf("retry replaced the active Fresh Session: before=%#v after=%#v exists=%t err=%v", session, reloaded, exists, err)
	}
}

func TestInitProjectCognitionOutputUsesBoundExecutableRepoAndNextAction(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = reader.Close()
		_ = writer.Close()
	})
	var commandOut, commandErr bytes.Buffer
	exitCode := executeCLI([]string{
		"--repo=" + root,
		"init",
		"--agent=",
		"--hooks=false",
		"--cognition=project",
	}, &commandOut, &commandErr)
	_ = writer.Close()
	os.Stdout = oldStdout
	output, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if exitCode != ExitOK {
		t.Fatalf("project init failed: exit=%d stdout=%s stderr=%s output=%s", exitCode, commandOut.String(), commandErr.String(), output)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, required := range []string{executable, root, "next_action=authoring_next", "cognition", "onboard", "next", "--json"} {
		if !strings.Contains(text, required) {
			t.Fatalf("project init output lacks bound next-action token %q:\n%s", required, text)
		}
	}
	if strings.Contains(text, "%!") || strings.Contains(text, "aoci scan") || strings.Contains(text, "index agent guide") {
		t.Fatalf("project init emitted a formatting error or Legacy next step:\n%s", text)
	}
}

func TestInitProjectCognitionCompletesGovernanceForConfigOnlyRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=opencode", "--hooks=false", "--cognition=project"); err != nil {
		t.Fatalf("config-only project init failed: %v", err)
	}
	reloaded, err := config.LoadBase(root)
	if err != nil || reloaded.ManagedScope == nil || reloaded.CognitionBudget == nil {
		t.Fatalf("project init did not complete initial governance: cfg=%#v err=%v", reloaded, err)
	}
	session, exists, err := onboarding.Load(root)
	if err != nil || !exists {
		t.Fatalf("project Onboarding was not started: exists=%t err=%v", exists, err)
	}
	planData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(session.PlanArtifact)))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cognitionplan.DecodePlan(planData)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := cognitionplan.InitialManagedScopeEvidenceFromPlan(plan); err != nil || !present {
		t.Fatalf("config-only project Plan lacks initial governance evidence: present=%t err=%v", present, err)
	}
}

func TestInitProjectCognitionRejectsNonCanonicalIndexPathAndActiveSessionChanges(t *testing.T) {
	t.Run("non-canonical index path", func(t *testing.T) {
		root := t.TempDir()
		cfg := config.DefaultConfig()
		cfg.IndexPath = "custom-aoci.txt"
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		before := snapshotInitTree(t, root)
		_, err := runInit(t, root, "--agent=", "--hooks=false", "--cognition=project")
		if err == nil || !strings.Contains(err.Error(), "init_project_index_path_must_be_canonical") {
			t.Fatalf("project init accepted a non-canonical formal path: %v", err)
		}
		if after := snapshotInitTree(t, root); !equalStringMap(before, after) {
			t.Fatalf("rejected project init changed the repository\nbefore=%v\nafter=%v", before, after)
		}
	})

	t.Run("active session parameter change", func(t *testing.T) {
		root := t.TempDir()
		if _, err := runInit(t, root, "--agent=opencode", "--hooks=false", "--cognition=project"); err != nil {
			t.Fatal(err)
		}
		before := snapshotInitTree(t, root)
		_, err := runInit(t, root, "--agent=codex", "--hooks=false", "--cognition=project")
		if err == nil || !strings.Contains(err.Error(), "init_project_active_onboarding_parameter_change: agent") {
			t.Fatalf("active project Session accepted a new Host adapter: %v", err)
		}
		if after := snapshotInitTree(t, root); !equalStringMap(before, after) {
			t.Fatalf("rejected active Session change modified the repository\nbefore=%v\nafter=%v", before, after)
		}
	})
}

func TestInitExplicitGenericRequiresAbortedOnboardingAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	before := snapshotInitTree(t, root)
	_, err := runInit(t, root, "--agent=", "--hooks=false", "--cognition=generic")
	if err == nil || !strings.Contains(err.Error(), "init_generic_active_onboarding_abort_required") {
		t.Fatalf("explicit generic did not require Abort: %v", err)
	}
	if after := snapshotInitTree(t, root); !equalStringMap(before, after) {
		t.Fatalf("rejected generic fallback changed the repository\nbefore=%v\nafter=%v", before, after)
	}
}

func TestInitExplicitGenericAfterAbortUsesCurrentVolumeStarter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Start(root, "en-US", time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := onboarding.Abort(root); err != nil {
		t.Fatal(err)
	}
	// The project path normally persisted config before it started Onboarding.
	// Preserve that important fallback shape: config exists but no formal Root.
	if err := config.Save(root, legacyTestConfig()); err != nil {
		t.Fatal(err)
	}
	if _, err := runInit(t, root, "--agent=", "--hooks=false", "--cognition=generic"); err != nil {
		t.Fatalf("explicit generic fallback after safe Abort failed: %v", err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.LayoutMode != cognition.LayoutVolumesV1 || set.Meta.State != cognition.AssetPresent ||
		set.Volumes[cognition.ScopeCode] == nil {
		t.Fatalf("generic fallback did not reuse the current Volume starter: set=%#v err=%v", set, err)
	}
	if _, exists, err := baseline.Load(root); err != nil || exists {
		t.Fatalf("generic fallback must still leave first Baseline to scan: exists=%t err=%v", exists, err)
	}
}

func TestInitExplicitGenericRejectsBaselineFormalBytesAndApprovalArtifacts(t *testing.T) {
	tests := []struct {
		name string
		seed func(*testing.T, string)
		code string
	}{
		{name: "Baseline", code: "init_cognition_baseline_already_exists", seed: func(t *testing.T, root string) {
			if err := baseline.Save(root, baseline.NewBaseline(map[string]baseline.Fingerprint{})); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "formal bytes", code: "init_cognition_formal_asset_already_exists", seed: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "aoci.meta.txt"), []byte("third-party\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "Approval", code: "init_generic_approval_artifact_forbidden", seed: func(t *testing.T, root string) {
			path := filepath.Join(root, ".aoci", "onboarding", "aborted", "artifacts", "approval-third-party.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "pending recovery", code: "init_cognition_recovery_pending", seed: func(t *testing.T, root string) {
			path := filepath.Join(root, ".aoci", "transactions", "bootstrap-deadbeef.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.seed(t, root)
			before := snapshotInitTree(t, root)
			_, err := runInit(t, root, "--agent=", "--hooks=false", "--cognition=generic")
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("generic fallback accepted forbidden state: %v", err)
			}
			if after := snapshotInitTree(t, root); !equalStringMap(before, after) {
				t.Fatalf("rejected generic fallback changed the repository\nbefore=%v\nafter=%v", before, after)
			}
		})
	}
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
