package databasebootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
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
		result.NetworkAccessed || result.BusinessDataRead || result.DDLDMLStatements != 0 ||
		result.NextAction != "call_no_argument_aoci_maintain_for_current_machine_batch" {
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
	wantRoot := baseline.HashBytes("aoci.txt", mustRead(t, filepath.Join(root, "aoci.txt")))
	wantRoot.Role = machinecontract.ScopeRoleIndex
	if state.Files["aoci.txt"] != wantRoot {
		t.Fatalf("Root Baseline binding was not atomically advanced with its role preserved: got=%#v want=%#v", state.Files["aoci.txt"], wantRoot)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "migration-*.json")); len(matches) != 0 {
		t.Fatalf("Database Bootstrap created a Migration transaction: %v", matches)
	}
}

func TestDiagnoseReportsSafeBootstrapCauseWithoutDynamicDetails(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		cause  string
		action string
	}{
		{
			name:  "code governance",
			err:   fmt.Errorf("%w: database_bootstrap_code_governance_not_ready", ErrNotReady),
			cause: "database_bootstrap_code_governance_not_ready", action: "call_no_argument_aoci_maintain_for_current_machine_batch",
		},
		{
			name:  "baseline missing",
			err:   fmt.Errorf("%w: database_bootstrap_baseline_required", ErrNotReady),
			cause: "database_bootstrap_baseline_required", action: "review_database_cognition_findings",
		},
		{
			name:  "database evidence",
			err:   fmt.Errorf("%w: database_bootstrap_evidence_not_ready[database_bootstrap_secret_source]", ErrNotReady),
			cause: "database_bootstrap_evidence_not_ready", action: "snapshot_or_repair_evidence",
		},
		{
			name:  "recovery",
			err:   errors.New("database_bootstrap_recovery_conflict: C:/secret/project/aoci.txt"),
			cause: "database_bootstrap_recovery_conflict", action: "review_database_cognition_findings",
		},
		{
			name:  "unknown",
			err:   errors.New("database_bootstrap_secret_source: postgres://user:password@example/db"),
			cause: "database_bootstrap_stopped", action: "review_database_cognition_findings",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := Diagnose(test.err)
			if diagnostic.CauseCode != test.cause || diagnostic.SafeNextAction != test.action {
				t.Fatalf("unexpected diagnostic: %#v", diagnostic)
			}
			encoded := diagnostic.CauseCode + diagnostic.SafeNextAction
			for _, forbidden := range []string{"database_bootstrap_secret_source", "C:/secret", "postgres://", "password"} {
				if strings.Contains(encoded, forbidden) {
					t.Fatalf("diagnostic leaked %q: %#v", forbidden, diagnostic)
				}
			}
		})
	}
}

func TestResumeAndRollbackUseBoundPostimages(t *testing.T) {
	for _, test := range []struct {
		name       string
		faultPoint string
		rollback   bool
	}{
		{name: "resume after Database", faultPoint: "after_publish_aoci.database.txt"},
		{name: "resume after Root", faultPoint: "after_publish_aoci.txt"},
		{name: "resume after Baseline", faultPoint: "after_publish_baseline.json"},
		{name: "rollback after Database", faultPoint: "after_publish_aoci.database.txt", rollback: true},
		{name: "rollback after Root", faultPoint: "after_publish_aoci.txt", rollback: true},
		{name: "rollback after Baseline", faultPoint: "after_publish_baseline.json", rollback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := codeOnlyFixture(t)
			rootBefore := mustRead(t, filepath.Join(root, "aoci.txt"))
			baselineBefore := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
			preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
			if err != nil {
				t.Fatal(err)
			}
			originalFault := transactionFault
			transactionFault = func(point string) error {
				if point == test.faultPoint {
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
				if string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineBefore) {
					t.Fatal("rollback did not restore the exact Baseline preimage")
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
			state, exists, err := baseline.Load(root)
			if err != nil || !exists || state.Files["aoci.txt"].SHA256 != preview.RootPostimageSHA256 {
				t.Fatalf("resume did not complete the frozen Root Baseline binding: exists=%t err=%v state=%#v", exists, err, state)
			}
		})
	}
}

func TestBootstrapDoesNotEnrollUnmanagedRootInBaseline(t *testing.T) {
	root := codeOnlyFixture(t)
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	delete(state.Files, "aoci.txt")
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, preview)
	if err != nil || result.Status != StatusApplied {
		t.Fatalf("Apply failed: result=%#v err=%v", result, err)
	}
	state, exists, err = baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load postimage Baseline: exists=%t err=%v", exists, err)
	}
	if _, enrolled := state.Files["aoci.txt"]; enrolled {
		t.Fatal("Database Bootstrap enrolled an unmanaged Root")
	}
}

func TestBootstrapPreservesLegacyOmittedRootRole(t *testing.T) {
	root := codeOnlyFixture(t)
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint := state.Files["aoci.txt"]
	fingerprint.Role = ""
	state.Files["aoci.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(root, preview)
	if err != nil || result.Status != StatusApplied {
		t.Fatalf("Apply failed: result=%#v err=%v", result, err)
	}
	state, exists, err = baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load postimage Baseline: exists=%t err=%v", exists, err)
	}
	want := baseline.HashBytes("aoci.txt", mustRead(t, filepath.Join(root, "aoci.txt")))
	if state.Files["aoci.txt"] != want || state.Files["aoci.txt"].Role != "" {
		t.Fatalf("legacy omitted Root role was not preserved: got=%#v want=%#v", state.Files["aoci.txt"], want)
	}
}

func TestPrepareRejectsMismatchedManagedRootBaseline(t *testing.T) {
	root := codeOnlyFixture(t)
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint := state.Files["aoci.txt"]
	fingerprint.SHA256 = strings.Repeat("a", 64)
	state.Files["aoci.txt"] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	rootBefore := mustRead(t, filepath.Join(root, "aoci.txt"))
	baselineBefore := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
	if _, err := Prepare(root, time.Unix(1_700_000_000, 0)); err == nil || err.Error() != "database_bootstrap_baseline_conflict" {
		t.Fatalf("mismatched managed Root did not fail closed: %v", err)
	}
	if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(rootBefore) ||
		string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineBefore) {
		t.Fatal("failed Prepare changed a formal preimage")
	}
	if _, err := os.Stat(filepath.Join(root, "aoci.database.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed Prepare created Database Volume: %v", err)
	}
}

func TestRecoveryHonorsLegacyFrozenRootBaselinePostimage(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "resume"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			root := codeOnlyFixture(t)
			rootBefore := mustRead(t, filepath.Join(root, "aoci.txt"))
			baselineBefore := mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))
			preview, err := Prepare(root, time.Unix(1_700_000_000, 0))
			if err != nil {
				t.Fatal(err)
			}
			var before, post baseline.Baseline
			if err := json.Unmarshal([]byte(preview.BaselinePreimage), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(preview.BaselinePostimage), &post); err != nil {
				t.Fatal(err)
			}
			post.Files["aoci.txt"] = before.Files["aoci.txt"]
			legacyBaselinePostimage, err := baseline.MarshalExact(&post)
			if err != nil {
				t.Fatal(err)
			}
			preview.BaselinePostimage = string(legacyBaselinePostimage)
			preview.BaselinePostimageSHA256 = cognitiontxn.SHA256(legacyBaselinePostimage)
			preview.PreviewDigest, err = previewDigest(preview)
			if err != nil {
				t.Fatal(err)
			}
			transactionID := preview.PreviewDigest[:32]
			staging, err := cognitiontxn.Stage(root, Operation, transactionID, []cognitiontxn.Postimage{
				{Path: preview.DatabasePath, SHA: preview.DatabasePostimageSHA256, Data: []byte(preview.DatabasePostimage)},
				{Path: preview.RootPath, SHA: preview.RootPostimageSHA256, Data: []byte(preview.RootPostimage)},
				{Path: preview.BaselinePath, SHA: preview.BaselinePostimageSHA256, Data: []byte(preview.BaselinePostimage)},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			intent := &RecoveryIntent{Version: machinecontract.DatabaseCognitionBootstrapRecoveryV1,
				TransactionID: transactionID, Preview: *preview, Staging: staging, CreatedAt: preview.PreparedAt}
			intent.RecoveryDigest, err = recoveryDigest(intent)
			if err != nil {
				t.Fatal(err)
			}
			if err := saveIntent(intentPath(root, transactionID), intent); err != nil {
				t.Fatal(err)
			}
			if rollback {
				if err := publishCreate(root, intent, preview.DatabasePath, preview.DatabasePostimageSHA256); err != nil {
					t.Fatal(err)
				}
				if err := publishReplace(root, intent, preview.RootPath, preview.RootPreimageSHA256, preview.RootPostimageSHA256); err != nil {
					t.Fatal(err)
				}
				if err := publishReplace(root, intent, preview.BaselinePath, preview.BaselinePreimageSHA256, preview.BaselinePostimageSHA256); err != nil {
					t.Fatal(err)
				}
				result, err := Rollback(root, transactionID)
				if err != nil || result.Status != StatusRolledBack {
					t.Fatalf("legacy pending Rollback failed: result=%#v err=%v", result, err)
				}
				if string(mustRead(t, filepath.Join(root, "aoci.txt"))) != string(rootBefore) ||
					string(mustRead(t, filepath.Join(root, ".aoci", "baseline.json"))) != string(baselineBefore) {
					t.Fatal("legacy pending Rollback did not restore its exact frozen preimages")
				}
				return
			}
			result, err := Resume(root, transactionID)
			if err != nil || result.Status != StatusApplied {
				t.Fatalf("legacy pending Resume failed: result=%#v err=%v", result, err)
			}
			state, exists, err := baseline.Load(root)
			if err != nil || !exists {
				t.Fatalf("load resumed Baseline: exists=%t err=%v", exists, err)
			}
			if state.Files["aoci.txt"] != before.Files["aoci.txt"] {
				t.Fatalf("legacy frozen Baseline postimage was reinterpreted: got=%#v want=%#v", state.Files["aoci.txt"], before.Files["aoci.txt"])
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
	for _, rel := range []string{"aoci.txt", "main.go", "aoci.code.txt"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if rel == "aoci.txt" {
			fingerprint.Role = machinecontract.ScopeRoleIndex
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
