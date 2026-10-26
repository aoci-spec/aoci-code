package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// volumesFixture builds one aligned Code-only Volumes v1 repository: team
// configuration, Root manifest, Meta Volume, a Code Volume holding one Entry
// for main.go, and the Baseline over the source. It is the shape the MCP
// tests use, written here so the library is exercised from outside them.
func volumesFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Locale = "en-US"
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: engine fixture\n"+
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n"+
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\nmain.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

// legacyFixture builds one Legacy single-file repository with two Entries.
func legacyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package demo\n")
	write("b.go", "package demo\n")
	write("aoci.txt", "#====Legacy engine fixture====\n#===头部索引===\n#【系统】Legacy engine fixture\n#A层级: X-程序入口\n#B模块: UT-单元测试\n"+
		"#C重要度: 9核心 8高频 7业务 5常规 3辅助 1边缘\n#E规模: L大>400 M中200-400 S小100-200 T微<100\n"+
		"#S配额: "+machinecontract.NumericText().SQuotaDefaultCompact+"\n#===头部索引完毕===\n\n#代码索引\n"+
		"==="+filepath.ToSlash(root)+"/===\n"+
		"a.go[XUT5T]: F:旧职责甲 | R:- | A:- | S:-\n"+
		"b.go[XUT5T]: F:旧职责乙 | R:- | A:- | S:-\n\n#代码索引完毕\n")
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestOpenResolvesTheRootFromASubdirectory(t *testing.T) {
	root := volumesFixture(t)
	nested := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Root() != root || repo.Locale() != "en-US" || repo.IndexPath() != "aoci.txt" {
		t.Fatalf("open: root=%q locale=%q index=%q", repo.Root(), repo.Locale(), repo.IndexPath())
	}
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("a directory with neither .git nor .aoci must not open")
	}
}

func TestCognitionProjectsTheVolumesLayout(t *testing.T) {
	root := volumesFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cog, err := repo.Cognition()
	if err != nil {
		t.Fatal(err)
	}
	if cog.Layout != LayoutVolumesV1 || cog.Root.ID != "root" || cog.Meta.ID != "meta" || len(cog.Volumes) != 1 || cog.Volumes[0].ID != "code" {
		t.Fatalf("layout projection: %+v", cog)
	}
	if cog.Root.SHA256 != fileSHA256(t, filepath.Join(root, "aoci.txt")) || cog.Volumes[0].SHA256 != fileSHA256(t, filepath.Join(root, "aoci.code.txt")) {
		t.Fatalf("asset digests are not the file digests: %+v", cog)
	}
	if len(cog.Entries) != 1 {
		t.Fatalf("entries: %+v", cog.Entries)
	}
	entry := cog.Entries[0]
	if entry.Volume != "code" || entry.Domain != "code" || entry.Kind != "file" || entry.ObjectRef != "code:main.go" || entry.Path != "main.go" ||
		entry.Name != "main.go" || entry.Tag != "CD9S" || entry.F != "run the fixture" || entry.R != "-" ||
		entry.A != "main" || entry.S != "Keep execution deterministic" || !strings.HasPrefix(entry.Line, "main.go[CD9S]: F:run the fixture") || entry.LineNumber < 1 {
		t.Fatalf("entry projection: %+v", entry)
	}
	if cog.CompositeIdentity == "" || cog.Volumes[0].ObjectCount != 1 {
		t.Fatalf("identity or count missing: %+v", cog)
	}
}

func TestOverviewMatchesTheInProcessToolDelivery(t *testing.T) {
	root := volumesFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := repo.Overview("")
	if err != nil {
		t.Fatal(err)
	}
	if overview.Scope != "all" || !overview.Available || overview.EntryCount != 1 || overview.ChunkCount != 1 ||
		overview.Bytes != len(overview.Text) || !strings.Contains(overview.Text, "main.go[CD9S]") {
		t.Fatalf("overview: %+v", overview)
	}
	session, err := repo.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.Call(context.Background(), "aoci_overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || !strings.Contains(result.Text, overview.Text) || !strings.Contains(result.Text, "body_sha256: "+overview.SHA256) ||
		!strings.Contains(result.Text, "index_sha256: "+overview.ScopeIdentity) {
		t.Fatalf("the tool and the library do not deliver the same body: lib sha=%s\n%s", overview.SHA256, result.Text)
	}
}

func TestIdentityAgreesWithCognitionAndOverview(t *testing.T) {
	root := volumesFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cog, err := repo.Cognition()
	if err != nil {
		t.Fatal(err)
	}
	overview, err := repo.Overview("all")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := repo.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Layout != LayoutVolumesV1 || identity.RootSHA256 != cog.Root.SHA256 || identity.MetaSHA256 != cog.Meta.SHA256 ||
		identity.CodeSHA256 != cog.Volumes[0].SHA256 || identity.DatabaseSHA256 != "" || identity.CompositeIdentity != cog.CompositeIdentity ||
		identity.ScopeIdentity != overview.ScopeIdentity || identity.EntryCount != 1 || identity.EngineVersion != Version() ||
		identity.BaselineSHA256 != fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json")) {
		t.Fatalf("identity: %+v\ncognition root=%s meta=%s code=%s composite=%s\noverview scope identity=%s\nbaseline=%s", identity, cog.Root.SHA256, cog.Meta.SHA256, cog.Volumes[0].SHA256, cog.CompositeIdentity, overview.ScopeIdentity, fileSHA256(t, filepath.Join(root, ".aoci", "baseline.json")))
	}
}

func TestGovernanceReadsTheSharedFacts(t *testing.T) {
	root := volumesFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	governance, err := repo.Governance()
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err := json.Unmarshal(governance.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts["result"] != governance.Result || facts["governance_aligned"] != governance.Aligned || governance.CodeEntryCount != 1 || governance.CodeSourceCount != 1 {
		t.Fatalf("projection disagrees with the facts: %+v\n%s", governance, governance.Facts)
	}
	if !governance.Aligned || governance.Result != "aligned" || governance.Drift != (DriftCounts{}) || len(governance.Findings) != 0 {
		t.Fatalf("the aligned fixture must assess aligned: %+v", governance)
	}
	// Drift is seen the same way verify sees it.
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	governance, err = repo.Governance()
	if err != nil {
		t.Fatal(err)
	}
	if governance.Aligned || governance.Drift.Stale != 1 || len(governance.Findings) == 0 || governance.Findings[0].Code != "code_stale" {
		t.Fatalf("a changed source must read as stale: %+v", governance)
	}
}

func TestSessionServesTheNineTools(t *testing.T) {
	root := volumesFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	session, err := repo.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	names, err := session.Tools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	want := append([]string(nil), machinecontract.MCPToolNames()...)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools: %v want %v", names, want)
	}
	rules, err := session.Call(context.Background(), "aoci_rules", nil)
	if err != nil || rules.IsError || !strings.Contains(rules.Text, "AOCI Runtime Contract") {
		t.Fatalf("rules: err=%v %+v", err, rules)
	}
	entries, err := session.Call(context.Background(), "aoci_get_entries", map[string]any{"paths": []string{"main.go"}})
	if err != nil || entries.IsError || !strings.Contains(entries.Text, "main.go[CD9S]") {
		t.Fatalf("get_entries: err=%v %+v", err, entries)
	}
	if _, err := session.Call(context.Background(), "", nil); err == nil {
		t.Fatal("an empty tool name must be refused before the call")
	}
}

func TestLegacyLayoutStaysReadOnlyThroughTheLibrary(t *testing.T) {
	root := legacyFixture(t)
	repo, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cog, err := repo.Cognition()
	if err != nil {
		t.Fatal(err)
	}
	if cog.Layout != LayoutLegacy || len(cog.Entries) != 2 || cog.Entries[0].Path != "a.go" || cog.Entries[0].Volume != "legacy" ||
		cog.Entries[0].Domain != "code" || cog.Entries[0].Kind != "file" || len(cog.Volumes) != 0 {
		t.Fatalf("legacy projection: %+v", cog)
	}
	if _, err := repo.Overview("all"); !errors.Is(err, ErrLayoutUnsupported) {
		t.Fatalf("overview on legacy: %v", err)
	}
	if _, err := repo.Governance(); !errors.Is(err, ErrLayoutUnsupported) {
		t.Fatalf("governance on legacy: %v", err)
	}
	identity, err := repo.Identity()
	if err != nil || identity.Layout != LayoutLegacy || identity.EntryCount != 2 || identity.RootSHA256 != cog.Root.SHA256 {
		t.Fatalf("legacy identity: err=%v %+v", err, identity)
	}
	session, err := repo.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	maintain, err := session.Call(context.Background(), "aoci_maintain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(maintain.Text, `"status":"stopped"`) || !strings.Contains(maintain.Text, machinecontract.StatusLegacyReadOnlyMigrateRequired) {
		t.Fatalf("legacy maintain must stop on the read-only token: %s", maintain.Text)
	}
}

func TestVersionReportsDevWithoutInjection(t *testing.T) {
	if Version() != "dev" || Commit() != "none" {
		t.Fatalf("version=%q commit=%q", Version(), Commit())
	}
}
