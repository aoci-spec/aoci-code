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
	for _, want := range []string{"#Canonical-Tag-Authoring: compact A+B+C+[D]+E", "CG7T", "code:path/to/file.go", "#S quota:"} {
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
		`"authoring_meta": "#AOCI-META-VOLUME: 1`, "CG7T", "code:path/to/file", "database://source/namespace/table",
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
	broken := strings.Replace(string(meta), "#C Importance: 9-core 8-high-frequency 7-business 5-routine 3-supporting 1-edge", "#C Importance: none", 1)
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
