/*
===========================================================================

diagnostic_refusal_metrics_test.go - DiagnosticRefusal counts by reason

#4222: a refused request that answers no wire bytes still has to name
its cause on /transport/metrics, or the next live report cannot.

===========================================================================
*/
package action

import "testing"

/*
================
TestDiagnosticRefusalCountersAccumulateByReason
================
*/
func TestDiagnosticRefusalCountersAccumulateByReason(t *testing.T) {
	rt := &Runtime{}
	rt.countDiagnosticRefusal("pet attack: target not found")
	rt.countDiagnosticRefusal("pet attack: target not found")
	rt.countDiagnosticRefusal("pet attack: not an attack pet")
	rt.countDiagnosticRefusal("")
	got := rt.DiagnosticRefusalCounters()
	if got["pet attack: target not found"] != 2 {
		t.Fatalf("target not found = %d, want 2", got["pet attack: target not found"])
	}
	if got["pet attack: not an attack pet"] != 1 {
		t.Fatalf("not an attack pet = %d, want 1", got["pet attack: not an attack pet"])
	}
	if _, present := got[""]; present {
		t.Fatal("an empty reason was counted")
	}
}
