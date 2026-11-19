package volumegovernance

import (
	"github.com/aoci-spec/aoci-code/internal/baseline"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A collectable SQLite source must participate in governance, rather than
// remain an informational declaration that silently contributes no Evidence.
func TestSQLiteSourceRequiresEvidenceForGovernance(t *testing.T) {
	root, cfg := alignedFixture(t, true, true)
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	named := false
	for _, finding := range facts.Findings {
		named = named || finding.Code == FindingCodeDatabaseEngineNotImplemented && finding.Target == "local"
	}
	if facts.Result != ResultEvidenceRequired || facts.GovernanceAligned || facts.NextRequiredAction != "snapshot_or_repair_evidence" || named {
		t.Fatalf("SQLite Evidence was not required: result=%s next=%s findings=%#v", facts.Result, facts.NextRequiredAction, facts.Findings)
	}
}

func TestMongoDBDeclarationKeepsGovernanceAligned(t *testing.T) {
	for _, withDatabase := range []bool{false, true} {
		root, cfg := alignedFixture(t, true, withDatabase)
		cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "app", Engine: dbevidence.EngineMongoDB,
			Database: "app", CredentialEnv: "AOCI_DB_APP_URI", Enabled: true})
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		set, err := cognition.Load(root, cfg.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := Assess(root, cfg, set)
		if err != nil {
			t.Fatal(err)
		}
		if !facts.GovernanceAligned || facts.DatabaseCognition.BlockingSourceCount != 0 {
			t.Fatalf("declaration blocked governance (database volume=%t): %+v", withDatabase, facts)
		}
	}
}

// With only MongoDB declarations and no Database Entry there is no Evidence
// to accept: a missing Evidence Baseline owes nothing, with or without an
// (empty) Database Volume.
func TestMongoDBOnlyRepositoryOwesNoEvidenceBaseline(t *testing.T) {
	for _, withDatabase := range []bool{false, true} {
		root, cfg := alignedFixture(t, true, withDatabase)
		cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "app", Engine: dbevidence.EngineMongoDB,
			Database: "app", CredentialEnv: "AOCI_DB_APP_URI", Enabled: true}}
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(dbevidence.BaselinePath(root))
		if withDatabase {
			// An empty Database Volume, recorded in the Baseline as it stands.
			volume := filepath.Join(root, "aoci.database.txt")
			if err := os.WriteFile(volume, []byte(cognition.DatabaseMarker+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			state, _, err := baseline.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint, err := baseline.HashFile(volume)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint.Role = state.Files["aoci.database.txt"].Role
			state.Files["aoci.database.txt"] = fingerprint
			state.DatabaseCognition = nil
			if err := baseline.Save(root, state); err != nil {
				t.Fatal(err)
			}
		}
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		set, err := cognition.Load(root, cfg.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		if withDatabase && (set.Volumes[cognition.ScopeDatabase] == nil || set.Volumes[cognition.ScopeDatabase].ObjectCount != 0) {
			t.Fatalf("the fixture must hold an empty Database Volume: %+v", set.Volumes[cognition.ScopeDatabase])
		}
		facts, err := Assess(root, cfg, set)
		if err != nil {
			t.Fatal(err)
		}
		if !facts.GovernanceAligned || facts.DatabaseEvidence.State == ResultEvidenceRequired {
			t.Fatalf("a MongoDB-only repository is not aligned (database volume=%t): state=%s findings=%+v", withDatabase, facts.DatabaseEvidence.State, facts.Findings)
		}
		for _, finding := range facts.Findings {
			if finding.Code == "database_evidence_baseline_required" || finding.Code == "database_evidence_required" {
				t.Fatalf("a MongoDB-only repository owes an Evidence Baseline (database volume=%t): %+v", withDatabase, facts.Findings)
			}
		}
	}
}

// Without a Database Volume a disabled MongoDB declaration owes nothing:
// it is never reported as a disabled source owing Evidence.
func TestDisabledMongoDBDeclarationWithoutVolumeKeepsGovernanceAligned(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "parked", Engine: dbevidence.EngineMongoDB,
		Database: "app", CredentialEnv: "AOCI_DB_APP_URI", Enabled: false}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range facts.Findings {
		if finding.Code == "source_disabled" || finding.Code == "database_source_disabled" {
			t.Fatalf("a disabled MongoDB declaration was reported as owing: %+v", facts.Findings)
		}
	}
	if !facts.GovernanceAligned {
		t.Fatalf("a disabled MongoDB declaration broke alignment: %+v", facts.Findings)
	}
}
