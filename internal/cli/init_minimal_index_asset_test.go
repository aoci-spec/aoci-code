// MinimalIndex文本资产迁移的字节级兼容和真实物化测试。
package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func readMinimalIndexGoldenHash(
	t *testing.T,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			"minimal_index_output.sha256",
		),
	)
	if err != nil {
		t.Fatalf(
			"读取MinimalIndex Golden失败: %v",
			err,
		)
	}

	return string(data)
}

func TestMinimalIndexProductionOutputMatchesCompatibilityDigest(
	t *testing.T,
) {
	output, err := renderMinimalIndex("/snapshot/demo-repo")
	if err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256([]byte(output))
	actualHash := fmt.Sprintf("%x", digest[:])
	expectedHash := strings.TrimSpace(readMinimalIndexGoldenHash(t))

	if actualHash != expectedHash {
		t.Fatalf(
			"MinimalIndex生产装配兼容摘要不一致: actual=%s expected=%s",
			actualHash,
			expectedHash,
		)
	}
}

func TestVolumeMetaTemplatesExposeExpandedFixedDictionaries(t *testing.T) {
	type expectedMeta struct {
		codeExample string
		codeA       string
		codeB       string
		importance  string
		codeE       string
		databaseA   string
		databaseB   string
		databaseE   string
	}
	expected := map[string]expectedMeta{
		textassets.DefaultLocale: {
			codeExample: "#Code Entry example: file.go[EG7T]: F:Runs the example application | R:- | A:- | S:-",
			codeA:       "#A Layer: C-SharedFoundation E-EntryBoundary A-ApplicationOrchestration D-DomainLogic K-AlgorithmComputation M-Middleware P-Persistence I-IntegrationAdapter R-RuntimeFoundation L-LibrarySDK F-DeclarativeConfiguration O-OperationsDelivery T-TestValidation S-DocumentationSpecification X-DevelopmentTooling Z-Other",
			codeB:       "#B Module: G-CrossDomain U-UserInteraction B-CoreBusiness D-DataState I-IdentityAccess N-NetworkProtocol M-MessageEvent S-SecurityPrivacy C-ConfigurationPolicy O-Observability R-ReliabilityRecovery P-PerformanceResource W-WorkflowScheduling A-AnalyticsIntelligence H-HardwareDevice L-Localization V-BuildRelease Q-QualityAssurance E-ExtensionPlugin Z-Other",
			importance:  "#C Importance: 9-highest 8-very-high 7-high 6-above-average 5-medium 4-below-average 3-low 2-very-low 1-lowest",
			codeE:       "#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100",
			databaseA:   "#A Layer: E-EntityMaster T-TransactionFact R-RelationMapping M-DetailDependent C-ReferenceDictionary S-StateStorage H-HistoryVersion L-LogAudit Q-QueueOutbox A-AggregateProjection K-KeyValueConfiguration B-DocumentLargeObject Z-Other",
			databaseB:   "#B Module: G-CrossDomain B-CoreBusiness I-IdentityAccess T-OrganizationTenant U-UserExperience F-FinanceBilling K-ContentKnowledge C-ConfigurationPolicy W-WorkflowTask M-MessageEvent N-ExternalIntegration S-SecurityPrivacy O-ObservabilityAudit R-ReliabilityRecovery P-PerformanceResource A-AnalyticsIntelligence H-HardwareDevice L-Localization V-BuildRelease Q-QualityTesting E-ExtensionPlugin Z-Other",
			databaseE:   "#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100",
		},
		textassets.LegacyLocale: {
			codeExample: "#Code Entry example: file.go[EG7T]: F:运行示例应用 | R:- | A:- | S:-",
			codeA:       "#A Layer: C-共享基础 E-入口边界 A-应用编排 D-领域逻辑 K-算法计算 M-中间件 P-持久化 I-集成适配 R-运行基础 L-库与SDK F-声明配置 O-运维交付 T-测试验证 S-文档规范 X-开发工具 Z-其他",
			codeB:       "#B Module: G-跨域通用 U-用户交互 B-核心业务 D-数据状态 I-身份权限 N-网络协议 M-消息事件 S-安全隐私 C-配置策略 O-可观测性 R-可靠性恢复 P-性能资源 W-流程调度 A-分析智能 H-硬件设备 L-本地化 V-构建发布 Q-质量保障 E-扩展插件 Z-其他",
			importance:  "#C Importance: 9-最高 8-很高 7-高 6-较高 5-中等 4-较低 3-低 2-很低 1-最低",
			codeE:       "#E Scale: L-大>400 M-中200-400 S-小100-200 T-微<100",
			databaseA:   "#A Layer: E-实体主表 T-事务事实 R-关联映射 M-明细从属 C-参考字典 S-状态存储 H-历史版本 L-日志审计 Q-队列发件 A-聚合投影 K-键值配置 B-文档大对象 Z-其他",
			databaseB:   "#B Module: G-跨域通用 B-核心业务 I-身份权限 T-组织租户 U-用户体验 F-财务计费 K-内容知识 C-配置策略 W-流程任务 M-消息事件 N-外部集成 S-安全隐私 O-可观测审计 R-可靠性恢复 P-性能资源 A-分析智能 H-硬件设备 L-本地化 V-构建发布 Q-质量测试 E-扩展插件 Z-其他",
			databaseE:   "#E Scale: L-大>400 M-中200-400 S-小100-200 T-微<100",
		},
	}

	for locale, want := range expected {
		locale, want := locale, want
		t.Run(locale, func(t *testing.T) {
			meta, err := textassets.Render(locale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range []string{want.codeExample, want.codeA, want.codeB, want.databaseA, want.databaseB} {
				if strings.Count(meta, line) != 1 {
					t.Fatalf("%s Meta must contain exactly one line %q:\n%s", locale, line, meta)
				}
			}
			if strings.Count(meta, want.importance) != 2 {
				t.Fatalf("%s Meta must use the complete 9-1 importance line for both domains:\n%s", locale, meta)
			}
			if strings.Count(meta, want.codeE) != 2 || want.codeE != want.databaseE {
				t.Fatalf("%s Meta changed or split the existing E-scale contract:\n%s", locale, meta)
			}
			for _, domain := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
				dictionary := index.ExtractScopedTagDict(meta, domain)
				if dictionary == nil || !dictionary.HasObjectContract() {
					t.Fatalf("%s %s dictionary is not a complete object contract: %#v", locale, domain, dictionary)
				}
				if len(dictionary.D) != 0 {
					t.Fatalf("%s %s dictionary unexpectedly declares D: %#v", locale, domain, dictionary.D)
				}
				for importance := 1; importance <= 9; importance++ {
					if !dictionary.C[fmt.Sprintf("%d", importance)] {
						t.Fatalf("%s %s dictionary lacks C=%d: %#v", locale, domain, importance, dictionary.C)
					}
				}
				if len(dictionary.C) != 9 {
					t.Fatalf("%s %s dictionary must expose exactly C=1..9: %#v", locale, domain, dictionary.C)
				}
			}
		})
	}
}

func TestInitMaterializesVolumeFirstAssetsWithoutDatabaseCognition(
	t *testing.T,
) {
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(textassets.LegacyLocale) })
	root := t.TempDir()

	if _, err := runInit(
		t,
		root,
		"--agent=",
		"--hooks=false",
	); err != nil {
		t.Fatalf(
			"init失败: %v",
			err,
		)
	}

	actualRoot, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatalf("读取init Root失败: %v", err)
	}
	expected, err := renderInitialVolumeAssets(root)
	if err != nil {
		t.Fatalf("渲染Volume-first期望值失败: %v", err)
	}
	actualMeta, err := os.ReadFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	actualCode, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(actualRoot) != string(expected.Root) || string(actualMeta) != string(expected.Meta) || string(actualCode) != string(expected.Code) {
		t.Fatal("init物化的Root/Meta/Code与Volume-first文本资产不一致")
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
		t.Fatalf("init不得伪造Database Cognition: %v", err)
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if set.LayoutMode != cognition.LayoutVolumesV1 || set.Meta.State != cognition.AssetPresent ||
		set.Volumes[cognition.ScopeCode].State != cognition.AssetPresent || set.Volumes[cognition.ScopeDatabase] != nil {
		t.Fatalf("init结果不是Code-only Volumes: %#v", set)
	}
	dictionary := index.ExtractScopedTagDict(string(actualMeta), cognition.ScopeCode)
	if dictionary == nil || !dictionary.HasObjectContract() {
		t.Fatalf("init Meta不是完整可解析的Code作者化权威: %#v", dictionary)
	}
	for _, want := range []string{"#Canonical-Tag-Authoring: compact A+B+C+[D]+E", "EG7T", "code:path/to/file.go", "#S quota:"} {
		if !strings.Contains(string(actualMeta), want) {
			t.Fatalf("init Meta缺少首次作者化合同 %q:\n%s", want, actualMeta)
		}
	}
	line := metaEntryExample(t, actualMeta, "#Code Entry example: ")
	if violations := index.ValidateEntryLine("file.go", line); len(violations) != 0 {
		t.Fatalf("Meta官方示例未通过普通Entry Validator: %#v", violations)
	}
	projected := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(root) + "/===\n" + line + "\n"
	if findings := cognition.ValidateProjectedCodeVolume(set, []byte(projected)); len(findings) != 0 {
		t.Fatalf("Meta官方示例未通过Volume/projected Validator: %#v", findings)
	}
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("首次scan失败: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err = cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	oldJSON := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = oldJSON })
	var guideOutput bytes.Buffer
	guideCommand := &cobra.Command{}
	guideCommand.SetOut(&guideOutput)
	if err := writeVolumeAgentGuide(guideCommand, root, cfg, set, "codex"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"stage": "authoring_required"`, "index header show",
		`"authoring_meta": "#AOCI-META-VOLUME: 1`, "EG7T", "code:path/to/file", "database://source/namespace/table",
		machinecontract.NumericText().SQuotaDefaultCompact, "call no-argument aoci_maintain", "remaining nonzero",
		"commands.verify, commands.check (the aggregate Check), and commands.guide in that order", line} {
		if !strings.Contains(guideOutput.String(), want) {
			t.Fatalf("首次Guide未自动交付作者化合同 %q:\n%s", want, guideOutput.String())
		}
	}
	if strings.Contains(guideOutput.String(), `"plan"`) {
		t.Fatalf("Volumes Guide暴露了Legacy-only Plan命令:\n%s", guideOutput.String())
	}
	var renderedGuide volumeAgentGuide
	if err := json.Unmarshal(guideOutput.Bytes(), &renderedGuide); err != nil {
		t.Fatal(err)
	}
	executable, err := currentAgentExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := agentCommandPrefixFor(runtime.GOOS, executable)
	if err != nil {
		t.Fatal(err)
	}
	for name, command := range map[string]string{
		"guide":       renderedGuide.Commands.Guide,
		"header_show": renderedGuide.Commands.HeaderShow,
		"check":       renderedGuide.Commands.Check,
		"verify":      renderedGuide.Commands.Verify,
	} {
		if !strings.HasPrefix(command, prefix+" ") {
			t.Fatalf("Volumes Guide %s命令未绑定当前绝对可执行文件: %q", name, command)
		}
	}
}

func TestVolumeGuideCompletesOldFormalMetaContractWithoutRewritingMeta(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	oldMeta, err := os.ReadFile(filepath.Join("..", "..", "testdata", "volumes", "compat-52bc4af", "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(root, "aoci.meta.txt")
	if err := os.WriteFile(metaPath, oldMeta, 0o644); err != nil {
		t.Fatal(err)
	}
	var scanOut, scanErr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--quiet", "scan"}, &scanOut, &scanErr); code != ExitOK {
		t.Fatalf("old Meta scan failed: code=%d stdout=%s stderr=%s", code, scanOut.String(), scanErr.String())
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	oldJSON := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = oldJSON })
	if err := writeVolumeAgentGuide(command, root, cfg, set, "codex"); err != nil {
		t.Fatal(err)
	}
	var guide volumeAgentGuide
	if err := json.Unmarshal(output.Bytes(), &guide); err != nil {
		t.Fatal(err)
	}
	if guide.AuthoringMeta != string(oldMeta) {
		t.Fatal("Guide rewrote or substituted the old formal Meta bytes")
	}
	example := deliveredEntryExample(t, guide.Instructions)
	dictionary := index.ExtractScopedTagDict(string(oldMeta), cognition.ScopeCode)
	if findings := cognition.ValidateVolumeAuthoringExample(cognition.ScopeCode, example, dictionary); len(findings) != 0 {
		t.Fatalf("Guide-delivered old-Meta compatibility example is invalid: %#v", findings)
	}
	after, err := os.ReadFile(metaPath)
	if err != nil || string(after) != string(oldMeta) {
		t.Fatal("Guide changed the old formal Meta")
	}
}

func metaEntryExample(t *testing.T, raw []byte, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("Meta lacks example prefix %q", prefix)
	return ""
}

func deliveredEntryExample(t *testing.T, instructions []string) string {
	t.Helper()
	for _, instruction := range instructions {
		if entry, ok := index.ParseEntryLine(instruction, 1); ok && entry.FullLine == instruction {
			return instruction
		}
	}
	t.Fatalf("instructions do not contain one complete Entry: %#v", instructions)
	return ""
}

func TestVolumeGuideRoutesUnusableMetaWithoutCodeReplan(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root := t.TempDir()
	if _, err := runInit(t, root, "--agent=", "--hooks=false"); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(root, "aoci.meta.txt")
	meta, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(meta), "#C Importance: 9-highest 8-very-high 7-high 6-above-average 5-medium 4-below-average 3-low 2-very-low 1-lowest", "#C Importance: none", 1)
	if err := os.WriteFile(metaPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, loadErr := cognition.Load(root, cfg.IndexPath)
	if loadErr == nil || set == nil {
		t.Fatalf("broken Meta did not fail closed: set=%#v err=%v", set, loadErr)
	}
	stop := volumeMetaDictionaryStop(loadErr)
	if stop == nil {
		t.Fatalf("Guide did not classify the Meta failure: %v", loadErr)
	}
	oldJSON := flagJSON
	flagJSON = true
	t.Cleanup(func() { flagJSON = oldJSON })
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	if err := writeVolumeMetaBlockedGuide(command, "codex", stop); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"stage": "blocked"`, `"next_action": "repair_meta_tag_dictionary"`,
		`"executable_targets": 0`, `"affected_asset": "aoci.meta.txt"`, `"field": "tag_dictionary"`,
		`"rule_code": "meta_tag_dictionary_invalid"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("Meta Guide route missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "author_complete_candidate_batch") || strings.Contains(output.String(), "generic replan") {
		t.Fatalf("Meta Guide route requested an unchanged Code batch:\n%s", output.String())
	}
	if strings.Contains(output.String(), `"check"`) {
		t.Fatalf("blocked Volumes Guide exposed a terminal Check command:\n%s", output.String())
	}
}
