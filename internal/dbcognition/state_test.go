package dbcognition

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestAssessDatabaseCognitionLifecycle(t *testing.T) {
	root, sources := databaseFixture(t, []string{"orders", "users"}, []string{"users"})
	set := loadFixtureSet(t, root)
	state := baseline.NewBaseline(nil)
	assessment := Assess(root, sources, set, state)
	if assessment.Summary.Missing != 1 || assessment.Summary.Unbaselined != 1 || assessment.CognitionCurrent {
		t.Fatalf("unexpected initial state: %#v", assessment)
	}
	if assessment.NextAction != "call_no_argument_aoci_maintain_for_current_machine_batch" {
		t.Fatalf("database debt did not route to ordinary no-argument Maintain: %q", assessment.NextAction)
	}
	users := itemByRef(t, assessment, "database://primary/public/users")
	if err := baseline.UpdateDatabaseCognitionBinding(state, baseline.DatabaseCognitionBinding{
		ObjectRef: users.ObjectRef, SourceID: users.SourceID, EvidenceVersion: users.EvidenceVersion,
		TableEvidenceSHA256: users.TableEvidenceSHA256, EntrySHA256: EntrySHA256(users.CurrentEntry),
	}); err != nil {
		t.Fatal(err)
	}
	assessment = Assess(root, sources, set, state)
	if assessment.Summary.Current != 1 || assessment.Summary.Missing != 1 || assessment.Summary.Unbaselined != 0 {
		t.Fatalf("binding did not advance one object: %#v", assessment.Summary)
	}
	changedUsers := fixtureTable("database://primary/public/users")
	changedUsers.Columns = append(changedUsers.Columns, dbevidence.Column{Ordinal: 2, Name: "email", NativeType: "text", CanonicalType: "text", Nullable: false})
	writeEvidence(t, root, []dbevidence.TableEvidence{fixtureTable("database://primary/public/orders"), changedUsers})
	assessment = Assess(root, sources, set, state)
	if assessment.Summary.Stale != 1 || itemByRef(t, assessment, "database://primary/public/users").State != machinecontract.DatabaseCognitionStale {
		t.Fatalf("Evidence drift did not make cognition stale: %#v", assessment)
	}

	writeDatabaseVolume(t, root, []string{"users", "ghost"})
	set = loadFixtureSet(t, root)
	assessment = Assess(root, sources, set, state)
	if assessment.Summary.Orphan != 1 || itemByRef(t, assessment, "database://primary/public/ghost").State != machinecontract.DatabaseCognitionOrphan {
		t.Fatalf("orphan not detected: %#v", assessment)
	}
}

func TestNoConfigurationCreatesNoDatabaseDebt(t *testing.T) {
	root, _ := databaseFixture(t, []string{"users"}, []string{"users"})
	assessment := Assess(root, nil, loadFixtureSet(t, root), baseline.NewBaseline(nil))
	if !assessment.CognitionCurrent || len(assessment.Items) != 0 || assessment.NextAction != "no_database_configuration" {
		t.Fatalf("unconfigured repository acquired database debt: %#v", assessment)
	}
}

func TestNoConfigurationAndAbsentDatabaseVolumeRemainDebtFree(t *testing.T) {
	assessment := Assess(t.TempDir(), nil, nil, baseline.NewBaseline(nil))
	if !assessment.CognitionCurrent || assessment.DatabaseVolumeState != cognition.AssetAbsent ||
		assessment.NextAction != machinecontract.DatabaseCognitionActionNoConfiguration || len(assessment.Items) != 0 {
		t.Fatalf("unconfigured Legacy or absent-Volume repository acquired database debt: %#v", assessment)
	}
}

func TestAcceptedEvidenceRoutesAbsentDatabaseVolumeToIndependentBootstrap(t *testing.T) {
	root, sources := databaseFixture(t, []string{"users"}, nil)
	rootPath := filepath.Join(root, "aoci.txt")
	rootRaw, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw = []byte(strings.ReplaceAll(string(rootRaw), "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n", ""))
	if err := os.WriteFile(rootPath, rootRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "aoci.database.txt")); err != nil {
		t.Fatal(err)
	}
	_, snapshot, exists, err := dbevidence.LoadSnapshot(root, "primary")
	if err != nil || !exists {
		t.Fatalf("load snapshot: exists=%t err=%v", exists, err)
	}
	if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
		t.Fatal(err)
	}
	set := loadFixtureSet(t, root)
	assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
	if assessment.DatabaseVolumeState != cognition.AssetAbsent || assessment.BlockingSourceCount != 0 ||
		assessment.NextAction != machinecontract.DatabaseCognitionActionBootstrapVolume || assessment.EvidenceTableCount != 1 {
		t.Fatalf("accepted Evidence did not route to independent Database Bootstrap: %#v", assessment)
	}
}

func TestEvidenceUnavailableInvalidAndDisabledRemainDistinct(t *testing.T) {
	root, sources := databaseFixture(t, []string{"users"}, []string{"users"})
	set := loadFixtureSet(t, root)
	if err := os.RemoveAll(dbevidence.RuntimeEvidenceRoot(root)); err != nil {
		t.Fatal(err)
	}
	unavailable := Assess(root, sources, set, baseline.NewBaseline(nil))
	if unavailable.CognitionCurrent || unavailable.Summary.EvidenceUnavailable != 1 || itemByRef(t, unavailable, "database://primary/public/users").State != machinecontract.DatabaseCognitionEvidenceUnavailable {
		t.Fatalf("unavailable Evidence was not distinct: %#v", unavailable)
	}

	writeEvidence(t, root, []dbevidence.TableEvidence{fixtureTable("database://primary/public/users")})
	manifestPath := filepath.Join(dbevidence.RuntimeEvidenceRoot(root), "primary", "snapshot.json")
	if err := os.WriteFile(manifestPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalid := Assess(root, sources, set, baseline.NewBaseline(nil))
	if invalid.CognitionCurrent || invalid.Summary.EvidenceInvalid != 1 || itemByRef(t, invalid, "database://primary/public/users").State != machinecontract.DatabaseCognitionEvidenceInvalid {
		t.Fatalf("invalid Evidence was not distinct: %#v", invalid)
	}

	sources[0].Enabled = false
	disabled := Assess(root, sources, set, baseline.NewBaseline(nil))
	if disabled.CognitionCurrent || disabled.Summary.SourceDisabled != 1 || itemByRef(t, disabled, "database://primary/public/users").State != machinecontract.DatabaseCognitionSourceDisabled {
		t.Fatalf("disabled source was not distinct: %#v", disabled)
	}
}

func TestEvidenceBlockerSummaryCountsObjectsNotSources(t *testing.T) {
	root, sources := databaseFixture(t, []string{"first", "second"}, []string{"first", "second"})
	if err := os.RemoveAll(dbevidence.RuntimeEvidenceRoot(root)); err != nil {
		t.Fatal(err)
	}
	assessment := Assess(root, sources, loadFixtureSet(t, root), baseline.NewBaseline(nil))
	if assessment.BlockingSourceCount != 1 || assessment.Summary.EvidenceUnavailable != 2 || len(assessment.Items) != 2 {
		t.Fatalf("source and object state units were mixed: %#v", assessment)
	}
}

func TestSavedEvidenceMustMatchCurrentSourceSelection(t *testing.T) {
	root, sources := databaseFixture(t, []string{"users"}, nil)
	set := loadFixtureSet(t, root)
	assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
	plan, err := BuildPlan(root, assessment, set, 20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	submission := []Submission{{ObjectRef: plan.Candidates[0].ObjectRef, CandidateID: plan.Candidates[0].CandidateID}}

	sources[0].Database = "other_app"
	sources[0].Namespaces = []string{"other_public"}
	mismatched := Assess(root, sources, set, baseline.NewBaseline(nil))
	if mismatched.CognitionCurrent || mismatched.BlockingSourceCount != 1 || mismatched.Summary.EvidenceUnavailable != 0 ||
		len(mismatched.Sources) != 1 || mismatched.Sources[0].ErrorCode != "database_snapshot_selection_mismatch" {
		t.Fatalf("old Evidence was accepted for a changed source selection: %#v", mismatched)
	}
	if _, err := ValidateSubmission(root, sources, plan.BatchID, submission); err == nil ||
		!strings.Contains(err.Error(), "source_selection_changed") {
		t.Fatalf("candidate survived source selection drift: %v", err)
	}
}

func TestCandidatePlanIsCompleteDeterministicAndEvidenceBound(t *testing.T) {
	root, sources := databaseFixture(t, []string{"accounts", "orders", "users"}, nil)
	set := loadFixtureSet(t, root)
	assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
	first, err := BuildPlan(root, assessment, set, 2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(root, assessment, set, 2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if first.BatchID != second.BatchID || first.TargetCount != 2 || first.Remaining != 1 || len(first.Candidates) != 2 {
		t.Fatalf("plan is not deterministic or paged: first=%#v second=%#v", first, second)
	}
	oversized, err := BuildPlan(root, assessment, set, 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if oversized.TargetCount != 1 || oversized.EvidenceBytes <= 1 || oversized.Candidates[0].EvidenceBundle.SemanticCandidateIncluded ||
		oversized.Candidates[0].EvidenceBundle.NextAction != machinecontract.DatabaseEvidenceActionAuthorCompleteTableFRAS {
		t.Fatalf("single oversized Evidence was truncated or acquired semantics: %#v", oversized)
	}
	submissions := []Submission{
		{ObjectRef: first.Candidates[1].ObjectRef, CandidateID: first.Candidates[1].CandidateID},
		{ObjectRef: first.Candidates[0].ObjectRef, CandidateID: first.Candidates[0].CandidateID},
	}
	if _, err := ValidateSubmission(root, sources, first.BatchID, submissions); err != nil {
		t.Fatalf("order-independent complete submission failed: %v", err)
	}
	if _, err := ValidateSubmission(root, sources, first.BatchID, submissions[:1]); err == nil {
		t.Fatal("partial batch was accepted")
	}

	changed := fixtureTable(first.Candidates[0].ObjectRef)
	changed.Columns = append(changed.Columns, dbevidence.Column{Ordinal: 2, Name: "changed", NativeType: "text", CanonicalType: "text", Nullable: true})
	other := fixtureTable(first.Candidates[1].ObjectRef)
	third := fixtureTable("database://primary/public/users")
	writeEvidence(t, root, []dbevidence.TableEvidence{changed, other, third})
	if _, err := ValidateSubmission(root, sources, first.BatchID, submissions); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("changed target Evidence did not invalidate the receipt: %v", err)
	}
}

func TestUnrelatedTableEvidenceChangeDoesNotInvalidateCandidate(t *testing.T) {
	root, sources := databaseFixture(t, []string{"accounts", "orders", "users"}, nil)
	set := loadFixtureSet(t, root)
	assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
	plan, err := BuildPlan(root, assessment, set, 2, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	submissions := []Submission{}
	for _, candidate := range plan.Candidates {
		submissions = append(submissions, Submission{ObjectRef: candidate.ObjectRef, CandidateID: candidate.CandidateID})
	}
	changedUnrelated := fixtureTable("database://primary/public/users")
	changedUnrelated.Columns = append(changedUnrelated.Columns, dbevidence.Column{Ordinal: 2, Name: "status", NativeType: "text", CanonicalType: "text", Nullable: false})
	writeEvidence(t, root, []dbevidence.TableEvidence{
		fixtureTable("database://primary/public/accounts"),
		fixtureTable("database://primary/public/orders"),
		changedUnrelated,
	})
	if _, err := ValidateSubmission(root, sources, plan.BatchID, submissions); err != nil {
		t.Fatalf("unrelated table Evidence invalidated a target-bound batch: %v", err)
	}
}

func TestReceiptRejectsUnsafeVolumePathAndNonActionableState(t *testing.T) {
	target := ReceiptTarget{
		ObjectRef: "database://primary/public/users", SourceID: "primary",
		CognitionState:  machinecontract.DatabaseCognitionMissing,
		EvidenceVersion: dbevidence.EvidenceVersion, TableEvidenceSHA256: strings.Repeat("a", 64),
		EvidenceRef: "primary/tables/" + strings.Repeat("a", 64) + ".json",
	}
	receipt := Receipt{
		Version:            machinecontract.DatabaseCognitionCandidateVersion,
		DatabaseVolumePath: "../aoci.database.txt", DatabaseVolumeSHA256: strings.Repeat("b", 64),
		Targets: []ReceiptTarget{target},
	}
	receipt.BatchID = receiptHash("database-cognition-batch/v1", receipt.DatabaseVolumePath, receipt.DatabaseVolumeSHA256, encodeTargets(receipt.Targets, false))
	receipt.Targets[0].CandidateID = receiptHash("database-cognition-object/v1", receipt.BatchID, target.ObjectRef, target.SourceID, target.CognitionState, target.EvidenceVersion, target.TableEvidenceSHA256, target.EvidenceRef)
	if err := validateReceipt(receipt); err == nil {
		t.Fatal("unsafe Database Volume path was accepted")
	}
	receipt.DatabaseVolumePath = "aoci.database.txt"
	receipt.Targets[0].CognitionState = machinecontract.DatabaseCognitionCurrent
	receipt.BatchID = receiptHash("database-cognition-batch/v1", receipt.DatabaseVolumePath, receipt.DatabaseVolumeSHA256, encodeTargets(receipt.Targets, false))
	receipt.Targets[0].CandidateID = receiptHash("database-cognition-object/v1", receipt.BatchID, receipt.Targets[0].ObjectRef, receipt.Targets[0].SourceID, receipt.Targets[0].CognitionState, receipt.Targets[0].EvidenceVersion, receipt.Targets[0].TableEvidenceSHA256, receipt.Targets[0].EvidenceRef)
	if err := validateReceipt(receipt); err == nil {
		t.Fatal("non-actionable candidate state was accepted")
	}
}

func TestCandidateReceiptTamperingFailsClosed(t *testing.T) {
	root, sources := databaseFixture(t, []string{"users"}, nil)
	set := loadFixtureSet(t, root)
	plan, err := BuildPlan(root, Assess(root, sources, set, baseline.NewBaseline(nil)), set, 20, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath(root, plan.BatchID), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ValidateSubmission(root, sources, plan.BatchID, []Submission{{ObjectRef: plan.Candidates[0].ObjectRef, CandidateID: plan.Candidates[0].CandidateID}})
	if err == nil {
		t.Fatal("tampered candidate receipt was accepted")
	}
}

func TestCandidatePlanScalesDeterministically(t *testing.T) {
	for _, count := range []int{1, 10, 100, 1000} {
		t.Run(fmt.Sprintf("tables_%d", count), func(t *testing.T) {
			names := make([]string, count)
			for index := range names {
				names[index] = fmt.Sprintf("table_%04d", index)
			}
			root, sources := databaseFixture(t, names, nil)
			set := loadFixtureSet(t, root)
			assessment := Assess(root, sources, set, baseline.NewBaseline(nil))
			limit := 17
			first, err := BuildPlan(root, assessment, set, limit, 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			second, err := BuildPlan(root, assessment, set, limit, 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			want := count
			if want > limit {
				want = limit
			}
			if first.BatchID != second.BatchID || first.TargetCount != want || first.Remaining != count-want {
				t.Fatalf("non-deterministic scale plan: first=%#v second=%#v", first, second)
			}
		})
	}
}

func databaseFixture(t testing.TB, evidenceTables, cognitionTables []string) (string, []dbevidence.SourceConfig) {
	t.Helper()
	root := t.TempDir()
	rootText := cognition.RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=-\n#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta\n"
	metaText := cognition.MetaVolumeMarker + "\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n#[Tag dictionary: code]\n#A Layer: C Code\n#B Module: D Domain\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n#[Tag dictionary: database]\n#A Layer: D Database\n#B Module: B Business\n#C Importance: 9 8 7 5 3 1\n#E Scale: L M S T\n"
	mustWrite(t, filepath.Join(root, "aoci.txt"), rootText)
	mustWrite(t, filepath.Join(root, "aoci.meta.txt"), metaText)
	writeDatabaseVolume(t, root, cognitionTables)
	tables := make([]dbevidence.TableEvidence, 0, len(evidenceTables))
	for _, name := range evidenceTables {
		if !strings.HasPrefix(name, "database://") {
			name = "database://primary/public/" + name
		}
		tables = append(tables, fixtureTable(name))
	}
	writeEvidence(t, root, tables)
	return root, []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, CredentialEnv: "TEST_DSN", ConnectTimeoutSeconds: 10, QueryTimeoutSeconds: 30, Enabled: true}}
}

func writeEvidence(t testing.TB, root string, tables []dbevidence.TableEvidence) {
	t.Helper()
	manifest := dbevidence.SourceManifest{Version: dbevidence.SourceManifestVersion, SourceID: "primary", Engine: dbevidence.EnginePostgreSQL, Database: "app", Namespaces: []string{"public"}, IncludeNamespaces: []string{}, ExcludeNamespaces: []string{}, IncludeTables: []string{}, ExcludeTables: []string{}, CaseSemantics: dbevidence.CaseSemantics{IdentifierCase: "preserve_quoted_fold_unquoted_lower"}, BusinessDataRead: false}
	snapshot, files, err := dbevidence.BuildSnapshot(manifest, tables)
	if err != nil {
		t.Fatal(err)
	}
	if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
		t.Fatal(err)
	}
}

func fixtureTable(ref string) dbevidence.TableEvidence {
	name := ref[strings.LastIndexByte(ref, '/')+1:]
	return dbevidence.TableEvidence{Version: dbevidence.EvidenceVersion, ObjectRef: ref, Engine: dbevidence.EnginePostgreSQL, SourceID: "primary", Database: "app", Namespace: "public", Name: name, Kind: "base_table", Columns: []dbevidence.Column{{Ordinal: 1, Name: "id", NativeType: "bigint", CanonicalType: "bigint", Nullable: false}}, PrimaryKey: &dbevidence.KeyConstraint{Name: name + "_pkey", Columns: []string{"id"}}, UniqueConstraints: []dbevidence.KeyConstraint{}, ForeignKeys: []dbevidence.ForeignKey{}, Checks: []dbevidence.CheckConstraint{}, Indexes: []dbevidence.Index{}}
}

func writeDatabaseVolume(t testing.TB, root string, tables []string) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString(cognition.DatabaseMarker + "\n")
	if len(tables) > 0 {
		builder.WriteString("===Primary tables/database://primary/public/===\n")
		for _, name := range tables {
			fmt.Fprintf(&builder, "%s[DB7S]: F:store %s business state | R:- | A:- | S:-\n", name, name)
		}
	}
	mustWrite(t, filepath.Join(root, "aoci.database.txt"), builder.String())
}

func loadFixtureSet(t testing.TB, root string) *cognition.Set {
	t.Helper()
	set, err := cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func itemByRef(t testing.TB, assessment Assessment, ref string) Item {
	t.Helper()
	for _, item := range assessment.Items {
		if item.ObjectRef == ref {
			return item
		}
	}
	t.Fatalf("missing item %s", ref)
	return Item{}
}

func mustWrite(t testing.TB, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
