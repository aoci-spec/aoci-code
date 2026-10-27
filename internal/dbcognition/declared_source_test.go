package dbcognition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A SQLite source is a declaration until its collector lands. It neither feeds
// nor blocks Database Cognition: beside the source that fed the Volume the
// assessment stays current, beside accepted Evidence an absent Volume is still
// routed to Bootstrap, and on its own it is the no-configuration case.
func TestDeclaredOnlySourceNeitherFeedsNorBlocks(t *testing.T) {
	declared := dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite, Path: "data/app.db",
		Namespaces: []string{"main"}, Enabled: true}

	root, sources := databaseFixture(t, []string{"users"}, []string{"users"})
	set := loadFixtureSet(t, root)
	state := baseline.NewBaseline(nil)
	users := itemByRef(t, Assess(root, sources, set, state), "database://primary/public/users")
	if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
		ObjectRef: users.ObjectRef, SourceID: users.SourceID, EvidenceVersion: users.EvidenceVersion,
		TableEvidenceSHA256: users.TableEvidenceSHA256, EntrySHA256: EntrySHA256(users.CurrentEntry),
	}); err != nil {
		t.Fatal(err)
	}
	beside := Assess(root, append(append([]dbevidence.SourceConfig{}, sources...), declared), set, state)
	if !beside.CognitionCurrent || beside.BlockingSourceCount != 0 || beside.NextAction != machinecontract.DatabaseCognitionActionNoActionRequired ||
		declaredSourceState(beside, "local") != machinecontract.DatabaseCognitionEngineNotImplemented {
		t.Fatalf("a declared-only source blocked the Database Volume it does not feed: %#v", beside)
	}

	absent, absentSources := databaseFixture(t, []string{"users"}, nil)
	rootPath := filepath.Join(absent, "aoci.txt")
	rootRaw, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw = []byte(strings.ReplaceAll(string(rootRaw), "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n", ""))
	if err := os.WriteFile(rootPath, rootRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(absent, "aoci.database.txt")); err != nil {
		t.Fatal(err)
	}
	_, snapshot, exists, err := dbevidence.LoadSnapshot(absent, "primary")
	if err != nil || !exists {
		t.Fatalf("load snapshot: exists=%t err=%v", exists, err)
	}
	if err := dbevidence.AcceptSnapshot(absent, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	absentSet := loadFixtureSet(t, absent)
	routed := Assess(absent, append(absentSources, declared), absentSet, baseline.NewBaseline(nil))
	if routed.BlockingSourceCount != 0 || routed.NextAction != machinecontract.DatabaseCognitionActionBootstrapVolume ||
		declaredSourceState(routed, "local") != machinecontract.DatabaseCognitionEngineNotImplemented {
		t.Fatalf("a declared-only source kept accepted Evidence from Bootstrap: %#v", routed)
	}
	alone := Assess(absent, []dbevidence.SourceConfig{declared}, absentSet, baseline.NewBaseline(nil))
	if !alone.CognitionCurrent || alone.NextAction != machinecontract.DatabaseCognitionActionNoConfiguration ||
		declaredSourceState(alone, "local") != machinecontract.DatabaseCognitionEngineNotImplemented {
		t.Fatalf("declarations alone were not the no-configuration case: %#v", alone)
	}
}

func declaredSourceState(assessment Assessment, sourceID string) string {
	for _, source := range assessment.Sources {
		if source.SourceID == sourceID {
			return source.State
		}
	}
	return ""
}
