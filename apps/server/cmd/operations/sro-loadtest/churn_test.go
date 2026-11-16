/*
===========================================================================

churn_test.go - churn mode's guard

A churn run without a positive rate and duration is refused before any
socket opens: a zero rate would divide the ticker period by zero.

===========================================================================
*/

package main

import (
	"testing"
	"time"
)

/*
================
TestChurnRefusesNoRate
================
*/
func TestChurnRefusesNoRate(t *testing.T) {
	cfg := runConfig{agentURL: "http://127.0.0.1:1", gameURL: "http://127.0.0.1:1", timeout: time.Second}
	for _, opts := range []churnOptions{{rate: 0, duration: time.Second}, {rate: 3, duration: 0}, {rate: -1, duration: time.Second}} {
		if _, err := runChurn(cfg, opts, botAccount{ID: "a", Password: "b"}, t.TempDir()); err == nil {
			t.Fatalf("churn with rate %v and duration %v ran", opts.rate, opts.duration)
		}
	}
}
