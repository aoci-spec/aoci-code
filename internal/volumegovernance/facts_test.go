package volumegovernance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestFourLegalVolumeLayoutsAreGovernanceAligned(t *testing.T) {
	for _, test := range []struct {
		name        string
		code        bool
		database    bool
		wantDomains int
	}{
		{"root_meta", false, false, 0},
		{"code_only", true, false, 1},
		{"database_only", false, true, 1},
		{"code_database", true, true, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, cfg := alignedFixture(t, test.code, test.database)
			set, err := cognition.Load(root, cfg.IndexPath)
			if err != nil {
				t.Fatal(err)
			}
			facts, err := Assess(root, cfg, set)
			if err != nil {
				t.Fatal(err)
			}
			if !facts.StructureValid || !facts.GovernanceAligned || facts.Result != ResultAligned ||
				facts.NextRequiredAction != "none" || len(facts.EnabledDomains) != test.wantDomains || facts.NetworkAccessed {
				t.Fatalf("legal layout is not aligned: %#v", facts)
			}
			if !test.code && (facts.Code.Applicable || facts.Code.DomainState != "not_applicable" || len(facts.CodeDrift.Missing)+len(facts.CodeDrift.Orphan)+len(facts.CodeDrift.Stale)+len(facts.CodeDrift.Unbaselined) != 0) {
				t.Fatalf("absent Code acquired debt: %#v", facts.CodeDrift)
			}
			if !test.database && (facts.Database.Applicable || facts.Database.DomainState != "not_applicable" || facts.DatabaseEvidence.State != "not_applicable" || facts.DatabaseBindingCount != 0 || facts.DatabaseCognition.NetworkAccessed) {
				t.Fatalf("absent Database acquired debt: %#v", facts.DatabaseCognition)
			}
		})
	}
}

func TestEnabledDatabaseWithoutAcceptedEvidenceRequiresEvidence(t *testing.T) {
	root, cfg := baseFixture(t, false, true)
	state := baseline.NewBaseline(nil)
	fingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.database.txt"))
	if err != nil {
		t.Fatal(err)
	}
	state.Files["aoci.database.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
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
	if facts.Result != ResultEvidenceRequired || facts.GovernanceAligned || facts.NetworkAccessed {
		t.Fatalf("missing Evidence did not fail closed: %#v", facts)
	}
}

func TestRootOwnedAssetInCodeVolumeReportsActionableOwnershipConflict(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	writeFixtureFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD7S]: F:run the test-only fixture | R:- | A:main | S:Execution must remain deterministic\n"+
		"aoci.txt[CD7S]: F:describe the repository cognition root | R:- | A:- | S:Root ownership must remain exclusive\n")
	state := baseline.NewBaseline(nil)
	for _, rel := range []string{"main.go", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = fingerprint
	}
	if err := baseline.Save(root, state); err != nil {
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
	if facts.GovernanceAligned || facts.CodeSourceCount != 1 || facts.CodeEntryCount != 2 ||
		len(facts.CodeDrift.Orphan) != 1 || facts.CodeDrift.Orphan[0] != "aoci.txt" {
		t.Fatalf("ownership conflict counts are incorrect: %#v", facts)
	}
	found := false
	for _, finding := range facts.Findings {
		if finding.Code != "code_orphan" || finding.Target != "aoci.txt" {
			continue
		}
		found = finding.Cause == "volume_ownership_conflict" &&
			finding.ExpectedOwner == cognition.OwnerRoot && finding.ActualOwner == cognition.OwnerCode &&
			finding.AffectedPath == "aoci.txt" && finding.SafeRepairAction == "aoci_remove_entry path=code:aoci.txt"
	}
	if !found {
		t.Fatalf("ownership conflict lacks actionable diagnostics: %#v", facts.Findings)
	}
}

func TestFacts1000CodeObjectsRemainDeterministicAndBounded(t *testing.T) {
	root, cfg := baseFixture(t, true, false)
	var volume strings.Builder
	volume.WriteString(cognition.CodeVolumeMarker + "\n")
	volume.WriteString("===Root source" + filepath.ToSlash(root) + "/===\n")
	volume.WriteString("main.go[CD7S]: F:run the test-only fixture | R:- | A:main | S:Execution must remain deterministic\n")
	volume.WriteString("===Scale sources" + filepath.ToSlash(filepath.Join(root, "internal", "scale")) + "/===\n")
	for index := 1; index < 1000; index++ {
		name := fmt.Sprintf("object_%04d.go", index)
		rel := filepath.ToSlash(filepath.Join("internal", "scale", name))
		writeFixtureFile(t, root, rel, fmt.Sprintf("package scale\n// TestOnly%04d is deterministic.\n", index))
		volume.WriteString(name + "[CD7S]: F:provide deterministic scale fixture behavior | R:- | A:- | S:Replay preserves the exact object boundary\n")
	}
	writeFixtureFile(t, root, "aoci.code.txt", volume.String())
	state := baseline.NewBaseline(nil)
	for _, rel := range []string{"main.go", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = fingerprint
	}
	for index := 1; index < 1000; index++ {
		rel := filepath.ToSlash(filepath.Join("internal", "scale", fmt.Sprintf("object_%04d.go", index)))
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = fingerprint
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	first, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Since(started)
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) || !first.GovernanceAligned || first.CodeSourceCount != 1000 || first.CodeEntryCount != 1000 {
		t.Fatalf("1000-object facts are not deterministic/aligned: first=%#v second=%#v", first, second)
	}
	if duration > 5*time.Second {
		t.Fatalf("1000-object facts exceeded bounded test budget: %s", duration)
	}
	t.Logf("1000-object shared facts twice: %s", duration)
}

func TestFactsTwentyCodeThreeDatabaseFixtureIsAligned(t *testing.T) {
	root, cfg := baseFixture(t, true, true)
	codeLines := make([]string, 0, 20)
	for index := 0; index < 20; index++ {
		rel := "main.go"
		name := "main.go"
		if index > 0 {
			name = fmt.Sprintf("object_%02d.go", index)
			rel = filepath.ToSlash(filepath.Join("internal", "fixture", name))
			writeFixtureFile(t, root, rel, fmt.Sprintf("package fixture\n// Object%02d is test-only.\n", index))
		}
		relation := "-"
		if index == 0 {
			relation = "database://primary/public/users"
		}
		codeLines = append(codeLines, name+"[CD7S]: F:provide deterministic test-only behavior | R:"+relation+" | A:- | S:Replay preserves the exact object boundary")
	}
	codeText := cognition.CodeVolumeMarker + "\n===Root" + filepath.ToSlash(root) + "/===\n" + codeLines[0] + "\n" +
		"===Fixture" + filepath.ToSlash(filepath.Join(root, "internal", "fixture")) + "/===\n" + strings.Join(codeLines[1:], "\n") + "\n"
	writeFixtureFile(t, root, "aoci.code.txt", codeText)

	databaseLines := []string{
		"users[DB7S]: F:store canonical user state | R:code:main.go | A:id | S:Retained ownership records prevent direct identity deletion",
		"orders[DB7S]: F:store canonical order state | R:- | A:id | S:Order transitions preserve deterministic transaction boundaries",
		"audit_events[DB7S]: F:store append-only audit state | R:- | A:id | S:Audit records are never rewritten by ordinary maintenance",
	}
	writeFixtureFile(t, root, "aoci.database.txt", cognition.DatabaseMarker+"\n===Primary tables/database://primary/public/===\n"+strings.Join(databaseLines, "\n")+"\n")
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
	tables := make([]dbevidence.TableEvidence, 0, 3)
	for _, name := range []string{"users", "orders", "audit_events"} {
		tables = append(tables, dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion,
			ObjectRef: "database://primary/public/" + name, Engine: dbevidence.EnginePostgreSQL,
			SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table",
			Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
			PrimaryKey:        &dbevidence.KeyConstraint{Name: name + "_pkey", Columns: []string{"id"}},
			UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{},
			Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}})
	}
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

	state := baseline.NewBaseline(nil)
	for index := 0; index < 20; index++ {
		rel := "main.go"
		if index > 0 {
			rel = filepath.ToSlash(filepath.Join("internal", "fixture", fmt.Sprintf("object_%02d.go", index)))
		}
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		state.Files[rel] = fingerprint
	}
	for _, rel := range []string{"aoci.code.txt", "aoci.database.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		state.Files[rel] = fingerprint
	}
	lineByRef := map[string]string{
		"database://primary/public/users":        databaseLines[0],
		"database://primary/public/orders":       databaseLines[1],
		"database://primary/public/audit_events": databaseLines[2],
	}
	for _, table := range snapshot.Tables {
		if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
			ObjectRef: table.ObjectRef, SourceID: "primary", EvidenceVersion: snapshot.EvidenceVersion,
			TableEvidenceSHA256: table.TableEvidenceSHA256, EntrySHA256: dbcognition.EntrySHA256(lineByRef[table.ObjectRef]),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.CodeSourceCount != 20 || facts.CodeEntryCount != 20 ||
		facts.DatabaseEntryCount != 3 || facts.DatabaseBindingCount != 3 || len(facts.RelationFindings) != 0 {
		t.Fatalf("20+3 shared facts mismatch: facts=%#v err=%v", facts, err)
	}
}

func alignedFixture(t *testing.T, code, database bool) (string, *config.Config) {
	t.Helper()
	root, cfg := baseFixture(t, code, database)
	state := baseline.NewBaseline(nil)
	if code {
		for _, rel := range []string{"main.go", "aoci.code.txt"} {
			fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			state.Files[rel] = fingerprint
		}
	}
	if database {
		snapshot := writeAcceptedEvidence(t, root)
		fingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.database.txt"))
		if err != nil {
			t.Fatal(err)
		}
		state.Files["aoci.database.txt"] = fingerprint
		line := "users[DB7S]: F:store canonical user state | R:- | A:id | S:Retained ownership records prevent direct identity deletion"
		if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
			ObjectRef: "database://primary/public/users", SourceID: "primary", EvidenceVersion: snapshot.EvidenceVersion,
			TableEvidenceSHA256: snapshot.Tables[0].TableEvidenceSHA256, EntrySHA256: dbcognition.EntrySHA256(line),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func baseFixture(t *testing.T, code, database bool) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	declarations := "#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"
	if code {
		declarations += "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"
	}
	if database {
		declarations += "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled\n"
	}
	writeFixtureFile(t, root, "aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: governance fixture\n#Global-Invariants: deterministic test-only bytes\n"+declarations)
	writeFixtureFile(t, root, "aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	if code {
		writeFixtureFile(t, root, "main.go", "package main\n")
		writeFixtureFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\nmain.go[CD7S]: F:run the test-only fixture | R:- | A:main | S:Execution must remain deterministic\n")
	}
	if database {
		writeFixtureFile(t, root, "aoci.database.txt", cognition.DatabaseMarker+"\n===Primary tables/database://primary/public/===\nusers[DB7S]: F:store canonical user state | R:- | A:id | S:Retained ownership records prevent direct identity deletion\n")
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = false
	if database {
		cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
			Database: "app", Namespaces: []string{"public"}, CredentialEnv: "TEST_ONLY_DSN",
			ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func writeAcceptedEvidence(t *testing.T, root string) dbevidence.Snapshot {
	t.Helper()
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/users",
		Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: "users", Kind: "base_table",
		Columns:    []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
		PrimaryKey: &dbevidence.KeyConstraint{Name: "users_pkey", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{},
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
	return snapshot
}

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(fmt.Errorf("write %s: %w", rel, err))
	}
}
