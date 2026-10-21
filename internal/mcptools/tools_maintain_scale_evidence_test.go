package mcptools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"strings"
	"testing"
)

// The source and Entries here are synthetic. Each lane must expose the same
// source-size facts before asking the host to author a complete Entry.
func TestVolumeMaintainCarriesSourceScaleEvidence(t *testing.T) {
	for _, lane := range []string{"create", "update", "optimization"} {
		t.Run(lane, func(t *testing.T) {
			root := buildVolumeRepoWithMeta(t, true, false, divergentEScaleMeta)
			name := "main.go"
			if lane == "create" {
				name = "sample.go"
			}
			writeVolumeTestFile(t, root, name, "package main\n\nfunc sample() {\n}")
			args := map[string]any{}
			if lane == "optimization" {
				refreshVolumeBaseline(t, root)
				args["intent"] = "cognition_optimization"
				args["object_refs"] = []string{"code:main.go"}
			}
			formal := cognitionOptimizationFormalPreimages(t, root, []string{"aoci.code.txt", ".aoci/baseline.json"})
			text := callVolumeTool(t, connectMCPClient(t, root), "aoci_maintain", args)
			var result struct {
				Candidates []struct {
					Path         string   `json:"path"`
					SourceSHA256 string   `json:"source_sha256"`
					FileLines    *int     `json:"file_lines"`
					ExpectedE    []string `json:"expected_e"`
				} `json:"candidates"`
				SemanticGenerated bool `json:"semantic_generated"`
			}
			if err := json.Unmarshal([]byte(text), &result); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, candidate := range result.Candidates {
				if candidate.Path != name {
					continue
				}
				found = true
				if candidate.FileLines == nil || *candidate.FileLines != 4 || strings.Join(candidate.ExpectedE, "/") != "T" {
					t.Fatalf("%s candidate lacks source-bound scale evidence: lines=%v expected=%v", lane, candidate.FileLines, candidate.ExpectedE)
				}
				if candidate.SourceSHA256 != sourceSHA256(t, root, name) {
					t.Fatal("source identity changed")
				}
			}
			if !found || result.SemanticGenerated {
				t.Fatalf("missing candidate or generated semantics: %s", text)
			}
			after := cognitionOptimizationFormalPreimages(t, root, []string{"aoci.code.txt", ".aoci/baseline.json"})
			for path, before := range formal {
				if string(after[path]) != string(before) {
					t.Fatalf("Maintain changed formal asset %s", path)
				}
			}
		})
	}
}

func TestVolumeScaleEvidenceBoundariesAndUnavailableSources(t *testing.T) {
	for _, test := range []struct {
		name, content, bands, expected string
		lines                          int
		changed, missing               bool
	}{
		{name: "empty", bands: "T<100 L>400", expected: "T"},
		{name: "CRLF", content: "a\r\nb\r\n", bands: "T<100 L>400", expected: "T", lines: 2},
		{name: "unterminated", content: "a\nb", bands: "T<100 L>400", expected: "T", lines: 2},
		{name: "overlap", content: strings.Repeat("x\n", 200), bands: "T<100 S100-200 M200-400 L>400", expected: "M/S", lines: 200},
		{name: "custom", content: "a\nb\nc\nd", bands: "T<3 S3-8 L>8", expected: "S", lines: 4},
		{name: "gap", content: "a\nb\nc\nd", bands: "T<3 L>8", lines: 4},
		{name: "unparseable", content: "a\nb", bands: "T-tiny L-large", lines: 2},
		{name: "source changed", content: "a\nb", bands: "T<100", changed: true},
		{name: "source absent", content: "a\nb", bands: "T<100", missing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "sample.txt")
			if !test.missing {
				if err := os.WriteFile(source, []byte(test.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(test.content)))
			if test.changed {
				digest = strings.Repeat("0", 64)
			}
			candidate := volumeMaintainCandidate{Domain: cognition.ScopeCode, Path: "sample.txt", SourceSHA256: digest, ModelAuthoringOnly: true}
			thresholds := index.ExtractEScaleThresholds("#E Scale: " + test.bands + "\n")
			fillVolumeCandidateScale(root, thresholds, &candidate)
			if test.changed || test.missing {
				if candidate.FileLines != nil || len(candidate.ExpectedE) != 0 {
					t.Fatal("unavailable source acquired scale evidence")
				}
				return
			}
			if candidate.FileLines == nil || *candidate.FileLines != test.lines || strings.Join(candidate.ExpectedE, "/") != test.expected {
				t.Fatalf("unexpected evidence: %+v", candidate)
			}
			plain := candidate
			plain.FileLines = nil
			plain.ExpectedE = nil
			with, _ := json.Marshal(candidate)
			without, _ := json.Marshal(plain)
			set := &cognition.Set{Meta: cognition.Asset{Raw: []byte("#E Scale: " + test.bands + "\n")}}
			if len(with)-len(without) > scaleEvidenceWireReserve(set) {
				t.Fatal("scale facts exceed their transport reservation")
			}
		})
	}
}
