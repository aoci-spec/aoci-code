// Header Apply恢复意图 —— 把Host-Agent生成计划的preimage、确定性postimage与
// 草稿摘要绑定到同一run，使正式索引已写而Baseline/Application未完成时可零写入收口。
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/draft"
	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

type headerApplyRecovery struct {
	Version         int    `json:"version"`
	RunID           string `json:"run_id"`
	DraftSHA256     string `json:"draft_sha256"`
	PreIndexSHA256  string `json:"pre_index_sha256"`
	PostIndexSHA256 string `json:"post_index_sha256"`
}

var removeHeaderRecoveryFile = os.Remove

func headerRecoveryPath(root, runID string) (string, error) {
	if _, err := draft.RunDir(root, runID); err != nil {
		return "", err
	}
	return filepath.Join(root, ".aoci", "transactions", "header-"+runID+".json"), nil
}

func saveHeaderRecovery(root string, recovery headerApplyRecovery) error {
	path, err := headerRecoveryPath(root, recovery.RunID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(recovery, "", "  ")
	if err != nil {
		return err
	}
	return afs.AtomicWrite(path, append(data, '\n'))
}

func loadHeaderRecovery(root, runID string) (*headerApplyRecovery, error) {
	path, err := headerRecoveryPath(root, runID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var recovery headerApplyRecovery
	if err := json.Unmarshal(data, &recovery); err != nil {
		return nil, err
	}
	if recovery.Version != 1 || recovery.RunID != runID ||
		recovery.DraftSHA256 == "" || recovery.PreIndexSHA256 == "" ||
		recovery.PostIndexSHA256 == "" {
		return nil, fmt.Errorf("%s", cliMessage("header.recovery.invalid"))
	}
	return &recovery, nil
}

func cleanupHeaderRecovery(root, runID string) error {
	path, err := headerRecoveryPath(root, runID)
	if err != nil {
		return err
	}
	if err := removeHeaderRecoveryFile(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
