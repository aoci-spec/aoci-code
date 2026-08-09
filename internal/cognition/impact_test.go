package cognition

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/index"
)

// resolveImpactForTest assigns the direct unit-test candidate slice its
// explicit original order. Production batch paths must carry this identity
// before ResolveImpact is called.
func resolveImpactForTest(set *Set, candidates []ImpactCandidate) (AffectedCognitionSet, error) {
	indexed := append([]ImpactCandidate{}, candidates...)
	for index := range indexed {
		if indexed[index].OriginalCandidateIndex == 0 {
			indexed[index].OriginalCandidateIndex = index + 1
		}
	}
	return ResolveImpact(set, indexed)
}

func TestResolveImpactRequiresExplicitOriginalCandidateIndex(t *testing.T) {
	set, _ := loadImpactFixture(t, []string{codeImpactLine("service.go", "-")}, "")
	result, err := ResolveImpact(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go",
		CanonicalLine: codeImpactLine("service.go", "-"),
	}})
	if err == nil || len(result.Findings) != 0 {
		t.Fatalf("ResolveImpact accepted an implicit or repairable original candidate index: result=%#v err=%v", result, err)
	}
}

func TestResolveImpactSingleCodeObject(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_service.go", "-")},
		"",
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "code:src/user_service.go",
		CanonicalLine: "user_service.go[CD9S]: F:coordinate user account workflows | R:- | A:UserService | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet, "code:src/user_service.go")
	assertStrings(t, result.WriteSet, "code")
	assertStrings(t, result.GuardSet, "root", "meta", "code")
	if result.Strategy != ImpactStrategyDependencyClosure || result.Upgrade {
		t.Fatalf("unexpected strategy: %#v", result)
	}
}

func TestResolveImpactAllowsHighImportanceDashAndShortS(t *testing.T) {
	tests := []struct {
		name          string
		candidateLine string
	}{
		{
			name:          "S_dash_when_no_constraint_qualifies",
			candidateLine: "service.go[CD9S]: F:coordinate changed workflows | R:- | A:- | S:-",
		},
		{
			name:          "S_shorter_than_F",
			candidateLine: "service.go[CD9S]: F:coordinate changed workflows | R:- | A:- | S:x",
		},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			set, _ := loadImpactFixture(t, []string{codeImpactLine("service.go", "-")}, "")
			result, err := resolveImpactForTest(set, []ImpactCandidate{{
				Change:        ImpactChangeUpdate,
				ObjectRef:     "code:src/service.go",
				CanonicalLine: current.candidateLine,
			}})
			if err != nil {
				t.Fatalf("soft-S compatible C9 candidate was rejected: %v", err)
			}
			if len(result.Findings) != 0 {
				t.Fatalf("soft-S compatible C9 candidate produced findings: %#v", result.Findings)
			}
			assertImpactObjects(t, result.ReviewSet, "code:src/service.go")
		})
	}
}

func TestResolveImpactSingleDatabaseObject(t *testing.T) {
	set, _ := loadImpactFixture(t, nil, primaryDatabaseImpact(
		databaseImpactLine("users", "-"),
	))
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user account state | R:- | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet, "database://primary/public/users")
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "database")
}

func TestResolveImpactDatabaseForwardRelationDoesNotExpandWriteSet(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "code:src/user_repository.go")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user account state | R:code:src/user_repository.go | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/user_repository.go",
		"database://primary/public/users",
	)
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
	if !hasReviewReason(result.ReviewSet, "code:src/user_repository.go", "forward_relation") {
		t.Fatalf("code relation target lacks forward reason: %#v", result.ReviewSet)
	}
}

func TestResolveImpactCodeReverseRelation(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "database://primary/public/users")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user account state | R:- | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/user_repository.go",
		"database://primary/public/users",
	)
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
	if !hasReviewReason(result.ReviewSet, "code:src/user_repository.go", "reverse_relation") {
		t.Fatalf("incoming code relation lacks reverse reason: %#v", result.ReviewSet)
	}
}

func TestResolveImpactClosureReachesFixedPoint(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{
			codeImpactLine("user_repository.go", "database://primary/public/audit_events"),
			codeImpactLine("audit_service.go", "code:src/user_repository.go"),
		},
		primaryDatabaseImpact(
			databaseImpactLine("users", "code:src/user_repository.go"),
			databaseImpactLine("audit_events", "code:src/audit_service.go"),
		),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user account state | R:code:src/user_repository.go | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/audit_service.go",
		"code:src/user_repository.go",
		"database://primary/public/audit_events",
		"database://primary/public/users",
	)
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
}

func TestResolveImpactCrossVolumeFourHopClosure(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{
			codeImpactLine("user_repository.go", "code:src/user_service.go"),
			codeImpactLine("user_service.go", "code:src/users_api.go"),
			codeImpactLine("users_api.go", "-"),
		},
		primaryDatabaseImpact(databaseImpactLine("users", "code:src/user_repository.go")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user state | R:code:src/user_repository.go | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/user_repository.go",
		"code:src/user_service.go",
		"code:src/users_api.go",
		"database://primary/public/users",
	)
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
}

func TestResolveImpactLargeUnrelatedSetStaysBounded(t *testing.T) {
	set := largeImpactSet(t, 5000)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "code:src/file_00000.go",
		CanonicalLine: "file_00000.go[CD9S]: F:changed deterministic fixture object | R:- | A:- | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet, "code:src/file_00000.go")
	assertStrings(t, result.WriteSet, "code")
	assertStrings(t, result.GuardSet, "root", "meta", "code")
}

func TestResolveImpactMultipleCandidatesChooseWriteVolumes(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("service.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	candidates := []ImpactCandidate{
		{Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users", CanonicalLine: "users[DB9S]: F:store authoritative user state | R:- | A:- | S:-"},
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: "service.go[CD9S]: F:coordinate changed workflows | R:- | A:- | S:-"},
	}
	result, err := resolveImpactForTest(set, candidates)
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet, "code:src/service.go", "database://primary/public/users")
	assertStrings(t, result.WriteSet, "code", "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")

	reversed := []ImpactCandidate{candidates[1], candidates[0]}
	reversedResult, err := resolveImpactForTest(set, reversed)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := json.Marshal(result)
	right, _ := json.Marshal(reversedResult)
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("candidate order changed deterministic result:\n%s\n%s", left, right)
	}
}

func TestResolveImpactCrossVolumeCandidatesNeverImplySemanticWrites(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "database://primary/public/users")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	databaseOnly, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store changed authoritative user state | R:- | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, databaseOnly.ReviewSet,
		"code:src/user_repository.go",
		"database://primary/public/users",
	)
	assertStrings(t, databaseOnly.WriteSet, "database")
	assertStrings(t, databaseOnly.GuardSet, "root", "meta", "code", "database")

	both, err := resolveImpactForTest(set, []ImpactCandidate{
		{Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users", CanonicalLine: "users[DB9S]: F:store changed authoritative user state | R:- | A:UserRepository | S:-"},
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/user_repository.go", CanonicalLine: "user_repository.go[CD9S]: F:access changed user persistence | R:database://primary/public/users | A:UserRepository | S:-"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, both.WriteSet, "code", "database")
	assertStrings(t, both.GuardSet, "root", "meta", "code", "database")
}

func TestResolveImpactMetaChangeUpgradesFullSet(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("service.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{Change: ImpactChangeAsset, VolumeID: "meta"}})
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, result.WriteSet, "meta")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
	assertStrings(t, result.UpgradeReason, "meta_change")
	if result.Strategy != ImpactStrategyFullCognitionSet || !result.Upgrade {
		t.Fatalf("Meta change did not upgrade: %#v", result)
	}
}

func TestResolveImpactRootChangeUpgradesFullSet(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("service.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{Change: ImpactChangeAsset, VolumeID: "root"}})
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, result.WriteSet, "root")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
	assertStrings(t, result.UpgradeReason, "root_change")
	if result.Strategy != ImpactStrategyFullCognitionSet || !result.Upgrade {
		t.Fatalf("Root change did not upgrade: %#v", result)
	}
}

func TestResolveImpactStructuralChangesUpgradeFullSet(t *testing.T) {
	tests := []struct {
		name      string
		candidate ImpactCandidate
		write     []string
		reason    string
	}{
		{"volume create", ImpactCandidate{Change: ImpactChangeVolumeCreate, VolumeID: "database"}, []string{"root", "database"}, ImpactChangeVolumeCreate},
		{"volume delete", ImpactCandidate{Change: ImpactChangeVolumeDelete, VolumeID: "code"}, []string{"root", "code"}, ImpactChangeVolumeDelete},
		{"layout", ImpactCandidate{Change: ImpactChangeLayout}, []string{"root"}, ImpactChangeLayout},
		{"migration", ImpactCandidate{Change: ImpactChangeMigration}, []string{"root"}, ImpactChangeMigration},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codeLines := []string{codeImpactLine("service.go", "-")}
			database := primaryDatabaseImpact(databaseImpactLine("users", "-"))
			if test.candidate.Change == ImpactChangeVolumeCreate {
				database = ""
			}
			set, _ := loadImpactFixture(t, codeLines, database)
			result, err := resolveImpactForTest(set, []ImpactCandidate{test.candidate})
			if err != nil {
				t.Fatal(err)
			}
			assertStrings(t, result.WriteSet, test.write...)
			if !result.Upgrade || result.Strategy != ImpactStrategyFullCognitionSet || !containsString(result.UpgradeReason, test.reason) {
				t.Fatalf("structural change did not upgrade: %#v", result)
			}
		})
	}
}

func TestResolveImpactAmbiguousRelationFailsClosed(t *testing.T) {
	database := primaryDatabaseImpact(databaseImpactLine("users", "-")) +
		"===Warehouse public tables/database://warehouse/public/===\n" + databaseImpactLine("users", "-") + "\n"
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("service.go", "users")},
		database,
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go",
		CanonicalLine: "service.go[CD9S]: F:coordinate user workflows | R:users | A:UserService | S:-",
	}})
	if err == nil {
		t.Fatal("ambiguous relation was accepted")
	}
	if !hasImpactFinding(result.Findings, "impact_relation_ambiguous") {
		t.Fatalf("missing ambiguity finding: %#v", result.Findings)
	}
	if len(result.WriteSet) != 0 || len(result.GuardSet) != 0 {
		t.Fatalf("failed analysis returned authoritative sets: %#v", result)
	}
}

func TestResolveImpactInvalidOrUnresolvedRelationFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name, relation, finding string
	}{
		{"unresolved", "code:src/missing.go", "impact_relation_unresolved"},
		{"empty item", "code:src/repository.go,,database://primary/public/users", "impact_relation_invalid"},
		{"mixed placeholder", "-,code:src/repository.go", "impact_relation_invalid"},
		{"full width separator", "code:src/repository.go，database://primary/public/users", "impact_relation_invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			set, _ := loadImpactFixture(t,
				[]string{
					codeImpactLine("service.go", test.relation),
					codeImpactLine("repository.go", "-"),
				},
				primaryDatabaseImpact(databaseImpactLine("users", "-")),
			)
			candidateLine := "service.go[CD9S]: F:coordinate user workflows | R:" + test.relation + " | A:UserService | S:-"
			result, err := resolveImpactForTest(set, []ImpactCandidate{{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: candidateLine}})
			if err == nil || !hasImpactFinding(result.Findings, test.finding) {
				t.Fatalf("relation was not rejected: err=%v findings=%#v", err, result.Findings)
			}
		})
	}
}

func TestResolveImpactRejectsDuplicateObjectCandidates(t *testing.T) {
	set, _ := loadImpactFixture(t, []string{codeImpactLine("service.go", "-")}, "")
	result, err := resolveImpactForTest(set, []ImpactCandidate{
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: "service.go[CD9S]: F:first candidate | R:- | A:- | S:-"},
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: "service.go[CD9S]: F:second candidate | R:- | A:- | S:-"},
	})
	if err == nil || !hasImpactFinding(result.Findings, "impact_candidate_duplicate") {
		t.Fatalf("duplicate object candidates were accepted: err=%v result=%#v", err, result)
	}
}

func TestResolveImpactUsesNumericCandidateFindingOrder(t *testing.T) {
	set, _ := loadImpactFixture(t, []string{codeImpactLine("service.go", "-")}, "")
	line := "service.go[CD9S]: F:duplicate candidate | R:- | A:- | S:-"
	result, err := ResolveImpact(set, []ImpactCandidate{
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: line, OriginalCandidateIndex: 1},
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: line, OriginalCandidateIndex: 10},
		{Change: ImpactChangeUpdate, ObjectRef: "code:src/service.go", CanonicalLine: line, OriginalCandidateIndex: 2},
	})
	if err == nil || len(result.Findings) != 4 {
		t.Fatalf("duplicate candidates did not produce the expected Findings: err=%v findings=%+v", err, result.Findings)
	}
	want := []int{2, 2, 10, 10}
	for index, finding := range result.Findings {
		if finding.CandidateIndex != want[index] {
			t.Fatalf("Impact Finding %d was not numerically ordered: %+v", index, result.Findings)
		}
	}
}

func TestResolveImpactDeleteIncludesIncomingReference(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "database://primary/public/users")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeDelete, ObjectRef: "database://primary/public/users",
	}})
	if err == nil {
		t.Fatal("delete leaving a post-state dangling relation was accepted")
	}
	if !hasImpactFinding(result.Findings, "impact_relation_unresolved") {
		t.Fatalf("missing dangling relation finding: %#v", result.Findings)
	}
}

func TestResolveImpactDeleteRetainsPreimageRelationsInReview(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "code:src/user_repository.go")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeDelete, ObjectRef: "database://primary/public/users",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/user_repository.go",
		"database://primary/public/users",
	)
	if !hasReviewReason(result.ReviewSet, "code:src/user_repository.go", "forward_relation") {
		t.Fatalf("pre-delete relation was lost: %#v", result.ReviewSet)
	}
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
}

func TestResolveImpactRenameUpgradesAndRetainsBothIdentities(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "code:src/user_repository.go")),
	)
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change:            ImpactChangeRename,
		PreviousObjectRef: "database://primary/public/users",
		ObjectRef:         "database://primary/public/accounts",
		CanonicalLine:     "accounts[DB9S]: F:store authoritative account state | R:code:src/user_repository.go | A:UserRepository | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertImpactObjects(t, result.ReviewSet,
		"code:src/user_repository.go",
		"database://primary/public/accounts",
		"database://primary/public/users",
	)
	assertStrings(t, result.WriteSet, "database")
	assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
	if !result.Upgrade || result.Strategy != ImpactStrategyFullCognitionSet || !containsString(result.UpgradeReason, "identity_change") {
		t.Fatalf("rename did not upgrade protection: %#v", result)
	}
}

func TestResolveImpactProjectedCognitionValidation(t *testing.T) {
	set, _ := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "-")),
	)
	tests := []struct {
		name      string
		candidate ImpactCandidate
		finding   string
	}{
		{
			name: "canonical identity",
			candidate: ImpactCandidate{Change: ImpactChangeUpdate, ObjectRef: "code:src/user_repository.go",
				CanonicalLine: "other.go[CD9S]: F:access user persistence | R:- | A:- | S:-"},
			finding: "impact_object_identity_invalid",
		},
		{
			name: "volume type",
			candidate: ImpactCandidate{Change: ImpactChangeUpdate, ObjectRef: "code:src/user_repository.go", VolumeID: "database",
				CanonicalLine: "user_repository.go[CD9S]: F:access user persistence | R:- | A:- | S:-"},
			finding: "impact_candidate_volume_mismatch",
		},
		{
			name: "FRAS structure",
			candidate: ImpactCandidate{Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
				CanonicalLine: "users[DB9S]: F:store user state | R:- | A:-"},
			finding: "impact_candidate_fras_invalid",
		},
		{
			name: "Meta tag dictionary",
			candidate: ImpactCandidate{Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
				CanonicalLine: "users[DC9S]: F:store user state | R:- | A:- | S:-"},
			finding: "impact_candidate_tag_dictionary_violation",
		},
		{
			name: "relation existence",
			candidate: ImpactCandidate{Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
				CanonicalLine: "users[DB9S]: F:store user state | R:code:src/missing.go | A:- | S:-"},
			finding: "impact_relation_unresolved",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := resolveImpactForTest(set, []ImpactCandidate{test.candidate})
			if err == nil || !hasImpactFinding(result.Findings, test.finding) {
				t.Fatalf("projected state violation was accepted: err=%v result=%#v", err, result)
			}
			if len(result.WriteSet) != 0 || len(result.GuardSet) != 0 {
				t.Fatalf("failed projected state returned authoritative sets: %#v", result)
			}
		})
	}
}

func TestResolveImpactDeterministicJSONAndReadOnlyInput(t *testing.T) {
	set, root := loadImpactFixture(t,
		[]string{codeImpactLine("user_repository.go", "-")},
		primaryDatabaseImpact(databaseImpactLine("users", "code:src/user_repository.go")),
	)
	before := readImpactFixtureFiles(t, root)
	candidates := []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/users",
		CanonicalLine: "users[DB9S]: F:store authoritative user account state | R:code:src/user_repository.go | A:UserRepository | S:-",
	}}
	var canonical []byte
	for iteration := 0; iteration < 100; iteration++ {
		result, err := resolveImpactForTest(set, candidates)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 0 {
			canonical = encoded
		} else if !reflect.DeepEqual(canonical, encoded) {
			t.Fatalf("same input produced different output:\n%s\n%s", canonical, encoded)
		}
	}
	after := readImpactFixtureFiles(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only Impact Resolver changed fixture files")
	}
}

func TestResolveImpactTenThousandMixedObjectsIsDeterministic(t *testing.T) {
	set := largeImpactSet(t, 5000)
	setImpactObjectRelation(t, set, "code", "code:src/file_00000.go", "database://primary/public/table_00000")
	candidates := []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/table_00000",
		CanonicalLine: "table_00000[DB9S]: F:store changed deterministic fixture state | R:- | A:- | S:-",
	}}
	var canonical []byte
	started := time.Now()
	const iterations = 10
	for iteration := 0; iteration < iterations; iteration++ {
		result, err := resolveImpactForTest(set, candidates)
		if err != nil {
			t.Fatal(err)
		}
		assertImpactObjects(t, result.ReviewSet,
			"code:src/file_00000.go",
			"database://primary/public/table_00000",
		)
		assertStrings(t, result.WriteSet, "database")
		assertStrings(t, result.GuardSet, "root", "meta", "code", "database")
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 0 {
			canonical = encoded
		} else if !reflect.DeepEqual(canonical, encoded) {
			t.Fatalf("10,000-object fixture produced non-deterministic output:\n%s\n%s", canonical, encoded)
		}
	}
	t.Logf("10,000-object mixed fixture: %d deterministic resolutions in %s (average %s)", iterations, time.Since(started), time.Since(started)/iterations)
}

func TestResolveImpactLegacyIsSingleAssetAdapter(t *testing.T) {
	entry, _ := index.ParseEntryLine("service.go[CD9S]: F:coordinate workflows | R:- | A:Service | S:-", 1)
	object := Object{VolumeID: "legacy", Kind: "code", Name: "service.go", CanonicalRef: "src/service.go", Entry: entry, CanonicalLine: entry.FullLine}
	set := &Set{LayoutMode: LayoutLegacyMonolithic, LayoutVersion: "1", Root: Asset{
		Descriptor: Descriptor{ID: "legacy", Kind: "monolithic", Path: "aoci.txt"}, State: AssetPresent, Objects: []Object{object}, ObjectCount: 1,
	}, Volumes: map[string]*Asset{}}
	result, err := resolveImpactForTest(set, []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "src/service.go",
		CanonicalLine: "service.go[CD9S]: F:coordinate changed workflows | R:- | A:Service | S:-",
	}})
	if err != nil {
		t.Fatal(err)
	}
	assertStrings(t, result.WriteSet, "legacy")
	assertStrings(t, result.GuardSet, "legacy")
	if result.Strategy != ImpactStrategyLegacyMonolithic {
		t.Fatalf("unexpected Legacy strategy: %#v", result)
	}
}

func BenchmarkResolveImpactLargeIndependentSet(b *testing.B) {
	set := largeImpactSet(b, 5000)
	candidates := []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "code:src/file_00000.go",
		CanonicalLine: "file_00000.go[CD9S]: F:changed deterministic fixture object | R:- | A:- | S:-",
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := resolveImpactForTest(set, candidates); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolveImpactTenThousandMixedSet(b *testing.B) {
	set := largeImpactSet(b, 5000)
	setImpactObjectRelation(b, set, "code", "code:src/file_00000.go", "database://primary/public/table_00000")
	candidates := []ImpactCandidate{{
		Change: ImpactChangeUpdate, ObjectRef: "database://primary/public/table_00000",
		CanonicalLine: "table_00000[DB9S]: F:store changed deterministic fixture state | R:- | A:- | S:-",
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if _, err := resolveImpactForTest(set, candidates); err != nil {
			b.Fatal(err)
		}
	}
}

func loadImpactFixture(t *testing.T, codeLines []string, databaseBody string) (*Set, string) {
	t.Helper()
	root := t.TempDir()
	ids := []string{"meta"}
	if len(codeLines) > 0 {
		ids = append(ids, "code")
	}
	if databaseBody != "" {
		ids = append(ids, "database")
	}
	files := map[string]string{"aoci.txt": rootText(ids...), "aoci.meta.txt": validMeta()}
	if len(codeLines) > 0 {
		files["aoci.code.txt"] = CodeVolumeMarker + "\n===Code fixture" + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + strings.Join(codeLines, "\n") + "\n"
	}
	if databaseBody != "" {
		files["aoci.database.txt"] = DatabaseMarker + "\n" + databaseBody
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set, err := Load(root, "aoci.txt")
	if err != nil {
		t.Fatalf("load impact fixture: %v", err)
	}
	return set, root
}

func codeImpactLine(name, relation string) string {
	return fmt.Sprintf("%s[CD9S]: F:coordinate the deterministic fixture | R:%s | A:- | S:-", name, relation)
}

func databaseImpactLine(name, relation string) string {
	return fmt.Sprintf("%s[DB9S]: F:store the deterministic fixture state | R:%s | A:- | S:-", name, relation)
}

func primaryDatabaseImpact(lines ...string) string {
	return "===Primary public tables/database://primary/public/===\n" + strings.Join(lines, "\n") + "\n"
}

func largeImpactSet(tb testing.TB, perVolume int) *Set {
	tb.Helper()
	set := &Set{
		LayoutMode: LayoutVolumesV1, LayoutVersion: "1",
		Root: Asset{Descriptor: Descriptor{ID: "root", Kind: "root", Path: "aoci.txt"}, State: AssetPresent},
		Meta: Asset{Descriptor: canonicalDescriptors["meta"], State: AssetPresent, Raw: []byte(validMeta())},
		Volumes: map[string]*Asset{
			"meta":     {Descriptor: canonicalDescriptors["meta"], State: AssetPresent},
			"code":     {Descriptor: canonicalDescriptors["code"], State: AssetPresent},
			"database": {Descriptor: canonicalDescriptors["database"], State: AssetPresent},
		},
		DeclaredOrder: []string{"meta", "code", "database"},
	}
	for objectIndex := 0; objectIndex < perVolume; objectIndex++ {
		codeName := fmt.Sprintf("file_%05d.go", objectIndex)
		codeEntry, _ := index.ParseEntryLine(codeImpactLine(codeName, "-"), objectIndex+1)
		set.Volumes["code"].Objects = append(set.Volumes["code"].Objects, Object{
			VolumeID: "code", Kind: "file", Name: codeName, CanonicalRef: "code:src/" + codeName, Entry: codeEntry, CanonicalLine: codeEntry.FullLine,
		})
		tableName := fmt.Sprintf("table_%05d", objectIndex)
		databaseEntry, _ := index.ParseEntryLine(databaseImpactLine(tableName, "-"), objectIndex+1)
		set.Volumes["database"].Objects = append(set.Volumes["database"].Objects, Object{
			VolumeID: "database", Kind: "table", Name: tableName, Namespace: "database://primary/public",
			CanonicalRef: "database://primary/public/" + tableName, Entry: databaseEntry, CanonicalLine: databaseEntry.FullLine,
		})
	}
	return set
}

func setImpactObjectRelation(tb testing.TB, set *Set, volumeID, objectRef, relation string) {
	tb.Helper()
	asset := set.Volumes[volumeID]
	for objectIndex := range asset.Objects {
		object := &asset.Objects[objectIndex]
		if object.CanonicalRef != objectRef {
			continue
		}
		var line string
		if volumeID == "code" {
			line = codeImpactLine(object.Name, relation)
		} else {
			line = databaseImpactLine(object.Name, relation)
		}
		entry, ok := index.ParseEntryLine(line, objectIndex+1)
		if !ok {
			tb.Fatal("cannot parse mutated impact fixture object")
		}
		object.Entry = entry
		object.CanonicalLine = entry.FullLine
		return
	}
	tb.Fatalf("impact fixture object %s was not found", objectRef)
}

func assertImpactObjects(t *testing.T, objects []ImpactReviewObject, expected ...string) {
	t.Helper()
	actual := make([]string, 0, len(objects))
	for _, object := range objects {
		actual = append(actual, object.Object)
	}
	sort.Strings(actual)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("review objects:\nwant %v\ngot  %v", want, actual)
	}
}

func assertStrings(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("values:\nwant %v\ngot  %v", expected, actual)
	}
}

func hasReviewReason(objects []ImpactReviewObject, ref, reason string) bool {
	for _, object := range objects {
		if object.Object == ref {
			for _, candidate := range object.Reasons {
				if candidate == reason {
					return true
				}
			}
		}
	}
	return false
}

func hasImpactFinding(findings []ImpactFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readImpactFixtureFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		result[entry.Name()] = string(content)
	}
	return result
}
