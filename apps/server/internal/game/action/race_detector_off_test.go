//go:build !race

/*
===========================================================================

race_detector_off_test.go - whether this test binary is race-instrumented

The non-instrumented half of race_detector_test.go.

===========================================================================
*/
package action

const raceDetectorOn = false
