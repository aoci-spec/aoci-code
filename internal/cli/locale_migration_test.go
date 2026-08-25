package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/curation"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestLocaleMigrationKeepsOrdinaryEntriesAndBlocksOnManagedSurfaces(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	agentPlanWriteFile(t, root, "keep.go", "package main\n")
	agentPlanWriteFile(t, root, "empty.bin", "")
	indexText := "#Locale: zh-CN\n" + agentPlanHeader(true) +
		"\n===代码索引" + rootSlash + "/===\n" +
		"aoci.txt[XRT9T]: F:索引本体 | R:- | A:- | S:-\n" +
		"keep.go[XAP7T]: F:对齐文件 | R:- | A:- | S:-\n"
	agentPlanWriteFile(t, root, "aoci.txt", indexText)

	profile, err := curation.ProfilePath(root, "empty.bin")
	if err != nil {
		t.Fatal(err)
	}
	curationDocument := curation.NewDocument()
	curationDocument.Decisions = []curation.Decision{{
		Path:         "empty.bin",
		Decision:     curation.DecisionExclude,
		Role:         "空占位文件",
		Reason:       "不承载运行语义",
		Confidence:   100,
		SourceSHA256: profile.SourceSHA256,
		Agent:        "locale-test",
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}}
	if err := curation.Save(root, curationDocument); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)

	if err := prepareLocaleChange(root, cfg, textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	assertMigrationReceipt(t, cfg.LocaleMigration, true, []string{}, []string{"empty.bin"})
	assertLocalePlanStage(t, root, agentPlanStageHeaderRequired)

	englishIndex := strings.Replace(indexText, "#Locale: zh-CN", "#Locale: en-US", 1)
	englishIndex = strings.ReplaceAll(englishIndex, "#A层级:", "#A Layer:")
	englishIndex = strings.ReplaceAll(englishIndex, "#B模块:", "#B Module:")
	englishIndex = strings.ReplaceAll(englishIndex, "#C重要度:", "#C Importance:")
	englishIndex = strings.ReplaceAll(englishIndex, "#E规模:", "#E Scale:")
	agentPlanWriteFile(t, root, "aoci.txt", englishIndex)
	if err := config.AdvanceLocaleMigration(root, true, nil, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)
	assertLocalePlanStage(t, root, agentPlanStageCurationRequired)
	if !strings.Contains(englishIndex, "F:索引本体") || !strings.Contains(englishIndex, "F:对齐文件") {
		t.Fatal("Header migration unexpectedly translated ordinary Entries")
	}

	curationDocument.Decisions[0].Role = "Empty placeholder"
	curationDocument.Decisions[0].Reason = "Carries no repository semantics"
	curationDocument.Decisions[0].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := curation.Save(root, curationDocument); err != nil {
		t.Fatal(err)
	}
	if err := config.AdvanceLocaleMigration(root, false, nil, []string{"empty.bin"}); err != nil {
		t.Fatal(err)
	}
	assertLocalePlanStage(t, root, agentPlanStageAligned)

	cfg, err = config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareLocaleChange(root, cfg, textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	assertLocalePlanStage(t, root, agentPlanStageHeaderRequired)
}

func saveCurrentBaseline(t *testing.T, root string, cfg *config.Config) {
	t.Helper()
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
}

func assertLocalePlanStage(t *testing.T, root, expected string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	document, _ := index.Parse(string(data))
	index.ResolveRelPaths(document, root)
	plan, err := buildAgentPlan(root, cfg, document, filepath.Join(root, cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Stage != expected {
		t.Fatalf("locale migration plan stage = %s, want %s: %+v", plan.Stage, expected, plan)
	}
}

func assertMigrationReceipt(t *testing.T, receipt *config.LocaleMigration, header bool, entries, curationPaths []string) {
	t.Helper()
	if receipt == nil || receipt.HeaderPending != header ||
		strings.Join(receipt.EntryPaths, ",") != strings.Join(entries, ",") ||
		strings.Join(receipt.CurationPaths, ",") != strings.Join(curationPaths, ",") {
		t.Fatalf("unexpected locale migration receipt: %+v", receipt)
	}
}

func TestLocaleChangeInventoriesOnlyGovernanceEntries(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	var body strings.Builder
	body.WriteString("legacy header\n===源码仓" + rootSlash + "/===\n")
	governance := []string{
		".aoci/baseline.json",
		".aoci/config.json",
		".aoci/ledger.jsonl",
		".aoci/reports.jsonl",
		".aoci/verify_history",
	}
	for _, relPath := range governance {
		fmt.Fprintf(&body, "%s[XRT9T]: F:机器治理资产 | R:- | A:- | S:-\n", relPath)
	}
	for position := 0; position < 822; position++ {
		fmt.Fprintf(&body, "src/file-%03d.go[XRT9T]: F:源码语义 | R:- | A:API%d | S:-\n", position, position)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	if err := prepareLocaleChange(root, cfg, textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	receipt := cfg.LocaleMigration
	if receipt == nil || receipt.EntryTotal != 0 || receipt.GovernanceEntryTotal != 5 ||
		len(receipt.EntryPaths) != 0 || len(receipt.GovernanceEntryPaths) != 5 {
		t.Fatalf("ordinary Entries became batch-translation debt or governance inventory was incomplete: %+v", receipt)
	}
	coverage := buildLocaleMigrationCoverage(cfg)
	if coverage.OrdinaryEntries.Total != 0 || coverage.GovernanceEntries.Total != 5 ||
		coverage.Header.Pending != 1 || coverage.ManagedIndexText.Pending == 0 ||
		coverage.AgentsManagedBlock.Pending != 1 {
		t.Fatalf("unexpected migration coverage: %+v", coverage)
	}
}

func TestLocaleManagedIndexCandidateReclassifiesGovernanceWithoutVolatileEvidence(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	oldHeader := "#Locale: zh-CN\n" + agentPlanHeader(true)
	newHeader := strings.NewReplacer(
		"#Locale: zh-CN", "#Locale: en-US",
		"#A层级:", "#A Layer:",
		"#B模块:", "#B Module:",
		"#C重要度:", "#C Importance:",
		"#E规模:", "#E Scale:",
	).Replace(oldHeader)
	ordinary := "keep.go[XAP7T]: F:保留到源码绑定阶段 | R:中文专有名词 | A:KeepAPI | S:-"
	governance := ".aoci/ledger.jsonl[XRT9T]: F:易变台账 | R:- | A:- | S:-"
	current := oldHeader + "\n===中文目录标签" + rootSlash + "/===\n" + ordinary + "\n" + governance + "\n#中文索引说明\n"
	candidate := newHeader + "\n===English directory label" + rootSlash + "/===\n" + ordinary + "\n#English index explanation\n"
	doc, _ := index.Parse(current)
	index.ResolveRelPaths(doc, root)
	migration := &config.LocaleMigration{
		Version: 2, FromLocale: textassets.LegacyLocale, ToLocale: textassets.DefaultLocale,
		HeaderPending: true, HeaderTotal: 1,
		EntryPaths: []string{"keep.go"}, EntryTotal: 1,
		GovernanceEntryPaths: []string{".aoci/ledger.jsonl"}, GovernanceEntryTotal: 1,
		ManagedIndexTextPending: true, ManagedIndexTextTotal: countManagedIndexTextTargets(doc),
		AgentsManagedBlockPending: true, AgentsManagedBlockTotal: 1,
	}
	summary, err := validateLocaleIndexCandidate(current, candidate, root, textassets.DefaultLocale, migration)
	if err != nil {
		t.Fatal(err)
	}
	if summary.OrdinaryEntries != 1 || summary.GovernanceEntries != 1 || summary.ManagedText != 2 {
		t.Fatalf("unexpected candidate summary: %+v", summary)
	}
	if !strings.Contains(candidate, "中文专有名词") || !strings.Contains(candidate, "KeepAPI") {
		t.Fatal("protected project name or API was translated")
	}

	changedOrdinary := strings.Replace(candidate, ordinary,
		"keep.go[XAP7T]: F:translated too early | R:中文专有名词 | A:KeepAPI | S:-", 1)
	if _, err := validateLocaleIndexCandidate(current, changedOrdinary, root, textassets.DefaultLocale, migration); err == nil ||
		!strings.Contains(err.Error(), "keep.go") {
		t.Fatalf("ordinary Entry mutation was not rejected: %v", err)
	}
	retainedGovernance := strings.Replace(candidate, ordinary+"\n", ordinary+"\n"+governance+"\n", 1)
	if _, err := validateLocaleIndexCandidate(current, retainedGovernance, root, textassets.DefaultLocale, migration); err == nil ||
		!strings.Contains(err.Error(), ".aoci/ledger.jsonl") {
		t.Fatalf("governance Entry retention was not rejected: %v", err)
	}
}

func TestLocaleHeaderStageManagedSurfaceErrorIsBilingual(t *testing.T) {
	root := buildAgentPlanMixedRepo(t, false, true)
	cfg, _, _ := agentPlanLoadDocument(t, root)
	if err := prepareLocaleChange(root, cfg, textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, document, indexPath := agentPlanLoadDocument(t, root)
	plan, err := buildAgentPlan(root, cfg, document, indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Stage != agentPlanStageHeaderRequired {
		t.Fatalf("migration plan stage=%s, want %s", plan.Stage, agentPlanStageHeaderRequired)
	}
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, current := range []struct {
		locale  string
		anchors []string
	}{
		{textassets.DefaultLocale, []string{"requires managed_index_text", "Header", "directory metadata", "governance Entry reclassification"}},
		{textassets.LegacyLocale, []string{"必须提供managed_index_text", "Header", "目录元数据", "治理Entry重分类"}},
	} {
		if err := textassets.SetActiveLocale(current.locale); err != nil {
			t.Fatal(err)
		}
		localizedPlan, planErr := buildAgentPlan(root, cfg, document, indexPath)
		if planErr != nil {
			t.Fatal(planErr)
		}
		request := agentHeaderStageRequest{
			Version: agentHeaderStageVersion,
			PlanID:  localizedPlan.PlanID,
			Agent:   "codex",
			Header:  "#Locale: en-US\n#A Layer: X-Source\n#B Module: Y-Core\n",
		}
		_, stageErr := stageAgentHeader(root, cfg, document, indexPath, request)
		if stageErr == nil {
			t.Fatalf("%s accepted Locale migration Header without managed_index_text", current.locale)
		}
		for _, anchor := range current.anchors {
			if !strings.Contains(stageErr.Error(), anchor) {
				t.Errorf("%s Header Stage error is missing %q: %v", current.locale, anchor, stageErr)
			}
		}
	}
}

func TestLocaleProtectedAndHostErrorsAreBilingual(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	oldEntry := "main.go[XAP9T]: F:旧职责 | R:src/client.go | A:Client.Send | S:-"
	newEntry := "main.go[XAP9T]: F:New responsibility | R:src/client.go | A:Client.Drop | S:-"
	for _, current := range []struct {
		locale          string
		protectedAnchor string
		hostAnchor      string
	}{
		{textassets.DefaultLocale, "protected paths, identifiers, APIs", "cannot be quoted safely for PowerShell"},
		{textassets.LegacyLocale, "受保护的路径、标识符、API", "无法为PowerShell安全引用"},
	} {
		if err := textassets.SetActiveLocale(current.locale); err != nil {
			t.Fatal(err)
		}
		protectedErr := validateLocaleEntryProtectedFacts(oldEntry, newEntry)
		if protectedErr == nil || !strings.Contains(protectedErr.Error(), current.protectedAnchor) ||
			!strings.Contains(protectedErr.Error(), "main.go") || !strings.Contains(protectedErr.Error(), "Client.Send") {
			t.Errorf("%s protected-fact error lost semantics or facts: %v", current.locale, protectedErr)
		}
		_, hostErr := agentCommandPrefixFor("windows", `C:\aoci\bad$path.exe`)
		if hostErr == nil || !strings.Contains(hostErr.Error(), current.hostAnchor) ||
			!strings.Contains(hostErr.Error(), "PowerShell") {
			t.Errorf("%s Host executable error lost semantics: %v", current.locale, hostErr)
		}
	}
}

func TestVolumeReadOnlyDiagnosticIsBilingualAndDoesNotClaimVersionMismatch(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	for _, current := range []struct {
		locale             string
		message            string
		hint               string
		instruction        string
		batchInstruction   string
		continuation       string
		terminalProof      string
		runtimeInstruction string
	}{
		{textassets.DefaultLocale, "compatibility write path", "does not prove a CLI/MCP version mismatch", "Do not run the Legacy-only", "code_batch_id must equal code_plan.batch_id", "remaining nonzero", "commands.verify, commands.check (the aggregate Check), and commands.guide in that order", "does not by itself prove a CLI/MCP version mismatch"},
		{textassets.LegacyLocale, "兼容写入路径", "不能判断CLI与MCP版本不一致", "不要运行仅适用于Legacy布局", "code_batch_id必须等于code_plan.batch_id", "remaining非零", "依次执行commands.verify、commands.check（Aggregate Check）和commands.guide", "不能单独证明CLI与MCP版本不一致"},
	} {
		if err := textassets.SetActiveLocale(current.locale); err != nil {
			t.Fatal(err)
		}
		if got := cliMessage("mcp.error.volume_read_only"); !strings.Contains(got, current.message) {
			t.Errorf("%s volume_read_only message lost path diagnosis: %q", current.locale, got)
		}
		if got := cliMessage("mcp.error.volume_read_only_hint"); !strings.Contains(got, current.hint) {
			t.Errorf("%s volume_read_only hint claimed an unproven runtime mismatch: %q", current.locale, got)
		}
		if got := cliMessage("guide.volumes_instruction_maintain"); !strings.Contains(got, current.instruction) ||
			!strings.Contains(got, current.batchInstruction) || !strings.Contains(got, current.continuation) ||
			!strings.Contains(got, current.terminalProof) {
			t.Errorf("%s Volumes Guide lost the Code batch identity, continuation, terminal proof, or Legacy-only Plan boundary: %q", current.locale, got)
		}
		if got := cliMessage("guide.volumes_instruction_runtime_identity"); !strings.Contains(got, current.runtimeInstruction) {
			t.Errorf("%s Volumes Guide runtime instruction claimed an unproven version mismatch: %q", current.locale, got)
		}
	}
}

func TestHeaderCompletionClearsAllHeaderSurfacesButKeepsOrdinaryEntryReceipt(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.DefaultLocale
	cfg.LocaleMigration = &config.LocaleMigration{
		Version: 2, FromLocale: textassets.LegacyLocale, ToLocale: textassets.DefaultLocale,
		HeaderPending: true, HeaderTotal: 1,
		EntryPaths: []string{"keep.go"}, EntryTotal: 1,
		GovernanceEntryPaths: []string{".aoci/verify_history"}, GovernanceEntryTotal: 1,
		ManagedIndexTextPending: true, ManagedIndexTextTotal: 4,
		AgentsManagedBlockPending: true, AgentsManagedBlockTotal: 1,
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.AdvanceLocaleMigration(root, true, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := config.AdvanceLocaleMigration(root, true, nil, nil); err != nil {
		t.Fatalf("repeated Header completion must be idempotent: %v", err)
	}
	loaded, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt := loaded.LocaleMigration
	if receipt == nil || receipt.HeaderPending || receipt.ManagedIndexTextPending ||
		receipt.AgentsManagedBlockPending || len(receipt.GovernanceEntryPaths) != 0 ||
		strings.Join(receipt.EntryPaths, ",") != "keep.go" {
		t.Fatalf("header completion did not advance exact surfaces: %+v", receipt)
	}
	coverage := buildLocaleMigrationCoverage(loaded)
	if coverage.OrdinaryEntries.Pending != 1 || coverage.GovernanceEntries.Pending != 0 ||
		coverage.Header.Pending != 0 || coverage.ManagedIndexText.Pending != 0 {
		t.Fatalf("unexpected post-Header coverage: %+v", coverage)
	}
}

func TestManagedIndexCandidateSupportsEnglishToChinese(t *testing.T) {
	root := t.TempDir()
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	englishHeader := "#Locale: en-US\n" + strings.NewReplacer(
		"#A层级:", "#A Layer:",
		"#B模块:", "#B Module:",
		"#C重要度:", "#C Importance:",
		"#E规模:", "#E Scale:",
	).Replace(agentPlanHeader(true))
	chineseHeader := "#Locale: zh-CN\n" + agentPlanHeader(true)
	ordinary := "keep.go[XAP7T]: F:Keep semantics until Entries stage | R:ProjectName | A:KeepAPI | S:-"
	governance := ".aoci/reports.jsonl[XRT9T]: F:machine report stream | R:- | A:- | S:-"
	current := englishHeader + "\n===Configuration " + rootSlash + "/===\n" + ordinary + "\n" + governance + "\n#Index note\n"
	candidate := chineseHeader + "\n===配置 " + rootSlash + "/===\n" + ordinary + "\n#索引说明\n"
	document, _ := index.Parse(current)
	index.ResolveRelPaths(document, root)
	migration := &config.LocaleMigration{
		Version: 2, FromLocale: textassets.DefaultLocale, ToLocale: textassets.LegacyLocale,
		HeaderPending: true, HeaderTotal: 1,
		EntryPaths: []string{"keep.go"}, EntryTotal: 1,
		GovernanceEntryPaths: []string{".aoci/reports.jsonl"}, GovernanceEntryTotal: 1,
		ManagedIndexTextPending: true, ManagedIndexTextTotal: countManagedIndexTextTargets(document),
		AgentsManagedBlockPending: true, AgentsManagedBlockTotal: 1,
	}
	if _, err := validateLocaleIndexCandidate(current, candidate, root, textassets.LegacyLocale, migration); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFailsWhileAnyLocaleManagedSurfaceIsPending(t *testing.T) {
	root := buildUpdateRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareLocaleChange(root, cfg, textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = root
	flagJSON = false
	t.Cleanup(func() { flagRepo, flagJSON = oldRepo, oldJSON })
	command := newCheckCmd()
	var outputBuffer bytes.Buffer
	command.SetOut(&outputBuffer)
	command.SetErr(&outputBuffer)
	checkErr := runCheckCommand(command, nil)
	output := outputBuffer.String()
	if checkErr == nil {
		t.Fatalf("Check passed with pending Locale managed surfaces:\n%s", output)
	}
	var exitErr *ExitError
	if !errors.As(checkErr, &exitErr) || exitErr.Code != ExitDrift {
		t.Fatalf("Check returned the wrong failure: %T %v", checkErr, checkErr)
	}
	if !strings.Contains(output, "Locale") {
		t.Fatalf("Check did not identify the Locale migration blocker:\n%s", output)
	}
}

func TestLocaleEntryMigrationPreservesPathsTagsAPIsAndProjectNames(t *testing.T) {
	oldLine := "src/main.go[XAP9T]: F:AOCI进程入口 | R:internal/cli/root.go,中文专有名词(共享夹具) | A:Run,PublicAPI,追加当前状态 | S:保持source_sha256与Plan一致"
	valid := "src/main.go[XAP9T]: F:AOCI process entry point | R:internal/cli/root.go,中文专有名词(shared fixture) | A:Run,PublicAPI,returns current status | S:keeps source_sha256 consistent with Plan in a full-silo workflow."
	if err := validateLocaleEntryProtectedFacts(oldLine, valid); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"src/main.go[XAP9T]: F:AOCI process entry point | R:internal/cli/new.go,中文专有名词(shared fixture) | A:Run,PublicAPI,returns current status | S:keeps source_sha256 consistent with Plan",
		"src/main.go[XAP9T]: F:AOCI process entry point | R:internal/cli/root.go,中文专有名词(shared fixture) | A:Execute,PublicAPI,returns current status | S:keeps source_sha256 consistent with Plan",
		"src/main.go[XAP9T]: F:AOCI process entry point | R:internal/cli/root.go,中文专有名词(shared fixture) | A:Run,PublicAPI,returns current status | S:keeps source_hash consistent with Plan",
		"src/main.go[YAP9T]: F:AOCI process entry point | R:internal/cli/root.go,中文专有名词(shared fixture) | A:Run,PublicAPI,returns current status | S:keeps source_sha256 consistent with Plan",
	}
	for _, candidate := range cases {
		if err := validateLocaleEntryProtectedFacts(oldLine, candidate); err == nil {
			t.Fatalf("protected fact mutation passed: %s", candidate)
		}
	}
}

func TestLocaleEntryMigrationPreservesSlashSeparatedIdentifiers(t *testing.T) {
	oldLine := `platform_style.txt[TFX5T]: F:平台紧凑标签夹具 | R:internal/index/parser_test.go | A:- | S:锁"CLI能读平台旧索引"元协议主张;含中文|[]<>全角混排与S1/S2变体与分区注释;裸头部旧形态保留作兼容用例`
	valid := `platform_style.txt[TFX5T]: F:Platform compact label fixture | R:internal/index/parser_test.go | A:- | S:Locks the "CLI reads legacy platform index" contract; covers Chinese punctuation and S1/S2 variants.`
	if err := validateLocaleEntryProtectedFacts(oldLine, valid); err != nil {
		t.Fatal(err)
	}
}

func TestLocaleEntryMigrationDoesNotFreezeTargetLocaleCompoundProse(t *testing.T) {
	zh := `main.go[CRT9OT]: F:AOCI进程入口 | R:internal/cli/root.go | A:Run | S:使用--json并保持S1/S2协议与source_sha256绑定`
	en := `main.go[CRT9OT]: F:AOCI Go process ID | R:internal/cli/root.go | A:Run | S:Uses --json throughout a --json S1/S2 contract bound to source_sha256 in a Cross-Compilation freezeA, sign(s), 4-state, terms/values/pools workflow.`
	evidence := []byte("package main\nfunc Run() { use(source_sha256, `S1/S2`, `--json`) }\n")
	if err := validateLocaleEntryProtectedFactsWithEvidence(zh, en, evidence); err != nil {
		t.Fatal(err)
	}
	if err := validateLocaleEntryProtectedFactsWithEvidence(en, zh, evidence); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(en, "S1/S2", "S1/S3", 1)
	if err := validateLocaleEntryProtectedFactsWithEvidence(en, changed, evidence); err == nil {
		t.Fatal("changed slash-separated identifier passed")
	}
	changed = strings.Replace(en, "A:Run", "A:Execute", 1)
	if err := validateLocaleEntryProtectedFactsWithEvidence(en, changed, evidence); err == nil {
		t.Fatal("changed source-evidenced API passed")
	}
}

func TestLocaleMigrationChangesOnlyAgentsManagedBlock(t *testing.T) {
	root := t.TempDir()
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	prefix := "# 用户规则\r\n保持中文专有名词\r\n"
	suffix := "\r\n# User footer\r\nKeepAPI\r\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(prefix+"<!-- aoci:begin -->\nold\n<!-- aoci:end -->"+suffix), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.EnsureAgentsBlock(root); err != nil {
		t.Fatal(err)
	}
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.EnsureAgentsBlock(root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(after)
	begin := strings.Index(text, "<!-- aoci:begin -->")
	end := strings.Index(text, "<!-- aoci:end -->")
	if begin < 0 || end < begin {
		t.Fatalf("managed block markers missing:\n%s", text)
	}
	if text[:begin] != prefix || text[end+len("<!-- aoci:end -->"):] != suffix {
		t.Fatalf("AGENTS unmanaged bytes changed:\n%q\n%q", text[:begin], text[end+len("<!-- aoci:end -->"):])
	}
	managed := text[begin : end+len("<!-- aoci:end -->")]
	if hanTextPattern.MatchString(managed) {
		t.Fatalf("en-US managed AGENTS block still contains unexpected Han text:\n%s", managed)
	}
}
