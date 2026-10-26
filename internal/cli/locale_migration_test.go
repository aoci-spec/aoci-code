package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/textassets"
)

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

func TestCheckFailsWhileAnyLocaleManagedSurfaceIsPending(t *testing.T) {
	root := buildUpdateRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	// A receipt an earlier release minted: 0.2.0 no longer mints one (the
	// stage pipeline that completed it is retired) but still reports it.
	cfg.Locale = textassets.DefaultLocale
	cfg.LocaleMigration = &config.LocaleMigration{
		Version: 2, FromLocale: textassets.LegacyLocale, ToLocale: textassets.DefaultLocale,
		HeaderPending: true, HeaderTotal: 1, EntryPaths: []string{"a.go"}, EntryTotal: 1,
		AgentsManagedBlockPending: true, AgentsManagedBlockTotal: 1,
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runCLI(t, root, "check")
	output := stdout + stderr
	if code == ExitOK {
		t.Fatalf("Check passed with pending Locale managed surfaces:\n%s", output)
	}
	if code != ExitDrift {
		t.Fatalf("Check returned the wrong failure: exit %d\n%s", code, output)
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

// Since 0.2.0 a Legacy index keeps the Locale it was written in: the stage
// pipeline that re-authored it is retired, so a different target Locale is
// refused with a bilingual-safe message, the same Locale is a no-op, and a
// repository without an index simply takes the new Locale.
func TestLegacyLocaleChangeIsRefusedOnceAnIndexExists(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	if err := prepareLocaleChange(root, cfg, textassets.DefaultLocale); err != nil {
		t.Fatalf("a repository without an index must take the new Locale: %v", err)
	}
	if cfg.Locale != textassets.DefaultLocale || cfg.LocaleMigration != nil {
		t.Fatalf("locale not set or a migration receipt was minted: %#v", cfg)
	}
	index := "#AOCI-LOCALE: zh-CN\n#头\n===段 /repo/===\nf.go[XC5T]: F:x | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, cfg.IndexPath), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Locale = textassets.LegacyLocale
	if err := prepareLocaleChange(root, cfg, textassets.LegacyLocale); err != nil || cfg.LocaleMigration != nil {
		t.Fatalf("the same Locale must be a no-op: err=%v receipt=%#v", err, cfg.LocaleMigration)
	}
	err := prepareLocaleChange(root, cfg, textassets.DefaultLocale)
	if err == nil || !strings.Contains(err.Error(), "0.2.0") || cfg.LocaleMigration != nil || cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("a Legacy index must refuse a new Locale without minting a receipt: err=%v cfg=%#v", err, cfg)
	}
}
