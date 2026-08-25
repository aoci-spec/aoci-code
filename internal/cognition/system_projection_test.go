package cognition

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestProjectModuleCognitionNormalizesPathAndKeepsSubtreeBoundary(t *testing.T) {
	set := projectModuleFixture(t)
	projection, err := BuildProjectModuleCognition(set, "module\\")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Version != machinecontract.ProjectModuleCognitionV1 || projection.ModulePath != "module" ||
		!projection.Derived || projection.Authoritative || projection.Persisted || projection.SourceBound ||
		projection.NetworkAccessed || projection.BusinessDataRead || projection.ProjectionIdentity == "" {
		t.Fatalf("unexpected module projection contract: %#v", projection)
	}
	if got, want := projection.Objects, []ProjectModuleObject{
		{ObjectRef: "code:module/a.go", CanonicalEntry: projectModuleLine("a.go", "-")},
		{ObjectRef: "code:module/b.go", CanonicalEntry: projectModuleLine("b.go", "code:outside/inbound.go,database://primary/public/orders")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("module boundary selection:\nwant %#v\ngot  %#v", want, got)
	}
	for _, unsafe := range []string{"", ".", "../module", "/module", "C:\\module"} {
		if _, err := BuildProjectModuleCognition(set, unsafe); err == nil {
			t.Fatalf("unsafe module path was accepted: %q", unsafe)
		}
	}
}

func TestProjectModuleCognitionIdentityIsDeterministicAndModuleBound(t *testing.T) {
	set := projectModuleFixture(t)
	first, err := BuildProjectModuleCognition(set, "module")
	if err != nil {
		t.Fatal(err)
	}
	set.Volumes[ScopeCode].Objects[0], set.Volumes[ScopeCode].Objects[1] =
		set.Volumes[ScopeCode].Objects[1], set.Volumes[ScopeCode].Objects[0]
	second, err := BuildProjectModuleCognition(set, "module/")
	if err != nil {
		t.Fatal(err)
	}
	set.Volumes[ScopeCode].Objects[0], set.Volumes[ScopeCode].Objects[1] =
		set.Volumes[ScopeCode].Objects[1], set.Volumes[ScopeCode].Objects[0]
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equivalent module paths were not deterministic:\nfirst %#v\nsecond %#v", first, second)
	}

	set.CompositeIdentity = strings.Repeat("d", 64)
	set.Volumes[ScopeCode].Objects[2].CanonicalLine = projectModuleLine("skip.go", "-") + " "
	unrelated, err := BuildProjectModuleCognition(set, "module")
	if err != nil {
		t.Fatal(err)
	}
	if unrelated.ProjectionIdentity != first.ProjectionIdentity || unrelated.CompositeIdentity == first.CompositeIdentity {
		t.Fatalf("unrelated Code change affected module identity: first=%#v next=%#v", first, unrelated)
	}

	setImpactObjectRelation(t, set, ScopeCode, "code:outside/inbound.go", "-")
	relationChanged, err := BuildProjectModuleCognition(set, "module")
	if err != nil {
		t.Fatal(err)
	}
	if relationChanged.ProjectionIdentity == first.ProjectionIdentity {
		t.Fatal("touching explicit relation did not affect module identity")
	}

	set.Volumes[ScopeCode].Objects[1].CanonicalLine = projectModuleLine("a.go", "-") + " "
	changed, err := BuildProjectModuleCognition(set, "module")
	if err != nil {
		t.Fatal(err)
	}
	if changed.ProjectionIdentity == first.ProjectionIdentity {
		t.Fatal("selected canonical entry did not affect module identity")
	}

	rootChanged := projectModuleFixture(t)
	rootChanged.Root.SHA256 = strings.Repeat("e", 64)
	rootProjection, err := BuildProjectModuleCognition(rootChanged, "module")
	if err != nil {
		t.Fatal(err)
	}
	if rootProjection.ProjectionIdentity == first.ProjectionIdentity {
		t.Fatal("Root identity did not affect module identity")
	}
	rootChanged.Root.SHA256 = first.RootSHA256
	rootChanged.Meta.SHA256 = strings.Repeat("f", 64)
	metaProjection, err := BuildProjectModuleCognition(rootChanged, "module")
	if err != nil {
		t.Fatal(err)
	}
	if metaProjection.ProjectionIdentity == first.ProjectionIdentity {
		t.Fatal("Meta identity did not affect module identity")
	}
}

func TestProjectModuleCognitionKeepsOnlyExplicitTouchingRelations(t *testing.T) {
	set := projectModuleFixture(t)
	beforeObjects := append([]Object(nil), set.Volumes[ScopeCode].Objects...)
	beforeOrder := append([]string(nil), set.DeclaredOrder...)
	projection, err := BuildProjectModuleCognition(set, "module")
	if err != nil {
		t.Fatal(err)
	}
	want := []SystemRelation{
		{From: "code:module/b.go", To: "code:outside/inbound.go", Kind: "cognition_relation", Authority: "model_authored_R"},
		{From: "code:module/b.go", To: "database://primary/public/orders", Kind: "cognition_relation", Authority: "model_authored_R"},
		{From: "code:outside/inbound.go", To: "code:module/a.go", Kind: "cognition_relation", Authority: "model_authored_R"},
	}
	if !reflect.DeepEqual(projection.Relations, want) {
		t.Fatalf("touching explicit relations:\nwant %#v\ngot  %#v", want, projection.Relations)
	}
	if !reflect.DeepEqual(set.Volumes[ScopeCode].Objects, beforeObjects) || !reflect.DeepEqual(set.DeclaredOrder, beforeOrder) {
		t.Fatal("module projection mutated the active cognition set")
	}
}

func TestProjectModuleCognitionRejectsEmptyAndLegacySets(t *testing.T) {
	set := projectModuleFixture(t)
	if _, err := BuildProjectModuleCognition(set, "missing"); err == nil {
		t.Fatal("empty module projection was accepted")
	}
	set.LayoutMode = LayoutLegacyMonolithic
	if _, err := BuildProjectModuleCognition(set, "module"); err == nil {
		t.Fatal("legacy cognition set was accepted")
	}
}

func TestSystemProjectionDerivesLineageAndDatabaseImpactWithoutState(t *testing.T) {
	set, root := loadImpactFixture(t, []string{
		codeImpactLine("repository.go", "database://primary/public/orders"),
		codeImpactLine("handler.go", "code:src/repository.go"),
	}, primaryDatabaseImpact(databaseImpactLine("orders", "-")))
	for _, name := range []string{"repository.go", "handler.go"} {
		path := filepath.Join(root, "src", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := baseline.NewBaseline(map[string]baseline.Fingerprint{})
	for _, name := range []string{"repository.go", "handler.go"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, "src", name))
		if err != nil {
			t.Fatal(err)
		}
		state.Files["src/"+name] = fingerprint
	}
	orders := set.Volumes[ScopeDatabase].Objects[0]
	if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
		ObjectRef: orders.CanonicalRef, SourceID: "primary", EvidenceVersion: "database-evidence/v1",
		TableEvidenceSHA256: strings.Repeat("a", 64), EntrySHA256: testLineSHA256(orders.CanonicalLine),
	}); err != nil {
		t.Fatal(err)
	}
	projection, err := BuildSystemProjection(set, state)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Derived || projection.Authoritative || projection.ProjectionIdentity == "" ||
		projection.NetworkAccessed || projection.BusinessDataRead || len(projection.Findings) != 0 {
		t.Fatalf("unexpected projection contract: %#v", projection)
	}
	for _, record := range projection.Lineage {
		if record.Status != "current" {
			t.Fatalf("lineage is not current: %#v", record)
		}
	}
	impact, err := ResolveDatabaseImpact(projection, orders.CanonicalRef)
	if err != nil {
		t.Fatal(err)
	}
	if !impact.Complete || len(impact.AffectedCodeObjects) != 2 ||
		impact.AffectedCodeObjects[0].ObjectRef != "code:src/handler.go" || impact.AffectedCodeObjects[0].Distance != 2 ||
		impact.AffectedCodeObjects[1].ObjectRef != "code:src/repository.go" || impact.AffectedCodeObjects[1].Distance != 1 {
		t.Fatalf("database impact did not follow explicit relations: %#v", impact)
	}
}

func testLineSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// 系统投影是投影而不是校验: 指不到受管对象的 R 只是投不出边, 既不是缺陷,
// 也不让投影"不完整"。R 允许指向尚未创作、已经移除或不受管的东西。
func TestSystemProjectionSkipsUnresolvableRelationsWithoutFindings(t *testing.T) {
	set, _ := loadImpactFixture(t, []string{codeImpactLine("repository.go", "database://primary/public/missing")},
		primaryDatabaseImpact(databaseImpactLine("orders", "-")))
	projection, err := BuildSystemProjection(set, baseline.NewBaseline(nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, relation := range projection.Relations {
		if relation.To == "database://primary/public/missing" {
			t.Fatalf("投影不应凭空造出对象: %#v", projection.Relations)
		}
	}
	impact, err := ResolveDatabaseImpact(projection, "database://primary/public/orders")
	if err != nil {
		t.Fatal(err)
	}
	if !impact.Complete || len(impact.Findings) != 0 {
		t.Fatalf("关系指不到不应让投影不完整: %#v", impact)
	}
}

func TestCognitionEvolutionComparesExplicitDerivedSnapshots(t *testing.T) {
	previous := &CognitionSnapshot{Version: CognitionSnapshotV1, ProjectionIdentity: strings.Repeat("1", 64), Derived: true,
		Objects: []CognitionSnapshotObject{
			{ObjectRef: "code:a.go", Domain: ScopeCode, ObjectSHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64)},
			{ObjectRef: "database://primary/public/orders", Domain: ScopeDatabase, ObjectSHA256: strings.Repeat("c", 64), EvidenceSHA256: strings.Repeat("d", 64)},
			{ObjectRef: "code:removed.go", Domain: ScopeCode, ObjectSHA256: strings.Repeat("e", 64)},
		}}
	current := &CognitionSnapshot{Version: CognitionSnapshotV1, ProjectionIdentity: strings.Repeat("2", 64), Derived: true,
		Objects: []CognitionSnapshotObject{
			{ObjectRef: "code:a.go", Domain: ScopeCode, ObjectSHA256: strings.Repeat("f", 64), SourceSHA256: strings.Repeat("b", 64)},
			{ObjectRef: "database://primary/public/orders", Domain: ScopeDatabase, ObjectSHA256: strings.Repeat("c", 64), EvidenceSHA256: strings.Repeat("0", 64)},
			{ObjectRef: "code:new.go", Domain: ScopeCode, ObjectSHA256: strings.Repeat("1", 64)},
		}}
	evolution, err := CompareCognitionSnapshots(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	if evolution.Version != CognitionEvolutionV1 || !evolution.Derived || len(evolution.Changes) != 4 ||
		evolution.Summary.Created != 1 || evolution.Summary.Removed != 1 ||
		evolution.Summary.SemanticChanged != 1 || evolution.Summary.LineageChanged != 1 || evolution.Summary.Unchanged != 0 {
		t.Fatalf("unexpected evolution result: %#v", evolution)
	}
}

func TestSystemProjectionDoesNotChangeMCPToolContract(t *testing.T) {
	if got := len(machinecontract.MCPToolNames()); got != 9 {
		t.Fatalf("system projection changed MCP tool count: %d", got)
	}
}

func projectModuleFixture(t *testing.T) *Set {
	t.Helper()
	objects := []Object{
		projectModuleObject(t, ScopeCode, "code:module/b.go", "b.go", "code:outside/inbound.go,database://primary/public/orders"),
		projectModuleObject(t, ScopeCode, "code:module/a.go", "a.go", "-"),
		projectModuleObject(t, ScopeCode, "code:moduleish/skip.go", "skip.go", "-"),
		projectModuleObject(t, ScopeCode, "code:outside/inbound.go", "inbound.go", "code:module/a.go"),
		projectModuleObject(t, ScopeCode, "code:outside/skip.go", "skip.go", "database://primary/public/orders"),
	}
	database := projectModuleObject(t, ScopeDatabase, "database://primary/public/orders", "orders", "-")
	return &Set{
		LayoutMode: LayoutVolumesV1, LayoutVersion: "1",
		Root: Asset{State: AssetPresent, SHA256: strings.Repeat("a", 64)},
		Meta: Asset{State: AssetPresent, SHA256: strings.Repeat("b", 64)},
		Volumes: map[string]*Asset{
			ScopeCode:     {State: AssetPresent, Objects: objects},
			ScopeDatabase: {State: AssetPresent, Objects: []Object{database}},
		},
		DeclaredOrder: []string{ScopeCode, ScopeDatabase}, CompositeIdentity: strings.Repeat("c", 64),
	}
}

func projectModuleObject(t *testing.T, volumeID, ref, name, relation string) Object {
	t.Helper()
	line := projectModuleLine(name, relation)
	if volumeID == ScopeDatabase {
		line = name + "[DB9S]: F:store the module projection fixture | R:" + relation + " | A:- | S:-"
	}
	entry, ok := index.ParseEntryLine(line, 1)
	if !ok {
		t.Fatalf("cannot parse module fixture entry: %s", line)
	}
	return Object{VolumeID: volumeID, Kind: "file", Name: name, CanonicalRef: ref, Entry: entry, CanonicalLine: entry.FullLine}
}

func projectModuleLine(name, relation string) string {
	return name + "[CD9S]: F:coordinate the module projection fixture | R:" + relation + " | A:- | S:-"
}
