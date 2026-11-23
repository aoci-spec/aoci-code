package baseline

import (
	"testing"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

// An exact high-risk opt-in is not excluded from drift, as Safe Inventory
// admits it; any other sensitive name still is (audit G1-07).
func TestIsExcludedRelHonoursExactHighRiskOptIn(t *testing.T) {
	options := afs.WalkOptions{HighRiskOptIn: []string{"config/.env"}}
	if isExcludedRel("config/.env", options) {
		t.Fatal("an opted-in sensitive path must stay visible to drift")
	}
	if !isExcludedRel("other/.env", options) {
		t.Fatal("a sensitive path that was not opted in stays excluded")
	}
}
