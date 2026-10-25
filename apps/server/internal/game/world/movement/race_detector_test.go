//go:build race

/*
===========================================================================

race_detector_test.go - whether this test binary is race-instrumented

Wall-clock assertions in this package are skipped under the race
detector: its instrumentation prices nothing like the tick does, and
the deterministic work budgets carry the pin there instead.

===========================================================================
*/
package movement

const raceDetectorOn = true
