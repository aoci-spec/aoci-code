package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A declared-only source (an engine with no collector yet) blocks nothing in
// Maintain, and the final Apply's alignment inspector agrees (audit G2-03: it
// reported engine_not_implemented, so the last batch never carried
// terminal_proof).
func TestAlignmentInspectorIgnoresDeclaredOnlySources(t *testing.T) {
	root := buildThreeTableDatabaseVolumeRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{
		SourceID: "documents", Engine: dbevidence.EngineMongoDB, Database: "app",
		CredentialEnv: "TEST_ONLY_MONGO", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatalf("load: %+v", fail)
	}
	outcome := &AtomicBatchOutcome{Volumes: []string{"database"}, Items: []*UpdateOutcome{{Action: "no_change", Rel: "database://primary/public/users"}}, BaselineComplete: true}
	_, _, findings, _, _ := inspectCognitionVolumeAlignment(root, "test", loaded, outcome)
	for _, finding := range findings {
		if strings.Contains(finding, "engine_not_implemented") {
			t.Fatalf("a declared-only source must not block alignment: %v", findings)
		}
	}
}
