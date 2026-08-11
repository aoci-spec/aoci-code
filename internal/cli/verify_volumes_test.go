package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionoptimization"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/mcptools"
	"github.com/aoci-spec/aoci-code/textassets"
	"github.com/spf13/cobra"
)

func TestVerifyVolumesSeparatesStructureFromGovernance(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "volumes", "database-quality")
	for _, name := range []string{"aoci.txt", "aoci.meta.txt", "aoci.database.txt", "evidence.sql"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	cfg.LedgerEnabled = false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := runVerify(cmd, nil); err == nil {
		t.Fatal("structurally valid Volumes without Baseline or accepted Evidence must not be governance-aligned")
	}
	text := output.String()
	for _, want := range []string{`"layout_mode": "volumes-v1"`, `"structure_valid": true`, `"governance_aligned": false`, `"read_only_candidate": true`, `"object_count": 8`, `"asset_state": "absent"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("Volume Verify missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"baseline_exists": true`) {
		t.Fatal("Volume structural Verify fabricated Baseline alignment")
	}
}

func TestRootMetaVolumesVerifyCheckAndGuideAlign(t *testing.T) {
	root := t.TempDir()
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: test-only root meta fixture\n#Global-Invariants: deterministic fixture bytes\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	for name, data := range map[string]string{"aoci.txt": rootText, "aoci.meta.txt": metaText} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath, cfg.LedgerEnabled = "aoci.txt", false
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(nil)); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	for name, invoke := range map[string]func(*cobra.Command) error{
		"verify": func(cmd *cobra.Command) error { return runVerify(cmd, nil) },
		"check":  func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) },
		"guide":  func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "codex") },
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			if err := invoke(cmd); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"governance_aligned": true`, `"network_accessed": false`} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("%s output missing %s:\n%s", name, want, output.String())
				}
			}
			if name == "check" && !strings.Contains(output.String(), `"ok": true`) {
				t.Fatalf("Check did not align:\n%s", output.String())
			}
			if name == "guide" && (!strings.Contains(output.String(), `"stage": "aligned"`) || !strings.Contains(output.String(), `"next_action": "none"`)) {
				t.Fatalf("Guide did not align:\n%s", output.String())
			}
		})
	}
}

func TestVolumeOwnershipConflictRepairClosesVerifyCheckAndGuide(t *testing.T) {
	root, cfg := alignedVolumeCLIFixture(t, true, true)
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	content := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(root) + "/===\n" +
		"main.go[CD7S]: F:run the deterministic CLI fixture | R:aoci.txt | A:main | S:Execution preserves the exact fixture boundary\n" +
		"aoci.txt[CD7S]: F:describe the repository cognition root | R:- | A:- | S:Root ownership remains exclusive\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load aligned Baseline: exists=%v err=%v", exists, err)
	}
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
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	for name, invoke := range map[string]func(*cobra.Command) error{
		"verify": func(cmd *cobra.Command) error { return runVerify(cmd, nil) },
		"check":  func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) },
		"guide":  func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "codex") },
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			err := invoke(cmd)
			if name != "guide" && err == nil {
				t.Fatalf("%s accepted an ownership conflict", name)
			}
			for _, want := range []string{
				`"code": "code_orphan"`,
				`"cause": "volume_ownership_conflict"`,
				`"expected_owner": "root"`,
				`"actual_owner": "code"`,
				`"affected_path": "aoci.txt"`,
				`"safe_repair_action": "aoci_remove_entry path=code:aoci.txt"`,
			} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("%s output missing %s:\n%s", name, want, output.String())
				}
			}
		})
	}

	rootBefore, err := os.ReadFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		t.Fatal(err)
	}
	metaBefore, err := os.ReadFile(filepath.Join(root, "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	sourceBefore, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	databaseBefore, err := os.ReadFile(filepath.Join(root, "aoci.database.txt"))
	if err != nil {
		t.Fatal(err)
	}
	previousLocale := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previousLocale) })
	outcome, fail := mcptools.ApplyRemoveEntry(root, "code:aoci.txt", "agent", true, false)
	if fail != nil || outcome == nil || !outcome.OwnershipRepair || outcome.PreservedOwner != cognition.OwnerRoot {
		t.Fatalf("ownership repair failed: outcome=%#v fail=%+v", outcome, fail)
	}
	rendered := mcptools.RenderRemoveOutcome(outcome)
	for _, want := range []string{
		"removed entry:\ncode:aoci.txt",
		"preserved owner:\nroot",
		"source unchanged:\ntrue",
		"database unchanged:\ntrue",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("ownership repair output missing %q:\n%s", want, rendered)
		}
	}
	for rel, before := range map[string][]byte{
		"aoci.txt": rootBefore, "aoci.meta.txt": metaBefore,
		"main.go": sourceBefore, "aoci.database.txt": databaseBefore,
	} {
		after, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil || !bytes.Equal(after, before) {
			t.Fatalf("ownership repair changed guarded %s: err=%v", rel, readErr)
		}
	}
	if codeAfter, readErr := os.ReadFile(filepath.Join(root, "aoci.code.txt")); readErr != nil ||
		strings.Contains(string(codeAfter), "aoci.txt[") {
		t.Fatalf("ownership repair retained the misplaced Code Entry: err=%v\n%s", readErr, codeAfter)
	}

	set, err = cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, invoke := range map[string]func(*cobra.Command) error{
		"verify": func(cmd *cobra.Command) error { return runVerify(cmd, nil) },
		"check":  func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) },
		"guide":  func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "codex") },
	} {
		t.Run("after_repair_"+name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			if err := invoke(cmd); err != nil {
				t.Fatalf("%s remained blocked after ownership repair: %v\n%s", name, err, output.String())
			}
			if !strings.Contains(output.String(), `"governance_aligned": true`) {
				t.Fatalf("%s did not report alignment:\n%s", name, output.String())
			}
			if name == "check" && !strings.Contains(output.String(), `"ok": true`) {
				t.Fatalf("Check did not report ok=true:\n%s", output.String())
			}
			if name == "guide" && (!strings.Contains(output.String(), `"stage": "aligned"`) ||
				!strings.Contains(output.String(), `"next_action": "none"`)) {
				t.Fatalf("Guide did not align:\n%s", output.String())
			}
		})
	}
}

func TestFourLegalVolumeLayoutsVerifyCheckAndGuideMatrix(t *testing.T) {
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
			root, cfg := alignedVolumeCLIFixture(t, test.code, test.database)
			set, err := cognition.Load(root, cfg.IndexPath)
			if err != nil {
				t.Fatal(err)
			}
			oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
			flagRepo, flagJSON, flagQuiet = root, true, false
			t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
			for name, invoke := range map[string]func(*cobra.Command) error{
				"verify": func(cmd *cobra.Command) error { return runVerify(cmd, nil) },
				"check":  func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) },
				"guide":  func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "codex") },
			} {
				t.Run(name, func(t *testing.T) {
					var output bytes.Buffer
					cmd := &cobra.Command{}
					cmd.SetOut(&output)
					cmd.SetErr(&output)
					if err := invoke(cmd); err != nil {
						t.Fatalf("%s rejected a legal layout: %v\n%s", name, err, output.String())
					}
					for _, want := range []string{`"structure_valid": true`, `"governance_aligned": true`, `"network_accessed": false`} {
						if !strings.Contains(output.String(), want) {
							t.Fatalf("%s output missing %s:\n%s", name, want, output.String())
						}
					}
					if strings.Contains(output.String(), "volume_read_only") {
						t.Fatalf("%s retained the rc17.5 read-only blocker:\n%s", name, output.String())
					}
					if name == "check" && (!strings.Contains(output.String(), `"ok": true`) || !strings.Contains(output.String(), `"exit_code": 0`)) {
						t.Fatalf("Check did not align:\n%s", output.String())
					}
					if name == "guide" && (!strings.Contains(output.String(), `"stage": "aligned"`) ||
						!strings.Contains(output.String(), `"next_action": "none"`) || !strings.Contains(output.String(), `"executable_targets": 0`)) {
						t.Fatalf("Guide did not align:\n%s", output.String())
					}
				})
			}
		})
	}
}

func TestCognitionOptimizationCheckpointDoesNotChangeVerifyCheckOrGuide(t *testing.T) {
	root, cfg := alignedVolumeCLIFixture(t, true, false)
	if _, err := cognitionoptimization.Create(root, cognitionoptimization.CreateInput{
		OptimizationID:      strings.Repeat("a", 64),
		CurrentBatchID:      strings.Repeat("b", 64),
		RemainingObjectRefs: []string{"code:main.go"},
	}); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })

	for name, invoke := range map[string]func(*cobra.Command) error{
		"verify": func(cmd *cobra.Command) error { return runVerify(cmd, nil) },
		"check":  func(cmd *cobra.Command) error { return runCheckCommand(cmd, nil) },
		"guide":  func(cmd *cobra.Command) error { return writeVolumeAgentGuide(cmd, root, cfg, set, "codex") },
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			if err := invoke(cmd); err != nil {
				t.Fatalf("%s treated an optimization draft as formal governance debt: %v\n%s", name, err, output.String())
			}
			if !strings.Contains(output.String(), `"governance_aligned": true`) {
				t.Fatalf("%s lost formal alignment while an optimization checkpoint was active:\n%s", name, output.String())
			}
			if name == "check" && !strings.Contains(output.String(), `"ok": true`) {
				t.Fatalf("Check did not remain ok:\n%s", output.String())
			}
			if name == "guide" && (!strings.Contains(output.String(), `"stage": "aligned"`) ||
				!strings.Contains(output.String(), `"next_action": "none"`)) {
				t.Fatalf("Guide did not remain aligned:\n%s", output.String())
			}
		})
	}
}

func TestRC175TestOnlyVolumeReadOnlyGapIsClosedByCurrentCheck(t *testing.T) {
	// This fixed value is a test-only black-box replay of the rc17.5 public
	// outcome. It is historical fixture evidence, not a live model or runtime
	// result. The same deidentified legal layout must now pass Check directly.
	const rc175BlackBoxOutcome = "volume_read_only"
	if rc175BlackBoxOutcome != "volume_read_only" {
		t.Fatal("rc17.5 black-box fixture no longer reproduces the historical stop")
	}
	root, _ := alignedVolumeCLIFixture(t, true, true)
	oldRepo, oldJSON, oldQuiet := flagRepo, flagJSON, flagQuiet
	flagRepo, flagJSON, flagQuiet = root, true, false
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = oldRepo, oldJSON, oldQuiet })
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := runCheckCommand(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), rc175BlackBoxOutcome) || !strings.Contains(output.String(), `"ok": true`) ||
		!strings.Contains(output.String(), `"governance_aligned": true`) {
		t.Fatalf("rc18 did not close the test-only rc17.5 gap:\n%s", output.String())
	}
}

func alignedVolumeCLIFixture(t *testing.T, code, database bool) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	declarations := "#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"
	if code {
		declarations += "#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"
	}
	if database {
		declarations += "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled\n"
	}
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: CLI four-layout fixture\n#Global-Invariants: deterministic fixture bytes\n" + declarations
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
		"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n" +
		"#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	for name, data := range map[string]string{"aoci.txt": rootText, "aoci.meta.txt": metaText} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code {
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		content := cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(root) + "/===\nmain.go[CD7S]: F:run the deterministic CLI fixture | R:- | A:main | S:Execution preserves the exact fixture boundary\n"
		if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	databaseLine := "items[DB7S]: F:store the deterministic fixture item | R:- | A:id | S:Removal preserves the explicit retention boundary"
	if database {
		content := cognition.DatabaseMarker + "\n===Database/database://primary/public/===\n" + databaseLine + "\n"
		if err := os.WriteFile(filepath.Join(root, "aoci.database.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath, cfg.LedgerEnabled = "aoci.txt", false
	if database {
		cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
			Database: "app", Namespaces: []string{"public"}, CredentialEnv: "TEST_ONLY_DSN",
			ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
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
		manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary",
			Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{},
			ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{},
			CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
		table := dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/items",
			Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: "items", Kind: "base_table",
			Columns:    []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
			PrimaryKey: &dbevidence.KeyConstraint{Name: "items_pkey", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{},
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
		fingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.database.txt"))
		if err != nil {
			t.Fatal(err)
		}
		state.Files["aoci.database.txt"] = fingerprint
		if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{ObjectRef: table.ObjectRef,
			SourceID: "primary", EvidenceVersion: snapshot.EvidenceVersion, TableEvidenceSHA256: snapshot.Tables[0].TableEvidenceSHA256,
			EntrySHA256: dbcognition.EntrySHA256(databaseLine)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func TestVolumeStatusInventoryAndScoreShareReadProjection(t *testing.T) {
	root, _ := alignedVolumeCLIFixture(t, true, false)
	for _, invocation := range [][]string{
		{"--repo", root, "--json", "status", "--deep"},
		{"--repo", root, "--json", "index", "inventory"},
		{"--repo", root, "--json", "index", "score"},
	} {
		var stdout, stderr bytes.Buffer
		if code := executeCLI(invocation, &stdout, &stderr); code != ExitOK {
			t.Fatalf("%v failed: code=%d\nstdout=%s\nstderr=%s", invocation, code, stdout.String(), stderr.String())
		}
		combined := stdout.String() + stderr.String()
		if !strings.Contains(combined, `"layout_mode": "volumes-v1"`) ||
			!strings.Contains(combined, `"composite_identity":`) ||
			!strings.Contains(combined, `"governance_aligned": true`) {
			t.Fatalf("%v did not expose the shared Volumes projection:\n%s", invocation, combined)
		}
		if strings.Contains(combined, "volume_read_only") {
			t.Fatalf("%v retained the obsolete read-only rejection:\n%s", invocation, combined)
		}
	}
}

func TestDoctorReadsVolumeLayoutWithoutLegacyDocument(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "testdata", "volumes", "database-quality")
	for _, name := range []string{"aoci.txt", "aoci.meta.txt", "aoci.database.txt"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.IndexPath = "aoci.txt"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	oldRepo := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = oldRepo })

	output, err := runDoctor(t, "")
	if err == nil {
		t.Fatal("doctor should still report the intentionally absent Baseline")
	}
	if !strings.Contains(output, "2") || !strings.Contains(output, "8") {
		t.Fatalf("doctor did not report Volume descriptors and objects:\n%s", output)
	}
}
