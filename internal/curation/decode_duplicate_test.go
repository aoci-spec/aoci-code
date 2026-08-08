// 正式Curation资产重复JSON字段拒绝测试。
package curation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func duplicateDecisionJSON() string {
	return `{
  "version": 1,
  "decisions": [
    {
      "path": "a.bin",
      "decision": "include",
      "decision": "exclude",
      "role": "x",
      "reason": "y",
      "confidence": 98,
      "source_sha256": "` +
		strings.Repeat("a", 64) +
		`",
      "agent": "codex",
      "updated_at": "2026-07-16T00:00:00Z"
    }
  ]
}`
}

func TestDecodeDocumentRejectsDuplicateKeys(
	t *testing.T,
) {
	_, err := DecodeDocument(
		[]byte(duplicateDecisionJSON()),
		true,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"decisions[0].decision",
		) {
		t.Fatalf(
			"重复decision必须拒绝: %v",
			err,
		)
	}
}

func TestLoadRejectsDuplicateKeys(
	t *testing.T,
) {
	root := t.TempDir()
	target := FilePath(root)

	if err := os.MkdirAll(
		filepath.Dir(target),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		target,
		[]byte(duplicateDecisionJSON()),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, exists, _, err := Load(root)
	if !exists ||
		err == nil ||
		!strings.Contains(
			err.Error(),
			"decisions[0].decision",
		) {
		t.Fatalf(
			"正式资产重复decision必须拒绝: exists=%v err=%v",
			exists,
			err,
		)
	}
}
