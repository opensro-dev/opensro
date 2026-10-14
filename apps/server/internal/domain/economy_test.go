/*
===========================================================================

economy_test.go - every reason the door tables produce has a kind

===========================================================================
*/
package domain

import "testing"

/*
================
TestEveryDoorReasonHasAKind

A reason the endpoint cannot classify would drop out of the mint and burn
totals; unattributed alone is unknown.
================
*/
func TestEveryDoorReasonHasAKind(t *testing.T) {
	for door, reasons := range economyDoorReasons {
		for _, reason := range []string{reasons.gain, reasons.loss} {
			if reason != "" && EconomyReasonKind(reason) == EconomyKindUnknown {
				t.Fatalf("door %s reason %s has no kind", door, reason)
			}
		}
	}
	for door, reasons := range economyMemberReasons {
		for _, reason := range []string{reasons.subject, reasons.member} {
			if EconomyReasonKind(reason) == EconomyKindUnknown {
				t.Fatalf("settlement door %s reason %s has no kind", door, reason)
			}
		}
	}
	if EconomyReasonKind(EconomyReasonUnattributed) != EconomyKindUnknown {
		t.Fatal("unattributed must stay unknown")
	}
}
