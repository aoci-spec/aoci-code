package machinecontract

import "testing"

func TestObjectFRASV2LimitsSingleMachineAuthority(t *testing.T) {
	limits := ObjectFRASV2Limits()
	if limits.FMaxRunes != ObjectFRASV2FMaxRunes ||
		limits.RMaxRunes != ObjectFRASV2RMaxRunes ||
		limits.RMaxItems != ObjectFRASV2RMaxItems ||
		limits.AMaxRunes != ObjectFRASV2AMaxRunes ||
		limits.AMaxItems != ObjectFRASV2AMaxItems {
		t.Fatalf("unexpected FRAS v2 density contract: %+v", limits)
	}
	copy := limits
	copy.FMaxRunes = 1
	if ObjectFRASV2Limits().FMaxRunes != ObjectFRASV2FMaxRunes {
		t.Fatal("callers can mutate FRAS v2 machine authority")
	}
}
