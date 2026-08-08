package index

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeRulesMatchCompatibilityDigest(
	t *testing.T,
) {
	goldenPath := filepath.Join(
		"..",
		"..",
		"testdata",
		"golden",
		"runtime_rules.sha256",
	)

	expected, err := os.ReadFile(
		goldenPath,
	)
	if err != nil {
		t.Fatalf(
			"read runtime rules golden: %v",
			err,
		)
	}

	actual, err := BuildRuntimeRules(nil)
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256([]byte(actual))
	actualHash := fmt.Sprintf("%x", digest[:])
	expectedHash := strings.TrimSpace(string(expected))
	if actualHash != expectedHash {
		t.Fatalf(
			"runtime rules compatibility digest changed: actual=%s expected=%s",
			actualHash,
			expectedHash,
		)
	}
}
