// Boundary tests of the maintain package against the single source of truth for curation matching.
// Index entry pending: tools_maintain_curation_test.go
//
// This file no longer tests the private mcptools matching function, which has been deleted. maintain relies uniformly on
// config.Config.CurationExcluded (v1.1), and this test locks the three key semantics, bare directory, directory boundary,
// and single file, from the consuming package's side, so nobody restores a local copy in mcptools later.
package mcptools

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestMaintainCurationMatcherContract(t *testing.T) {
	cfg := &config.Config{CurationExclude: []string{"docs", "README.md"}}

	cases := []struct {
		rel  string
		want bool
	}{
		{"docs/api.md", true},
		{"docs2/api.md", false},
		{"README.md", true},
		{"README.md.bak", false},
		{"src/main.go", false},
	}

	for _, tc := range cases {
		if got := cfg.CurationExcluded(tc.rel); got != tc.want {
			t.Fatalf("Config.CurationExcluded(%q)=%v, want %v", tc.rel, got, tc.want)
		}
	}
}
