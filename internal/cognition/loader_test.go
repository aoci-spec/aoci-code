package cognition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func validMeta() string {
	return MetaVolumeMarker + "\n" +
		"#Object-Protocol: repository-cognition-object/v2\n" +
		"#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n" +
		"#S-Admission: non-inferable-and-error-preventing\n" +
		"#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
}

func rootText(ids ...string) string {
	lines := []string{RootManifestMarker, "#Format-Version: cognition-volumes/v1", "#Locale: en-US", "#Project: fixture"}
	for _, id := range ids {
		d := canonicalDescriptors[id]
		depends := "-"
		if len(d.DependsOn) > 0 {
			depends = strings.Join(d.DependsOn, ",")
		}
		lines = append(lines, "#Volume: id="+d.ID+" kind="+d.Kind+" path="+d.Path+" format="+d.FormatVersion+" depends="+depends)
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func codeVolume(root string) string {
	return CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\n" +
		"main.go[CD9S]: F:run the fixture | R:aoci.meta.txt | A:main | S:Keep the process deterministic\n"
}

func databaseVolume(entries ...string) string {
	return DatabaseMarker + "\n===Primary PostgreSQL public tables/database://primary/public/===\n" + strings.Join(entries, "\n") + "\n"
}

func TestLoadLegacyAdapterPreservesRawBytes(t *testing.T) {
	text := "#legacy\r\n===src/C:/repo/src/===\r\na.go[CD9S]: F:x | R:- | A:- | S:-\r\n"
	root := writeFixture(t, map[string]string{"aoci.txt": text})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if set.LayoutMode != LayoutLegacyMonolithic || string(set.Root.Raw) != text || set.Root.ObjectCount != 1 {
		t.Fatalf("legacy adapter changed bytes or shape: %#v", set)
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.meta.txt")); !os.IsNotExist(err) {
		t.Fatal("legacy load created a Volume file")
	}
}

func TestLoadLegacySkeletonRemainsRepresentable(t *testing.T) {
	root := writeFixture(t, map[string]string{"aoci.txt": "# legacy skeleton without sections\n"})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if set.LayoutMode != LayoutLegacyMonolithic || set.Root.Document == nil || len(set.Root.Document.Sections) != 0 {
		t.Fatalf("Legacy skeleton adapter changed: %#v", set)
	}
}

func TestVolumesRootMustBeRegularWhileLegacySymlinkCompatibilityRemains(t *testing.T) {
	t.Run("volumes root symlink", func(t *testing.T) {
		root := writeFixture(t, map[string]string{
			"root-target.txt": rootText("meta"),
			"aoci.meta.txt":   validMeta(),
		})
		if err := os.Symlink("root-target.txt", filepath.Join(root, "aoci.txt")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		set, err := Load(root, "aoci.txt")
		if err == nil || !hasFinding(set.Errors, "root_path_not_regular") {
			t.Fatalf("Volumes Root symlink was accepted: err=%v findings=%#v", err, set.Errors)
		}
	})
	t.Run("legacy root symlink", func(t *testing.T) {
		root := writeFixture(t, map[string]string{
			"legacy-target.txt": "===src/C:/repo/src/===\na.go[CD9S]: F:x | R:- | A:- | S:-\n",
		})
		if err := os.Symlink("legacy-target.txt", filepath.Join(root, "aoci.txt")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		set, err := Load(root, "aoci.txt")
		if err != nil || set.LayoutMode != LayoutLegacyMonolithic {
			t.Fatalf("Legacy symlink compatibility changed: set=%#v err=%v", set, err)
		}
	})
}

func TestActiveLoaderAcceptsCanonicalStateAndSkipsDisabledObjectVolume(t *testing.T) {
	rootTextWithState := strings.ReplaceAll(rootText("meta", "code"), "depends=-", "depends=- state=enabled")
	rootTextWithState = strings.ReplaceAll(rootTextWithState, "depends=meta", "depends=meta state=disabled")
	root := writeFixture(t, map[string]string{
		"aoci.txt":      rootTextWithState,
		"aoci.meta.txt": validMeta(),
	})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatalf("canonical six-field Root was rejected: %v findings=%#v", err, set.Errors)
	}
	if set.Volumes["meta"].Descriptor.State != machinecontract.CognitionVolumeEnabled ||
		set.Volumes["code"].Descriptor.State != machinecontract.CognitionVolumeDisabled ||
		set.Volumes["code"].State != AssetAbsent {
		t.Fatalf("descriptor state mismatch: %#v", set.Volumes)
	}
}

func TestVolumesOptionalScopesAndObjects(t *testing.T) {
	root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta"), "aoci.meta.txt": validMeta()})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{ScopeAll, ScopeProject} {
		view, err := set.Scope(scope)
		if err != nil || !view.Available || len(view.Assets) != 2 {
			t.Fatalf("%s: %#v %v", scope, view, err)
		}
	}
	for _, scope := range []string{ScopeCode, ScopeDatabase} {
		view, err := set.Scope(scope)
		if err != nil || view.Available || view.AssetState != AssetAbsent || len(view.Assets) != 2 {
			t.Fatalf("%s: %#v %v", scope, view, err)
		}
	}
}

func TestDeclaredEmptyVolumeDiffersFromAbsent(t *testing.T) {
	absentRoot := writeFixture(t, map[string]string{"aoci.txt": rootText("meta"), "aoci.meta.txt": validMeta()})
	absent, err := Load(absentRoot, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	emptyRoot := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "code"), "aoci.meta.txt": validMeta(), "aoci.code.txt": CodeVolumeMarker + "\n"})
	empty, err := Load(emptyRoot, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	absentView, _ := absent.Scope(ScopeCode)
	emptyView, _ := empty.Scope(ScopeCode)
	if absentView.Available || !emptyView.Available || empty.Volumes["code"].State != AssetPresent || empty.Volumes["code"].ObjectCount != 0 {
		t.Fatal("absent and present-empty states collapsed")
	}
	if absent.CompositeIdentity == empty.CompositeIdentity || absentView.ScopeIdentity == emptyView.ScopeIdentity {
		t.Fatal("absent and present-empty identities collapsed")
	}
}

func TestProjectedCodeValidationPreservesMarkerFindings(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"aoci.txt":      rootText("meta", "code"),
		"aoci.meta.txt": validMeta(),
		"aoci.code.txt": CodeVolumeMarker + "\n",
	})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	findings := ValidateProjectedCodeVolume(
		set,
		[]byte(CodeVolumeMarker+"\n"+CodeVolumeMarker+"\n"),
	)
	for _, finding := range findings {
		if finding.Code == "volume_marker_duplicate" {
			return
		}
	}
	t.Fatalf("duplicate marker finding was lost: %#v", findings)
}

func TestVolumesCodeAndDatabase(t *testing.T) {
	root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "code", "database"), "aoci.meta.txt": validMeta()})
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(codeVolume(root)), 0o644); err != nil {
		t.Fatal(err)
	}
	database := databaseVolume(
		"users[DB9S]: F:store canonical user account state | R:tenants,user_profiles | A:user_id,UserRepository,AuthService | S:Hard deletion is forbidden because retained ownership records require the identity",
		"audit_events[DB8L]: F:retain immutable security audit events | R:users | A:AuditWriter | S:Rows are append-only and must be committed in the same transaction as the audited change",
	)
	if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(database), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if set.Volumes["code"].ObjectCount != 1 || set.Volumes["database"].ObjectCount != 2 {
		t.Fatalf("bad object counts: %#v", set.Volumes)
	}
	if got := set.Volumes["database"].Objects[0].CanonicalRef; got != "database://primary/public/users" {
		t.Fatalf("bad table identity %q", got)
	}
	all, _ := set.Scope(ScopeAll)
	code, _ := set.Scope(ScopeCode)
	databaseView, _ := set.Scope(ScopeDatabase)
	project, _ := set.Scope(ScopeProject)
	meta, _ := set.Scope(ScopeMeta)
	if all.ObjectCount != 3 || code.ObjectCount != 1 || databaseView.ObjectCount != 2 || project.ObjectCount != 0 || meta.ObjectCount != 0 {
		t.Fatal("scope object closure is wrong")
	}
	if meta.RequestedScope != ScopeMeta || meta.EffectiveScope != ScopeMeta || len(meta.Assets) != 2 || meta.Assets[0].Descriptor.ID != "root" || meta.Assets[1].Descriptor.ID != "meta" {
		t.Fatalf("meta scope did not deliver Root dependency plus Meta: %#v", meta)
	}
	if all.ScopeIdentity == code.ScopeIdentity {
		t.Fatal("typed scope identities must differ even for overlapping assets")
	}
}

func TestVolumesCodeCloneUsesLogicalRelocationWithoutRewritingHistoricalRoot(t *testing.T) {
	const historicalRoot = "/srv/aoci-origin"
	code := CodeVolumeMarker + "\n" +
		"===Repository" + historicalRoot + "/===\n" +
		"go.mod[CD9S]: F:define the module identity | R:- | A:- | S:Keep the module path stable\n" +
		"===Runtime" + historicalRoot + "/internal/runtime/===\n" +
		"server.go[CD9M]: F:serve the runtime protocol | R:go.mod | A:Server | S:Preserve protocol framing\n" +
		"===Documentation" + historicalRoot + "/docs/===\n" +
		"guide.md[CD7S]: F:document the runtime contract | R:internal/runtime/server.go | A:- | S:-\n"
	files := map[string]string{
		"aoci.txt":      rootText("meta", "code"),
		"aoci.meta.txt": validMeta(),
		"aoci.code.txt": code,
	}
	originalRoot := writeFixture(t, files)
	cloneRoot := writeFixture(t, files)
	if originalRoot == cloneRoot {
		t.Fatal("fixture roots must differ to exercise clone relocation")
	}

	original, err := Load(originalRoot, "aoci.txt")
	if err != nil {
		t.Fatalf("load original Volumes tree: %v", err)
	}
	clone, err := Load(cloneRoot, "aoci.txt")
	if err != nil {
		t.Fatalf("load relocated Volumes tree: %v", err)
	}
	if original.RepositoryRoot != originalRoot || clone.RepositoryRoot != cloneRoot {
		t.Fatalf("runtime roots were not bound to each load: original=%q clone=%q", original.RepositoryRoot, clone.RepositoryRoot)
	}

	for label, set := range map[string]*Set{"original": original, "clone": clone} {
		asset := set.Volumes["code"]
		if asset == nil || asset.Document == nil {
			t.Fatalf("%s Code Volume was not loaded", label)
		}
		if string(asset.Raw) != code {
			t.Fatalf("%s load rewrote the Code Volume bytes", label)
		}
		gotHistoricalRoots := make([]string, 0, len(asset.Document.Sections))
		for _, section := range asset.Document.Sections {
			if section.AbsPath != "" {
				gotHistoricalRoots = append(gotHistoricalRoots, section.AbsPath)
			}
		}
		wantHistoricalRoots := []string{
			historicalRoot + "/",
			historicalRoot + "/internal/runtime/",
			historicalRoot + "/docs/",
		}
		if strings.Join(gotHistoricalRoots, "\x00") != strings.Join(wantHistoricalRoots, "\x00") {
			t.Fatalf("%s historical section roots changed: got=%q want=%q", label, gotHistoricalRoots, wantHistoricalRoots)
		}
		persisted, err := os.ReadFile(filepath.Join(set.RepositoryRoot, "aoci.code.txt"))
		if err != nil {
			t.Fatalf("read %s persisted Code Volume: %v", label, err)
		}
		if string(persisted) != code {
			t.Fatalf("%s load physically rewrote the cloned Code Volume", label)
		}
	}

	refs := func(set *Set) []string {
		objects := set.Volumes["code"].Objects
		result := make([]string, 0, len(objects))
		for _, object := range objects {
			result = append(result, object.CanonicalRef)
		}
		return result
	}
	wantRefs := []string{
		"code:go.mod",
		"code:internal/runtime/server.go",
		"code:docs/guide.md",
	}
	originalRefs := refs(original)
	cloneRefs := refs(clone)
	if strings.Join(originalRefs, "\x00") != strings.Join(wantRefs, "\x00") {
		t.Fatalf("original CanonicalRef order changed: got=%q want=%q", originalRefs, wantRefs)
	}
	if strings.Join(cloneRefs, "\x00") != strings.Join(originalRefs, "\x00") {
		t.Fatalf("clone CanonicalRef order changed: original=%q clone=%q", originalRefs, cloneRefs)
	}
	if original.Volumes["code"].SHA256 != clone.Volumes["code"].SHA256 ||
		original.CompositeIdentity != clone.CompositeIdentity {
		t.Fatalf("runtime root changed formal cognition identity: original_code=%s clone_code=%s original_composite=%s clone_composite=%s",
			original.Volumes["code"].SHA256, clone.Volumes["code"].SHA256,
			original.CompositeIdentity, clone.CompositeIdentity)
	}
}

func TestSingleOptionalObjectVolumeLayouts(t *testing.T) {
	t.Run("code only", func(t *testing.T) {
		root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "code"), "aoci.meta.txt": validMeta()})
		if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(codeVolume(root)), 0o644); err != nil {
			t.Fatal(err)
		}
		set, err := Load(root, "aoci.txt")
		if err != nil {
			t.Fatal(err)
		}
		all, _ := set.Scope(ScopeAll)
		code, _ := set.Scope(ScopeCode)
		database, _ := set.Scope(ScopeDatabase)
		if all.ObjectCount != 1 || code.ObjectCount != 1 || database.Available || set.Volumes["database"] != nil {
			t.Fatalf("code-only closure is wrong: all=%#v code=%#v database=%#v", all, code, database)
		}
		if all.ScopeIdentity == code.ScopeIdentity {
			t.Fatal("all and code identities collapsed despite distinct scope type")
		}
	})
	t.Run("database only", func(t *testing.T) {
		root := writeFixture(t, map[string]string{
			"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(),
			"aoci.database.txt": databaseVolume("users[DB9S]: F:store users | R:- | A:UserRepository | S:-"),
		})
		set, err := Load(root, "aoci.txt")
		if err != nil {
			t.Fatal(err)
		}
		all, _ := set.Scope(ScopeAll)
		database, _ := set.Scope(ScopeDatabase)
		code, _ := set.Scope(ScopeCode)
		if all.ObjectCount != 1 || database.ObjectCount != 1 || code.Available || set.Volumes["code"] != nil {
			t.Fatalf("database-only closure is wrong: all=%#v database=%#v code=%#v", all, database, code)
		}
	})
}

func TestScopeIdentityInvalidationBoundaries(t *testing.T) {
	root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "code", "database"), "aoci.meta.txt": validMeta()})
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(codeVolume(root)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(databaseVolume("users[DB9S]: F:store users | R:- | A:UserRepository | S:-")), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	beforeProject, _ := before.Scope(ScopeProject)
	beforeCode, _ := before.Scope(ScopeCode)
	beforeDB, _ := before.Scope(ScopeDatabase)
	beforeAll, _ := before.Scope(ScopeAll)
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(codeVolume(root)+"#comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	afterCode, _ := after.Scope(ScopeCode)
	afterProject, _ := after.Scope(ScopeProject)
	afterDB, _ := after.Scope(ScopeDatabase)
	afterAll, _ := after.Scope(ScopeAll)
	if beforeCode.ScopeIdentity == afterCode.ScopeIdentity || beforeAll.ScopeIdentity == afterAll.ScopeIdentity {
		t.Fatal("code change did not invalidate code/all")
	}
	if beforeDB.ScopeIdentity != afterDB.ScopeIdentity {
		t.Fatal("code change invalidated database scope")
	}
	if beforeProject.ScopeIdentity != afterProject.ScopeIdentity {
		t.Fatal("code change invalidated project scope")
	}
	if before.CompositeIdentity == after.CompositeIdentity {
		t.Fatal("code change did not invalidate composite identity")
	}
}

func TestDatabaseMetaAndRootIdentityInvalidation(t *testing.T) {
	newBase := func(t *testing.T) string {
		root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "code", "database"), "aoci.meta.txt": validMeta()})
		if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(codeVolume(root)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(databaseVolume("users[DB9S]: F:store users | R:- | A:UserRepository | S:-")), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	scopes := []string{ScopeProject, ScopeCode, ScopeDatabase, ScopeAll}
	identities := func(t *testing.T, root string) map[string]string {
		set, err := Load(root, "aoci.txt")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, scope := range scopes {
			view, _ := set.Scope(scope)
			out[scope] = view.ScopeIdentity
		}
		return out
	}
	t.Run("database", func(t *testing.T) {
		root := newBase(t)
		before := identities(t, root)
		path := filepath.Join(root, "aoci.database.txt")
		data, _ := os.ReadFile(path)
		changed := strings.Replace(string(data), "F:store users", "F:store canonical users", 1)
		_ = os.WriteFile(path, []byte(changed), 0o644)
		after := identities(t, root)
		if before[ScopeDatabase] == after[ScopeDatabase] || before[ScopeAll] == after[ScopeAll] {
			t.Fatal("database change did not invalidate database/all")
		}
		if before[ScopeCode] != after[ScopeCode] || before[ScopeProject] != after[ScopeProject] {
			t.Fatal("database change invalidated code/project")
		}
	})
	for _, kind := range []string{"meta", "root"} {
		t.Run(kind, func(t *testing.T) {
			root := newBase(t)
			before := identities(t, root)
			name := "aoci.meta.txt"
			if kind == "root" {
				name = "aoci.txt"
			}
			path := filepath.Join(root, name)
			data, _ := os.ReadFile(path)
			_ = os.WriteFile(path, append(data, []byte("#stable-project-fact\n")...), 0o644)
			after := identities(t, root)
			for _, scope := range scopes {
				if before[scope] == after[scope] {
					t.Fatalf("%s change did not invalidate %s", kind, scope)
				}
			}
		})
	}
}

func TestDatabaseFRASStrictDensityAndNoRewrite(t *testing.T) {
	original := "wide_table[DB9L]: F:store one business aggregate | R:" + strings.Repeat("related_table,", 9) + "tail | A:Repository | S:-"
	root := writeFixture(t, map[string]string{
		"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(),
		"aoci.database.txt": databaseVolume(original),
	})
	set, err := Load(root, "aoci.txt")
	if err == nil {
		t.Fatal("overlong relation set was accepted")
	}
	if !hasFinding(set.Errors, "fras_r_too_many_items") {
		t.Fatalf("missing density finding: %#v", set.Errors)
	}
	if got := set.Volumes["database"].Objects[0].CanonicalLine; got != original {
		t.Fatalf("loader rewrote model semantics:\nwant %s\ngot  %s", original, got)
	}
}

func TestFRASV2EveryDensityLimitReturnsFinding(t *testing.T) {
	tests := []struct{ name, line, code string }{
		{"F runes", "t[DB9S]: F:" + strings.Repeat("f", 161) + " | R:- | A:- | S:-", "fras_f_too_long"},
		{"R runes", "t[DB9S]: F:x | R:" + strings.Repeat("r", 361) + " | A:- | S:-", "fras_r_too_long"},
		{"R items", "t[DB9S]: F:x | R:a,b,c,d,e,f,g,h,i | A:- | S:-", "fras_r_too_many_items"},
		{"A runes", "t[DB9S]: F:x | R:- | A:" + strings.Repeat("a", 401) + " | S:-", "fras_a_too_long"},
		{"A items", "t[DB9S]: F:x | R:- | A:a,b,c,d,e,f,g | S:-", "fras_a_too_many_items"},
		{"S high", "t[DB9S]: F:x | R:- | A:- | S:" + strings.Repeat("s", 601), "fras_s_too_long"},
		{"S mid", "t[DB7S]: F:x | R:- | A:- | S:" + strings.Repeat("s", machinecontract.SQuotaMidRunes+1), "fras_s_too_long"},
		{"S low", "t[DB3S]: F:x | R:- | A:- | S:" + strings.Repeat("s", 51), "fras_s_too_long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": databaseVolume(test.line)})
			set, err := Load(root, "aoci.txt")
			if err == nil || !hasFinding(set.Errors, test.code) {
				t.Fatalf("missing %s: err=%v findings=%#v", test.code, err, set.Errors)
			}
			if got := set.Volumes["database"].Objects[0].CanonicalLine; got != test.line {
				t.Fatal("validator truncated or rewrote rejected semantics")
			}
		})
	}
}

func TestFRASV2AcceptsMidSQuotaBoundary(t *testing.T) {
	line := "t[DB7S]: F:x | R:- | A:- | S:" + strings.Repeat("字", machinecontract.SQuotaMidRunes)
	root := writeFixture(t, map[string]string{
		"aoci.txt":          rootText("meta", "database"),
		"aoci.meta.txt":     validMeta(),
		"aoci.database.txt": databaseVolume(line),
	})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatalf("C7 S quota boundary was rejected: %v findings=%#v", err, set.Errors)
	}
	if got := set.Volumes["database"].Objects[0].CanonicalLine; got != line {
		t.Fatal("validator rewrote accepted boundary semantics")
	}
}

func TestFRASV2RequiresCanonicalFieldsAndCompleteTag(t *testing.T) {
	for _, line := range []string{
		"users[DB9S]: R:- | F:store users | A:- | S:-",
		"users[DB9S]: F:store users | R:- | A:- | S1:not canonical",
		"users[DB9S]: F:store users | R:- | A:- | S:- | Note:extra",
		"users[D.9.S]: F:store users | R:- | A:- | S:-",
	} {
		root := writeFixture(t, map[string]string{"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": databaseVolume(line)})
		set, err := Load(root, "aoci.txt")
		if err == nil || (!hasFinding(set.Errors, "fras_structure_invalid") && !hasFinding(set.Errors, "fras_tag_invalid")) {
			t.Fatalf("non-canonical Volume Entry was accepted: %s findings=%#v", line, set.Errors)
		}
	}
}

func TestDamagedLayoutsFailClosed(t *testing.T) {
	cases := map[string]map[string]string{
		"damaged marker":         {"aoci.txt": "#AOCI-ROOT-MANIFEST: 2\n"},
		"missing meta":           {"aoci.txt": rootText("meta")},
		"missing code":           {"aoci.txt": rootText("meta", "code"), "aoci.meta.txt": validMeta()},
		"missing database":       {"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta()},
		"duplicate id":           {"aoci.txt": rootText("meta", "meta"), "aoci.meta.txt": validMeta()},
		"duplicate path":         {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n#Volume: id=code kind=code path=aoci.meta.txt format=object-fras-v2 depends=meta\n", "aoci.meta.txt": validMeta()},
		"path escape":            {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=../aoci.meta.txt format=meta-v1 depends=-\n"},
		"absolute path":          {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=/tmp/aoci.meta.txt format=meta-v1 depends=-\n"},
		"windows drive path":     {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=C:\\repo\\aoci.meta.txt format=meta-v1 depends=-\n"},
		"UNC path":               {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=\\\\server\\share\\aoci.meta.txt format=meta-v1 depends=-\n"},
		"dependency cycle":       {"aoci.txt": RootManifestMarker + "\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=code\n#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta\n", "aoci.meta.txt": validMeta(), "aoci.code.txt": CodeVolumeMarker + "\n"},
		"marker mismatch":        {"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": CodeVolumeMarker + "\n"},
		"duplicate table":        {"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": databaseVolume("users[DB9S]: F:x | R:- | A:- | S:-", "users[DB9S]: F:y | R:- | A:- | S:-")},
		"illegal FRAS":           {"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": databaseVolume("users[DB9S]: F:x | R:- | S:-")},
		"root business entry":    {"aoci.txt": rootText("meta") + "users[DB9S]: F:x | R:- | A:- | S:-\n", "aoci.meta.txt": validMeta()},
		"root copied dictionary": {"aoci.txt": rootText("meta") + "#[Tag dictionary: database]\n", "aoci.meta.txt": validMeta()},
		"meta copied layout":     {"aoci.txt": rootText("meta"), "aoci.meta.txt": validMeta() + "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n"},
		"database copied rules":  {"aoci.txt": rootText("meta", "database"), "aoci.meta.txt": validMeta(), "aoci.database.txt": databaseVolume("users[DB9S]: F:x | R:- | A:- | S:-") + "#FRAS-Discipline: 2\n"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeFixture(t, files)
			set, err := Load(root, "aoci.txt")
			if err == nil || set == nil || set.LayoutMode != LayoutVolumesV1 {
				t.Fatalf("layout did not fail closed: set=%#v err=%v", set, err)
			}
		})
	}
}

func TestUnsafeDescriptorIsRejectedBeforeFileAccess(t *testing.T) {
	parent := t.TempDir()
	repository := filepath.Join(parent, "repository")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := validMeta() + "#outside-sentinel\n"
	if err := os.WriteFile(filepath.Join(parent, "aoci.meta.txt"), []byte(outside), 0o644); err != nil {
		t.Fatal(err)
	}
	root := RootManifestMarker + "\n#Volume: id=meta kind=meta path=../aoci.meta.txt format=meta-v1 depends=-\n"
	if err := os.WriteFile(filepath.Join(repository, "aoci.txt"), []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}

	set, err := Load(repository, "aoci.txt")
	if err == nil || !hasFinding(set.Errors, "volume_descriptor_invalid") {
		t.Fatalf("unsafe descriptor was not rejected: err=%v findings=%#v", err, set.Errors)
	}
	if len(set.Meta.Raw) != 0 || len(set.Volumes["meta"].Raw) != 0 {
		t.Fatal("loader read an unsafe descriptor before rejecting it")
	}
}

func TestLineEndingsBOMAndUnmanagedCandidate(t *testing.T) {
	rootContent := "\ufeff" + strings.ReplaceAll(rootText("meta"), "\n", "\r\n")
	root := writeFixture(t, map[string]string{
		"aoci.txt":          rootContent,
		"aoci.meta.txt":     "\ufeff" + strings.ReplaceAll(validMeta(), "\n", "\r\n"),
		"aoci.database.txt": databaseVolume("ignored[DB9S]: F:not declared | R:- | A:- | S:-"),
	})
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Warnings) != 1 || set.Warnings[0].Code != "unmanaged_volume_candidate" || set.Volumes["database"] != nil {
		t.Fatalf("unmanaged candidate changed cognition: %#v", set)
	}
}

func TestRawLineEndingsRemainIdentitySignificant(t *testing.T) {
	lfRoot := writeFixture(t, map[string]string{"aoci.txt": rootText("meta"), "aoci.meta.txt": validMeta()})
	crlfRoot := writeFixture(t, map[string]string{
		"aoci.txt":      strings.ReplaceAll(rootText("meta"), "\n", "\r\n"),
		"aoci.meta.txt": strings.ReplaceAll(validMeta(), "\n", "\r\n"),
	})
	lf, err := Load(lfRoot, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	crlf, err := Load(crlfRoot, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if lf.Root.SHA256 == crlf.Root.SHA256 || lf.Meta.SHA256 == crlf.Meta.SHA256 || lf.CompositeIdentity == crlf.CompositeIdentity {
		t.Fatal("raw-byte identity normalized CRLF and LF")
	}
}

func TestDatabaseQualityFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "volumes", "database-quality")
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	database := set.Volumes["database"]
	if database == nil || database.ObjectCount != 8 {
		t.Fatalf("database quality fixture count: %#v", database)
	}
	text := string(database.Raw)
	for _, schemaDumpToken := range []string{"field_50", "created_at,timestamp", "ref_10"} {
		if strings.Contains(text, schemaDumpToken) {
			t.Fatalf("FRAS copied schema detail %q", schemaDumpToken)
		}
	}
	for _, forbiddenObject := range []string{"PRIMARY KEY[", "FOREIGN KEY[", "customer_id[", "Relationship["} {
		if strings.Contains(text, forbiddenObject) {
			t.Fatalf("non-table object escaped into Database v1: %q", forbiddenObject)
		}
	}
	limitsSeen := map[string]bool{}
	entriesByName := map[string]Object{}
	for _, object := range database.Objects {
		if object.Entry.F == "" || object.Entry.R == "" || object.Entry.Api == "" || object.Entry.S == "" {
			t.Fatalf("incomplete table FRAS: %s", object.CanonicalLine)
		}
		limitsSeen[object.Name] = true
		entriesByName[object.Name] = object
	}
	for _, representative := range []string{"country_codes", "customers", "customer_profiles", "orders", "order_dependencies", "payments", "audit_events", "customer_roles"} {
		if !limitsSeen[representative] {
			t.Fatalf("missing representative table %s", representative)
		}
	}
	for _, wideTable := range []string{"customer_profiles", "orders"} {
		if got := utf8.RuneCountInString(entriesByName[wideTable].Entry.F); got > 80 {
			t.Fatalf("wide table %s expanded F to %d characters", wideTable, got)
		}
	}
	if got := countListItems(entriesByName["order_dependencies"].Entry.R); got != 4 {
		t.Fatalf("multi-FK fixture copied the relationship graph: R items=%d", got)
	}
	if got := countListItems(entriesByName["orders"].Entry.Api); got != 4 {
		t.Fatalf("wide transaction table A density changed: items=%d", got)
	}
	if entriesByName["audit_events"].Entry.S == "-" || !strings.Contains(strings.ToLower(entriesByName["audit_events"].Entry.S), "append-only") {
		t.Fatal("append-only fixture lost its non-inferable lifecycle constraint")
	}
}

func TestCanonicalDatabaseObjectReference(t *testing.T) {
	for _, valid := range []string{
		"database://primary/public/users",
		"database://warehouse_1/reporting-v2/order_$facts",
	} {
		if !IsCanonicalDatabaseRef(valid) {
			t.Fatalf("canonical database reference was rejected: %q", valid)
		}
	}
	for _, invalid := range []string{
		"users", "code:users", "database://primary/public", "database://primary/public/users/column",
		"database://primary/public/users\nforged", "database:///public/users", "database://primary/Public.Users",
	} {
		if IsCanonicalDatabaseRef(invalid) {
			t.Fatalf("invalid database reference was accepted: %q", invalid)
		}
	}
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
