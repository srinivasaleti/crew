package agent

import "testing"

// TestHoldAwake checks caffeinate starts with the first turn and stops only after the last.
func TestHoldAwake(t *testing.T) {
	a, b := holdAwake(), holdAwake()
	a()
	a() // a second release of the same turn changes nothing
	if awake.n != 1 {
		t.Fatalf("n = %d after one of two released, want 1", awake.n)
	}
	b()
	if awake.n != 0 || awake.stop != nil {
		t.Fatalf("n = %d, stop set = %v after both released", awake.n, awake.stop != nil)
	}
}
