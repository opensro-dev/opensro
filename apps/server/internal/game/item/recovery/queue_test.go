/*
===========================================================================

queue_test.go - native potion pulse admission, ordering and trimming

Fixed vectors cover the asymmetric full-gauge rule, integer pulse sizing
and front-only queue advancement. They do not inspect implementation text.

===========================================================================
*/

package recovery

import "testing"

/*
================
TestPotionQueueUsesFiveIntegerPulses
================
*/
func TestPotionQueueUsesFiveIntegerPulses(t *testing.T) {
	for _, tc := range []struct{ amount, step int64 }{{1, 1}, {4, 1}, {125, 25}, {129, 25}, {10_000_000, 1_000_000}} {
		var q Queue
		first := q.Admit(Admission{Maximum: Amount{100_000_000, 100_000_000}, Credit: Amount{tc.amount, tc.amount}, Absolute: true})
		if first != (Amount{tc.step, tc.step}) {
			t.Fatalf("amount %d first %+v", tc.amount, first)
		}
		for range 4 {
			if got := q.Tick(); got != first {
				t.Fatalf("remaining pulse %+v want %+v", got, first)
			}
		}
		if !q.Empty() || q.Tick() != (Amount{}) {
			t.Fatal("completed potion retained recovery")
		}
	}
}

/*
================
TestPotionQueueFullGaugeAndFrontOnlyTick
================
*/
func TestPotionQueueFullGaugeAndFrontOnlyTick(t *testing.T) {
	var q Queue
	first := q.Admit(Admission{Current: Amount{100, 100}, Maximum: Amount{100, 100}, Credit: Amount{100, 100}, Absolute: true})
	if first != (Amount{20, 0}) {
		t.Fatalf("full gauge admission %+v", first)
	}
	q = Queue{}
	q.Admit(Admission{Maximum: Amount{1000, 1000}, Credit: Amount{100, 200}, Absolute: true})
	q.Admit(Admission{Maximum: Amount{1000, 1000}, Credit: Amount{200, 100}, Absolute: true})
	for range 4 {
		if got := q.Tick(); got != (Amount{20, 40}) {
			t.Fatalf("first queued potion %+v", got)
		}
	}
	for range 4 {
		if got := q.Tick(); got != (Amount{40, 20}) {
			t.Fatalf("second queued potion %+v", got)
		}
	}
}

/*
================
TestInstantPotionTrimsOnlyFollowingOverflowEntries
================
*/
func TestInstantPotionTrimsOnlyFollowingOverflowEntries(t *testing.T) {
	var q Queue
	for range 3 {
		q.Admit(Admission{Maximum: Amount{1000, 1000}, Credit: Amount{100, 100}, Absolute: true})
	}
	q.Admit(Admission{Maximum: Amount{1000, 1000}, Credit: Amount{950, 1001}})
	for range 4 {
		if got := q.Tick(); got != (Amount{20, 0}) {
			t.Fatalf("trimmed pulse %+v", got)
		}
	}
	if !q.Empty() {
		t.Fatal("percentage potion retained overflow successors")
	}
}
