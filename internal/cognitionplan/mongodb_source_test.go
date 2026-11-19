package cognitionplan

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestMongoDBDeclarationDoesNotRecommendDatabaseBootstrap(t *testing.T) {
	for _, retainedSnapshot := range []bool{false, true} {
		root := t.TempDir()
		writeFile(t, root, "src/main.go", "package main\n")
		if retainedSnapshot {
			installDatabaseEvidence(t, root)
		}
		cfg := config.DefaultConfig()
		cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EngineMongoDB,
			Database: "fixture", CredentialEnv: "AOCI_DB_PRIMARY_URI", Enabled: true}}
		if err := config.Save(root, cfg); err != nil {
			t.Fatal(err)
		}
		plan, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.NetworkAccessed || len(plan.Evidence) != 0 || containsString(plan.RecommendedKinds, "database") {
			t.Fatalf("declaration entered Database bootstrap (retained snapshot=%t): %+v", retainedSnapshot, plan)
		}
	}
}
