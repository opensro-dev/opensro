/*
===========================================================================

session_ready_hook_test.go - scene-ready hooks run once, in order

FinishSceneReentry is the moment every owner holding work from the
loading window relies on: its hooks must fire exactly once, in the
order registered, and never again for a scene already finished.

===========================================================================
*/
package transport

import "testing"

/*
================
TestFinishSceneReentryRunsSceneReadyHooks
================
*/
func TestFinishSceneReentryRunsSceneReadyHooks(t *testing.T) {
	t.Parallel()
	s := &Session{}
	s.BeginSceneAdmission()
	var ran []int
	s.OnSceneReady(func() { ran = append(ran, 1) })
	s.OnSceneReady(func() { ran = append(ran, 2) })
	if !s.FinishSceneReentry() {
		t.Fatal("reentry did not finish a loading scene")
	}
	if len(ran) != 2 || ran[0] != 1 || ran[1] != 2 {
		t.Fatalf("hooks ran %v, want [1 2]", ran)
	}
	if s.FinishSceneReentry() {
		t.Fatal("a finished scene finished twice")
	}
	if len(ran) != 2 {
		t.Fatalf("consumed hooks ran again: %v", ran)
	}
}
