package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/onboarding"
)

const (
	initCognitionDefault = ""
	initCognitionProject = "project"
	initCognitionGeneric = "generic"
)

func validateInitCognitionMode(mode string) error {
	switch mode {
	case initCognitionDefault, initCognitionProject, initCognitionGeneric:
		return nil
	default:
		return fmt.Errorf("init_cognition_mode_invalid: %q", mode)
	}
}

// preflightProjectCognitionInit keeps the model-authored Fresh Bootstrap path
// uninitialized until its existing Root-last transaction applies the complete
// Candidate. An existing Fresh Session is accepted so init remains retryable
// after all Host integration files have already been installed.
func preflightProjectCognitionInit(root string, cfg *config.Config, agent string, withHooks bool, locale string) error {
	if err := rejectInitPendingCognitionTransaction(root); err != nil {
		return err
	}
	if err := rejectInitBaseline(root); err != nil {
		return err
	}
	if err := rejectInitFormalCognition(root, cfg); err != nil {
		return err
	}
	if cfg != nil && (cfg.ManagedScope == nil) != (cfg.CognitionBudget == nil) {
		return fmt.Errorf("init_project_governance_partial")
	}
	if cfg != nil && filepath.ToSlash(filepath.Clean(cfg.IndexPath)) != "aoci.txt" {
		return fmt.Errorf("init_project_index_path_must_be_canonical: %s", cfg.IndexPath)
	}
	if session, exists, err := onboarding.Load(root); err != nil {
		return fmt.Errorf("init_project_onboarding_state_invalid: %w", err)
	} else if exists && session.Operation != cognitionplan.OperationBootstrap {
		return fmt.Errorf("init_project_onboarding_operation_conflict: %s", session.Operation)
	} else if exists {
		if locale != "" && cfg != nil && locale != cfg.Locale {
			return fmt.Errorf("init_project_active_onboarding_parameter_change: locale")
		}
		for _, name := range initRequestedAgents(agent) {
			if cfg == nil || !contains(cfg.InstalledAgents, name) {
				return fmt.Errorf("init_project_active_onboarding_parameter_change: agent")
			}
		}
		if withHooks && (agent == "claude" || agent == "all") && !hooks.IsClaudeHookInstalled(root) {
			return fmt.Errorf("init_project_active_onboarding_parameter_change: hooks")
		}
	}
	return nil
}

func initRequestedAgents(agent string) []string {
	if agent == "all" {
		return []string{"claude", "codex", "cursor"}
	}
	if agent == "" {
		return nil
	}
	return []string{agent}
}

// preflightGenericCognitionInit is deliberately stricter than the historical
// no-flag init path. Explicit generic mode is a quality downgrade from a
// project-authored Fresh Bootstrap, so it never silently abandons an active
// Session, Approval, transaction, Baseline, or third-party formal bytes.
func preflightGenericCognitionInit(root string, cfg *config.Config) error {
	if err := rejectInitPendingCognitionTransaction(root); err != nil {
		return err
	}
	if _, exists, err := onboarding.Load(root); err != nil {
		return fmt.Errorf("init_generic_onboarding_state_invalid: %w", err)
	} else if exists {
		return fmt.Errorf("init_generic_active_onboarding_abort_required")
	}
	if err := rejectInitBaseline(root); err != nil {
		return err
	}
	if err := rejectInitFormalCognition(root, cfg); err != nil {
		return err
	}
	if approval, err := findInitOnboardingApproval(root); err != nil {
		return err
	} else if approval != "" {
		return fmt.Errorf("init_generic_approval_artifact_forbidden: %s", approval)
	}
	return nil
}

func rejectInitPendingCognitionTransaction(root string) error {
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return fmt.Errorf("init_cognition_recovery_state_unavailable: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	names := make([]string, 0, len(pending))
	for _, item := range pending {
		names = append(names, item.Filename)
	}
	sort.Strings(names)
	return fmt.Errorf("init_cognition_recovery_pending: %s", strings.Join(names, ","))
}

func rejectInitBaseline(root string) error {
	_, exists, err := baseline.Load(root)
	if err != nil {
		return fmt.Errorf("init_cognition_baseline_invalid: %w", err)
	}
	if exists {
		return fmt.Errorf("init_cognition_baseline_already_exists")
	}
	return nil
}

func rejectInitFormalCognition(root string, cfg *config.Config) error {
	paths := map[string]struct{}{
		"aoci.txt":          {},
		"aoci.meta.txt":     {},
		"aoci.code.txt":     {},
		"aoci.database.txt": {},
	}
	if cfg != nil && strings.TrimSpace(cfg.IndexPath) != "" {
		paths[filepath.ToSlash(filepath.Clean(cfg.IndexPath))] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, relative := range ordered {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative)))
		if err == nil {
			return fmt.Errorf("init_cognition_formal_asset_already_exists: %s", relative)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("init_cognition_formal_asset_inspect_failed[%s]: %w", relative, err)
		}
	}
	return nil
}

func findInitOnboardingApproval(root string) (string, error) {
	base := filepath.Join(root, ".aoci", "onboarding")
	info, err := os.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("init_generic_onboarding_artifacts_unavailable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("init_generic_onboarding_artifacts_wrong_type")
	}
	found := ""
	err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if !strings.Contains(name, "approval") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		found = filepath.ToSlash(relative)
		return fs.SkipAll
	})
	if err != nil {
		return "", fmt.Errorf("init_generic_onboarding_artifacts_unavailable: %w", err)
	}
	return found, nil
}

func initProjectOnboardingCommand(root, nextAction string) string {
	if nextAction == "authoring_next" {
		return hostInteractionCommand("--repo", root, "cognition", "onboard", "next", "--json")
	}
	return hostInteractionCommand("--repo", root, "cognition", "onboard", "status", "--json")
}
