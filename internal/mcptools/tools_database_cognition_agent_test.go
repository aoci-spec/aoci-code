package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
	"github.com/aoci-spec/aoci-code/textassets"
)

const databaseAgentNativePrompt = "Establish complete Database Cognition from the saved local Evidence in the existing Database Volume, investigating the related code and preserving still-supported constraints; do not connect to a database or modify business source."

func TestDatabaseCognitionAgentAuthoredProtocolReplay(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	started := time.Now()
	root, tablesBySource, businessPreimages := buildDatabaseAgentNativeRepo(t)
	fixtureSet, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	knownCode := map[string]bool{}
	for _, object := range fixtureSet.Volumes["code"].Objects {
		knownCode[object.CanonicalRef] = true
	}
	for _, required := range []string{
		"code:internal/domain/accounts.go", "code:internal/domain/orders.go", "code:internal/domain/contracts_test.go",
		"code:internal/repository/orders_repository.go", "code:internal/service/checkout_service.go",
		"code:internal/api/payments_handler.go", "code:migrations/postgresql.sql", "code:migrations/mysql.sql",
	} {
		if !knownCode[required] {
			t.Fatalf("isolated fixture lacks canonical Code object %s; got=%v", required, knownCode)
		}
	}
	session := connectMCPClient(t, root)

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 9 {
		t.Fatalf("MCP surface changed: tools=%d err=%v", len(listed.Tools), err)
	}
	if rules := callVolumeTool(t, session, "aoci_rules", map[string]any{}); !strings.Contains(rules, "aoci_maintain") {
		t.Fatal("Agent-native Rules did not establish the maintenance contract")
	}
	if overview := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll}); !strings.Contains(overview, "full_text_included: true") || !strings.Contains(overview, cognition.DatabaseMarker) {
		t.Fatal("Agent-native ordinary Overview did not deliver the complete Volumes scope")
	}

	// Experiment A replays complete entries authored by the current Host model
	// after reading the frozen Evidence and business fixtures. Keeping reviewed
	// outputs fixed makes the MCP/Apply regression deterministic; this test's wall
	// time is protocol runtime and is not presented as live model-generation time.
	maintainText := callVolumeTool(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase})
	var maintain databaseMaintainResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	if maintain.Status != autoStatusRepairRequired || maintain.Plan == nil || maintain.Plan.TargetCount != 12 ||
		maintain.Assessment.NetworkAccessed || !strings.Contains(maintain.AuthoringContract, "F/R/A/S") {
		t.Fatalf("initial Agent-native plan is incomplete: %#v", maintain)
	}
	arguments := databaseAgentBatchArguments(t, maintain.Plan, databaseAgentEntries(false))
	applyText := callVolumeTool(t, session, "aoci_update_entry", arguments)
	var applied autoResult
	if err := json.Unmarshal([]byte(applyText), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 12 {
		t.Fatalf("initial Agent-native batch did not apply: %#v", applied)
	}
	current := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if !current.Aligned || current.Assessment.Summary.Current != 12 || current.Assessment.NetworkAccessed {
		t.Fatalf("initial Database Cognition did not become current: %#v", current)
	}

	// Experiment B: only orders Evidence changes. The model rewrites that full
	// Entry and retains the still-supported transaction S constraint.
	pgTables := append([]dbevidence.TableEvidence{}, tablesBySource["pgtemp"]...)
	for index := range pgTables {
		if pgTables[index].Name == "orders" {
			pgTables[index].Columns = append(pgTables[index].Columns, dbevidence.Column{
				Ordinal: len(pgTables[index].Columns) + 1, Name: "fulfillment_revision",
				NativeType: "integer", CanonicalType: "integer", Nullable: false,
			})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), pgTables)
	changed := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if changed.Plan == nil || changed.Plan.TargetCount != 1 || changed.Assessment.Summary.Stale != 1 ||
		changed.Plan.Candidates[0].ObjectRef != "database://pgtemp/aoci_d0/orders" {
		t.Fatalf("unrelated tables were included after one Evidence change: %#v", changed)
	}
	updatedArguments := databaseAgentBatchArguments(t, changed.Plan, databaseAgentEntries(true))
	updatedText := callVolumeTool(t, session, "aoci_update_entry", updatedArguments)
	var updated autoResult
	if err := json.Unmarshal([]byte(updatedText), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status != autoStatusApplied || !updated.Aligned || updated.Applied != 1 {
		t.Fatalf("changed table did not return to current: %#v", updated)
	}
	volumeBytes, err := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
	if err != nil || !strings.Contains(string(volumeBytes), "order state and dependent ledger effects commit in one transaction") {
		t.Fatal("still-supported high-entropy orders S constraint was lost")
	}
	final := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if !final.Aligned || final.Assessment.Summary.Current != 12 {
		t.Fatalf("changed cognition did not return to current: %#v", final)
	}
	finalSet, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	totalF, totalR, totalA, totalS := 0, 0, 0, 0
	for _, object := range finalSet.Volumes["database"].Objects {
		totalF += utf8.RuneCountInString(object.Entry.F)
		totalR += utf8.RuneCountInString(object.Entry.R)
		totalA += utf8.RuneCountInString(object.Entry.Api)
		totalS += utf8.RuneCountInString(object.Entry.S)
	}
	databaseOverview := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeDatabase})

	for rel, before := range businessPreimages {
		after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || string(after) != before {
			t.Fatalf("Agent-native flow modified business source %s", rel)
		}
	}
	t.Logf("prompt=%q tables=12 maintain_calls=4 apply_calls=2 overview_calls=2 candidate_repairs=0 protocol_wall=%s authoring_wall=not_separately_metered host_tokens=unavailable evidence_bytes=%d average_evidence_bytes=%d average_fras_runes=F:%d,R:%d,A:%d,S:%d database_volume_bytes=%d database_overview_bytes=%d",
		databaseAgentNativePrompt, time.Since(started), maintain.Plan.EvidenceBytes, maintain.Plan.EvidenceBytes/12,
		totalF/12, totalR/12, totalA/12, totalS/12, len(volumeBytes), len(databaseOverview))
}

func TestFourLegalLayoutsOverviewAndNoArgumentMaintainStayAligned(t *testing.T) {
	for _, test := range []struct {
		name     string
		code     bool
		database bool
	}{
		{name: "root_meta"},
		{name: "code_only", code: true},
		{name: "database_only", database: true},
		{name: "code_database", code: true, database: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousLocale := textassets.ActiveLocale()
			t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
			root, _, _ := buildDatabaseAgentNativeRepo(t)
			rootText := volumeFileText(t, root, "aoci.txt")
			lines := []string{}
			for _, line := range strings.Split(strings.TrimSuffix(rootText, "\n"), "\n") {
				if (!test.code && strings.Contains(line, "id=code")) || (!test.database && strings.Contains(line, "id=database")) {
					continue
				}
				lines = append(lines, line)
			}
			writeVolumeTestFile(t, root, "aoci.txt", strings.Join(lines, "\n")+"\n")
			if !test.code {
				if err := os.Remove(filepath.Join(root, "aoci.code.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if !test.database {
				if err := os.Remove(filepath.Join(root, "aoci.database.txt")); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.LoadReadOnly(root)
				if err != nil {
					t.Fatal(err)
				}
				cfg.DatabaseSources = nil
				if err := config.Save(root, cfg); err != nil {
					t.Fatal(err)
				}
			}

			session := connectMCPClient(t, root)
			if test.database {
				initial := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
				if initial.Plan == nil {
					t.Fatal("Database fixture did not produce initial authoring targets")
				}
				authored := databaseAgentEntries(false)
				if !test.code {
					authored = databaseAgentEntriesWithoutRelations(authored)
				}
				output := callVolumeTool(t, session, "aoci_update_entry", databaseAgentBatchArguments(t, initial.Plan, authored))
				if !strings.Contains(output, `"status":"applied"`) {
					t.Fatalf("Database fixture did not align: %s", output)
				}
			}
			overview := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
			if !strings.Contains(overview, cognition.RootManifestMarker) || !strings.Contains(overview, cognition.MetaVolumeMarker) ||
				strings.Contains(overview, "volume_read_only") || (test.code && !strings.Contains(overview, cognition.CodeVolumeMarker)) ||
				(test.database && !strings.Contains(overview, cognition.DatabaseMarker)) {
				t.Fatalf("Overview did not deliver the enabled legal layout: %s", overview)
			}
			maintainText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
			var maintain volumeMaintainResult
			if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
				t.Fatal(err)
			}
			if !maintain.Aligned || maintain.Result != volumegovernance.ResultAligned || maintain.NextAction != "none" ||
				len(maintain.AffectedDomains) != 0 || len(maintain.Candidates) != 0 || maintain.NetworkAccessed {
				t.Fatalf("no-argument Maintain invented disabled-domain debt: %#v", maintain)
			}
		})
	}
}

func TestDailyMaintainBuildsAndAppliesOneMixedCodeDatabaseBatch(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root, tablesBySource, _ := buildDatabaseAgentNativeRepo(t)
	session := connectMCPClient(t, root)
	initial := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if initial.Plan == nil {
		t.Fatal("test-only fixture did not produce initial Database candidates")
	}
	first := callVolumeTool(t, session, "aoci_update_entry", databaseAgentBatchArguments(t, initial.Plan, databaseAgentEntries(false)))
	if !strings.Contains(first, `"status":"applied"`) {
		t.Fatalf("initial Database fixture did not align: %s", first)
	}

	changedCode := []string{
		"internal/domain/accounts.go", "internal/domain/orders.go", "internal/domain/audit.go",
		"internal/domain/catalog.go", "internal/domain/payments.go",
	}
	for _, rel := range changedCode {
		before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		writeVolumeTestFile(t, root, rel, string(before)+"// rc18 test-only business change\n")
	}
	newPath := "internal/domain/new_feature.go"
	writeVolumeTestFile(t, root, newPath, "package domain\n// Test-only new feature boundary.\n")
	pgTables := append([]dbevidence.TableEvidence{}, tablesBySource["pgtemp"]...)
	for index := range pgTables {
		if pgTables[index].Name == "orders" {
			pgTables[index].Columns = append(pgTables[index].Columns, dbevidence.Column{Ordinal: len(pgTables[index].Columns) + 1,
				Name: "governance_revision", NativeType: "integer", CanonicalType: "integer", Nullable: false})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), pgTables)

	maintainText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	codeCount, databaseCount := 0, 0
	for _, candidate := range maintain.Candidates {
		switch candidate.Domain {
		case cognition.ScopeCode:
			codeCount++
		case cognition.ScopeDatabase:
			databaseCount++
		}
	}
	if maintain.Status != autoStatusRepairRequired || maintain.Result != "authoring_required" ||
		codeCount != 6 || databaseCount != 1 || strings.Join(maintain.AffectedDomains, ",") != "code,database" ||
		maintain.DatabasePlan == nil || maintain.DatabasePlan.TargetCount != 1 || maintain.SemanticGenerated || maintain.NetworkAccessed ||
		maintain.Governance.Budget.WholeIndexTokens == 0 || len(maintain.Governance.Budget.R) == 0 || len(maintain.Governance.Budget.S) == 0 {
		t.Fatalf("daily mixed plan is incomplete: %#v governance=%#v", maintain, maintain.Governance)
	}
	codeExample := deliveredVolumeExample(t, maintain.Instructions, maintain.AuthoringMeta, cognition.ScopeCode)
	databaseExample := deliveredVolumeExample(t, maintain.Instructions, maintain.AuthoringMeta, cognition.ScopeDatabase)
	codePosition, databasePosition := -1, -1
	for position, instruction := range maintain.Instructions {
		switch instruction {
		case codeExample:
			codePosition = position
		case databaseExample:
			databasePosition = position
		}
	}
	if codePosition < 0 || databasePosition <= codePosition || maintain.AuthoringMeta != volumeFileText(t, root, "aoci.meta.txt") {
		t.Fatalf("mixed-domain authoring contracts are missing, reordered, or detached from Meta: instructions=%#v", maintain.Instructions)
	}
	wantWrite := candidateRefs(maintain.Candidates)
	if strings.Join(maintain.Sets.Write, ",") != strings.Join(wantWrite, ",") ||
		strings.Join(maintain.Sets.Guard, ",") != "code,database,database_binding,database_evidence,meta,root" ||
		len(maintain.Sets.Review) <= len(maintain.Sets.Write) {
		t.Fatalf("daily Review/Write/Guard closure mismatch: sets=%#v candidates=%#v", maintain.Sets, maintain.Candidates)
	}
	reviewed := map[string]bool{}
	for _, objectRef := range maintain.Sets.Review {
		reviewed[objectRef] = true
	}
	for _, objectRef := range maintain.Sets.Write {
		if !reviewed[objectRef] {
			t.Fatalf("write target escaped Review closure: %s sets=%#v", objectRef, maintain.Sets)
		}
	}

	entries := make([]map[string]any, 0, len(maintain.Candidates))
	for _, candidate := range maintain.Candidates {
		if candidate.Domain == cognition.ScopeDatabase {
			entries = append(entries, map[string]any{"object_ref": candidate.ObjectRef,
				"candidate_id": candidate.CandidateID, "new_entry": databaseAgentEntries(true)[candidate.ObjectRef]})
			continue
		}
		name := filepath.Base(candidate.Path)
		entries = append(entries, map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"new_entry": name + "[CD7S]: F:implement the rc18 test-only changed domain behavior | R:- | A:- | S:Behavior remains deterministic under replay"})
	}
	previousWrite, previousSave := writeAtomicIndex, saveAtomicBaseline
	writes := map[string]int{}
	baselineWrites := 0
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes[filepath.Base(path)]++
		return previousWrite(path, data, expected)
	}
	saveAtomicBaseline = func(root string, state *baseline.Baseline) error {
		baselineWrites++
		return previousSave(root, state)
	}
	t.Cleanup(func() { writeAtomicIndex, saveAtomicBaseline = previousWrite, previousSave })
	applyText := callVolumeTool(t, session, "aoci_update_entry", map[string]any{"batch_id": maintain.DatabasePlan.BatchID, "entries": entries})
	writeAtomicIndex, saveAtomicBaseline = previousWrite, previousSave
	var applied autoResult
	if err := json.Unmarshal([]byte(applyText), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 7 ||
		writes["aoci.code.txt"] != 1 || writes["aoci.database.txt"] != 1 || baselineWrites != 1 {
		t.Fatalf("mixed Apply did not use one transaction: result=%#v writes=%v baseline_writes=%d", applied, writes, baselineWrites)
	}
	finalText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var final volumeMaintainResult
	if err := json.Unmarshal([]byte(finalText), &final); err != nil {
		t.Fatal(err)
	}
	if !final.Aligned || final.Result != "aligned" || final.NextAction != "none" || final.NetworkAccessed {
		t.Fatalf("mixed daily flow did not close governance: %#v", final)
	}
}

func TestMixedBatch100ObjectsWritesEachFormalAssetOnce(t *testing.T) {
	previousLocale := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	root, tablesBySource, _ := buildDatabaseAgentNativeRepo(t)
	session := connectMCPClient(t, root)
	initial := decodeDatabaseMaintain(t, toolCall(t, session, "aoci_maintain", map[string]any{"scope": cognition.ScopeDatabase}))
	if initial.Plan == nil {
		t.Fatal("test-only fixture did not produce initial Database candidates")
	}
	if output := callVolumeTool(t, session, "aoci_update_entry", databaseAgentBatchArguments(t, initial.Plan, databaseAgentEntries(false))); !strings.Contains(output, `"status":"applied"`) {
		t.Fatalf("initial Database fixture did not align: %s", output)
	}

	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	codePaths := make([]string, 0, 97)
	for _, object := range set.Volumes[cognition.ScopeCode].Objects {
		codePaths = append(codePaths, strings.TrimPrefix(object.CanonicalRef, "code:"))
	}
	for index := 0; len(codePaths) < 97; index++ {
		rel := fmt.Sprintf("internal/scale/object_%03d.go", index)
		writeVolumeTestFile(t, root, rel, fmt.Sprintf("package scale\n// TestOnlyObject%03d is deterministic fixture evidence.\n", index))
		codePaths = append(codePaths, rel)
	}
	for _, rel := range codePaths[:len(set.Volumes[cognition.ScopeCode].Objects)] {
		path := filepath.Join(root, filepath.FromSlash(rel))
		before, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		writeVolumeTestFile(t, root, rel, string(before)+"// rc18 100-object test-only update\n")
	}

	pgTables := append([]dbevidence.TableEvidence{}, tablesBySource["pgtemp"]...)
	for index := range pgTables {
		if pgTables[index].Name == "accounts" || pgTables[index].Name == "orders" {
			pgTables[index].Columns = append(pgTables[index].Columns, dbevidence.Column{
				Ordinal: len(pgTables[index].Columns) + 1, Name: "batch_revision", NativeType: "integer", CanonicalType: "integer", Nullable: false,
			})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), pgTables)
	myTables := append([]dbevidence.TableEvidence{}, tablesBySource["mysqltemp"]...)
	for index := range myTables {
		if myTables[index].Name == "payment_attempts" {
			myTables[index].Columns = append(myTables[index].Columns, dbevidence.Column{
				Ordinal: len(myTables[index].Columns) + 1, Name: "batch_revision", NativeType: "integer", CanonicalType: "integer", Nullable: false,
			})
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EngineMySQL), myTables)

	maintainText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	codeCount, databaseCount := 0, 0
	for _, candidate := range maintain.Candidates {
		switch candidate.Domain {
		case cognition.ScopeCode:
			codeCount++
		case cognition.ScopeDatabase:
			databaseCount++
		}
	}
	if codeCount != 97 || databaseCount != 3 || len(maintain.Candidates) != 100 || maintain.DatabasePlan == nil {
		t.Fatalf("100-object mixed plan mismatch: code=%d database=%d total=%d result=%#v", codeCount, databaseCount, len(maintain.Candidates), maintain)
	}

	authoredDatabase := databaseAgentEntries(true)
	entries := make([]map[string]any, 0, 100)
	for _, candidate := range maintain.Candidates {
		if candidate.Domain == cognition.ScopeDatabase {
			entries = append(entries, map[string]any{"object_ref": candidate.ObjectRef,
				"candidate_id": candidate.CandidateID, "new_entry": authoredDatabase[candidate.ObjectRef]})
			continue
		}
		entries = append(entries, map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"new_entry": filepath.Base(candidate.Path) + "[CD7S]: F:provide deterministic test-only batch behavior | R:- | A:- | S:Replay preserves the exact object boundary"})
	}
	previousWrite, previousSave := writeAtomicIndex, saveAtomicBaseline
	writes := map[string]int{}
	baselineWrites := 0
	writeAtomicIndex = func(path string, data []byte, expected string) error {
		writes[filepath.Base(path)]++
		return previousWrite(path, data, expected)
	}
	saveAtomicBaseline = func(root string, state *baseline.Baseline) error {
		baselineWrites++
		return previousSave(root, state)
	}
	t.Cleanup(func() { writeAtomicIndex, saveAtomicBaseline = previousWrite, previousSave })
	applyText := callVolumeTool(t, session, "aoci_update_entry", map[string]any{"batch_id": maintain.DatabasePlan.BatchID, "entries": entries})
	writeAtomicIndex, saveAtomicBaseline = previousWrite, previousSave
	var applied autoResult
	if err := json.Unmarshal([]byte(applyText), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != 100 ||
		writes["aoci.code.txt"] != 1 || writes["aoci.database.txt"] != 1 || baselineWrites != 1 {
		t.Fatalf("100-object Apply was not one governed batch: result=%#v writes=%v baseline_writes=%d", applied, writes, baselineWrites)
	}
	finalText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var final volumeMaintainResult
	if err := json.Unmarshal([]byte(finalText), &final); err != nil {
		t.Fatal(err)
	}
	if !final.Aligned || final.NextAction != "none" {
		t.Fatalf("100-object batch did not close governance: %#v", final)
	}
}

func TestDatabaseOrphanUsesExplicitAuditableRemoveWithoutDatabaseAccess(t *testing.T) {
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
	target := "database://pgtemp/aoci_d0/documents"
	remaining := make([]dbevidence.TableEvidence, 0, len(tablesBySource["pgtemp"])-1)
	for _, table := range tablesBySource["pgtemp"] {
		if table.ObjectRef != target {
			remaining = append(remaining, table)
		}
	}
	writeAgentSourceEvidence(t, root, agentSourceManifest(dbevidence.EnginePostgreSQL), remaining)
	maintainText := callVolumeTool(t, session, "aoci_maintain", map[string]any{})
	var maintain volumeMaintainResult
	if err := json.Unmarshal([]byte(maintainText), &maintain); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, orphan := range maintain.OrphanRemovals {
		found = found || orphan == target
	}
	if !found || maintain.NextAction != "explicit_orphan_remove_or_resolve_blocker" {
		t.Fatalf("Database orphan was not returned as an explicit action: %#v", maintain)
	}
	rootBefore := volumeFileText(t, root, "aoci.txt")
	metaBefore := volumeFileText(t, root, "aoci.meta.txt")
	codeBefore := volumeFileText(t, root, "aoci.code.txt")
	previousWrite := writeRemoveIndex
	writes := 0
	writeRemoveIndex = func(path string, data []byte, expected string) error {
		writes++
		return previousWrite(path, data, expected)
	}
	t.Cleanup(func() { writeRemoveIndex = previousWrite })
	outcome, fail := ApplyRemoveEntry(root, target, "agent", true, false)
	writeRemoveIndex = previousWrite
	if fail != nil || outcome == nil || writes != 1 {
		t.Fatalf("explicit Database orphan remove failed: outcome=%#v writes=%d fail=%+v", outcome, writes, fail)
	}
	if volumeFileText(t, root, "aoci.txt") != rootBefore || volumeFileText(t, root, "aoci.meta.txt") != metaBefore ||
		volumeFileText(t, root, "aoci.code.txt") != codeBefore {
		t.Fatal("Database orphan remove modified a guarded non-target asset")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if _, bound := baseline.FindDatabaseCognitionBinding(state, target); bound {
		t.Fatal("removed Database orphan retained a cognition binding")
	}
	cfg, _ := config.LoadReadOnly(root)
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.DatabaseCognition.Summary.Orphan != 0 {
		t.Fatalf("Database orphan remove did not close governance: facts=%#v err=%v", facts, err)
	}
}

func toolCall(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func databaseAgentBatchArguments(t *testing.T, plan *dbcognition.Plan, authored map[string]string) map[string]any {
	t.Helper()
	entries := make([]map[string]any, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		entry, exists := authored[candidate.ObjectRef]
		if !exists {
			t.Fatalf("model output lacks %s", candidate.ObjectRef)
		}
		entries = append(entries, map[string]any{
			"object_ref": candidate.ObjectRef, "candidate_id": candidate.CandidateID, "new_entry": entry,
		})
	}
	return map[string]any{"batch_id": plan.BatchID, "entries": entries}
}

func databaseAgentEntries(changedOrders bool) map[string]string {
	ordersF := "coordinate the durable commercial order lifecycle"
	if changedOrders {
		ordersF = "coordinate durable order state and fulfillment revisions"
	}
	return map[string]string{
		"database://pgtemp/aoci_d0/accounts":                    "accounts[DB7S]: F:hold the durable customer account and authentication-status boundary | R:code:internal/domain/accounts.go | A:id,email | S:account disablement preserves ownership links and is not a direct-delete operation",
		"database://pgtemp/aoci_d0/tenants":                     "tenants[DB7S]: F:define the durable tenant ownership boundary | R:code:internal/domain/accounts.go | A:id,slug | S:tenant removal requires the retention workflow to finish before owned identities are released",
		"database://pgtemp/aoci_d0/orders":                      "orders[DB7S]: F:" + ordersF + " | R:database://pgtemp/aoci_d0/order_items,code:internal/domain/orders.go | A:id,order_number | S:order state and dependent ledger effects commit in one transaction",
		"database://pgtemp/aoci_d0/order_items":                 "order_items[DB7S]: F:capture the commercial line snapshot belonging to an order | R:database://pgtemp/aoci_d0/orders,code:internal/domain/orders.go | A:id | S:unit price is captured at ordering time and must not be recomputed from later catalog values",
		"database://pgtemp/aoci_d0/audit_events":                "audit_events[DB7S]: F:preserve security and lifecycle audit events | R:code:internal/domain/audit.go | A:id | S:events are append-only and retained by policy outside ordinary entity deletion",
		"database://pgtemp/aoci_d0/documents":                   "documents[DB7S]: F:hold large document payload records and their durable identity | R:code:internal/domain/audit.go | A:id | S:published document content is immutable after release",
		"database://mysqltemp/aoci_test/accounts":               "accounts[DB7S]: F:hold the commerce account identity and activation boundary | R:code:internal/domain/accounts.go | A:id,email | S:deactivation preserves ownership links and is not a direct-delete operation",
		"database://mysqltemp/aoci_test/product_catalog":        "product_catalog[DB7S]: F:publish stable commerce product identities and sellable attributes | R:database://mysqltemp/aoci_test/inventory_reservations,code:internal/domain/catalog.go | A:id,sku | S:historical order pricing never follows a later catalog update",
		"database://mysqltemp/aoci_test/inventory_reservations": "inventory_reservations[DB7S]: F:reserve inventory for an order until fulfillment or expiry | R:database://mysqltemp/aoci_test/product_catalog,code:internal/domain/catalog.go | A:id | S:acquire and release operations are idempotent and serialized per product allocation",
		"database://mysqltemp/aoci_test/payment_attempts":       "payment_attempts[DB7S]: F:record payment requests and durable provider outcomes | R:database://mysqltemp/aoci_test/accounts,code:internal/domain/payments.go | A:id,provider_key | S:provider callbacks are idempotent at the payment boundary",
		"database://mysqltemp/aoci_test/user_roles":             "user_roles[DB7S]: F:bind commerce accounts to granted authorization roles | R:database://mysqltemp/aoci_test/accounts,code:internal/domain/roles.go | A:account_id+role_id | S:role removal takes effect through the authorization transaction boundary",
		"database://mysqltemp/aoci_test/event_outbox":           "event_outbox[DB7S]: F:stage integration events for reliable publication | R:code:internal/domain/payments.go | A:id | S:the business mutation and outbox append commit together",
	}
}

func databaseAgentEntriesWithoutRelations(entries map[string]string) map[string]string {
	result := make(map[string]string, len(entries))
	for objectRef, entry := range entries {
		start := strings.Index(entry, " | R:")
		end := strings.Index(entry[start+1:], " | A:")
		if start < 0 || end < 0 {
			result[objectRef] = entry
			continue
		}
		end += start + 1
		result[objectRef] = entry[:start] + " | R:-" + entry[end:]
	}
	return result
}

func buildDatabaseAgentNativeRepo(t *testing.T) (string, map[string][]dbevidence.TableEvidence, map[string]string) {
	t.Helper()
	root := t.TempDir()
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta\n" +
		"#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n" +
		"#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	writeVolumeTestFile(t, root, "aoci.txt", rootText)
	writeVolumeTestFile(t, root, "aoci.meta.txt", metaText)
	writeVolumeTestFile(t, root, "aoci.database.txt", cognition.DatabaseMarker+"\n")

	business := map[string]string{
		"internal/domain/accounts.go":              "package domain\n// DisableAccount preserves ownership links; tenant retention completes before identity release.\n",
		"internal/domain/orders.go":                "package domain\n// CommitOrder persists order state and ledger effects in one transaction; line prices are snapshots.\n",
		"internal/domain/audit.go":                 "package domain\n// Audit events append only; published document revisions are immutable.\n",
		"internal/domain/catalog.go":               "package domain\n// Reservations serialize by product and acquire/release are idempotent.\n",
		"internal/domain/payments.go":              "package domain\n// Provider callbacks are idempotent; outbox and business mutations commit together.\n",
		"internal/domain/roles.go":                 "package domain\n// Role grants and removals share the authorization transaction boundary.\n",
		"internal/domain/contracts_test.go":        "package domain\n// Contract fixtures verify account retention, order transactionality, audit append-only behavior, and callback idempotency.\n",
		"internal/repository/orders_repository.go": "package repository\n// OrderRepository commits the order, line-price snapshots, and dependent ledger effects in one transaction.\ntype OrderRepository interface { CommitOrderWithLines() error }\n",
		"internal/service/checkout_service.go":     "package service\n// CheckoutService coordinates order persistence and the business outbox append in one transaction.\ntype CheckoutService interface { CommitCheckoutAndOutbox() error }\n",
		"internal/api/payments_handler.go":         "package api\n// ProviderCallbackHandler applies a provider_key callback idempotently at the payment boundary.\ntype ProviderCallbackHandler interface { ApplyOnce(providerKey string) error }\n",
		"migrations/postgresql.sql":                "CREATE TABLE orders (id text PRIMARY KEY, tenant_id text, account_id text, order_number text, state text);\nCREATE TABLE order_items (id text PRIMARY KEY, order_id text REFERENCES orders(id), product_id text, unit_price text);\n",
		"migrations/mysql.sql":                     "CREATE TABLE payment_attempts (id text PRIMARY KEY, account_id text, provider_key text, state text);\nCREATE TABLE event_outbox (id text PRIMARY KEY, aggregate_id text, sequence text, payload text, published_at text);\n",
	}
	rels := make([]string, 0, len(business))
	for rel, text := range business {
		writeVolumeTestFile(t, root, rel, text)
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	byDirectory := map[string][]string{}
	for _, rel := range rels {
		dir := filepath.ToSlash(filepath.Dir(rel))
		byDirectory[dir] = append(byDirectory[dir], rel)
	}
	directories := make([]string, 0, len(byDirectory))
	for dir := range byDirectory {
		directories = append(directories, dir)
	}
	sort.Strings(directories)
	var code strings.Builder
	code.WriteString(cognition.CodeVolumeMarker + "\n")
	for _, dir := range directories {
		description := strings.NewReplacer("/", "-", "_", "-").Replace(dir)
		code.WriteString("===" + description + filepath.ToSlash(filepath.Join(root, filepath.FromSlash(dir))) + "/===\n")
		for _, rel := range byDirectory[dir] {
			name := filepath.Base(rel)
			code.WriteString(name + "[CD7S]: F:provide reviewed business evidence for Database Cognition | R:- | A:- | S:-\n")
		}
	}
	writeVolumeTestFile(t, root, "aoci.code.txt", code.String())

	cfg := config.DefaultConfig()
	cfg.DatabaseSources = []dbevidence.SourceConfig{
		{SourceID: "pgtemp", Engine: dbevidence.EnginePostgreSQL, Database: "postgres", Namespaces: []string{"aoci_d0"}, CredentialEnv: "AOCI_D1_TEST_PG_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true},
		{SourceID: "mysqltemp", Engine: dbevidence.EngineMySQL, Database: "aoci_test", Namespaces: []string{"aoci_test"}, CredentialEnv: "AOCI_D1_TEST_MYSQL_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true},
	}
	// 混合 100 对象批检验的是上限处的原子写入, 不是默认批量。
	if err := cfg.SetCodeCognitionBatchEntries(machinecontract.EntriesBatchMaxItems); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	tablesBySource := agentEvidenceTables(t)
	for sourceID, tables := range tablesBySource {
		manifest := agentSourceManifest(tables[0].Engine)
		if manifest.SourceID != sourceID {
			t.Fatalf("fixture source mismatch: %s", sourceID)
		}
		writeAgentSourceEvidence(t, root, manifest, tables)
	}
	snapshot, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snapshot)); err != nil {
		t.Fatal(err)
	}
	return root, tablesBySource, business
}

func agentEvidenceTables(t *testing.T) map[string][]dbevidence.TableEvidence {
	t.Helper()
	readGolden := func(name string) dbevidence.TableEvidence {
		data, err := os.ReadFile(filepath.Join("..", "dbevidence", "testdata", "real", name))
		if err != nil {
			t.Fatal(err)
		}
		var table dbevidence.TableEvidence
		if err := json.Unmarshal(data, &table); err != nil {
			t.Fatal(err)
		}
		return table
	}
	pg := []dbevidence.TableEvidence{readGolden("postgresql_accounts.json")}
	pg = append(pg,
		agentTable(dbevidence.EnginePostgreSQL, "pgtemp", "postgres", "aoci_d0", "tenants", []string{"id", "slug"}, nil),
		agentTable(dbevidence.EnginePostgreSQL, "pgtemp", "postgres", "aoci_d0", "orders", []string{"id", "tenant_id", "account_id", "order_number", "state"}, []string{"tenants", "accounts"}),
		agentTable(dbevidence.EnginePostgreSQL, "pgtemp", "postgres", "aoci_d0", "order_items", []string{"id", "order_id", "product_id", "unit_price"}, []string{"orders"}),
		agentTable(dbevidence.EnginePostgreSQL, "pgtemp", "postgres", "aoci_d0", "audit_events", []string{"id", "tenant_id", "event_type", "payload", "created_at"}, []string{"tenants"}),
		agentLargeTable(dbevidence.EnginePostgreSQL, "pgtemp", "postgres", "aoci_d0", "documents", 56),
	)
	my := []dbevidence.TableEvidence{readGolden("mysql_accounts.json")}
	my = append(my,
		agentTable(dbevidence.EngineMySQL, "mysqltemp", "aoci_test", "aoci_test", "product_catalog", []string{"id", "sku", "attributes"}, nil),
		agentTable(dbevidence.EngineMySQL, "mysqltemp", "aoci_test", "aoci_test", "inventory_reservations", []string{"id", "product_id", "order_id", "state", "expires_at"}, []string{"product_catalog"}),
		agentTable(dbevidence.EngineMySQL, "mysqltemp", "aoci_test", "aoci_test", "payment_attempts", []string{"id", "account_id", "provider_key", "state"}, []string{"accounts"}),
		agentTable(dbevidence.EngineMySQL, "mysqltemp", "aoci_test", "aoci_test", "user_roles", []string{"account_id", "role_id"}, []string{"accounts"}),
		agentTable(dbevidence.EngineMySQL, "mysqltemp", "aoci_test", "aoci_test", "event_outbox", []string{"id", "aggregate_id", "sequence", "payload", "published_at"}, nil),
	)
	for index := range my {
		if my[index].Name == "user_roles" {
			my[index].PrimaryKey.Columns = []string{"account_id", "role_id"}
		}
	}
	return map[string][]dbevidence.TableEvidence{"pgtemp": pg, "mysqltemp": my}
}

func agentTable(engine dbevidence.Engine, sourceID, database, namespace, name string, columns, referenced []string) dbevidence.TableEvidence {
	result := dbevidence.TableEvidence{
		Version: dbevidence.EvidenceVersion, ObjectRef: "database://" + sourceID + "/" + namespace + "/" + name,
		Engine: engine, SourceID: sourceID, Database: database, Namespace: namespace, Name: name, Kind: "base_table",
		Columns: []dbevidence.Column{}, PrimaryKey: &dbevidence.KeyConstraint{Name: name + "_pk", Columns: []string{columns[0]}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{},
	}
	for index, column := range columns {
		result.Columns = append(result.Columns, dbevidence.Column{Ordinal: index + 1, Name: column, NativeType: "text", CanonicalType: "text", Nullable: index > 0})
	}
	for index, target := range referenced {
		column := target + "_id"
		candidates := map[string]bool{
			target + "_id":                                 true,
			strings.TrimSuffix(target, "s") + "_id":        true,
			strings.TrimSuffix(target, "_catalog") + "_id": true,
		}
		for _, available := range columns {
			if candidates[available] {
				column = available
				break
			}
		}
		result.ForeignKeys = append(result.ForeignKeys, dbevidence.ForeignKey{
			Name: fmt.Sprintf("%s_fk_%d", name, index+1), Columns: []string{column},
			ReferencedObject:  "database://" + sourceID + "/" + namespace + "/" + target,
			ReferencedColumns: []string{"id"}, UpdateAction: "NO ACTION", DeleteAction: "RESTRICT",
		})
	}
	return result
}

func agentLargeTable(engine dbevidence.Engine, sourceID, database, namespace, name string, count int) dbevidence.TableEvidence {
	columns := make([]string, count)
	columns[0] = "id"
	for index := 1; index < count; index++ {
		columns[index] = fmt.Sprintf("payload_%02d", index)
	}
	return agentTable(engine, sourceID, database, namespace, name, columns, nil)
}

func agentSourceManifest(engine dbevidence.Engine) dbevidence.SourceManifest {
	if engine == dbevidence.EnginePostgreSQL {
		return dbevidence.SourceManifest{
			Version: dbevidence.SourceManifestVersion, SourceID: "pgtemp", Engine: engine, Database: "postgres", Namespaces: []string{"aoci_d0"},
			IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
			CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false,
		}
	}
	lowerCase := 0
	return dbevidence.SourceManifest{
		Version: dbevidence.SourceManifestVersion, SourceID: "mysqltemp", Engine: engine, Database: "aoci_test", Namespaces: []string{"aoci_test"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "server_lower_case_table_names", LowerCaseTableNames: &lowerCase}, BusinessDataRead: false,
	}
}

func writeAgentSourceEvidence(t *testing.T, root string, manifest dbevidence.SourceManifest, tables []dbevidence.TableEvidence) {
	t.Helper()
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, tables)
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
}
