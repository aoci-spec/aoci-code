package databasebootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestApplyAddsOnlyDatabaseLifecycleAssets(t *testing.T) {
	root := codeOnlyFixture(t)
	codeBefore := mustRead(t, filepath.Join(root, "aoci.code.txt"))
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, preview)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusApplied || !result.DatabaseReady || result.DatabaseEntryCount != 0 ||
		result.NetworkAccessed || result.BusinessDataRead || result.DDLDMLStatements != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if string(mustRead(t, filepath.Join(root, "aoci.code.txt"))) != string(codeBefore) {
		t.Fatal("Database Bootstrap rewrote Code Cognition")
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	if set.Volumes[cognition.ScopeDatabase] == nil || len(set.Volumes[cognition.ScopeDatabase].Objects) != 0 {
		t.Fatalf("Database Volume was not created as a semantic-free authoring target: %#v", set)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files["aoci.database.txt"].SHA256 != set.Volumes[cognition.ScopeDatabase].SHA256 {
		t.Fatalf("Database Volume is not Baseline-bound: exists=%t err=%v state=%#v", exists, err, state)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "migration-*.json")); len(matches) != 0 {
		t.Fatalf("Database Bootstrap created a Migration transaction: %v", matches)
	}
}

func TestResumeAndRollbackUseBoundPostimages(t *testing.T) {
	for _, test := range []struct {
		name     string
		rollback bool
	}{
		{name: "resume"},
		{name: "rollback", rollback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := codeOnlyFixture(t)
			rootBefore := mustRead(t, filepath.Join(root, "aoci.txt"))
			preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
			if err != nil {
				t.Fatal(err)
			}
			originalFault := transactionFault
			transactionFault = func(point string) error {
				if point == "after_publish_aoci.txt" {
					return errors.New("injected interruption")
				}
				return nil
			}
			_, applyErr := Apply(root, preview)
			transactionFault = originalFault
			t.Cleanup(func() { transactionFault = originalFault })
			if applyErr == nil {
				t.Fatal("injected interruption did not stop Apply")
			}
			if test.rollback {
				result, err := Rollback(root, preview.PreviewDigest[:32])
				if err != nil || result.Status != StatusRolledBack {
					t.Fatalf("rollback failed: result=%#v err=%v", result, err)
				}
				if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(rootBefore) {
					t.Fatal("rollback did not restore the exact Root preimage")
				}
				if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
					t.Fatalf("rollback left the Database Volume active: %v", err)
				}
				return
			}
			result, err := Resume(root, preview.PreviewDigest[:32])
			if err != nil || result.Status != StatusApplied {
				t.Fatalf("resume failed: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestEvidenceBaselineChangeStopsBeforeFormalWrites(t *testing.T) {
	root := codeOnlyFixture(t)
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbevidence.BaselinePath(root), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(root, preview); err == nil || !strings.Contains(err.Error(), "preview_replay_mismatch") {
		t.Fatalf("Evidence change did not stop the bootstrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed replay created a Database Volume: %v", err)
	}
}

func codeOnlyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aoci.txt", cognition.RootManifestMarker+"\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: database bootstrap fixture\n#Global-Invariants: deterministic test-only bytes\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n")
	write("aoci.meta.txt", cognition.MetaVolumeMarker+"\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n")
	write("main.go", "package main\n")
	write("aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\nmain.go[CD7S]: F:run the test-only fixture | R:- | A:main | S:Execution must remain deterministic\n")
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = false
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EngineMySQL,
		Database: "app", Namespaces: []string{"app"}, CredentialEnv: "TEST_ONLY_DSN",
		ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
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
	lowerCaseTableNames := 0
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
		Engine: dbevidence.EngineMySQL, Database: "app", Namespaces: []string{"app"},
		IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "server_lower_case_table_names", LowerCaseTableNames: &lowerCaseTableNames}, BusinessDataRead: false}
	table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/app/users",
		Engine: dbevidence.EngineMySQL, SourceID: "primary", Database: "app", Namespace: "app", Name: "users", Kind: "base_table",
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
	return root
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
