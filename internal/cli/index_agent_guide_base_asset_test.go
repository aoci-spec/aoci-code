package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func renderGuideGoldenSubject(
	guide *agentGuide,
) string {
	return "MESSAGE:\n" +
		guide.Message +
		"\nINSTRUCTIONS:\n" +
		strings.Join(
			guide.Instructions,
			"\n",
		) +
		"\n"
}

func assertGuideMatchesGolden(
	t *testing.T,
	guide *agentGuide,
	name string,
) {
	t.Helper()

	expected, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			name,
		),
	)
	if err != nil {
		t.Fatalf(
			"read Guide golden %s: %v",
			name,
			err,
		)
	}

	actual := renderGuideGoldenSubject(
		guide,
	)

	if actual != string(expected) {
		t.Fatalf(
			"Guide text changed during asset migration: golden=%s actual_bytes=%d expected_bytes=%d\n%s",
			name,
			len(actual),
			len(expected),
			actual,
		)
	}
}

func TestBaselineFirstGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageBaselineRequired,
	)
	plan.BaselineExists = false

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertGuideMatchesGolden(
		t,
		guide,
		"guide_baseline_first.txt",
	)
}

func TestBaselineBlockedGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageBaselineRequired,
	)
	plan.BaselineExists = true
	plan.Unbaselined = []string{
		"new.go",
	}
	plan.Summary.Unbaselined = 1

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertGuideMatchesGolden(
		t,
		guide,
		"guide_baseline_blocked.txt",
	)
}

func TestObserveGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	plan.AutomationMode =
		config.AutomationModeOff
	plan.Targets = []agentPlanTarget{
		{
			Path: "x.go",
			Kind: "create",
			SourceSHA256: strings.Repeat(
				"a",
				64,
			),
		},
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertGuideMatchesGolden(
		t,
		guide,
		"guide_observe.txt",
	)
}

func TestAlignedCleanGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageAligned,
	)

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertGuideMatchesGolden(
		t,
		guide,
		"guide_aligned_clean.txt",
	)
}

func TestAlignedExplainedGuideMatchesGoldenByteForByte(
	t *testing.T,
) {
	plan := guideTestPlan(
		agentPlanStageAligned,
	)
	plan.Summary.Missing = 1
	plan.Summary.CurationExcluded = 1
	plan.CurationExcluded = []string{
		"docs/img/example.png",
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertGuideMatchesGolden(
		t,
		guide,
		"guide_aligned_explained.txt",
	)
}

func TestGuideResourcePreflightIsBranchLocal(t *testing.T) {
	plan := guideTestPlan(agentPlanStageEntriesRequired)
	plan.AutomationMode = config.AutomationModeAuto
	policy, err := resolveAgentAutomationPolicy(plan.AutomationMode)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := requiredGuideAssetIDs(plan, policy)
	if err != nil {
		t.Fatal(err)
	}
	want := []textassets.ID{
		textassets.ContractGuideBaseInstructions,
		textassets.ContractGuideEntriesBaseInstructions,
		textassets.ContractGuideEntriesAutoMessage,
		textassets.ContractGuideEntriesAutoInstructions,
	}
	if len(ids) != len(want) {
		t.Fatalf("unexpected resource preflight: %v", ids)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("resource preflight[%d]=%q want %q", index, ids[index], want[index])
		}
	}
}
