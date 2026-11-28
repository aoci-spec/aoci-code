// Contract tests for the structured repair of budget enforce violations.
//
// Background (a real incident): in a zh-CN index the token count of S, counted as UTF-8 bytes/3, hits the importance band ceiling before the rune
// quota does, and that refusal path once passed an int to a %s template; the strict asset rendering failed and triggered a
// writeMessage panic, so a candidate-repairable budget violation was escalated into an unlocatable internal error, and the host was forced
// to bisect in an isolated copy. Locked here: that path always returns repair_required with complete locations.
package mcptools

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
)

// activateEnforceBudget fits the Volumes fixture with a managed scope and an enforce budget receipt;
// the default fixture lands in LegacyPolicy (observe) and never reaches the enforce refusal path.
func activateEnforceBudget(t *testing.T, root string) {
	t.Helper()
	activateEnforceBudgetPolicy(t, root, cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
}

func activateEnforceBudgetPolicy(t *testing.T, root string, rawBudget cognitionbudget.Policy) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := managedscope.Normalize(managedscope.DefaultPolicy(machinecontract.ScopeProfileProduction))
	if err != nil {
		t.Fatal(err)
	}
	budget, err := cognitionbudget.Normalize(rawBudget)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope, cfg.CognitionBudget = &policy, &budget
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	files, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("加载夹具 Baseline 失败: exists=%t err=%v", exists, err)
	}
	budgetIdentity, err := cognitionbudget.Identity(budget)
	if err != nil {
		t.Fatal(err)
	}
	state.Files = files
	state.ManagedScope = &baseline.ManagedScopeState{Version: machinecontract.ManagedScopeBaselineV1,
		PolicyIdentity: evaluation.PolicyIdentity, ObserveChangePolicy: machinecontract.ObserveChangeReviewRequired,
		BudgetPolicyIdentity: budgetIdentity, BudgetPolicy: &budget}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
}

// The hard ceiling of S is Meta's rune quota (#146): 210 Chinese characters = 630 bytes = 210 tokens exceed
// the C9 band of 200 tokens but stay within the 600-rune quota; the authoring contract tells the model exactly
// that quota, the token band must not be tighter than it, and this Entry must be written as it is.
func TestCJKSWithinRuneQuotaIsNotABudgetViolation(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	activateEnforceBudget(t, root)
	line := "main.go[CD9S]: F:run the fixture | R:- | A:main | S:" + strings.Repeat("约", 210)
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"), NewEntry: line,
	}}))
	if result.Status != autoStatusApplied || result.Applied != 1 || len(result.Findings) != 0 {
		t.Fatalf("配额内的中文 S 被 token band 拒绝: %#v", result)
	}
	if !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), line) {
		t.Fatal("写入后的 Code Volume 没有这条 Entry")
	}
}

func TestSMaximumReportedInMaintainAndRepair(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 1)
	activateEnforceBudget(t, root)
	session := connectMCPClient(t, root)
	response := maintainJSON(t, session, nil)
	budget := response["governance"].(map[string]any)["budget"].(map[string]any)
	for _, check := range []struct{ c, want float64 }{{3, 50}, {4, 200}, {9, 600}} {
		matches := 0
		for _, item := range budget["s"].([]any) {
			band := item.(map[string]any)
			if band["min_c"].(float64) <= check.c && check.c <= band["max_c"].(float64) {
				matches++
				if band["max_tokens"] != check.want {
					t.Errorf("C%.0f effective S max = %v, want %.0f tokens", check.c, band["max_tokens"], check.want)
				}
			}
		}
		if matches != 1 {
			t.Errorf("C%.0f matched %d S budget bands, want one", check.c, matches)
		}
	}
	instructions := fmt.Sprint(response["instructions"])
	for _, want := range []string{"C3-1<=50", "C7-4<=200", "C9-8<=600"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("S guidance lacks %s", want)
		}
	}
	for _, check := range []struct{ name, value, rule, expected, actual, unit string }{
		{"tokens", strings.Repeat("😀", 39), "entry_field_budget_exceeded", "max_tokens=50", "actual_tokens=52", "50 tokens"},
		{"runes", strings.Repeat("a", 51), "fras_s_too_long", "max_runes=50", "rune_count=51", "50个Unicode字符"},
	} {
		t.Run(check.name, func(t *testing.T) {
			result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
				Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"),
				NewEntry: "main.go[CD3S]: F:run the fixture | R:- | A:main | S:" + check.value,
			}}))
			if result.Status != autoStatusRepairRequired || result.FormalWritesStarted || len(result.Findings) != 1 {
				t.Fatalf("expected one zero-write repair: %#v", result)
			}
			finding := result.Findings[0]
			if finding.RuleCode != check.rule || finding.Field != "S" || finding.Expected != check.expected ||
				finding.Actual != check.actual || !strings.Contains(finding.Cause, check.unit) {
				t.Fatalf("S finding must state the effective limit and unit: %+v", finding)
			}
		})
	}
}

func TestCustomMetaSQuotaMatchesMaintainAndApplyInAllSections(t *testing.T) {
	for _, section := range []string{"preamble", "code", "database"} {
		t.Run(section, func(t *testing.T) {
			root := buildManyFileVolumesRepo(t, 1)
			const quota = "#S quota: C1<=75\n"
			meta := volumeFileText(t, root, "aoci.meta.txt")
			switch section {
			case "preamble":
				meta = strings.Replace(meta, "#[Tag dictionary: code]", quota+"#[Tag dictionary: code]", 1)
			case "code":
				meta = strings.Replace(meta, "#[Tag dictionary: database]", quota+"#[Tag dictionary: database]", 1)
			case "database":
				meta += "\n" + quota
			}
			writeVolumeTestFile(t, root, "aoci.meta.txt", meta)
			activateEnforceBudget(t, root)
			session := connectMCPClient(t, root)
			response := maintainJSON(t, session, nil)
			budget := response["governance"].(map[string]any)["budget"].(map[string]any)
			var maximum any
			for _, item := range budget["s"].([]any) {
				band := item.(map[string]any)
				if band["min_c"] == float64(1) {
					maximum = band["max_tokens"]
				}
			}
			if maximum != float64(75) || !strings.Contains(fmt.Sprint(response["instructions"]), "C1<=75") {
				t.Fatalf("Maintain must report the effective limit of 75 tokens: %v", maximum)
			}
			apply := func(runes int) autoResult {
				t.Helper()
				return decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
					Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"),
					NewEntry: "main.go[CD1S]: F:run the fixture | R:- | A:main | S:" + strings.Repeat("😀", runes),
				}}))
			}
			over := apply(57)
			if over.Status != autoStatusRepairRequired || len(over.Findings) != 1 ||
				over.Findings[0].Expected != "max_tokens=75" || over.Findings[0].Actual != "actual_tokens=76" {
				t.Errorf("Apply must enforce the same 75-token limit: status=%s findings=%+v", over.Status, over.Findings)
			}
			if valid := apply(56); valid.Status != autoStatusApplied {
				t.Fatalf("Apply refused 74 tokens within both limits: status=%s findings=%+v", valid.Status, valid.Findings)
			}
		})
	}
}

func TestBudgetEnforceViolationReturnsStructuredRepair(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	activateEnforceBudget(t, root)
	indexBefore := volumeFileText(t, root, "aoci.code.txt")
	// The C3 band's R ceiling is 50 tokens (150 bytes); 8 canonical identities total 191 bytes = 63 tokens, over the band,
	// but 8 items and 191 runes are both within the FRAS v2 machine ceiling for R, so only the token budget layer refuses.
	refs := make([]string, 0, 8)
	for position := 0; position < 8; position++ {
		refs = append(refs, fmt.Sprintf("code:src/relation%02d.go", position))
	}
	relation := strings.Join(refs, ",")
	line := "main.go[CD3S]: F:run the fixture | R:" + relation + " | A:main | S:-"
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"), NewEntry: line,
	}}))
	if result.Status != autoStatusRepairRequired || result.Applied != 0 {
		t.Fatalf("预算违规必须是零写入的 repair_required, 不是内部错误: %#v", result)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("必须恰好一条定位 Finding: %#v", result.Findings)
	}
	finding := result.Findings[0]
	if finding.RuleCode != "entry_field_budget_exceeded" || finding.Field != "R" ||
		finding.CandidateIndex != 1 || finding.CanonicalObjectIdentity != "code:main.go" ||
		finding.Expected != "max_tokens=50" || finding.Actual != fmt.Sprintf("actual_tokens=%d", len(relation)/3) ||
		finding.Cause == "" || finding.SafeRepairAction == "" {
		t.Fatalf("Finding 缺定位或精确 token 事实: %+v", finding)
	}
	if len(result.RetryScope) != 1 || result.RetryScope[0] != "code:main.go" {
		t.Fatalf("retry_scope 必须只指向违规候选: %#v", result.RetryScope)
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("拒绝必须零正式写入")
	}
}

// Split logic: a field violation hitting a candidate of this batch is repairable; the historical violations of entries outside the
// batch always go to unrepairable, and the caller keeps the batch-level stop.
func TestBudgetRepairFindingsSplitsCandidateAndBatchLevel(t *testing.T) {
	normalized := []normalizedAtomicItem{
		{rel: "src/a.go", originalCandidateIndex: 3},
		{objectRef: "database://primary/public/users", originalCandidateIndex: 7},
	}
	violations := []cognitionbudget.Violation{
		{Code: "entry_field_budget_exceeded", Path: "code:src/a.go", Field: "S", Importance: 5, Actual: 82, Maximum: 80},
		{Code: "entry_field_budget_exceeded", Path: "database://primary/public/users", Field: "R", Importance: 6, Actual: 101, Maximum: 90},
		{Code: "entry_field_budget_exceeded", Path: "code:not/in/batch.go", Field: "S", Actual: 99, Maximum: 80},
		{Code: "entry_field_budget_exceeded", Path: "code:also/outside.go", Field: "R", Actual: 95, Maximum: 90},
	}
	findings, unmatched := budgetRepairFindings(violations, normalized)
	if len(findings) != 2 || len(unmatched) != 2 {
		t.Fatalf("拆分不对: findings=%d unmatched=%d", len(findings), len(unmatched))
	}
	if findings[0].CandidateIndex != 3 || findings[0].Domain != "code" || findings[0].Field != "S" {
		t.Fatalf("code 候选映射不对: %+v", findings[0])
	}
	if findings[1].CandidateIndex != 7 || findings[1].Domain != "database" ||
		findings[1].CanonicalObjectIdentity != "database://primary/public/users" {
		t.Fatalf("database 候选映射不对: %+v", findings[1])
	}
	// The message names unmatched[0], so that is the fact string a repository sees.
	for _, want := range []string{"entry_field_budget_exceeded", "code:not/in/batch.go", "S", "99", "80"} {
		if detail := budgetViolationDetail(unmatched[0]); !strings.Contains(detail, want) {
			t.Fatalf("批级违规事实串缺少 %q: %q", want, detail)
		}
	}
}

// One repair_required names everything a candidate must fix. The field budget
// used to be measured only after every candidate parsed, so a batch refused
// for a length defect learned its budget overruns on the next submission, one
// model round and one CAS later (#168).
func TestLengthAndBudgetFindingsArriveInOneRepair(t *testing.T) {
	root := buildSingleCodeWriteRepo(t, false)
	activateEnforceBudget(t, root)
	indexBefore := volumeFileText(t, root, "aoci.code.txt")
	refs := make([]string, 0, 8)
	for position := 0; position < 8; position++ {
		refs = append(refs, fmt.Sprintf("code:src/relation%02d.go", position))
	}
	longF := strings.Repeat("runs the fixture and explains it ", 7) // 231 runes, over the FRAS v2 ceiling of 160
	line := "main.go[CD3S]: F:" + longF + " | R:" + strings.Join(refs, ",") + " | A:main | S:-"
	result := decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
		Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"), NewEntry: line,
	}}))
	if result.Status != autoStatusRepairRequired || result.Applied != 0 {
		t.Fatalf("must be a zero-write repair_required: %#v", result)
	}
	rules := map[string]int{}
	for _, finding := range result.Findings {
		if finding.CandidateIndex != 1 || finding.CanonicalObjectIdentity != "code:main.go" {
			t.Fatalf("finding not bound to the candidate: %+v", finding)
		}
		rules[finding.RuleCode]++
	}
	if rules["fras_f_too_long"] != 1 || rules["entry_field_budget_exceeded"] != 1 || len(result.Findings) != 2 {
		t.Fatalf("length and budget findings must arrive together: %v\n%#v", rules, result.Findings)
	}
	if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
		t.Fatal("the refusal must write nothing")
	}
}

// Joint contract test for R's two independent enforcement layers (cognitionbudget
// UTF-8 byte/token ceiling and FRAS v2 Unicode rune ceiling) plus Maintain's
// effective display projection, exercised end to end through real MCP write and
// Maintain paths using canonical mixed UTF-8 code: references.
func TestRJointMixedUTF8ByteAndRuneBoundariesThroughWritePath(t *testing.T) {
	makeMixedCodeRef := func(t *testing.T, cjkCount, asciiCount, wantRunes, wantBytes int) string {
		t.Helper()
		ref := "code:" + strings.Repeat("模", cjkCount) + strings.Repeat("a", asciiCount) + ".go"
		if gotRunes, gotBytes := utf8.RuneCountInString(ref), len(ref); gotRunes != wantRunes || gotBytes != wantBytes {
			t.Fatalf("fixture ref runes=%d bytes=%d, want runes=%d bytes=%d", gotRunes, gotBytes, wantRunes, wantBytes)
		}
		return ref
	}

	submitR := func(t *testing.T, root string, importance int, rValue string) autoResult {
		t.Helper()
		line := fmt.Sprintf("main.go[CD%dS]: F:run the fixture | R:%s | A:main | S:-", importance, rValue)
		return decodeAutoResult(t, handleMCPUpdateBatch(root, "test-version", []updateEntryItemIn{{
			Path: "main.go", SourceSHA256: sourceSHA256(t, root, "main.go"), NewEntry: line,
		}}))
	}

	t.Run("default_c9_byte_and_rune_boundaries", func(t *testing.T) {
		root := buildManyFileVolumesRepo(t, 1)
		activateEnforceBudget(t, root)
		session := connectMCPClient(t, root)
		maintained := maintainJSON(t, session, nil)
		instructions := fmt.Sprint(maintained["instructions"])
		if !strings.Contains(instructions, "C9<=360字/542字节") && !strings.Contains(instructions, "C9<=360 runes/542 bytes") {
			t.Fatalf("Maintain instructions missing effective C9 R ceiling: %s", instructions)
		}

		// Byte ceiling hit first (runes=242 < 360, bytes=542 vs 543).
		atBytes := makeMixedCodeRef(t, 150, 84, 242, 542)
		if res := submitR(t, root, 9, atBytes); res.Status != autoStatusApplied || res.Applied != 1 || !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), atBytes) {
			t.Fatalf("C9 mixed UTF-8 R at exact 542-byte ceiling must apply: %#v", res)
		}
		indexBefore := volumeFileText(t, root, "aoci.code.txt")
		overBytes := makeMixedCodeRef(t, 150, 85, 243, 543)
		if res := submitR(t, root, 9, overBytes); res.Status != autoStatusRepairRequired || res.FormalWritesStarted || len(res.Findings) != 1 ||
			res.Findings[0].RuleCode != "entry_field_budget_exceeded" || res.Findings[0].Field != "R" ||
			res.Findings[0].Expected != "max_tokens=180" || res.Findings[0].Actual != "actual_tokens=181" {
			t.Fatalf("C9 mixed UTF-8 R at 543 bytes must fail only budget gate: %#v", res)
		}
		if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
			t.Fatal("budget refusal must write nothing")
		}

		// Rune ceiling hit first (runes=360 vs 361, bytes=440/441 <= 542).
		atRunes := makeMixedCodeRef(t, 40, 312, 360, 440)
		if res := submitR(t, root, 9, atRunes); res.Status != autoStatusApplied || res.Applied != 1 || !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), atRunes) {
			t.Fatalf("C9 mixed UTF-8 R at exact 360-rune ceiling must apply: %#v", res)
		}
		indexBefore = volumeFileText(t, root, "aoci.code.txt")
		overRunes := makeMixedCodeRef(t, 40, 313, 361, 441)
		if res := submitR(t, root, 9, overRunes); res.Status != autoStatusRepairRequired || res.FormalWritesStarted || len(res.Findings) != 1 ||
			res.Findings[0].RuleCode != "fras_r_too_long" || res.Findings[0].Field != "R" ||
			res.Findings[0].Expected != "max_runes=360" || res.Findings[0].Actual != "rune_count=361" {
			t.Fatalf("C9 mixed UTF-8 R at 361 runes must fail only FRAS rune gate: %#v", res)
		}
		if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
			t.Fatal("FRAS rune refusal must write nothing")
		}

		// Both ceilings exceeded together (runes=361 > 360 and bytes=543 > 542).
		overBoth := makeMixedCodeRef(t, 91, 262, 361, 543)
		res := submitR(t, root, 9, overBoth)
		if res.Status != autoStatusRepairRequired || res.FormalWritesStarted || len(res.Findings) != 2 {
			t.Fatalf("C9 mixed UTF-8 R exceeding both ceilings must report both findings: %#v", res)
		}
		rules := map[string]int{}
		for _, finding := range res.Findings {
			if finding.Field != "R" {
				t.Fatalf("unexpected non-R finding: %+v", finding)
			}
			rules[finding.RuleCode]++
		}
		if rules["fras_r_too_long"] != 1 || rules["entry_field_budget_exceeded"] != 1 {
			t.Fatalf("expected both fras_r_too_long and entry_field_budget_exceeded, got %v (%#v)", rules, res.Findings)
		}
		if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
			t.Fatal("joint refusal must write nothing")
		}
	})

	t.Run("default_c3_tight_byte_boundary", func(t *testing.T) {
		root := buildSingleCodeWriteRepo(t, false)
		activateEnforceBudget(t, root)
		atBytes := makeMixedCodeRef(t, 40, 24, 72, 152)
		if res := submitR(t, root, 3, atBytes); res.Status != autoStatusApplied || res.Applied != 1 || len(res.Findings) != 0 {
			t.Fatalf("C3 mixed UTF-8 R at exact 152-byte ceiling must apply: %#v", res)
		}
		indexBefore := volumeFileText(t, root, "aoci.code.txt")
		overBytes := makeMixedCodeRef(t, 40, 25, 73, 153)
		if res := submitR(t, root, 3, overBytes); res.Status != autoStatusRepairRequired || res.FormalWritesStarted || len(res.Findings) != 1 ||
			res.Findings[0].RuleCode != "entry_field_budget_exceeded" || res.Findings[0].Field != "R" ||
			res.Findings[0].Expected != "max_tokens=50" || res.Findings[0].Actual != "actual_tokens=51" {
			t.Fatalf("C3 mixed UTF-8 R at 153 bytes must fail budget gate: %#v", res)
		}
		if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
			t.Fatal("C3 budget refusal must write nothing")
		}
	})

	t.Run("huge_custom_c9_budget_saturates_display_and_enforces_fras_runes", func(t *testing.T) {
		root := buildManyFileVolumesRepo(t, 1)
		policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce)
		hugeTokens := int(int64(1) << 62)
		if math.MaxInt == math.MaxInt32 {
			hugeTokens = math.MaxInt
		}
		policy.R[0].MaxTokens = hugeTokens
		activateEnforceBudgetPolicy(t, root, policy)

		session := connectMCPClient(t, root)
		maintained := maintainJSON(t, session, nil)
		instructions := fmt.Sprint(maintained["instructions"])
		if strings.Contains(instructions, "C9<=-") {
			t.Fatalf("Maintain instructions overflowed to negative C9 limit: %s", instructions)
		}
		if !strings.Contains(instructions, "C9<=360字/1440字节") && !strings.Contains(instructions, "C9<=360 runes/1440 bytes") {
			t.Fatalf("Maintain instructions missing saturated C9 R ceiling (360/1440): %s", instructions)
		}

		// 360 mixed UTF-8 runes totaling 1064 bytes: well above default 542 bytes,
		// valid under both the huge token ceiling and FRAS 360-rune cap.
		atRunes := makeMixedCodeRef(t, 352, 0, 360, 1064)
		if res := submitR(t, root, 9, atRunes); res.Status != autoStatusApplied || res.Applied != 1 || !strings.Contains(volumeFileText(t, root, "aoci.code.txt"), atRunes) {
			t.Fatalf("C9 mixed UTF-8 R at 360 runes (1064 bytes) must apply under huge custom budget: %#v", res)
		}
		indexBefore := volumeFileText(t, root, "aoci.code.txt")
		overRunes := makeMixedCodeRef(t, 353, 0, 361, 1067)
		if res := submitR(t, root, 9, overRunes); res.Status != autoStatusRepairRequired || res.FormalWritesStarted || len(res.Findings) != 1 ||
			res.Findings[0].RuleCode != "fras_r_too_long" || res.Findings[0].Field != "R" ||
			res.Findings[0].Expected != "max_runes=360" || res.Findings[0].Actual != "rune_count=361" {
			t.Fatalf("C9 mixed UTF-8 R at 361 runes must still hit FRAS rune gate under huge budget: %#v", res)
		}
		if volumeFileText(t, root, "aoci.code.txt") != indexBefore {
			t.Fatal("FRAS rune refusal under huge budget must write nothing")
		}
	})
}
