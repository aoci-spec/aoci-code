package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/databasebootstrap"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestDatabaseSourceCLIStoresOnlyEnvironmentNameAndDoesNotConnect(t *testing.T) {
	root := databaseCLIRepo(t)
	t.Setenv("AOCI_DB_PRIMARY_DSN", "postgres://secret-user:secret-password@secret-host/app")
	args := []string{"--repo", root, "--json", "database", "source", "add",
		"--source-id", "primary", "--engine", "postgresql", "--database-name", "app",
		"--namespace", "public", "--credential-env", "AOCI_DB_PRIMARY_DSN"}
	var stdout, stderr bytes.Buffer
	if code := executeCLI(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("source add failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	configBytes, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-user", "secret-password", "secret-host", "postgres://"} {
		if strings.Contains(string(configBytes), forbidden) || strings.Contains(stdout.String()+stderr.String(), forbidden) {
			t.Fatalf("source add leaked %q", forbidden)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "database", "source", "list"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("source list failed: %d %s", code, stderr.String())
	}
	var listed struct {
		Sources         []dbevidence.SourceConfig `json:"sources"`
		CredentialSaved bool                      `json:"credential_saved"`
		NetworkAccessed bool                      `json:"network_accessed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sources) != 1 || listed.CredentialSaved || listed.NetworkAccessed || listed.Sources[0].CredentialEnv != "AOCI_DB_PRIMARY_DSN" {
		t.Fatalf("bad source list: %+v", listed)
	}
}

func TestDatabaseSourceCLIDefaultsCredentialReferenceAndReportsAccessPlan(t *testing.T) {
	root := databaseCLIRepo(t)
	var stdout, stderr bytes.Buffer
	args := []string{"--repo", root, "--json", "database", "source", "add",
		"--source-id", "orders-read", "--engine", "postgresql", "--database-name", "app", "--namespace", "public"}
	if code := executeCLI(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("source add failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	cfg, err := config.LoadBase(root)
	if err != nil || len(cfg.DatabaseSources) != 1 || cfg.DatabaseSources[0].CredentialEnv != "AOCI_DB_ORDERS_READ_DSN" {
		t.Fatalf("default access reference was not saved: cfg=%#v err=%v", cfg, err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "database", "source", "access", "--source", "orders-read"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("access preflight failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var plan dbevidence.AccessPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Status != dbevidence.AccessStatusActionRequired || plan.NetworkAccessed || plan.CredentialSaved || plan.BusinessDataRead {
		t.Fatalf("unexpected access plan: %#v", plan)
	}
}

func TestDatabaseSourceCLIAcceptsCanonicalOpenGaussEngine(t *testing.T) {
	root := databaseCLIRepo(t)
	var stdout, stderr bytes.Buffer
	args := []string{"--repo", root, "--json", "database", "source", "add",
		"--source-id", "warehouse", "--engine", "opengauss", "--database-name", "analytics"}
	if code := executeCLI(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("openGauss source add failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DatabaseSources) != 1 || cfg.DatabaseSources[0].Engine != dbevidence.EngineOpenGauss ||
		len(cfg.DatabaseSources[0].Namespaces) != 1 || cfg.DatabaseSources[0].Namespaces[0] != "public" {
		t.Fatalf("canonical openGauss source was not stored: %+v", cfg.DatabaseSources)
	}
}

func TestDatabaseSourceCLIRejectsUnknownAndOpenGaussAliases(t *testing.T) {
	for _, engine := range []string{"openGauss", "gaussdb", "open_gauss", "og", "postgres", "unknown"} {
		t.Run(engine, func(t *testing.T) {
			root := databaseCLIRepo(t)
			var stdout, stderr bytes.Buffer
			args := []string{"--repo", root, "--json", "database", "source", "add",
				"--source-id", "warehouse", "--engine", engine, "--database-name", "analytics"}
			if code := executeCLI(args, &stdout, &stderr); code != ExitConfig {
				t.Fatalf("engine %q was accepted: code=%d stdout=%s stderr=%s", engine, code, stdout.String(), stderr.String())
			}
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.DatabaseSources) != 0 {
				t.Fatalf("rejected engine %q changed configuration: %+v", engine, cfg.DatabaseSources)
			}
		})
	}
}

func TestDatabaseSourceCLIInvalidInputIsRedacted(t *testing.T) {
	root := databaseCLIRepo(t)
	secret := "postgres://secret-user:secret-password@secret-host/app"
	args := []string{"--repo", root, "--json", "database", "source", "add",
		"--source-id", secret, "--engine", "postgresql", "--database-name", "app",
		"--credential-env", secret}
	var stdout, stderr bytes.Buffer
	if code := executeCLI(args, &stdout, &stderr); code != ExitConfig {
		t.Fatalf("invalid source code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), secret) || strings.Contains(stdout.String()+stderr.String(), "secret-password") {
		t.Fatalf("invalid input leaked a credential-shaped value: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestDatabaseVerifyReportsSourceUnavailableWithoutLeakingDSN(t *testing.T) {
	root := databaseCLIRepo(t)
	configureDatabaseCLISource(t, root)
	secret := "host=secret-host user=secret-user password=secret-password invalid-token"
	t.Setenv("AOCI_DB_PRIMARY_DSN", secret)
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "database", "verify", "--source", "primary"}, &stdout, &stderr); code != ExitDrift {
		t.Fatalf("verify code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report dbevidence.DriftReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SourceStatus != string(dbevidence.DriftSourceUnavailable) || report.BusinessDataRead || report.ErrorCode == "" {
		t.Fatalf("bad source unavailable report: %+v", report)
	}
	for _, forbidden := range []string{secret, "secret-host", "secret-user", "secret-password"} {
		if strings.Contains(stdout.String()+stderr.String(), forbidden) {
			t.Fatalf("verify leaked %q", forbidden)
		}
	}
}

func TestDatabaseBaselineCLIRequiresExplicitSnapshotBinding(t *testing.T) {
	root := databaseCLIRepo(t)
	configureDatabaseCLISource(t, root)
	manifest, snapshot, files := databaseCLIFixtureSnapshot(t)
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	wrong := []string{"--repo", root, "--json", "database", "baseline", "accept", "--source", "primary", "--snapshot-sha", strings.Repeat("0", 64)}
	if code := executeCLI(wrong, &stdout, &stderr); code != ExitInvalid {
		t.Fatalf("wrong binding code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(dbevidence.BaselinePath(root)); !os.IsNotExist(err) {
		t.Fatalf("wrong binding wrote Baseline: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	accept := []string{"--repo", root, "--json", "database", "baseline", "accept", "--source", "primary", "--snapshot-sha", snapshot.SourceSnapshotSHA256}
	if code := executeCLI(accept, &stdout, &stderr); code != ExitOK {
		t.Fatalf("accept failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	before, err := os.ReadFile(dbevidence.BaselinePath(root))
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "database", "inventory", "--source", "primary"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("inventory failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	after, _ := os.ReadFile(dbevidence.BaselinePath(root))
	if !bytes.Equal(before, after) {
		t.Fatal("inventory advanced the evidence Baseline")
	}
}

func TestAutoBaselineAcceptanceBootstrapsDatabaseWithoutMigrationOrCodeRewrite(t *testing.T) {
	root := databaseCLIRepo(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetAutomationMode(config.AutomationModeAuto); err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true,
	}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: cli database bootstrap fixture\n#Global-Invariants: deterministic fixture\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	codeText := cognition.CodeVolumeMarker + "\n===Go sources" + filepath.ToSlash(root) + "/===\nmain.go[CD7S]: F:run the CLI fixture | R:- | A:main | S:Execution remains deterministic\n"
	for rel, content := range map[string]string{"aoci.txt": rootText, "aoci.meta.txt": metaText, "aoci.code.txt": codeText, "main.go": "package main\n"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	codeBefore, err := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if err != nil {
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
	manifest, snapshot, files := databaseCLIFixtureSnapshot(t)
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	oldRepo, oldJSON := flagRepo, flagJSON
	flagRepo, flagJSON = root, true
	defer func() { flagRepo, flagJSON = oldRepo, oldJSON }()
	command := newDatabaseBaselineAcceptCmd()
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"--source", "primary", "--snapshot-sha", snapshot.SourceSnapshotSHA256})
	if err := command.Execute(); err != nil {
		t.Fatalf("accept/bootstrap failed: err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	var result struct {
		DatabaseBootstrap *databasebootstrap.Result `json:"database_bootstrap"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.DatabaseBootstrap == nil || !result.DatabaseBootstrap.DatabaseReady || result.DatabaseBootstrap.DatabaseEntryCount != 0 {
		t.Fatalf("Database Bootstrap result missing: %s", stdout.String())
	}
	codeAfter, _ := os.ReadFile(filepath.Join(root, "aoci.code.txt"))
	if !bytes.Equal(codeBefore, codeAfter) {
		t.Fatal("Database Bootstrap rewrote Code Cognition")
	}
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil || set.Volumes[cognition.ScopeDatabase] == nil {
		t.Fatalf("Database Cognition was not activated: set=%#v err=%v", set, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(root, ".aoci", "transactions", "migration-*.json")); len(matches) != 0 {
		t.Fatalf("Database Bootstrap entered Migration: %v", matches)
	}
}

func TestDatabaseEvidenceBundleContainsFactsButNoSemanticCandidate(t *testing.T) {
	root := databaseCLIRepo(t)
	configureDatabaseCLISource(t, root)
	manifest, snapshot, files := databaseCLIFixtureSnapshot(t)
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
	objectRef := snapshot.Tables[0].ObjectRef
	indexText := "#AOCI-CLI Complete Index\n===Root/" + filepath.ToSlash(root) + "/===\n" +
		"main.go[CRT9T]: F:run fixture | R:" + objectRef + " | A:main | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--repo", root, "--json", "database", "evidence", "bundle", "--source", "primary", "--object", objectRef}
	if code := executeCLI(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("bundle failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var bundle dbevidence.EvidenceBundle
	if err := json.Unmarshal(stdout.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SemanticCandidateIncluded || bundle.ReadyFor != "model_authored_table_fras" || len(bundle.CodeEvidenceRefs) != 1 || bundle.CodeEvidenceRefs[0] != "main.go" {
		t.Fatalf("bad evidence bundle: %+v", bundle)
	}
	for _, forbidden := range []string{`"tags"`, `"suggested_f"`, `"suggested_r"`, `"suggested_a"`, `"suggested_s"`} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("bundle contained generated semantic field %s", forbidden)
		}
	}
}

func TestDatabaseHelpFollowsBothOfficialLocales(t *testing.T) {
	for _, test := range []struct {
		locale string
		want   string
	}{
		{"en-US", "Explicit database schema evidence workflows with read-only database access"},
		{"zh-CN", "显式的数据库Schema证据工作流(对数据库只读)"},
	} {
		t.Run(test.locale, func(t *testing.T) {
			root := databaseCLIRepo(t)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Locale = test.locale
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := executeCLI([]string{"--repo", root, "database", "--help"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("help failed: code=%d stderr=%s", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("database help is not localized for %s: %s", test.locale, stdout.String())
			}
		})
	}
}

func TestDatabaseBootstrapErrorKeepsOuterCodeAndAddsSafeDiagnostic(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		t.Run(locale, func(t *testing.T) {
			root := databaseCLIRepo(t)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Locale = locale
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := executeCLI([]string{"--repo", root, "--json", "database", "cognition", "bootstrap"}, &stdout, &stderr)
			if code != ExitInvalid || stderr.Len() != 0 {
				t.Fatalf("bootstrap error transport changed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			var envelope struct {
				ErrorCode string                       `json:"error_code"`
				Message   string                       `json:"message"`
				Details   databasebootstrap.Diagnostic `json:"details"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.ErrorCode != "database_bootstrap_stopped" ||
				envelope.Details.CauseCode != "database_bootstrap_layout_invalid" ||
				envelope.Details.SafeNextAction != "review_database_cognition_findings" {
				t.Fatalf("bootstrap diagnostic missing: %#v", envelope)
			}
			for _, token := range []string{envelope.Details.CauseCode, envelope.Details.SafeNextAction} {
				if !strings.Contains(envelope.Message, token) {
					t.Fatalf("localized message omitted %q: %s", token, envelope.Message)
				}
			}
		})
	}
}

func TestDatabaseCredentialMissingGuidanceIsSafeAndLocalized(t *testing.T) {
	tests := []struct {
		locale string
		want   []string
	}{
		{locale: "en-US", want: []string{"outside the Agent conversation", "Host's inherited environment", "relaunch the Host only if", "Never paste the credential value"}},
		{locale: "zh-CN", want: []string{"Agent对话", "Host的继承环境", "仅当当前Host无法刷新", "不要把凭据值粘贴"}},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			root := databaseCLIRepo(t)
			configureDatabaseCLISource(t, root)
			cfg, err := config.LoadBase(root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.Locale = test.locale
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AOCI_DB_PRIMARY_DSN", "")
			var stdout, stderr bytes.Buffer
			code := executeCLI([]string{"--repo", root, "--json", "database", "source", "inspect", "--source", "primary"}, &stdout, &stderr)
			if code != ExitConfig || stderr.Len() != 0 {
				t.Fatalf("missing credential transport changed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			var envelope struct {
				ErrorCode string `json:"error_code"`
				Message   string `json:"message"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.ErrorCode != "database_credential_env_missing" {
				t.Fatalf("wrong missing credential code: %#v", envelope)
			}
			for _, want := range test.want {
				if !strings.Contains(envelope.Message, want) {
					t.Fatalf("credential guidance omitted %q: %s", want, envelope.Message)
				}
			}
			for _, forbidden := range []string{"postgres://", "password=", "secret-value"} {
				if strings.Contains(stdout.String()+stderr.String(), forbidden) {
					t.Fatalf("credential guidance leaked %q", forbidden)
				}
			}
		})
	}
}

func TestDatabaseCognitionStatusIsOfflineAndNoConfigurationHasNoDebt(t *testing.T) {
	root := databaseCLIRepo(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexText := "#AOCI-CLI Complete Index\n===Root/" + filepath.ToSlash(root) + "/===\nmain.go[CRT9T]: F:run fixture | R:- | A:main | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "--json", "database", "cognition", "status"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("status failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report struct {
		ConfiguredSources int  `json:"configured_sources"`
		CognitionCurrent  bool `json:"cognition_current"`
		NetworkAccessed   bool `json:"network_accessed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ConfiguredSources != 0 || !report.CognitionCurrent || report.NetworkAccessed {
		t.Fatalf("no-config status created debt or network access: %+v", report)
	}
}

func TestDatabaseCognitionHumanStatusShowsSourceBlocker(t *testing.T) {
	root := databaseCLIRepo(t)
	configureDatabaseCLISource(t, root)
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n" +
		"#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	for name, text := range map[string]string{
		"aoci.txt": rootText, "aoci.meta.txt": metaText, "aoci.database.txt": cognition.DatabaseMarker + "\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := executeCLI([]string{"--repo", root, "database", "cognition", "status"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("status failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{"blocking_sources=1", "source_id=primary", "state=evidence_unavailable", "error_code=database_snapshot_missing", "network_accessed=false"} {
		if !strings.Contains(output, want) {
			t.Fatalf("human status hid source blocker %q: %s", want, output)
		}
	}
}

func databaseCLIRepo(t *testing.T) string {
	t.Helper()
	t.Cleanup(resetRootFlags)
	root := t.TempDir()
	initCLITestGitRepository(t, root)
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte("#AOCI-CLI Complete Index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func configureDatabaseCLISource(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true,
	}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
}

func databaseCLIFixtureSnapshot(t *testing.T) (dbevidence.SourceManifest, dbevidence.Snapshot, map[string][]byte) {
	t.Helper()
	manifest := dbevidence.SourceManifest{
		Version: dbevidence.SourceManifestVersion, SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
		Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{},
		ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false,
	}
	objectRef, _ := dbevidence.CanonicalObjectRef("primary", "public", "users")
	table := dbevidence.TableEvidence{
		Version: dbevidence.EvidenceVersion, ObjectRef: objectRef, Engine: dbevidence.EnginePostgreSQL,
		SourceID: "primary", Database: "app", Namespace: "public", Name: "users", Kind: "base_table",
		Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
		UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{},
	}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, []dbevidence.TableEvidence{table})
	if err != nil {
		t.Fatal(err)
	}
	return manifest, snapshot, files
}
