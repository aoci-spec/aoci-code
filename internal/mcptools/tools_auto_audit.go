// MCP Auto批次的紧凑Diff/P-23审计摘要。
package mcptools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type autoAudit struct {
	P23ContentSHA256 string   `json:"p23_content_sha256"`
	DiffFiles        int      `json:"diff_files"`
	Volume           string   `json:"volume,omitempty"`
	Volumes          []string `json:"volumes,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

func buildAutoAudit(input []updateEntryItemIn, outcome *AtomicBatchOutcome) *autoAudit {
	data, _ := json.Marshal(input)
	digest := sha256.Sum256(data)
	audit := &autoAudit{P23ContentSHA256: hex.EncodeToString(digest[:])}
	if outcome == nil {
		return audit
	}
	audit.DiffFiles = len(outcome.Items)
	audit.Volume = outcome.Volume
	audit.Volumes = append([]string{}, outcome.Volumes...)
	for _, item := range outcome.Items {
		if item == nil {
			continue
		}
		for _, warning := range item.Warnings {
			audit.Warnings = append(audit.Warnings, item.Rel+": "+warning)
		}
	}
	return audit
}
