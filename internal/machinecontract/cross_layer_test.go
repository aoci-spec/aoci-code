package machinecontract_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func readRepositoryFile(t *testing.T, path ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSQuotaModelAndStaticContractsMatchMachineAuthority(t *testing.T) {
	numeric := machinecontract.NumericText()

	for _, id := range []textassets.ID{
		textassets.PromptHeaderContentBlocks,
		textassets.PromptHeaderDictRules,
		textassets.PromptEntryFieldRules,
	} {
		rendered, err := textassets.Render(textassets.LegacyLocale, id, numeric)
		if err != nil {
			t.Fatalf("render %s: %v", id, err)
		}
		if !strings.Contains(rendered, numeric.SQuotaDefaultExample) {
			t.Fatalf("模型Prompt %s未派生默认S配额: %s", id, rendered)
		}
	}

	compactLine := "#S配额: " + numeric.SQuotaDefaultCompact
	staticContracts := map[string]string{
		"minimal template": readRepositoryFile(t, "textassets", "zh-CN", "templates", "minimal-index.txt.tmpl"),
		"index spec":       readRepositoryFile(t, "spec", "public", "aoci-index-format-v1.txt"),
		"S discipline":     readRepositoryFile(t, "spec", "public", "s-field-discipline.txt"),
	}
	for name, content := range staticContracts {
		declarations := extractSQuotaDeclarations(content)
		if len(declarations) != 1 || declarations[0] != numeric.SQuotaDefaultCompact {
			t.Errorf(
				"%s的S配额声明未严格匹配机器合同: got=%v want=%q",
				name,
				declarations,
				compactLine,
			)
		}
	}

	discipline := staticContracts["S discipline"]
	matchedRows := 0
	for _, band := range machinecontract.DefaultSQuotaBands() {
		row := fmt.Sprintf("C%d-C%d: ≤ %d 字", band.MaxC, band.MinC, band.MaxRunes)
		if !strings.Contains(discipline, row) {
			t.Errorf("S discipline缺少机器配额行%q", row)
		} else {
			matchedRows++
		}
	}
	quotaRowPattern := regexp.MustCompile(`(?m)^  C[1-9]-C[1-9]: ≤ [0-9]+ 字$`)
	if total := len(quotaRowPattern.FindAllString(discipline, -1)); total != matchedRows {
		t.Errorf("S discipline含机器合同之外的默认配额行: total=%d matched=%d", total, matchedRows)
	}
}

func TestStageModelAndWindowsContractsMatchMachineAuthority(t *testing.T) {
	numeric := machinecontract.NumericText()
	tests := []struct {
		id      textassets.ID
		anchors []string
	}{
		{
			id: textassets.ContractHostRuntimeHeaderStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.HeaderMaxBodyBytes),
				numeric.HeaderMaxBodyHuman,
				fmt.Sprintf("%d字节", numeric.HeaderMaxHeaderBytes),
				numeric.HeaderMaxHeaderHuman,
			},
		},
		{
			id: textassets.ContractHostRuntimeEntriesStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.EntriesMaxBodyBytes),
				numeric.EntriesMaxBodyHuman,
				fmt.Sprintf("最多%d条", numeric.EntriesMaxEntries),
			},
		},
		{
			id: textassets.ContractHostRuntimeCurationStageLimit,
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.CurationMaxBodyBytes),
				numeric.CurationMaxBodyHuman,
				fmt.Sprintf("最多%d项", numeric.CurationMaxDecisions),
			},
		},
	}

	for _, current := range tests {
		rendered, err := textassets.Render(textassets.LegacyLocale, current.id, numeric)
		if err != nil {
			t.Fatalf("render %s: %v", current.id, err)
		}
		for _, anchor := range current.anchors {
			if !strings.Contains(rendered, anchor) {
				t.Errorf("Host-Agent合同 %s 缺少机器派生值%q", current.id, anchor)
			}
		}
	}

}

func TestPublicSpecsDelegateToMachineAuthority(t *testing.T) {
	formatContract := readRepositoryFile(t, "spec", "public", "aoci-index-format-v1.txt")
	for _, anchor := range []string{
		"internal/machinecontract/lexical.go",
		"PublicTextTerms",
		"does not copy the machine vocabulary",
	} {
		if !strings.Contains(formatContract, anchor) {
			t.Errorf("公开格式合同缺少机器权威声明%q", anchor)
		}
	}

	sDiscipline := readRepositoryFile(t, "spec", "public", "s-field-discipline.txt")
	for _, anchor := range []string{
		"internal/machinecontract/lexical.go",
		"EvolutionNarrativeTerms",
		"不复制机器词表",
	} {
		if !strings.Contains(sDiscipline, anchor) {
			t.Errorf("S纪律文档缺少机器权威声明%q", anchor)
		}
	}

	shell := readRepositoryFile(t, "scripts", "check-public-text.sh")
	for _, term := range machinecontract.PublicTextTerms() {
		if strings.Contains(shell, term.Text) {
			t.Errorf("Shell入口重复了公开文案机器词表项%q", term.Text)
		}
	}
}

func TestStatusTokensMatchMachineAuthority(t *testing.T) {
	manifest, err := textassets.ReadManifest()
	if err != nil {
		t.Fatal(err)
	}
	assets := make(map[string]textassets.ManifestAsset, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		assets[asset.ID] = asset
	}

	tests := []struct {
		id     textassets.ID
		tokens []string
	}{
		{
			id: textassets.ContractRuntimeRules,
			tokens: append(
				append(
					append(
						append(machinecontract.CognitionStates(), machinecontract.CognitionLevelStates()...),
						machinecontract.RefreshStatuses()...,
					),
					machinecontract.RefreshReasons()...,
				),
				"status="+machinecontract.AutoStatusApplied,
				machinecontract.AutoStatusRepairRequired,
				machinecontract.AutoStatusStopped,
			),
		},
		{
			id: textassets.ContractGuideEntriesAutoInstructions,
			tokens: []string{
				"auto_finalize.status=" + machinecontract.AutoStatusApplied,
				"auto_finalize.status=" + machinecontract.AutoStatusRepairRequired,
				"auto_finalize.status=" + machinecontract.AutoStatusStopped,
			},
		},
		{
			id:     textassets.ContractHostHelpGuideLong,
			tokens: machinecontract.AutoStatuses(),
		},
		{
			id:     textassets.ContractInitAutomationAuto,
			tokens: machinecontract.AutoStatuses(),
		},
	}

	for _, current := range tests {
		asset, exists := assets[string(current.id)]
		if !exists {
			t.Errorf("状态合同资产未登记: %s", current.id)
			continue
		}
		body, loadErr := textassets.Load(textassets.LegacyLocale, current.id)
		if loadErr != nil {
			t.Errorf("加载状态合同%s失败: %v", current.id, loadErr)
			continue
		}
		for _, token := range current.tokens {
			if !containsExact(asset.ProtocolTokens, token) {
				t.Errorf("资产%s清单未绑定机器状态%q", current.id, token)
			}
			if !strings.Contains(body, token) {
				t.Errorf("资产%s正文未消费机器状态%q", current.id, token)
			}
		}
	}

}

func TestCognitionRefreshContractUsesMachineAuthority(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CognitionRefreshThreshold != machinecontract.CognitionRefreshThresholdDefault {
		t.Fatalf("repository threshold = %d, machine default = %d", cfg.CognitionRefreshThreshold, machinecontract.CognitionRefreshThresholdDefault)
	}

	spec := readRepositoryFile(t, "spec", "public", "aoci-cognition-refresh-v1.txt")
	for _, token := range append(machinecontract.RefreshStatuses(), machinecontract.RefreshReasons()...) {
		if !strings.Contains(spec, token) {
			t.Errorf("refresh spec is missing machine token %q", token)
		}
	}
	for _, value := range []int{
		machinecontract.CognitionRefreshThresholdDefault,
		machinecontract.CognitionRefreshThresholdMin,
		machinecontract.CognitionRefreshThresholdMax,
	} {
		if !strings.Contains(spec, fmt.Sprint(value)) {
			t.Errorf("refresh spec is missing machine numeric value %d", value)
		}
	}
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestContractAuthorityMapCoversEveryEntryLayer(t *testing.T) {
	authority := readRepositoryFile(t, "docs", "zh-cn-contract-authority.md")
	for _, anchor := range []string{
		"当前源码、配置与测试",
		"spec/",
		"internal/machinecontract",
		"Validator 与确定性状态机",
		"textassets",
		"Prompt",
		"Guide 实时输出",
		"MCP Description 与 CLI Help",
		"aoci_rules",
		"AGENTS.md",
		"README 与",
		"testdata/golden",
		"私有归档（不在公开树中）",
		"冲突处理",
	} {
		if !strings.Contains(authority, anchor) {
			t.Errorf("zh-CN合同权威矩阵缺少层级%q", anchor)
		}
	}

	agents := readRepositoryFile(t, "AGENTS.md")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	template, err := textassets.Load(cfg.Locale, textassets.TemplateAgentsMD)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(agents, "<!-- aoci:begin -->")
	end := strings.Index(agents, "<!-- aoci:end -->")
	if start < 0 || end < start {
		t.Fatal("repository AGENTS managed boundary is malformed")
	}
	managed := agents[start : end+len("<!-- aoci:end -->")]
	if managed != strings.TrimRight(template, "\n") {
		t.Fatalf("repository AGENTS does not match configured %s asset", cfg.Locale)
	}
}

var compactSQuotaPattern = regexp.MustCompile(`C[1-9](?:-C?[1-9])?≤[0-9]+`)

func extractSQuotaDeclarations(content string) []string {
	declarations := []string{}
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, "#S配额:") {
			continue
		}
		fields := compactSQuotaPattern.FindAllString(line, -1)
		if len(fields) > 0 {
			declarations = append(declarations, strings.Join(fields, " "))
		}
	}
	return declarations
}

func TestPublicSpecsMatchMachineAuthority(t *testing.T) {
	// Prose may render numbers with thousands separators ("8,000"); normalize
	// before matching so the binding accepts both spellings.
	normalize := func(content string) string {
		return strings.ReplaceAll(content, ",", "")
	}
	deliverySpec := normalize(readRepositoryFile(t, "spec", "public", "aoci-overview-delivery-v1.txt"))
	for _, value := range []int{
		machinecontract.OverviewChunkTokensDefault,
		machinecontract.OverviewChunkTokensMin,
		machinecontract.OverviewChunkTokensMax,
	} {
		if !strings.Contains(deliverySpec, fmt.Sprint(value)) {
			t.Errorf("overview delivery spec is missing machine chunk-token value %d", value)
		}
	}

	objectSpec := readRepositoryFile(t, "spec", "public", "aoci-object-fras-v2.txt")
	for _, anchor := range []string{"internal/machinecontract.ObjectFRASV2Limits", "normative values are owned only"} {
		if !strings.Contains(objectSpec, anchor) {
			t.Errorf("object FRAS spec is missing machine-authority anchor %q", anchor)
		}
	}
}
