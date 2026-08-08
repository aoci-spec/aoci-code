package counter

import "testing"

func TestIncrement(t *testing.T) {
	var value Counter
	value.Increment()
	if got := value.Value(); got != 1 {
		t.Fatalf("Value() = %d, want 1", got)
	}
}
