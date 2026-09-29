package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/textassets"
)

// rc16 took code_plan.candidates off the wire, but the repair text for a
// mistyped or self-computed binding kept telling the model to copy the value
// from there. Two first builds on rc17 stalled on exactly that: one candidate
// carried a source_sha256 the model had computed itself, the batch was refused
// with zero writes (correct), and the repair action pointed at a field the
// response no longer had. The text now names the places that exist: the
// Maintain response's candidates list and the finding's own expected value.
func TestSourceBindingRepairTextNamesLivingFieldsInBothLocales(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		if err := textassets.SetActiveLocale(locale); err != nil {
			t.Fatal(err)
		}
		for _, rule := range []string{"code_candidate_source_sha256_mismatch", "code_candidate_id_mismatch", "code_candidate_path_mismatch"} {
			finding := cognition.RepairFinding{RuleCode: rule, Field: "source_sha256",
				Expected: strings.Repeat("a", 64), Actual: strings.Repeat("0", 64)}
			action := safeRepairAction(finding)
			if strings.Contains(action, "code_plan.candidates") || !strings.Contains(action, "candidates") || !strings.Contains(action, "expected") {
				t.Fatalf("%s %s repair action must point at the candidates list and the finding's expected value, never at code_plan.candidates: %q", locale, rule, action)
			}
		}
		finding := cognition.RepairFinding{RuleCode: "code_candidate_source_sha256_mismatch", Field: "source_sha256"}
		localizeFRASCause(&finding)
		if finding.Cause == "" || !(strings.Contains(finding.Cause, "still matches") || strings.Contains(finding.Cause, "仍与签发值一致")) {
			t.Fatalf("%s mismatch cause must state that the file on disk still matches the issued binding: %q", locale, finding.Cause)
		}
		action := safeRepairAction(finding)
		if !(strings.Contains(action, "never compute") || strings.Contains(action, "绝不要自己计算")) {
			t.Fatalf("%s mismatch action must forbid computing the hash: %q", locale, action)
		}
	}
}

// A well-formed but wrong source_sha256 reaches the model as the mismatch
// finding whose expected value is the issued binding, so the repair needs no
// field the response lacks.
func TestWrongSourceBindingFindingCarriesTheIssuedValue(t *testing.T) {
	root := buildLargeCodeCandidateRepo(t, 3)
	session := connectMCPClient(t, root)
	maintain := maintainVolumeBatch(t, session)
	if maintain.CodePlan == nil || len(maintain.Candidates) != 3 {
		t.Fatalf("expected three candidates: %#v", maintain.CodePlan)
	}
	arguments := codeBatchArguments(wireCodePlan(maintain))
	entries := arguments["entries"].([]map[string]any)
	entries[1]["source_sha256"] = strings.Repeat("0", 64)
	result := applyVolumeBatch(t, session, arguments)
	if result.Status != autoStatusRepairRequired || result.Applied != 0 || result.FormalWritesStarted || len(result.Findings) != 1 {
		t.Fatalf("wrong binding must stop before any write with one finding: %#v", result)
	}
	finding := result.Findings[0]
	if finding.RuleCode != "code_candidate_source_sha256_mismatch" || finding.CandidateIndex != 2 ||
		finding.Expected != maintain.Candidates[1].SourceSHA256 || finding.Actual != strings.Repeat("0", 64) {
		t.Fatalf("finding must carry the issued binding as expected and the submitted value as actual: %#v", finding)
	}
	if strings.Contains(finding.SafeRepairAction, "code_plan.candidates") || !strings.Contains(finding.SafeRepairAction, "expected") {
		t.Fatalf("repair action still points at a field the response lacks: %q", finding.SafeRepairAction)
	}
	entries[1]["source_sha256"] = maintain.Candidates[1].SourceSHA256
	if again := applyVolumeBatch(t, session, arguments); again.Status != "applied" || again.Applied != 3 {
		t.Fatalf("copying the expected value must complete the same batch: %#v", again)
	}
}
