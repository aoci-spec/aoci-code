package cognition

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

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
