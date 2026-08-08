// `aoci index agent curation` 文件级语义策展命令组。
package cli

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/spf13/cobra"
)

func newIndexAgentCurationCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "curation",
		Short: cliMessage("cli.short.agent_curation"),
	}

	command.AddCommand(
		newAgentCurationStageCmd(),
	)
	command.AddCommand(
		newAgentCurationDiffCmd(),
	)
	command.AddCommand(
		newAgentCurationApplyJSONCmd(),
	)

	return command
}

func resolveCurationRunID(
	root string,
	args []string,
) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	runID, err := draft.LatestRunID(
		root,
		draft.KindCuration,
	)
	if err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"curation.run_not_found",
			localeSafeCLIDetail(err.Error()),
		))
	}

	return runID, nil
}

type curationDraftSnapshot struct {
	Hash     string
	Document *curation.Document
}

func loadCurationDraftSnapshot(
	root,
	runID string,
) (*curationDraftSnapshot, error) {
	files, hash, err := draft.ReadFilesSnapshot(
		root,
		runID,
		[]string{
			draft.CurationFileName,
		},
	)
	if err != nil {
		return nil, err
	}

	data, found := files[draft.CurationFileName]
	if !found {
		return nil, fmt.Errorf("%s", cliMessage(
			"curation.snapshot.missing_file",
			draft.CurationFileName,
		))
	}

	document, err := curation.DecodeDocument(
		data,
		false,
	)
	if err != nil {
		return nil, err
	}

	return &curationDraftSnapshot{
		Hash:     hash,
		Document: document,
	}, nil
}

func guardReviewedCurationHash(
	manifest *draft.Manifest,
	currentHash string,
) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("%s", cliMessage("curation.review.manifest_empty"))
	}

	for position := len(manifest.Reviews) - 1; position >= 0; position-- {
		review := manifest.Reviews[position]
		if review.Action != draft.ReviewActionDiff {
			continue
		}

		if review.DraftHash == "" {
			return "", fmt.Errorf("%s", cliMessage("curation.review.hash_missing"))
		}
		if review.DraftHash != currentHash {
			return "", fmt.Errorf("%s", cliMessage(
				"curation.review.hash_drift",
				shortDraftHash(review.DraftHash),
				shortDraftHash(currentHash),
			))
		}

		return "", nil
	}

	return cliMessage("curation.review.legacy_missing"), nil
}
