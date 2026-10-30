package volumegovernance

import (
	"encoding/json"
	"strings"
	"testing"
)

// A Maintain response drops the four per-asset records and nothing else: the
// remaining keys are the complete Facts keys in the same order, and a response
// decoded back reaches every promoted field. The verify --json shape (Facts
// itself) still carries the records.
func TestMaintainTransportOmitsOnlyTheAssetRecords(t *testing.T) {
	facts := &Facts{Version: "volumes-governance-facts/v1", Layout: "volumes-v1", GovernanceAligned: false,
		CompositeIdentity: strings.Repeat("a", 64),
		Root:              AssetState{Enabled: true, Applicable: true, DomainState: "enabled", AssetState: "present", Path: "aoci.txt", SHA256: strings.Repeat("1", 64)},
		Meta:              AssetState{Enabled: true, Applicable: true, DomainState: "enabled", AssetState: "present", Path: "aoci.meta.txt", SHA256: strings.Repeat("2", 64)},
		Code:              AssetState{Enabled: true, Applicable: true, DomainState: "enabled", AssetState: "present", Path: "aoci.code.txt", SHA256: strings.Repeat("3", 64), ObjectCount: 7},
		Database:          AssetState{DomainState: "not_applicable", AssetState: "absent", Path: "aoci.database.txt"},
		CodeSourceCount:   9, CodeEntryCount: 7, Result: "authoring_required", NextRequiredAction: "authoring_required",
		Findings: []Finding{{Code: "code_missing", Domain: "code", Target: "a.go"}}}
	facts.CodeDrift.Missing = []string{"a.go", "b.go"}
	facts.Budget.R = append(facts.Budget.R, facts.Budget.R...)

	keysOf := func(data []byte) []string {
		var ordered map[string]json.RawMessage
		if err := json.Unmarshal(data, &ordered); err != nil {
			t.Fatal(err)
		}
		// Recover the order from the bytes: the decoder's map loses it.
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		keys := []string{}
		depth := 0
		for {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			switch value := token.(type) {
			case json.Delim:
				if value == '{' || value == '[' {
					depth++
				} else {
					depth--
				}
			case string:
				if depth == 1 && decoder.More() {
					keys = append(keys, value)
				}
			}
		}
		return keys
	}
	full, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(BoundForMaintainTransport(facts, 5))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range MaintainTransportOmittedKeys {
		if strings.Contains(string(wire), `"`+key+`":{`) {
			t.Fatalf("Maintain transport still carries %q: %s", key, wire)
		}
		if !strings.Contains(string(full), `"`+key+`":{`) {
			t.Fatalf("the complete Facts lost %q: %s", key, full)
		}
	}
	omitted := map[string]bool{}
	for _, key := range MaintainTransportOmittedKeys {
		omitted[key] = true
	}
	expected := []string{}
	for _, key := range keysOf(full) {
		if !omitted[key] {
			expected = append(expected, key)
		}
	}
	if actual := keysOf(wire); strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("the transport projection must keep every other key in Facts order:\n%v\n%v", actual, expected)
	}
	if len(wire) >= len(full) {
		t.Fatalf("the projection saved nothing: %d >= %d", len(wire), len(full))
	}

	var decoded TransportFacts
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Facts == nil || decoded.CodeSourceCount != 9 || decoded.Result != "authoring_required" ||
		len(decoded.Findings) != 1 || len(decoded.CodeDrift.Missing) != 2 || decoded.CompositeIdentity != facts.CompositeIdentity {
		t.Fatalf("promoted fields must decode from the wire: %+v", decoded.Facts)
	}
	if decoded.Code.SHA256 != "" || decoded.RootOmitted != nil {
		t.Fatal("the records are not on the wire, so neither the promoted nor the shadow field may read one back")
	}
	// In-process readers keep the records through the embedding.
	if BoundForMaintainTransport(facts, 5).Code.SHA256 != facts.Code.SHA256 {
		t.Fatal("Go readers must still reach Facts.Code through the projection")
	}
	if BoundForMaintainTransport(nil, 5) != nil {
		t.Fatal("nil facts project to nil")
	}
}
