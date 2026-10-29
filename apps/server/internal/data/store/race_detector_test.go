//go:build race

/*
===========================================================================

race_detector_test.go - whether this test binary is race-instrumented

The allocation pins are skipped under the race detector: its
instrumentation allocates on its own (the borrowed encode measured 3
allocations per call against Marshal's 2 in CI's race shard), so the
pins hold for the plain build only.

===========================================================================
*/
package store

const raceDetectorOn = true
