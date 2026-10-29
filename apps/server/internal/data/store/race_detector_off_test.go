//go:build !race

/*
===========================================================================

race_detector_off_test.go - the plain build's side of raceDetectorOn

See race_detector_test.go.

===========================================================================
*/
package store

const raceDetectorOn = false
