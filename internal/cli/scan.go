// aoci scan —— 全量建立/重建基线
// 索引条目: scan.go[CLI.Scan.8.S]
//
// 语义: "承认当前源码为对齐新起点"(平台 RebuildBaseline 同语义)。
// 纪律: 已有基线时须 --force 确认(防一键洗白未处理漂移);写前 Save 内滚动备份旧基线;
//
//	--dry-run 只输出统计不落盘;绝不顺手改 index.txt;落 ledger。
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/managedstate"
	"github.com/aoci-spec/aoci-code/internal/scopechange"
	"github.com/spf13/cobra"
)

func init() {
	var force, dryRun bool

	cmd := &cobra.Command{
		Use:   "scan",
		Short: cliMessage("cli.short.scan"),
		Long:  scanLongHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			cfg, err := config.Load(root)
			if err != nil {
				return &ExitError{Code: ExitConfig, Msg: err.Error()}
			}
			if !dryRun {
				volumeLayout, layoutErr := activeVolumeLayout(root, cfg.IndexPath)
				if layoutErr != nil {
					return &ExitError{Code: ExitConfig, Msg: layoutErr.Error()}
				}
				if !volumeLayout {
					if err := requireLegacyWriteLayout(root, cfg, false); err != nil {
						return &ExitError{Code: ExitConfig, Msg: err.Error()}
					}
				}
				lock, lockErr := afs.AcquireIndexLock(root)
				if lockErr != nil {
					return &ExitError{Code: ExitInternal, Err: errors.New(cliMessage("scan.lock_error", lockErr))}
				}
				defer lock.Release()
			}

			// 防洗白闸门: 已有基线且非 dry-run 时必须 --force
			_, exists, lerr := baseline.Load(root)
			if lerr != nil && exists {
				// 基线存在但损坏: 允许 --force 重建,否则提示
				if !force && !dryRun {
					return &ExitError{Code: ExitConfig, Msg: lerr.Error()}
				}
			}
			if exists && !force && !dryRun {
				return &ExitError{Code: ExitConfig, Msg: cliMessage("scan.baseline_exists")}
			}

			// 首次Managed Scope初始化建立带角色的Baseline；已有Managed
			// Baseline只能通过正式Scope Change前移，--force也不得洗白。
			var snap map[string]baseline.Fingerprint
			var warns []string
			var inventorySummary afs.SafeInventorySummary
			var managedReceipt *baseline.ManagedScopeState
			if cfg.ManagedScope != nil || cfg.CognitionBudget != nil {
				if exists {
					return &ExitError{Code: ExitConfig, Msg: cliMessage("scan.managed_scope_transaction_required")}
				}
				state, stateErr := managedstate.Load(root, cfg)
				if stateErr != nil || state.Evaluation == nil {
					if stateErr == nil {
						stateErr = errors.New("managed_scope_evaluation_unavailable")
					}
					return errors.New(cliMessage("scan.snapshot_error", stateErr))
				}
				snap, err = managedscope.Snapshot(root, state.Evaluation, managedscope.SnapshotOptions{HighRiskContentApproved: false})
				if err != nil {
					return errors.New(cliMessage("scan.snapshot_error", err))
				}
				inventorySummary = state.Evaluation.SafeInventory
				budgetPolicy := cfg.EffectiveCognitionBudget()
				budgetIdentity, identityErr := cognitionbudget.Identity(budgetPolicy)
				if identityErr != nil {
					return errors.New(cliMessage("scan.snapshot_error", identityErr))
				}
				// 首份收据同样记录生效授权模式,后续 Scope Change 才能证明模式跃迁方向。
				authorizationMode, modeErr := scopechange.EffectiveApplyAuthorizationMode(cfg)
				if modeErr != nil {
					return errors.New(cliMessage("scan.snapshot_error", modeErr))
				}
				managedReceipt = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
					PolicyIdentity: state.DesiredPolicyIdentity, ObserveChangePolicy: cfg.EffectiveManagedScope().ObserveChangePolicy,
					BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budgetPolicy,
					ApplyAuthorizationMode: authorizationMode}
			} else {
				var inventory *afs.SafeInventory
				snap, warns, inventory, err = baseline.SnapshotWithInventory(root, cfg.WalkOptions())
				if err == nil {
					inventorySummary = inventory.Summary
				}
			}
			if err != nil {
				return errors.New(cliMessage("scan.snapshot_error", err))
			}
			for _, w := range warns {
				fmt.Fprintln(os.Stderr, cliMessage("scan.warning"), localeSafeCLIDetail(w))
			}
			// A formal cognition asset hidden from Git never enters the Baseline,
			// and the Baseline this scan is about to publish is what makes that
			// durable. The failure then surfaces far away as a blocked Guide over
			// code_volume_unbaselined, which names neither the ignore rule nor the
			// file that carries it. Refuse here, while the fix is still one line.
			//
			// This runs before the --dry-run return on purpose, so a dry run
			// reports the same fact instead of promising a scan that would fail.
			if err := refuseGitHiddenFormalAssets(root, cfg, snap); err != nil {
				return err
			}
			result := struct {
				Version          string                   `json:"version"`
				DryRun           bool                     `json:"dry_run"`
				Inventory        afs.SafeInventorySummary `json:"safe_inventory"`
				FingerprintCount int                      `json:"fingerprint_count"`
			}{Version: afs.SafeInventoryVersion, DryRun: dryRun, Inventory: inventorySummary, FingerprintCount: len(snap)}

			if dryRun {
				if flagJSON {
					return writePlannerJSON(cmd, result)
				}
				if !flagQuiet {
					fmt.Println(cliMessage("scan.dry_run", len(snap), len(warns)))
				}
				return nil
			}

			// 落盘(Save 内含旧基线滚动备份)
			newBaseline := baseline.NewBaseline(snap)
			newBaseline.ManagedScope = managedReceipt
			if err := baseline.SaveUnderIndexLock(root, newBaseline); err != nil {
				return errors.New(cliMessage("scan.save_error", err))
			}
			ledger.Append(root, cfg.LedgerEnabled, ledger.Event{
				Op: "scan", PathsCount: len(snap),
				DurationMs: time.Since(start).Milliseconds(), Source: "human",
			})
			if flagJSON {
				return writePlannerJSON(cmd, result)
			}
			if !flagQuiet {
				fmt.Println(cliMessage("scan.complete", len(snap), time.Since(start).Milliseconds()))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, cliMessage("cli.flag.scan_force"))
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, cliMessage("cli.flag.scan_dry_run"))
	registerCommand(cmd)
}

// refuseGitHiddenFormalAssets stops a scan that would publish a Baseline missing
// a formal cognition asset because Git hides it.
//
// Scan takes its inventory from Git, so an ignored asset is simply absent — no
// error, no warning, just a Baseline that can never govern the Volume it left
// out. Every later symptom points somewhere else, and the recorded case cost a
// capable operator a dozen rounds to trace back to one line in
// .git/info/exclude.
//
// Only assets that exist on disk are checked: an absent Volume is a different
// condition with its own loud reporting.
func refuseGitHiddenFormalAssets(
	root string,
	cfg *config.Config,
	snapshot map[string]baseline.Fingerprint,
) error {
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil || set == nil {
		// Cognition that cannot be loaded is not this guard's finding to make.
		return nil
	}
	hidden := make([]string, 0, 4)
	rules := make([]string, 0, 4)
	for _, rel := range formalAssetPaths(set) {
		if _, recorded := snapshot[rel]; recorded {
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil {
			continue
		}
		ignored, rule := afs.PathIgnoredByGit(root, rel)
		if !ignored {
			continue
		}
		hidden = append(hidden, rel)
		if rule != "" {
			rules = append(rules, rule)
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	return &ExitError{
		Code:        ExitConfig,
		MachineCode: "formal_cognition_assets_git_ignored",
		Msg: cliMessage("scan.formal_assets_git_ignored",
			len(hidden), strings.Join(hidden, ", "),
			localeSafeCLIDetail(strings.Join(rules, "; "))),
	}
}

// formalAssetPaths lists the cognition assets whose absence from the Baseline
// makes the repository ungovernable, in a stable order.
func formalAssetPaths(set *cognition.Set) []string {
	paths := make([]string, 0, 4)
	for _, candidate := range []string{set.Root.Descriptor.Path, set.Meta.Descriptor.Path} {
		if strings.TrimSpace(candidate) != "" {
			paths = append(paths, candidate)
		}
	}
	for _, volume := range set.Volumes {
		if volume == nil || strings.TrimSpace(volume.Descriptor.Path) == "" {
			continue
		}
		paths = append(paths, volume.Descriptor.Path)
	}
	return sortedUniqueStrings(paths)
}

func sortedUniqueStrings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for index, value := range values {
		if index == 0 || value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
