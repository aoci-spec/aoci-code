package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/textassets"
)

// While Code siblings are issued in parallel, Database work waits: a Database
// batch applied beside them stales every sibling still being authored, and
// remaining still counts the Database debt held back (audit G2-05).
func TestParallelCodeSiblingsHoldDatabaseWorkBack(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root, tablesBySource, _ := buildDatabaseAgentNativeRepo(t)
	session := connectMCPClient(t, root)
	initial := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if initial.Plan == nil {
		t.Fatal("fixture did not produce initial Database candidates")
	}
	if applied := callVolumeTool(t, session, "aoci_update_entry", databaseAgentBatchArguments(t, initial.Plan, databaseAgentEntries(false))); !json.Valid([]byte(applied)) {
		t.Fatalf("initial Database batch: %s", applied)
	}

	for _, rel := range []string{"internal/domain/accounts.go", "internal/domain/orders.go", "internal/domain/audit.go",
		"internal/domain/catalog.go", "internal/domain/payments.go"} {
		before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		writeVolumeTestFile(t, root, rel, string(before)+"// test-only change\n")
	}
	pgTables := append([]dbevidence.TableEvidence{}, tablesBySource["pgtemp"]...)
	for index := range pgTables {
		if pgTables[index].Name == "orders" {
			pgTables[index].Columns = append(pgTables[index].Columns, dbevidence.Column{Ordinal: len(pgTables[index].Columns) + 1,
				Name: "governance_revision", NativeType: "integer", CanonicalType: "integer", Nullable: false})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), pgTables)

	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionBatchEntries(2); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCodeCognitionParallelBatches(2); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", map[string]any{})), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.CodePlan == nil || maintain.CodePlan.LogicalPlan == "" {
		t.Skipf("fixture did not issue parallel Code siblings: %+v", maintain.CodePlan)
	}
	for _, candidate := range maintain.Candidates {
		if candidate.Domain == cognition.ScopeDatabase {
			t.Fatalf("a Database candidate rode beside parallel Code siblings: %+v", candidate)
		}
	}
	if maintain.DatabasePlan != nil {
		t.Fatalf("a Database plan was issued beside parallel Code siblings: %+v", maintain.DatabasePlan)
	}
	if maintain.Batch.Remaining <= maintain.CodePlan.Remaining || !maintain.Batch.ContinuationRequired {
		t.Fatalf("held-back Database work is missing from remaining: batch=%+v code=%+v", maintain.Batch, maintain.CodePlan)
	}
}
