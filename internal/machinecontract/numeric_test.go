package machinecontract

import "testing"

func TestSQuotaMidBandIsFiveHundredRunes(t *testing.T) {
	if SQuotaMidRunes != 500 || DefaultSQuotaForC(4) != 500 || DefaultSQuotaForC(7) != 500 {
		t.Fatalf("unexpected C7-C4 S quota: constant=%d C4=%d C7=%d", SQuotaMidRunes, DefaultSQuotaForC(4), DefaultSQuotaForC(7))
	}
}

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
