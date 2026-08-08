// Curation草稿在Apply前的Generation Plan一致性和集合级恢复诊断。
package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/spf13/cobra"
)

func guardHostAgentCurationGeneration(
	cmd *cobra.Command,
	repoRoot string,
	cfg *config.Config,
	manifest *draft.Manifest,
) (string, error) {
	if manifest == nil {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				"Curation",
				fmt.Errorf("%s", cliMessage("curation.guard.manifest_empty")),
			),
		}
	}

	if err := validateHostAgentManifestState(
		manifest,
		draft.KindCuration,
		true,
	); err != nil {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				"Curation",
				err,
			),
		}
	}

	doc, indexPath, err := loadIndexForCLI(
		cmd,
		repoRoot,
		cfg,
	)
	if err != nil {
		return "", &ExitError{
			Code: ExitConfig,
			Err:  err,
		}
	}

	currentPlan, err := buildAgentPlan(
		repoRoot,
		cfg,
		doc,
		indexPath,
	)
	if err != nil {
		var buildErr *agentPlanBuildError
		if errors.As(
			err,
			&buildErr,
		) {
			return "", &ExitError{
				Code: buildErr.Code,
				Err:  buildErr.Err,
			}
		}

		return "", &ExitError{
			Code: ExitInternal,
			Err:  err,
		}
	}

	if currentPlan.Stage !=
		agentPlanStageCurationRequired ||
		currentPlan.PlanID != manifest.PlanID {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentPlanExpiredError(
				"Curation",
				manifest.PlanID,
				agentPlanStageCurationRequired,
				currentPlan.PlanID,
				currentPlan.Stage,
			),
		}
	}

	if currentPlan.IndexSHA256 != manifest.IndexSHA256 ||
		currentPlan.HeaderSHA256 != manifest.HeaderSHA256 ||
		currentPlan.CurationSHA256 != manifest.CurationSHA256 {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				"Curation",
				fmt.Errorf("%s", cliMessage("curation.guard.manifest_plan_drift")),
			),
		}
	}

	expected := currentAgentCurationBatch(
		currentPlan,
	)
	if err := validateHostAgentCurationTargets(
		manifest,
		expected,
	); err != nil {
		return "", &ExitError{
			Code: ExitInvalid,
			Err: hostAgentManifestDamageError(
				"Curation",
				err,
			),
		}
	}

	return cliMessage(
		"curation.guard.plan_ok",
		shortAgentStageHash(
			currentPlan.PlanID,
		),
		shortAgentStageHash(
			currentPlan.CurationSHA256,
		),
	), nil
}

// validateHostAgentCurationTargets按路径集合核对Manifest与当前完整批次。
//
// 顺序不构成身份；路径集合、唯一性、状态及源码摘要才构成Generation State。
func validateHostAgentCurationTargets(
	manifest *draft.Manifest,
	expected []agentPlanCurationTarget,
) error {
	if manifest == nil {
		return fmt.Errorf("%s", cliMessage("curation.guard.manifest_empty"))
	}
	if len(expected) == 0 {
		return fmt.Errorf("%s", cliMessage("curation.guard.batch_empty"))
	}

	expectedByPath := make(
		map[string]agentPlanCurationTarget,
		len(expected),
	)
	for _, target := range expected {
		expectedByPath[target.Path] = target
	}

	counts := map[string]int{}
	extraSet := map[string]bool{}
	sourceMismatch := []string{}
	invalidStatus := []string{}

	for position, status := range manifest.Entries {
		if status.Status != "drafted" {
			invalidStatus = append(
				invalidStatus,
				fmt.Sprintf(
					"%s=%q",
					status.Path,
					status.Status,
				),
			)
		}

		rel, err := afs.NormalizeRelPath(
			status.Path,
		)
		if err != nil {
			return fmt.Errorf("%s", cliMessage(
				"curation.guard.path_unsafe",
				position,
				status.Path,
				localeSafeCLIDetail(err.Error()),
			))
		}
		if rel != status.Path {
			return fmt.Errorf("%s", cliMessage(
				"curation.guard.path_noncanonical",
				position,
				status.Path,
			))
		}

		counts[rel]++

		target, found := expectedByPath[rel]
		if !found {
			extraSet[rel] = true
			continue
		}

		field := fmt.Sprintf(
			"manifest.entries[%d].source_sha256",
			position,
		)
		if err := validateManifestSHA256(
			field,
			status.SourceSHA256,
		); err != nil {
			return err
		}

		if status.SourceSHA256 != target.SourceSHA256 {
			sourceMismatch = append(
				sourceMismatch,
				fmt.Sprintf(
					"%s(submitted=%s,current=%s)",
					rel,
					shortAgentStageHash(
						status.SourceSHA256,
					),
					shortAgentStageHash(
						target.SourceSHA256,
					),
				),
			)
		}
	}

	missing := []string{}
	extra := []string{}
	duplicate := []string{}

	for _, target := range expected {
		if counts[target.Path] == 0 {
			missing = append(
				missing,
				target.Path,
			)
		}
	}

	for path := range extraSet {
		extra = append(
			extra,
			path,
		)
	}

	for path, count := range counts {
		if count > 1 {
			duplicate = append(
				duplicate,
				path,
			)
		}
	}

	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(duplicate)
	sort.Strings(sourceMismatch)
	sort.Strings(invalidStatus)

	parts := []string{}

	if len(missing) > 0 {
		parts = append(
			parts,
			"missing=["+
				strings.Join(missing, ",")+
				"]",
		)
	}
	if len(extra) > 0 {
		parts = append(
			parts,
			"extra=["+
				strings.Join(extra, ",")+
				"]",
		)
	}
	if len(duplicate) > 0 {
		parts = append(
			parts,
			"duplicate=["+
				strings.Join(duplicate, ",")+
				"]",
		)
	}
	if len(sourceMismatch) > 0 {
		parts = append(
			parts,
			"source_mismatch=["+
				strings.Join(sourceMismatch, ",")+
				"]",
		)
	}
	if len(invalidStatus) > 0 {
		parts = append(
			parts,
			"invalid_status=["+
				strings.Join(invalidStatus, ",")+
				"]",
		)
	}

	if len(parts) > 0 {
		return fmt.Errorf("%s", cliMessage(
			"curation.guard.targets_drift",
			strings.Join(parts, "; "),
		))
	}

	return nil
}
