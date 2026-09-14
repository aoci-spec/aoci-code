package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
)

// A binding defect names its field and rule in the vocabulary of the
// candidate's own domain: a database candidate binds through the top-level
// batch_id and must not be told to copy anything from code_plan.
func TestCandidateBindingFailSpeaksTheCandidatesDomain(t *testing.T) {
	database := normalizedAtomicItem{objectRef: "database://primary/public/users", originalCandidateIndex: 3}
	code := normalizedAtomicItem{rel: "pkg/a.go", originalCandidateIndex: 2}
	for _, test := range []struct {
		name     string
		item     normalizedAtomicItem
		kind     string
		field    string
		rule     string
		domain   string
		identity string
	}{
		{"database candidate_id", database, "candidate_id", "candidate_id", "database_candidate_id_mismatch", cognition.ScopeDatabase, database.objectRef},
		{"database batch_id", database, "batch_id", "batch_id", "database_candidate_batch_id_mismatch", cognition.ScopeDatabase, database.objectRef},
		{"code batch_id", code, "batch_id", "code_batch_id", "code_candidate_batch_id_mismatch", cognition.ScopeCode, "code:pkg/a.go"},
		{"code source_sha256", code, "source_sha256", "source_sha256", "code_candidate_source_sha256_mismatch", cognition.ScopeCode, "code:pkg/a.go"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fail := candidateBindingFail("candidate_batch_fields_invalid", test.item, test.kind, "issued", "submitted")
			if fail.Code != errBadArgs || !fail.Repairable || len(fail.Findings) != 1 {
				t.Fatalf("expected one repairable bad_args finding: %#v", fail)
			}
			finding := fail.Findings[0]
			if finding.CandidateIndex != test.item.originalCandidateIndex || finding.Domain != test.domain ||
				finding.Field != test.field || finding.RuleCode != test.rule || finding.CanonicalObjectIdentity != test.identity ||
				finding.SafeRepairAction == "" {
				t.Fatalf("finding = %#v", finding)
			}
			if test.domain == cognition.ScopeDatabase && strings.Contains(finding.SafeRepairAction, "code_plan") {
				t.Fatalf("a database finding must not point at code_plan: %q", finding.SafeRepairAction)
			}
		})
	}
}
