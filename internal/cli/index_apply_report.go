// 三类Apply的结构化JSON观察包装器。
//
// 设计边界:
//   - 不复制Entries、Header或Curation的事务、校验、CAS与写入逻辑；
//   - 人读模式继续直接执行原Apply命令，输出和退出语义保持不变；
//   - JSON模式在执行前后观察正式资产、Baseline和Manifest状态；
//   - 已写资产但后续审计失败时必须明确禁止盲目重试Apply。
package cli

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/spf13/cobra"
)

const (
	applyReportVersion = 1

	applyKindEntries  = "entries"
	applyKindHeader   = "header"
	applyKindCuration = "curation"

	applyOutcomeApplied                 = "applied"
	applyOutcomeRejected                = "rejected"
	applyOutcomeAppliedWithWarnings     = "applied_with_warnings"
	applyOutcomeAssetWrittenAuditFailed = "asset_written_audit_failed"
)

// applyErrorReport描述一次Apply的机器错误。
type applyErrorReport struct {
	ExitCode int    `json:"exit_code"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// applyReportBase是三类Apply共享的稳定状态。
type applyReportBase struct {
	Version             int               `json:"version"`
	OK                  bool              `json:"ok"`
	Operation           string            `json:"operation"`
	Outcome             string            `json:"outcome"`
	RunID               string            `json:"run_id"`
	PlanID              string            `json:"plan_id,omitempty"`
	Agent               string            `json:"agent,omitempty"`
	DraftHash           string            `json:"draft_hash,omitempty"`
	ReviewHash          string            `json:"review_hash,omitempty"`
	AssetPath           string            `json:"asset_path"`
	AssetWritten        bool              `json:"asset_written"`
	AssetSHA256         string            `json:"asset_sha256,omitempty"`
	BaselineApplicable  bool              `json:"baseline_applicable"`
	BaselineAdvanced    bool              `json:"baseline_advanced"`
	AuditRecorded       bool              `json:"audit_recorded"`
	ApplicationRecorded bool              `json:"application_recorded"`
	ManifestApplied     bool              `json:"manifest_applied"`
	Attempted           int               `json:"attempted"`
	Applied             int               `json:"applied"`
	Rejected            int               `json:"rejected"`
	RejectKinds         string            `json:"reject_kinds,omitempty"`
	Warnings            []string          `json:"warnings"`
	Diagnostics         []string          `json:"diagnostics,omitempty"`
	Error               *applyErrorReport `json:"error,omitempty"`
	NextCommand         string            `json:"next_command,omitempty"`
	Recovery            string            `json:"recovery,omitempty"`
}

// entriesApplyJSONReport是Entries Apply的领域报告。
type entriesApplyJSONReport struct {
	applyReportBase

	Paths []string `json:"paths"`
}

// headerApplyJSONReport是Header Apply的领域报告。
type headerApplyJSONReport struct {
	applyReportBase

	BackupCreated bool `json:"backup_created"`
}

// curationApplyJSONReport是Curation Apply的领域报告。
type curationApplyJSONReport struct {
	applyReportBase

	Paths   []string `json:"paths"`
	Include int      `json:"include"`
	Exclude int      `json:"exclude"`
}

// applyFileState是一次文件观察，不直接进入JSON。
type applyFileState struct {
	Exists bool
	SHA256 string
	Info   any
}

// applyManifestState是一次Manifest观察。
type applyManifestState struct {
	Exists  bool
	Value   *draft.Manifest
	LoadErr error
}

// applyObservation保存执行前事实。
type applyObservation struct {
	Kind               string
	Root               string
	RunID              string
	AssetPath          string
	AssetRel           string
	BaselinePath       string
	BaselineApplicable bool
	BeforeAsset        applyFileState
	BeforeBaseline     applyFileState
	BeforeManifest     applyManifestState
	BeforeBackups      map[string]applyFileState
	DraftHash          string
	ReviewHash         string
	PlanID             string
	Agent              string
	Attempted          int
	Paths              []string
	Include            int
	Exclude            int
}

// newEntriesApplyJSONCmd构造生产命令树使用的Entries Apply包装器。
func newEntriesApplyJSONCmd() *cobra.Command {
	return newApplyJSONWrapper(
		applyKindEntries,
		newEntriesApplyCmd,
	)
}

// newHeaderApplyJSONCmd构造生产命令树使用的Header Apply包装器。
func newHeaderApplyJSONCmd() *cobra.Command {
	return newApplyJSONWrapper(
		applyKindHeader,
		newHeaderApplyCmd,
	)
}

// newAgentCurationApplyJSONCmd构造生产命令树使用的Curation Apply包装器。
func newAgentCurationApplyJSONCmd() *cobra.Command {
	return newApplyJSONWrapper(
		applyKindCuration,
		newAgentCurationApplyCmd,
	)
}

// newApplyJSONWrapper复用既有Apply实现，只在JSON模式增加前后状态观察。
func newApplyJSONWrapper(
	kind string,
	executionFactory func() *cobra.Command,
) *cobra.Command {
	template := executionFactory()

	command := &cobra.Command{
		Use:   template.Use,
		Short: template.Short,
		Long:  template.Long,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(
			cmd *cobra.Command,
			args []string,
		) error {
			execution := executionFactory()
			execution.SilenceUsage = true
			execution.SilenceErrors = true

			if !flagJSON {
				execution.SetOut(
					cmd.OutOrStdout(),
				)
				execution.SetErr(
					cmd.ErrOrStderr(),
				)
				return execution.RunE(
					execution,
					args,
				)
			}

			observation, err := prepareApplyObservation(
				kind,
				args,
			)
			if err != nil {
				return err
			}

			var captured bytes.Buffer
			execution.SetOut(
				&captured,
			)
			execution.SetErr(
				&captured,
			)

			runErr := execution.RunE(
				execution,
				args,
			)

			report := observation.buildReport(
				runErr,
				captured.String(),
			)

			if err := writeApplyJSON(
				cmd.OutOrStdout(),
				report,
			); err != nil {
				return &ExitError{
					Code: ExitInternal,
					Err: fmt.Errorf("%s", cliMessage(
						"apply.json_failed",
						localeSafeCLIDetail(err.Error()),
					)),
				}
			}

			if runErr != nil {
				return &ExitError{
					Code: executionExitCode(
						runErr,
					),
				}
			}

			return nil
		},
	}

	return command
}

// prepareApplyObservation解析Run并采集执行前事实。
func prepareApplyObservation(
	kind string,
	args []string,
) (*applyObservation, error) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	cfg, err := config.Load(
		repoRoot,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	runID, err := resolveApplyRunID(
		kind,
		repoRoot,
		args,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	paths := config.AOCIPaths(
		repoRoot,
		cfg.IndexPath,
	)

	observation := &applyObservation{
		Kind:          kind,
		Root:          repoRoot,
		RunID:         runID,
		Paths:         []string{},
		BeforeBackups: map[string]applyFileState{},
	}

	switch kind {
	case applyKindEntries:
		observation.AssetPath = paths.IndexPath
		observation.AssetRel = cfg.IndexPath
		observation.BaselineApplicable = true

	case applyKindHeader:
		observation.AssetPath = paths.IndexPath
		observation.AssetRel = cfg.IndexPath
		observation.BaselineApplicable = true

	case applyKindCuration:
		observation.AssetPath = paths.CurationPath
		observation.AssetRel = ".aoci/curation.json"

	default:
		return nil, &ExitError{
			Code: ExitInternal,
			Err:  fmt.Errorf("%s", cliMessage("apply.observation_kind_unknown", kind)),
		}
	}

	observation.BaselinePath = filepath.Join(
		repoRoot,
		".aoci",
		"baseline.json",
	)

	observation.BeforeAsset, err = readApplyFileState(
		observation.AssetPath,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"apply.before_asset_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	observation.BeforeBaseline, err = readApplyFileState(
		observation.BaselinePath,
	)
	if err != nil {
		return nil, &ExitError{
			Code: ExitInternal,
			Err: fmt.Errorf("%s", cliMessage(
				"apply.before_baseline_failed",
				localeSafeCLIDetail(err.Error()),
			)),
		}
	}

	observation.BeforeManifest = readApplyManifestState(
		repoRoot,
		runID,
	)

	observation.collectManifestFacts()
	observation.collectDraftFacts()

	if kind == applyKindHeader {
		observation.BeforeBackups = readApplyBackupStates(
			observation.AssetPath,
		)
	}

	return observation, nil
}

// resolveApplyRunID复用三类现有Run选择语义。
func resolveApplyRunID(
	kind,
	root string,
	args []string,
) (string, error) {
	switch kind {
	case applyKindEntries:
		return resolveEntriesRunID(
			root,
			args,
		)

	case applyKindHeader:
		return resolveHeaderRunID(
			root,
			args,
		)

	case applyKindCuration:
		return resolveCurationRunID(
			root,
			args,
		)

	default:
		return "", fmt.Errorf("%s", cliMessage("apply.kind_unknown", kind))
	}
}
