package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/textassets"
)

// The removal guard demanded saved Evidence from every enabled source, so a
// declared SQLite source, which can never have any, kept an orphan Database
// Entry from being removed. The guard now binds the collectable sources only.
func TestDatabaseOrphanRemovesBesideADeclaredOnlySource(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root, tablesBySource, _ := buildDatabaseAgentNativeRepo(t)
	session := connectMCPClient(t, root)
	initial := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if initial.Plan == nil {
		t.Fatal("test-only fixture did not produce Database authoring targets")
	}
	if output := callVolumeTool(t, session, "aoci_update_entry", databaseAgentBatchArguments(t, initial.Plan, databaseAgentEntries(false))); !strings.Contains(output, `"status":"applied"`) {
		t.Fatalf("initial Database fixture did not align: %s", output)
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = append(cfg.DatabaseSources, dbevidence.SourceConfig{SourceID: "local", Engine: dbevidence.EngineSQLite,
		Path: "data/app.db", Namespaces: []string{"main"}, Enabled: true})
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	target := "database://pgtemp/aoci_d0/documents"
	remaining := make([]dbevidence.TableEvidence, 0, len(tablesBySource["pgtemp"])-1)
	for _, table := range tablesBySource["pgtemp"] {
		if table.ObjectRef != target {
			remaining = append(remaining, table)
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), remaining)
	if outcome, fail := ApplyRemoveEntry(root, target, "agent", true, false); fail != nil || outcome == nil {
		t.Fatalf("a declared-only source kept the Database orphan from being removed: outcome=%#v fail=%+v", outcome, fail)
	}
}
