package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

func normalizeManagedScopeAndBudget(cfg *Config) error {
	if cfg.ManagedScope != nil {
		normalized, err := managedscope.Normalize(*cfg.ManagedScope)
		if err != nil {
			return err
		}
		cfg.ManagedScope = &normalized
	}
	if cfg.CognitionBudget != nil {
		normalized, err := cognitionbudget.Normalize(*cfg.CognitionBudget)
		if err != nil {
			return err
		}
		cfg.CognitionBudget = &normalized
	}
	return nil
}

func MutateSafeInventoryHighRiskOptIn(repositoryRoot string, mutate func([]string) ([]string, error)) error {
	if mutate == nil {
		return fmt.Errorf("safe_inventory_high_risk_mutation_required")
	}
	for attempt := 0; attempt < 3; attempt++ {
		cfg, expectedSHA256, err := loadBaseSnapshot(repositoryRoot)
		if err != nil {
			return err
		}
		paths, err := mutate(append([]string{}, cfg.SafeInventoryHighRiskOptIn...))
		if err != nil {
			return err
		}
		for index, path := range paths {
			if strings.ContainsAny(path, "*?[") {
				return fmt.Errorf("safe_inventory_high_risk_opt_in_invalid")
			}
			normalized, normalizeErr := afs.NormalizeRelPath(path)
			if normalizeErr != nil {
				return fmt.Errorf("safe_inventory_high_risk_opt_in_invalid")
			}
			category, _ := afs.BuiltInSafetyCategory(normalized)
			if category != afs.SafetySensitive {
				return fmt.Errorf("safe_inventory_high_risk_opt_in_forbidden")
			}
			paths[index] = normalized
		}
		sort.Strings(paths)
		deduplicated := paths[:0]
		for _, path := range paths {
			if len(deduplicated) == 0 || deduplicated[len(deduplicated)-1] != path {
				deduplicated = append(deduplicated, path)
			}
		}
		cfg.SafeInventoryHighRiskOptIn = deduplicated
		if err := saveToPathCAS(FilePath(repositoryRoot), cfg, expectedSHA256); err != nil {
			if errors.Is(err, afs.ErrAtomicCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("safe_inventory_high_risk_config_conflict: %w", afs.ErrAtomicCASConflict)
}

func cloneManagedScope(value *managedscope.Policy) *managedscope.Policy {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.Rules = append([]managedscope.Rule{}, value.Rules...)
	for index := range copyValue.Rules {
		copyValue.Rules[index].Exceptions = append([]string{}, value.Rules[index].Exceptions...)
	}
	return &copyValue
}

func cloneCognitionBudget(value *cognitionbudget.Policy) *cognitionbudget.Policy {
	if value == nil {
		return nil
	}
	copyValue := *value
	copyValue.R = append([]cognitionbudget.FieldBand{}, value.R...)
	copyValue.S = append([]cognitionbudget.FieldBand{}, value.S...)
	return &copyValue
}

func (cfg *Config) EffectiveManagedScope() managedscope.Policy {
	if cfg == nil || cfg.ManagedScope == nil {
		return managedscope.LegacyPolicy()
	}
	return *cloneManagedScope(cfg.ManagedScope)
}

func (cfg *Config) EffectiveCognitionBudget() cognitionbudget.Policy {
	if cfg == nil || cfg.CognitionBudget == nil {
		return cognitionbudget.LegacyPolicy()
	}
	return *cloneCognitionBudget(cfg.CognitionBudget)
}

// SetNewProjectGovernance materializes the defaults that are intentionally
// absent for old projects. Init alone calls this after proving config.json did
// not already exist, so upgrades never silently enable enforcement.
func (cfg *Config) SetNewProjectGovernance(profile string) error {
	scope := managedscope.DefaultPolicy(profile)
	normalizedScope, err := managedscope.Normalize(scope)
	if err != nil {
		return err
	}
	budget, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		return err
	}
	cfg.ManagedScope = &normalizedScope
	cfg.CognitionBudget = &budget
	return nil
}

// MutateManagedScope updates only the team-owned desired Scope Policy through
// the existing config CAS pipeline. The active policy identity remains in the
// Baseline until a separate Scope Change transaction applies it.
func MutateManagedScope(repositoryRoot string, mutate func(*managedscope.Policy) error) error {
	if mutate == nil {
		return fmt.Errorf("managed_scope_mutation_required")
	}
	for attempt := 0; attempt < 3; attempt++ {
		cfg, expectedSHA256, err := loadBaseSnapshot(repositoryRoot)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			cfg, err = LoadBase(repositoryRoot)
			if err != nil {
				return err
			}
			if _, statErr := os.Lstat(FilePath(repositoryRoot)); statErr == nil {
				continue
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			policy := cfg.EffectiveManagedScope()
			if err := mutate(&policy); err != nil {
				return err
			}
			normalized, err := managedscope.Normalize(policy)
			if err != nil {
				return err
			}
			cfg.ManagedScope = &normalized
			lock, lockErr := afs.AcquireIndexLock(repositoryRoot)
			if lockErr != nil {
				return lockErr
			}
			if _, statErr := os.Lstat(FilePath(repositoryRoot)); !errors.Is(statErr, os.ErrNotExist) {
				_ = lock.Release()
				continue
			}
			saveErr := Save(repositoryRoot, cfg)
			releaseErr := lock.Release()
			if saveErr != nil {
				return saveErr
			}
			return releaseErr
		}
		policy := cfg.EffectiveManagedScope()
		if err := mutate(&policy); err != nil {
			return err
		}
		normalized, err := managedscope.Normalize(policy)
		if err != nil {
			return err
		}
		cfg.ManagedScope = &normalized
		if err := saveToPathCAS(FilePath(repositoryRoot), cfg, expectedSHA256); err != nil {
			if errors.Is(err, afs.ErrAtomicCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("managed_scope_config_conflict: %w", afs.ErrAtomicCASConflict)
}

// MutateCognitionBudget updates only the desired budget policy. Its identity
// remains inactive until the shared Scope Change transaction records it in the
// Baseline receipt.
func MutateCognitionBudget(repositoryRoot string, mutate func(*cognitionbudget.Policy) error) error {
	if mutate == nil {
		return fmt.Errorf("cognition_budget_mutation_required")
	}
	for attempt := 0; attempt < 3; attempt++ {
		cfg, expectedSHA256, err := loadBaseSnapshot(repositoryRoot)
		if err != nil {
			return err
		}
		policy := cfg.EffectiveCognitionBudget()
		if err := mutate(&policy); err != nil {
			return err
		}
		normalized, err := cognitionbudget.Normalize(policy)
		if err != nil {
			return err
		}
		cfg.CognitionBudget = &normalized
		if err := saveToPathCAS(FilePath(repositoryRoot), cfg, expectedSHA256); err != nil {
			if errors.Is(err, afs.ErrAtomicCASConflict) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("cognition_budget_config_conflict: %w", afs.ErrAtomicCASConflict)
}
