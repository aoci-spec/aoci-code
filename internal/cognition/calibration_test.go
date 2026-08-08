package cognition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestR67B2CalibrationEvidenceSupportsMachineLimits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "volumes", "r67-b2-code-fras-statistics.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		FormalEntryCount int                               `json:"formal_entry_count"`
		Fields           map[string]struct{ P99, Max int } `json:"fields"`
		HypotheticalHits struct {
			FCharacters     int `json:"F_characters"`
			RCharacters     int `json:"R_characters"`
			RItems          int `json:"R_items"`
			ACharacters     int `json:"A_characters"`
			AItems          int `json:"A_items"`
			DistinctEntries int `json:"distinct_entries"`
		} `json:"hypothetical_v2_limit_hits"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.FormalEntryCount != 875 {
		t.Fatalf("calibration population changed: %d", snapshot.FormalEntryCount)
	}
	limits := machinecontract.ObjectFRASV2Limits()
	if limits.FMaxRunes < snapshot.Fields["F_characters"].Max {
		t.Fatal("F limit rejects the entire calibration population")
	}
	if limits.RMaxRunes < snapshot.Fields["R_characters"].P99 || limits.RMaxItems < snapshot.Fields["R_items"].P99 {
		t.Fatal("R limits fall below calibration p99")
	}
	if limits.AMaxRunes < snapshot.Fields["A_characters"].P99 || limits.AMaxItems < snapshot.Fields["A_items"].P99 {
		t.Fatal("A limits fall below calibration p99")
	}
	hits := snapshot.HypotheticalHits
	if hits.FCharacters != 0 || hits.RCharacters != 4 || hits.RItems != 8 || hits.ACharacters != 6 || hits.AItems != 1 || hits.DistinctEntries != 15 {
		t.Fatalf("calibration impact record changed: %+v", hits)
	}
}
