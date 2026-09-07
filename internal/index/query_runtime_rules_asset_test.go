package index

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// TestRuntimeRulesMatchCompatibilityDigest locks the assembled runtime-rules
// rendering for BOTH official locales, so an edit to either locale asset (or
// to the appended machine-fact lines) requires a deliberate golden update.
func TestRuntimeRulesMatchCompatibilityDigest(
	t *testing.T,
) {
	original := textassets.ActiveLocale()
	defer func() {
		if err := textassets.SetActiveLocale(original); err != nil {
			t.Fatal(err)
		}
	}()

	for _, tc := range []struct {
		locale string
		golden string
	}{
		{textassets.LegacyLocale, "runtime_rules.sha256"},
		{textassets.DefaultLocale, "runtime_rules_en-US.sha256"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			goldenPath := filepath.Join(
				"..",
				"..",
				"testdata",
				"golden",
				tc.golden,
			)

			if err := textassets.SetActiveLocale(tc.locale); err != nil {
				t.Fatal(err)
			}

			actual, err := BuildRuntimeRules(nil)
			if err != nil {
				t.Fatal(err)
			}

			digest := sha256.Sum256([]byte(actual))
			actualHash := fmt.Sprintf("%x", digest[:])
			if os.Getenv("AOCI_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, []byte(actualHash+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read runtime rules golden: %v", err)
			}
			expectedHash := strings.TrimSpace(string(expected))
			if actualHash != expectedHash {
				t.Fatalf(
					"runtime rules compatibility digest changed: locale=%s actual=%s expected=%s",
					tc.locale,
					actualHash,
					expectedHash,
				)
			}
		})
	}
}
