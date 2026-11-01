package volumegovernance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A declared, enabled, collectable source counts before its Database Volume
// exists. The absent-Volume branch used to assess no sources at all, so a
// repository could declare a source, never snapshot it, and stay aligned.
func TestDeclaredSourceWithoutVolumeRequiresEvidenceThenBootstrap(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	assess := func() *Facts {
		t.Helper()
		loaded, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		set, err := cognition.Load(root, loaded.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := Assess(root, loaded, set)
		if err != nil {
			t.Fatal(err)
		}
		return facts
	}

	facts := assess()
	if facts.GovernanceAligned || facts.Result != ResultEvidenceRequired ||
		facts.NextRequiredAction != machinecontract.DatabaseCognitionActionSnapshotOrRepair ||
		facts.DatabaseEvidence.Configured != 1 || facts.DatabaseCognition.BlockingSourceCount != 1 {
		t.Fatalf("declared source without Evidence stayed aligned: result=%s next=%s evidence=%+v findings=%+v",
			facts.Result, facts.NextRequiredAction, facts.DatabaseEvidence, facts.Findings)
	}
	named := false
	for _, finding := range facts.Findings {
		named = named || finding.Code == machinecontract.DatabaseCognitionEvidenceUnavailable && finding.Target == "local"
	}
	if !named {
		t.Fatalf("the blocking source is not named: %+v", facts.Findings)
	}

	// Accepted Evidence with the Volume still absent routes to bootstrap.
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "local",
		Engine: dbevidence.EngineSQLite, Database: "data/app.db", Namespaces: []string{"main"}, IncludeNamespaces: []string{},
		ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_ascii_case_insensitive"}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://local/main/items",
		Engine: dbevidence.EngineSQLite, SourceID: "local", Database: "data/app.db", Namespace: "main", Name: "items", Kind: "base_table",
		Columns:    []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "INTEGER", CanonicalType: "integer", Nullable: false}},
		PrimaryKey: &dbevidence.KeyConstraint{Name: "sqlite_primary_key", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{},
		ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	facts = assess()
	if facts.GovernanceAligned || facts.Result != ResultEvidenceRequired ||
		facts.NextRequiredAction != machinecontract.DatabaseCognitionActionBootstrapVolume || facts.DatabaseEvidence.Accepted != 1 {
		t.Fatalf("accepted Evidence without a Volume did not route to bootstrap: result=%s next=%s evidence=%+v findings=%+v",
			facts.Result, facts.NextRequiredAction, facts.DatabaseEvidence, facts.Findings)
	}

	// A disabled source governs nothing before a Volume exists.
	cfg.DatabaseSources[0].Enabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if facts = assess(); !facts.GovernanceAligned || facts.Result != ResultAligned {
		t.Fatalf("a disabled source blocked a Code-only repository: result=%s findings=%+v", facts.Result, facts.Findings)
	}
	_ = os.Remove(filepath.Join(root, ".aoci", "database-baseline.json"))
}
