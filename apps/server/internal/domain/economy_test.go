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

/*
================
TestOperatorAdjustReasonFollowsTheSign

The operator's correction (#3309) names one reason for both directions;
a credit is a mint and a debit a burn. Every other reason keeps its one
kind in both directions.
================
*/
func TestOperatorAdjustReasonFollowsTheSign(t *testing.T) {
	if got := EconomyReason("operator-adjust-gold", true); got != "operator_adjust" {
		t.Fatalf("credit reason %q", got)
	}
	if got := EconomyReason("operator-adjust-gold", false); got != "operator_adjust" {
		t.Fatalf("debit reason %q", got)
	}
	if EconomyReasonKindAt("operator_adjust", true) != EconomyKindMint ||
		EconomyReasonKindAt("operator_adjust", false) != EconomyKindBurn {
		t.Fatal("operator_adjust must mint on a credit and burn on a debit")
	}
	if EconomyReasonKindAt("monster_gold", false) != EconomyKindMint ||
		EconomyReasonKindAt("repair", true) != EconomyKindBurn {
		t.Fatal("a one-kind reason changed kind with direction")
	}
}
