package mcptools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/managedscope"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

func buildObservedVolumeRepo(t *testing.T) string {
	t.Helper()
	root := buildVolumeRepo(t, true, false)
	writeVolumeTestFile(t, root, "aoci.code.txt", cognition.CodeVolumeMarker+"\n===Go sources"+filepath.ToSlash(root)+"/===\n"+
		"main.go[CD9S]: F:run the fixture | R:- | A:main | S:Keep execution deterministic\n")
	writeVolumeTestFile(t, root, "notes.txt", "observe-only release notes\n")
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	policy := managedscope.LegacyPolicy()
	policy.Rules = append(policy.Rules, managedscope.Rule{
		RuleID: "observe-notes", Action: machinecontract.ScopeRoleObserve,
		Pattern: "notes.txt", PatternKind: machinecontract.ScopePatternFile,
		Reason: "retain release notes as observe-only evidence", Source: machinecontract.ScopeRuleUser,
		CreatedBy: "first-attestation-test", Order: 1, Enabled: true,
	})
	policy, err = managedscope.Normalize(policy)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManagedScope = &policy
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	evaluation, err := managedscope.Build(root, policy, managedscope.BuildOptions{WalkOptions: cfg.WalkOptions()})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := managedscope.Snapshot(root, evaluation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		fingerprint.Role = machinecontract.ScopeRoleIndex
		snapshot[rel] = fingerprint
	}
	budgetIdentity, err := cognitionbudget.Identity(cfg.EffectiveCognitionBudget())
	if err != nil {
		t.Fatal(err)
	}
	value := baseline.NewBaseline(snapshot)
	value.ManagedScope = &baseline.ManagedScopeState{
		Version: machinecontract.ManagedScopeBaselineV1, PolicyIdentity: evaluation.PolicyIdentity,
		ObserveChangePolicy: policy.ObserveChangePolicy, BudgetPolicyIdentity: budgetIdentity,
	}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.ManagedScope.ObserveCount != 1 || facts.CodeSourceCount != 1 || facts.CodeEntryCount != 1 {
		t.Fatalf("observe-only fixture is not aligned: facts=%#v err=%v", facts, err)
	}
	return root
}

func volumeAttestationArguments(t *testing.T, root, first string, wrongChallenge bool) map[string]any {
	t.Helper()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	view, err := loaded.set.Scope(cognition.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	_, sequence, err := buildVolumeOverviewBody(view)
	if err != nil {
		t.Fatal(err)
	}
	challenge := buildOverviewChallenge(view.ScopeIdentity, sequence)
	report := completeAttestation(challenge, view.ObjectCount, volumeScopeBytes(view)/3)
	if wrongChallenge && len(report.ChallengeAnswers) > 0 {
		report.ChallengeAnswers[0].ObjectIdentity = "code:deliberately-wrong.go"
	}
	return map[string]any{
		"scope": cognition.ScopeAll,
		"host_delivery_confirmation": map[string]any{
			"version": overviewDeliveryReceiptV1, "body_sha256": overviewMetadataValue(t, first, "body_sha256"),
			"body_bytes": mustOverviewInt(t, first, "body_utf8_bytes"), "end_marker_observed": true,
		},
		"model_cognition_attestation": attestationMap(t, report),
	}
}

func mustOverviewInt(t *testing.T, output, key string) int {
	t.Helper()
	value := overviewMetadataValue(t, output, key)
	result := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			t.Fatalf("Overview metadata %s is not an integer: %q", key, value)
		}
		result = result*10 + int(character-'0')
	}
	return result
}

func assertGovernedFirstAttestation(t *testing.T, output string) {
	t.Helper()
	for _, want := range []string{
		"governance_aligned: true", "model_attestation: pass", "cognition_level: 4",
		"cognition_level_state: cognition_governed", "cognition_assimilation: complete",
		"model_full_cognition_reliable: true",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("first Attestation missing %q:\n%s", want, output)
		}
	}
}

func formalVolumeHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json"} {
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		result[rel] = fingerprint.SHA256
	}
	return result
}

func runFixtureGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=AOCI Test", "GIT_AUTHOR_EMAIL=aoci-test@example.invalid",
		"GIT_COMMITTER_NAME=AOCI Test", "GIT_COMMITTER_EMAIL=aoci-test@example.invalid")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func commitFixture(t *testing.T, root string) {
	t.Helper()
	runFixtureGit(t, root, "init", "-b", "main")
	runFixtureGit(t, root, "add", ".")
	runFixtureGit(t, root, "commit", "-m", "test: commit aligned cognition")
}

func TestCleanCommittedObservedVolumeFirstAttestationIsGovernedWithoutVerifyHistory(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	commitFixture(t, root)
	if _, err := os.Stat(filepath.Join(root, ".aoci", "verify_history")); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has Verify History: %v", err)
	}
	formalBefore := formalVolumeHashes(t, root)
	session := connectMCPClient(t, root)
	if rules := callVolumeTool(t, session, "aoci_rules", nil); !strings.Contains(rules, "aoci_overview") {
		t.Fatal("Rules did not expose the ordinary cognition entry")
	}
	first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
	if !strings.Contains(first, "governance_aligned: true") {
		t.Fatalf("first delivery did not use live governance facts:\n%s", first)
	}
	attested := callVolumeTool(t, session, "aoci_overview", volumeAttestationArguments(t, root, first, false))
	assertGovernedFirstAttestation(t, attested)
	formalAfter := formalVolumeHashes(t, root)
	for rel, before := range formalBefore {
		if formalAfter[rel] != before {
			t.Fatalf("Overview/Attestation changed formal asset %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".aoci", "verify_history")); !os.IsNotExist(err) {
		t.Fatalf("Overview created Verify History: %v", err)
	}
}

func TestCommitOnlyHeadChangeDoesNotInvalidateDeliveredGovernance(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	commitFixture(t, root)
	session := connectMCPClient(t, root)
	first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
	arguments := volumeAttestationArguments(t, root, first, false)
	formalBefore := formalVolumeHashes(t, root)
	runFixtureGit(t, root, "commit", "--allow-empty", "-m", "test: advance head only")
	attested := callVolumeTool(t, session, "aoci_overview", arguments)
	assertGovernedFirstAttestation(t, attested)
	formalAfter := formalVolumeHashes(t, root)
	for rel, before := range formalBefore {
		if formalAfter[rel] != before {
			t.Fatalf("commit-only transition changed %s", rel)
		}
	}
}

func captureVolumeGovernanceSnapshot(t *testing.T, root string) (*cognitionRepoCtx, volumeGovernanceSnapshot) {
	t.Helper()
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		t.Fatal(fail.Msg)
	}
	_, snapshot, inspectFail := inspectVolumeGovernance(root, loaded, true)
	if inspectFail != nil {
		t.Fatal(inspectFail.Msg)
	}
	return loaded, snapshot
}

func TestVolumeGovernanceLightConfirmationRejectsInputDrift(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		writeVolumeTestFile(t, root, "main.go", "package main\n// changed during Overview rendering\n")
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
			t.Fatalf("source drift passed light confirmation: %+v", fail)
		}
	})

	t.Run("baseline exact bytes", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		path := filepath.Join(root, ".aoci", "baseline.json")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
			t.Fatalf("Baseline byte drift passed light confirmation: %+v", fail)
		}
	})

	t.Run("configuration exact bytes", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		path := config.FilePath(root)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
			t.Fatalf("configuration byte drift passed light confirmation: %+v", fail)
		}
	})

	t.Run("database evidence", func(t *testing.T) {
		root := buildThreeTableDatabaseVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		_, evidence, exists, err := dbevidence.LoadSnapshot(root, "primary")
		if err != nil || !exists || len(evidence.Tables) == 0 {
			t.Fatalf("load Database Evidence: exists=%t tables=%d err=%v", exists, len(evidence.Tables), err)
		}
		path := filepath.Join(dbevidence.RuntimeEvidenceRoot(root), filepath.FromSlash(evidence.Tables[0].EvidenceRef))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
			t.Fatalf("Database Evidence drift passed light confirmation: %+v", fail)
		}
	})

	t.Run("recovery", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		writeVolumeTestFile(t, root, ".aoci/transactions/entries-race.json", "{}\n")
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail == nil || fail.Code != errCognitionSnapshotUnavailable {
			t.Fatalf("Recovery drift passed light confirmation: %+v", fail)
		}
	})

	t.Run("commit only", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		commitFixture(t, root)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		runFixtureGit(t, root, "commit", "--allow-empty", "-m", "test: advance head during rendering")
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail != nil {
			t.Fatalf("commit-only transition invalidated light confirmation: %+v", fail)
		}
	})

	t.Run("runtime audit only", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		loaded, snapshot := captureVolumeGovernanceSnapshot(t, root)
		writeVolumeTestFile(t, root, ".aoci/ledger.jsonl", "{}\n")
		if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail != nil {
			t.Fatalf("runtime audit transition invalidated light confirmation: %+v", fail)
		}
	})
}

func BenchmarkVolumeGovernanceStrictTailConfirmation(b *testing.B) {
	root := buildVolumeRepo(b, true, false)
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		b.Fatal(fail.Msg)
	}
	_, snapshot, inspectFail := inspectVolumeGovernance(root, loaded, true)
	if inspectFail != nil {
		b.Fatal(inspectFail.Msg)
	}
	b.Run("light_identity_recheck", func(b *testing.B) {
		for range b.N {
			if fail := confirmVolumeGovernanceSnapshot(root, loaded, snapshot); fail != nil {
				b.Fatal(fail.Msg)
			}
		}
	})
	b.Run("complete_governance_assessment", func(b *testing.B) {
		for range b.N {
			if _, err := volumegovernance.Assess(root, loaded.cfg, loaded.set); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("strict_assess_and_light_recheck", func(b *testing.B) {
		for range b.N {
			_, observation, err := volumegovernance.AssessWithObservation(root, loaded.cfg, loaded.set)
			if err != nil {
				b.Fatal(err)
			}
			if err := volumegovernance.ConfirmObservation(root, loaded.cfg, loaded.set, observation); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("two_complete_governance_assessments", func(b *testing.B) {
		for range b.N {
			if _, err := volumegovernance.Assess(root, loaded.cfg, loaded.set); err != nil {
				b.Fatal(err)
			}
			if _, err := volumegovernance.Assess(root, loaded.cfg, loaded.set); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestGovernanceChangesBetweenDeliveryAndAttestationFailClosed(t *testing.T) {
	t.Run("source drift", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		session := connectMCPClient(t, root)
		first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
		arguments := volumeAttestationArguments(t, root, first, false)
		writeVolumeTestFile(t, root, "main.go", "package main\n// actual source drift\n")
		output := callVolumeTool(t, session, "aoci_overview", arguments)
		if !strings.Contains(output, "challenge_passed: 1/1") || !strings.Contains(output, "governance_aligned: false") ||
			!strings.Contains(output, "cognition_level: 3") || strings.Contains(output, "model_full_cognition_reliable: true") {
			t.Fatalf("source drift did not fail closed:\n%s", output)
		}
	})

	t.Run("formal code drift", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		session := connectMCPClient(t, root)
		first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
		arguments := volumeAttestationArguments(t, root, first, false)
		code := volumeFileText(t, root, "aoci.code.txt")
		writeVolumeTestFile(t, root, "aoci.code.txt", code+"# changed after delivery\n")
		output := callVolumeTool(t, session, "aoci_overview", arguments)
		if strings.Contains(output, "cognition_level: 4") || strings.Contains(output, "model_full_cognition_reliable: true") {
			t.Fatalf("changed Code Volume was combined with the old delivery:\n%s", output)
		}
	})

	t.Run("baseline drift", func(t *testing.T) {
		root := buildObservedVolumeRepo(t)
		session := connectMCPClient(t, root)
		first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
		arguments := volumeAttestationArguments(t, root, first, false)
		value, _, err := baseline.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := time.Parse(time.RFC3339, value.UpdatedAt)
		if err != nil {
			t.Fatal(err)
		}
		value.UpdatedAt = updated.Add(time.Second).UTC().Format(time.RFC3339)
		encoded, err := baseline.MarshalExact(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".aoci", "baseline.json"), encoded, 0o644); err != nil {
			t.Fatal(err)
		}
		output := callVolumeTool(t, session, "aoci_overview", arguments)
		if !strings.Contains(output, "governance_aligned: false") || strings.Contains(output, "cognition_level: 4") {
			t.Fatalf("changed Baseline was combined with the old delivery:\n%s", output)
		}
	})
}

func TestPendingRecoveryAndConflictNeverReachGovernedLevel(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
	}{
		{name: "pending transaction", path: ".aoci/transactions/scope-pending.json"},
		{name: "recovery", path: ".aoci/transactions/entries-pending.json"},
		{name: "third-party conflict", path: "aoci.code.txt.aoci-cas.intent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := buildObservedVolumeRepo(t)
			writeVolumeTestFile(t, root, test.path, "{}\n")
			output := callVolumeTool(t, connectMCPClient(t, root), "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
			if strings.Contains(output, "cognition_level: 4") || strings.Contains(output, "model_full_cognition_reliable: true") {
				t.Fatalf("%s reached governed cognition:\n%s", test.name, output)
			}
		})
	}
}

func buildThreeTableDatabaseVolumeRepo(t *testing.T) string {
	t.Helper()
	root := buildVolumeRepo(t, false, true)
	lines := map[string]string{
		"users":        "users[DB7S]: F:store canonical user state | R:- | A:id | S:Retained ownership records prevent direct identity deletion",
		"orders":       "orders[DB7S]: F:store canonical order state | R:- | A:id | S:Order transitions preserve deterministic transaction boundaries",
		"audit_events": "audit_events[DB7S]: F:store append-only audit state | R:- | A:id | S:Audit records are never rewritten by ordinary maintenance",
	}
	ordered := []string{"users", "orders", "audit_events"}
	databaseText := cognition.DatabaseMarker + "\n===Primary tables/database://primary/public/===\n"
	for _, name := range ordered {
		databaseText += lines[name] + "\n"
	}
	writeVolumeTestFile(t, root, "aoci.database.txt", databaseText)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{
		SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "TEST_ONLY_DSN",
		ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true,
	}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	manifest := dbevidence.SourceManifest{
		Version: dbevidence.SourceManifestVersion, SourceID: "primary", Engine: dbevidence.EnginePostgreSQL,
		Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{},
		IncludeTables: []string{}, ExcludeTables: []string{},
		CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false,
	}
	tables := make([]dbevidence.TableEvidence, 0, len(ordered))
	for _, name := range ordered {
		tables = append(tables, dbevidence.TableEvidence{
			Version: dbevidence.EvidenceVersion, ObjectRef: "database://primary/public/" + name,
			Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table",
			Columns:           []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}},
			PrimaryKey:        &dbevidence.KeyConstraint{Name: name + "_pkey", Columns: []string{"id"}},
			UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{},
			Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{},
		})
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
	value := baseline.NewBaseline(nil)
	for _, rel := range []string{"aoci.txt", "aoci.meta.txt", "aoci.database.txt"} {
		fingerprint, hashErr := baseline.HashFile(filepath.Join(root, rel))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		value.Files[rel] = fingerprint
	}
	for _, table := range snapshot.Tables {
		name := strings.TrimPrefix(table.ObjectRef, "database://primary/public/")
		if err := baseline.UpdateDatabaseCognitionBinding(value, baseline.DatabaseCognitionBinding{
			ObjectRef: table.ObjectRef, SourceID: "primary", EvidenceVersion: snapshot.EvidenceVersion,
			TableEvidenceSHA256: table.TableEvidenceSHA256, EntrySHA256: dbcognition.EntrySHA256(lines[name]),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := volumegovernance.Assess(root, cfg, set)
	if err != nil || !facts.GovernanceAligned || facts.DatabaseEntryCount != 3 || facts.DatabaseBindingCount != 3 || facts.DatabaseCognition.Summary.Current != 3 {
		t.Fatalf("3/3 Database fixture is not current: facts=%#v err=%v", facts, err)
	}
	return root
}

func TestDatabaseAbsentAndThreeOfThreeCurrentCanReachGovernedLevel(t *testing.T) {
	for _, test := range []struct {
		name string
		root func(*testing.T) string
	}{
		{name: "database absent", root: buildObservedVolumeRepo},
		{name: "database 3 of 3 current", root: buildThreeTableDatabaseVolumeRepo},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := test.root(t)
			session := connectMCPClient(t, root)
			_ = callVolumeTool(t, session, "aoci_rules", nil)
			first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
			output := callVolumeTool(t, session, "aoci_overview", volumeAttestationArguments(t, root, first, false))
			assertGovernedFirstAttestation(t, output)
		})
	}
}

func TestChallengeFailureCannotBecomeReliableWhenGovernanceIsAligned(t *testing.T) {
	root := buildObservedVolumeRepo(t)
	session := connectMCPClient(t, root)
	first := callVolumeTool(t, session, "aoci_overview", map[string]any{"scope": cognition.ScopeAll})
	output := callVolumeTool(t, session, "aoci_overview", volumeAttestationArguments(t, root, first, true))
	if !strings.Contains(output, "governance_aligned: true") || !strings.Contains(output, "model_attestation: fail") ||
		!strings.Contains(output, "cognition_assimilation: uncertain") || strings.Contains(output, "model_full_cognition_reliable: true") {
		t.Fatalf("Challenge failure became reliable:\n%s", output)
	}
}

func TestLegacyFirstAttestationLevelBehaviorRemainsCompatible(t *testing.T) {
	root := buildRepo(t)
	session := connectMCPClient(t, root)
	first, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "aoci_overview"})
	if err != nil {
		t.Fatal(err)
	}
	firstText := resText(t, first)
	attested, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "aoci_overview", Arguments: map[string]any{
		"host_delivery_confirmation":  hostConfirmationFromOverview(t, firstText),
		"model_cognition_attestation": validLegacyAttestationMap(t, root),
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertGovernedFirstAttestation(t, resText(t, attested))
}
