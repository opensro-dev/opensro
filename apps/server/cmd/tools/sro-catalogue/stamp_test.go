/*
===========================================================================

stamp_test.go - generatedAt follows SOURCE_DATE_EPOCH

===========================================================================
*/

package main

import (
	"testing"
	"time"
)

func fixedEnv(value string) func(string) string {
	return func(name string) string {
		if name == sourceDateEpoch {
			return value
		}
		return ""
	}
}

/*
================
TestGeneratedAtIsTheSourceDateWhenSet

Two builds of one freeze at different clock times stamp the same time.
================
*/
func TestGeneratedAtIsTheSourceDateWhenSet(t *testing.T) {
	for _, clock := range []time.Time{time.Unix(1, 0), time.Unix(2_000_000_000, 0)} {
		got, err := generatedAt(fixedEnv("1763646386"), func() time.Time { return clock })
		if err != nil || got != "2025-11-20T13:46:26Z" {
			t.Fatalf("clock %v: got %q, %v", clock, got, err)
		}
	}
}

/*
================
TestGeneratedAtFallsBackToTheClock
================
*/
func TestGeneratedAtFallsBackToTheClock(t *testing.T) {
	got, err := generatedAt(fixedEnv(""), func() time.Time { return time.Unix(60, 0) })
	if err != nil || got != "1970-01-01T00:01:00Z" {
		t.Fatalf("got %q, %v", got, err)
	}
}

/*
================
TestGeneratedAtRefusesAMalformedSourceDate
================
*/
func TestGeneratedAtRefusesAMalformedSourceDate(t *testing.T) {
	for _, value := range []string{"soon", "-5", "1.5"} {
		if _, err := generatedAt(fixedEnv(value), time.Now); err == nil {
			t.Fatalf("%q was accepted", value)
		}
	}
}
