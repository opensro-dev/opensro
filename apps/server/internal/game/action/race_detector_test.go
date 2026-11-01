//go:build race

/*
===========================================================================

race_detector_test.go - whether this test binary is race-instrumented

The tick allocation guard is skipped under the race detector: its
instrumentation allocates on its own (the synthetic tick measured about
45% more bytes), so the budget pins the plain build only.

===========================================================================
*/
package action

const raceDetectorOn = true
