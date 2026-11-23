package cli

import "testing"

// Every receipt kind the Guide tells an operator to close has its closing
// commands open under the recovery gate (audit G5-01: database-bootstrap had
// none, so an interrupted Database onboarding could not be resumed or rolled
// back from the CLI).
func TestRecoveryGateOpensDatabaseBootstrapClosures(t *testing.T) {
	for _, path := range []string{"aoci database cognition status", "aoci database cognition bootstrap resume", "aoci database cognition bootstrap rollback"} {
		if !recoveryGateAllows("database-bootstrap", path) {
			t.Errorf("%s must stay open while a Database bootstrap is pending", path)
		}
	}
	if recoveryGateAllows("database-bootstrap", "aoci scan") {
		t.Error("an unrelated write must stay closed")
	}
}

// The read-only viewer stays open under every pending receipt kind; it renders
// recovery_pending itself (audit G5-06).
func TestRecoveryGateKeepsViewerOpen(t *testing.T) {
	for _, operation := range []string{"bootstrap", "migration", "database-bootstrap", "reversal", "scope", "remove", "entries", "header"} {
		if !recoveryGateAllows(operation, "aoci ui") {
			t.Errorf("aoci ui is blocked while a %s receipt is pending", operation)
		}
	}
}
