package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Since 0.2.0 a Legacy single-file layout is read-only: every write tool
// answers legacy_read_only_migrate_required and names the migration, while
// the reads keep working. Each face is pinned here so a Host can rely on the
// one token.
func TestLegacyLayoutIsReadOnlyOnEveryWriteFace(t *testing.T) {
	root := buildRepo(t)

	maintain := handleMaintainInput(root, "test", maintainIn{}, newCognitionRefreshSession())
	text := resText(t, maintain)
	for _, token := range []string{
		`"status":"` + machinecontract.AutoStatusStopped + `"`,
		machinecontract.StatusLegacyReadOnlyMigrateRequired + ": ",
		`"next_action":"` + machinecontract.MaintainActionMigrateLegacyRepository + `"`,
		machinecontract.RemediationCommandCognitionOnboardStart,
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("Legacy maintain must answer %q:\n%s", token, text)
		}
	}
	if strings.Contains(text, "candidates:") && strings.Contains(text, "candidate_id") {
		t.Fatalf("Legacy maintain issued a candidate:\n%s", text)
	}
	all := handleMaintainInput(root, "test", maintainIn{Scope: "all"}, newCognitionRefreshSession())
	if !strings.Contains(resText(t, all), machinecontract.StatusLegacyReadOnlyMigrateRequired) {
		t.Fatalf("scope=all on Legacy must answer the same token:\n%s", resText(t, all))
	}

	_, fail := ApplyUpdateEntriesAtomic(root, []AtomicUpdateItem{{
		Path: "a.go", NewEntry: "a.go[XUT5T]: F:新职责 | R:- | A:- | S:-", SourceSHA256: strings.Repeat("0", 64),
	}}, ledger.SourceAgent, false)
	if fail == nil || fail.Code != machinecontract.StatusLegacyReadOnlyMigrateRequired || fail.Hint == "" {
		t.Fatalf("Legacy update must be refused with the read-only token and a migration hint: %+v", fail)
	}

	_, fail = ApplyRemoveEntry(root, "a.go", ledger.SourceAgent, true, false)
	if fail == nil || fail.Code != machinecontract.StatusLegacyReadOnlyMigrateRequired {
		t.Fatalf("Legacy remove must be refused with the read-only token: %+v", fail)
	}

	report := handleReport(root, reportIn{Path: "a.go", Note: "needs a look"})
	if !report.IsError || !strings.Contains(resText(t, report), machinecontract.StatusLegacyReadOnlyMigrateRequired) {
		t.Fatalf("Legacy report must be refused with the read-only token:\n%s", resText(t, report))
	}

	// Reads are untouched: the Legacy header still answers.
	if _, headerFail := BuildHeaderText(root, ledger.SourceAgent); headerFail != nil {
		t.Fatalf("Legacy header read must keep working: %+v", headerFail)
	}
}
