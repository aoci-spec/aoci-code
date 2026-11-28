package cognitionbudget

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestEffectiveSFieldBandsUsePolicyAndMetaCeilings(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.S[len(policy.S)-1].MaxTokens = 65
	quota := index.ExtractSQuotaThresholds("#S配额: C2<=75")
	bands := EffectiveSFieldBands(policy.S, quota)
	for c, want := range map[int]int{1: 65, 2: 75, 3: 65, 4: 200, 9: 600} {
		original, _ := LimitFor(policy.S, c)
		reported, found := LimitFor(bands, c)
		if !found || reported.MaxTokens != want || reported.MaxTokens != SFieldMaxTokens(original, c, quota) ||
			reported.TargetTokens != original.TargetTokens {
			t.Errorf("C%d effective S band = %+v, want ceiling %d and advisory target %d", c, reported, want, original.TargetTokens)
		}
	}
	if band, _ := LimitFor(policy.S, 2); band.MaxTokens != 65 {
		t.Fatalf("display changed the persisted policy band: %+v", band)
	}
}

func TestEffectiveRFieldBandsMatchValidatorRunesAndBytes(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	bands := EffectiveRFieldBands(policy.R)
	want := map[int][3]int{
		9: {180, 360, 542},
		8: {140, 360, 422},
		7: {90, 272, 272},
		6: {90, 272, 272},
		5: {90, 272, 272},
		4: {50, 152, 152},
		3: {50, 152, 152},
		2: {50, 152, 152},
		1: {50, 152, 152},
	}
	find := func(c int) (EffectiveRFieldBand, bool) {
		for _, band := range bands {
			if c >= band.MinC && c <= band.MaxC {
				return band, true
			}
		}
		return EffectiveRFieldBand{}, false
	}
	for c, expected := range want {
		original, _ := LimitFor(policy.R, c)
		got, found := find(c)
		maxTokens, maxRunes, maxBytes := RFieldLimits(original)
		if !found || got.MaxTokens != expected[0] || got.MaxRunes != expected[1] || got.MaxBytes != expected[2] ||
			got.MaxTokens != maxTokens || got.MaxRunes != maxRunes || got.MaxBytes != maxBytes ||
			got.TargetTokens != original.TargetTokens {
			t.Fatalf("C%d effective R band = %+v, want tokens=%d runes=%d bytes=%d target=%d",
				c, got, expected[0], expected[1], expected[2], original.TargetTokens)
		}
		// Boundary proof against ValidateEntryWithSQuota: maxBytes passes, maxBytes+1 fails.
		okLine := fmt.Sprintf("main.go[C.RT.%d.T]: F:core | R:%s | A:- | S:-", c, strings.Repeat("a", got.MaxBytes))
		if vs := ValidateEntryWithSQuota(okLine, policy, nil); len(vs) != 0 {
			t.Fatalf("C%d R at effective maxBytes=%d must pass budget gate, got %+v", c, got.MaxBytes, vs)
		}
		overLine := fmt.Sprintf("main.go[C.RT.%d.T]: F:core | R:%s | A:- | S:-", c, strings.Repeat("a", got.MaxBytes+1))
		if vs := ValidateEntryWithSQuota(overLine, policy, nil); len(vs) != 1 || vs[0].Field != "R" || vs[0].Maximum != got.MaxTokens {
			t.Fatalf("C%d R at maxBytes+1=%d must fail budget gate at max_tokens=%d, got %+v", c, got.MaxBytes+1, got.MaxTokens, vs)
		}
	}
}

func TestRFieldLimitsSaturatesLargeTokenCeilingsWithoutOverflow(t *testing.T) {
	maxRunes := machinecontract.ObjectFRASV2RMaxRunes
	maxPossibleBytes := maxRunes * utf8.UTFMax
	hugeTokens := int(int64(1) << 62) // 4611686018427387904 on 64-bit platforms
	if math.MaxInt == math.MaxInt32 {
		hugeTokens = math.MaxInt
	}
	for _, tc := range []struct {
		name      string
		maxTokens int
		wantTok   int
		wantRunes int
		wantBytes int
	}{
		{name: "negative", maxTokens: -5, wantTok: 0, wantRunes: 0, wantBytes: 0},
		{name: "zero", maxTokens: 0, wantTok: 0, wantRunes: 0, wantBytes: 0},
		{name: "below_saturation", maxTokens: (maxPossibleBytes - 2) / 3, wantTok: (maxPossibleBytes - 2) / 3, wantRunes: maxRunes, wantBytes: maxPossibleBytes - 1},
		{name: "at_saturation", maxTokens: (maxPossibleBytes-2)/3 + 1, wantTok: (maxPossibleBytes-2)/3 + 1, wantRunes: maxRunes, wantBytes: maxPossibleBytes},
		{name: "huge_custom_band", maxTokens: hugeTokens, wantTok: hugeTokens, wantRunes: maxRunes, wantBytes: maxPossibleBytes},
		{name: "max_int", maxTokens: math.MaxInt, wantTok: math.MaxInt, wantRunes: maxRunes, wantBytes: maxPossibleBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotTok, gotRunes, gotBytes := RFieldLimits(FieldBand{MinC: 9, MaxC: 9, TargetTokens: 1, MaxTokens: tc.maxTokens})
			if gotTok != tc.wantTok || gotRunes != tc.wantRunes || gotBytes != tc.wantBytes {
				t.Fatalf("RFieldLimits(maxTokens=%d) = (%d, %d, %d), want (%d, %d, %d)",
					tc.maxTokens, gotTok, gotRunes, gotBytes, tc.wantTok, tc.wantRunes, tc.wantBytes)
			}
		})
	}

	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.R[0].MaxTokens = hugeTokens
	if _, err := Normalize(policy); err != nil {
		t.Fatalf("custom huge C9 R budget must normalize cleanly: %v", err)
	}
	bands := EffectiveRFieldBands(policy.R)
	if len(bands) == 0 || bands[0].MinC != 9 || bands[0].MaxC != 9 ||
		bands[0].MaxRunes != maxRunes || bands[0].MaxBytes != maxPossibleBytes {
		t.Fatalf("C9 effective R band under huge max_tokens = %+v, want runes=%d bytes=%d", bands[0], maxRunes, maxPossibleBytes)
	}
	okLine := "main.go[C.RT.9.T]: F:core | R:code:a.go | A:- | S:-"
	if vs := ValidateEntryWithSQuota(okLine, policy, nil); len(vs) != 0 {
		t.Fatalf("valid C9 entry must pass under huge custom R budget, got %+v", vs)
	}
}

func budgetFixture(body string) []byte {
	return []byte("# header\n===/repo/===\nmain.go[C.RT.9.T]: F:core | R:" + body + " | A:- | S:" + body + "\n")
}

func TestValidateEnforceRejectsWithoutRewriting(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 10, WarningTokens: 20, MaxTokens: 30}
	policy.R = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	policy.S = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	raw := budgetFixture(strings.Repeat("关系", 350))
	validation, err := Validate("/repo", raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Allowed || len(validation.Violations) != 2 {
		t.Fatalf("enforce mode allowed over-budget fields, or counted more than the two fields: %+v", validation)
	}
	for _, violation := range validation.Violations {
		if violation.Code != "entry_field_budget_exceeded" {
			t.Fatalf("a violation other than a field band was reported: %+v", violation)
		}
	}
	if string(raw) != string(budgetFixture(strings.Repeat("关系", 350))) {
		t.Fatal("validation changed model-authored bytes")
	}
}

// An index may be read outside an Agent's context window, so its total size
// names the status and nothing else: an index over every Whole-Index threshold,
// with each field inside its band, is allowed even in enforce mode.
func TestWholeIndexSizeIsNeverAViolation(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 1, WarningTokens: 2, MaxTokens: 3}
	raw := budgetFixture("one relationship")
	validation, err := Validate("/repo", raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Allowed || len(validation.Violations) != 0 || validation.Report.Status != machinecontract.BudgetStatusExceeded {
		t.Fatalf("the Whole-Index size refused an index whose fields are inside their bands: %+v", validation)
	}
	report := validation.Report
	if err := report.RebindWholeIndexTokens(1000000, policy); err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) != 0 || report.Status != machinecontract.BudgetStatusExceeded {
		t.Fatalf("rebinding a larger total turned the size into a violation: %+v", report)
	}
}

func TestObserveReportsButDoesNotBlock(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeObserve)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 1, WarningTokens: 2, MaxTokens: 3}
	raw := budgetFixture("one relationship")
	validation, err := Validate("/repo", raw, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Allowed || validation.Report.Status != machinecontract.BudgetStatusExceeded {
		t.Fatalf("observe mode must report exceeded without blocking: %+v", validation)
	}
}

func TestNormalizeRequiresCompleteNonOverlappingCBands(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.R = []FieldBand{{MinC: 1, MaxC: 8, TargetTokens: 1, MaxTokens: 2}}
	if _, err := Normalize(policy); err == nil {
		t.Fatal("C9 budget gap accepted")
	}
	policy = DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.S = append(policy.S, FieldBand{MinC: 9, MaxC: 9, TargetTokens: 1, MaxTokens: 2})
	if _, err := Normalize(policy); err == nil {
		t.Fatal("overlapping C9 budget accepted")
	}
}

func TestProjectedReportCarriesCompleteHardGateEvidence(t *testing.T) {
	policy := DefaultPolicy(machinecontract.BudgetModeEnforce)
	policy.WholeIndex = WholeIndexPolicy{TargetTokens: 15, WarningTokens: 20, MaxTokens: 25}
	policy.R = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	policy.S = []FieldBand{{MinC: 1, MaxC: 9, TargetTokens: 2, MaxTokens: 3}}
	current := budgetFixture("short")
	projected := budgetFixture(strings.Repeat("高熵关系", 180))
	report, err := ValidateProjected("/repo", current, projected, policy)
	if err != nil {
		t.Fatal(err)
	}
	if report.Allowed || report.CurrentTokens == 0 || report.ProjectedWholeIndexTokens <= report.MaxTokens ||
		report.BatchDeltaTokens <= 0 || report.TargetTokens != 15 || report.WarningTokens != 20 || report.MaxTokens != 25 ||
		len(report.LargestEntries) == 0 || len(report.LargestR) == 0 || len(report.LargestS) == 0 ||
		len(report.SuggestedCompression) == 0 || len(report.Violations) != 2 {
		t.Fatalf("projected hard-gate evidence incomplete: %+v", report)
	}
	if string(projected) != string(budgetFixture(strings.Repeat("高熵关系", 180))) {
		t.Fatal("projection validation rewrote model-authored semantics")
	}
}

func TestBuildRejectsIndexParseWarnings(t *testing.T) {
	raw := []byte("# header\n===/repo/===\nmain.go[C.RT.9.T]: F:one | R:- | A:- | S:-\n" +
		"main.go[C.RT.9.T]: F:duplicate | R:- | A:- | S:-\n")
	if _, err := Build("/repo", raw, DefaultPolicy(machinecontract.BudgetModeObserve)); err == nil ||
		!strings.Contains(err.Error(), "cognition_budget_index_parse_warnings") {
		t.Fatalf("malformed index received a budget report: %v", err)
	}
}

func TestBudgetStatisticsScaleToTenThousandEntries(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("# header\n===/repo/===\n")
	for index := 0; index < 10000; index++ {
		fmt.Fprintf(&builder, "entry-%05d.go[C.RT.5.T]: F:core | R:- | A:- | S:-\n", index)
	}
	report, err := Build("/repo", []byte(builder.String()), DefaultPolicy(machinecontract.BudgetModeObserve))
	if err != nil {
		t.Fatal(err)
	}
	if report.EntryCount != 10000 || len(report.LargestEntries) != 10 || report.WholeIndexTokens == 0 {
		t.Fatalf("large budget report incomplete: entries=%d largest=%d tokens=%d", report.EntryCount, len(report.LargestEntries), report.WholeIndexTokens)
	}
}

func TestTokenEstimatorUsesUTF8BytesAcrossLocales(t *testing.T) {
	if got := EstimateTokens([]byte("abc")); got != 1 {
		t.Fatalf("ASCII byte estimate=%d", got)
	}
	if got := EstimateTokens([]byte("认")); got != 1 {
		t.Fatalf("UTF-8 locale byte estimate=%d", got)
	}
	if got := EstimateTokens([]byte("abc认")); got != 2 {
		t.Fatalf("mixed-locale byte estimate=%d", got)
	}
}

func TestWholeIndexStatusBoundaries(t *testing.T) {
	policy := WholeIndexPolicy{TargetTokens: 10, WarningTokens: 20, MaxTokens: 30}
	for _, testCase := range []struct {
		tokens int
		want   string
	}{
		{tokens: 10, want: machinecontract.BudgetStatusHealthy},
		{tokens: 11, want: machinecontract.BudgetStatusNearBudget},
		{tokens: 20, want: machinecontract.BudgetStatusWarning},
		{tokens: 30, want: machinecontract.BudgetStatusWarning},
		{tokens: 31, want: machinecontract.BudgetStatusExceeded},
	} {
		if got := statusFor(testCase.tokens, policy); got != testCase.want {
			t.Fatalf("tokens=%d status=%s want=%s", testCase.tokens, got, testCase.want)
		}
	}
}

// legacyBudgetIdentity is the digest every repository without a cognition_budget
// block has stamped in its Baseline. It is a persisted preimage, not a constant
// that may be refreshed to match the code: if this test fails, the change that
// broke it would have forced a human-approved Scope Change on every such
// repository, so revert the change rather than updating the literal.
const legacyBudgetIdentity = "a98988839d1818e2faa245e355c256be3698f7a3552edf87338cb8ce48444eb7"

func TestLegacyPolicyIdentityIsFrozen(t *testing.T) {
	identity, err := Identity(LegacyPolicy())
	if err != nil {
		t.Fatalf("legacy identity: %v", err)
	}
	if identity != legacyBudgetIdentity {
		t.Fatalf("the legacy budget preimage moved: want %s got %s\n"+
			"every repository whose config.json has no cognition_budget block resolves this policy, "+
			"so this change wedges all of them on a Scope Change they did not earn",
			legacyBudgetIdentity, identity)
	}
}

// The two functions were one until rc6. DefaultPolicy is free to move because
// only init writes it; LegacyPolicy is not. Re-collapsing them silently
// re-arms the wedge, so the decoupling itself is pinned.
func TestLegacyPolicyIsDecoupledFromNewProjectDefault(t *testing.T) {
	if LegacyPolicy().WholeIndex == DefaultPolicy(machinecontract.BudgetModeObserve).WholeIndex {
		t.Fatal("LegacyPolicy and DefaultPolicy carry the same Whole-Index numbers again; " +
			"if that is deliberate, delete this test only together with a release note explaining " +
			"why moving the new-project default may now move the legacy preimage")
	}
	if _, err := Normalize(DefaultPolicy(machinecontract.BudgetModeEnforce)); err != nil {
		t.Fatalf("new-project default must stay normalizable: %v", err)
	}
}
