/*
===========================================================================

limiter_test.go - per-account pacing

===========================================================================
*/
package bugreport

import (
	"testing"
	"time"
)

/*
================
TestLimiterPacesEachAccount
================
*/
func TestLimiterPacesEachAccount(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter(func() time.Time { return now })

	if ok, _ := l.begin("tester"); !ok {
		t.Fatal("first report refused")
	}
	if ok, _ := l.begin("tester"); ok {
		t.Fatal("second report admitted while the first is in flight")
	}
	if ok, _ := l.begin("other"); !ok {
		t.Fatal("another account must not be paced by tester")
	}
	l.finish("other", false)
	l.finish("tester", true)

	now = now.Add(30 * time.Second)
	if ok, wait := l.begin("tester"); ok || wait != 30*time.Second {
		t.Fatalf("want 30s wait after a delivered report, got ok=%t wait=%s", ok, wait)
	}
	now = now.Add(30 * time.Second)
	if ok, _ := l.begin("tester"); !ok {
		t.Fatal("report refused after the interval")
	}
	l.finish("tester", true)
}

/*
================
TestLimiterIgnoresFailedDeliveries
================
*/
func TestLimiterIgnoresFailedDeliveries(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter(func() time.Time { return now })
	for range 3 {
		if ok, _ := l.begin("tester"); !ok {
			t.Fatal("retry after a failed delivery refused")
		}
		l.finish("tester", false)
	}
}

/*
================
TestLimiterCapsReportsPerDay
================
*/
func TestLimiterCapsReportsPerDay(t *testing.T) {
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	now := start
	l := newLimiter(func() time.Time { return now })
	for index := range reportsPerWindow {
		now = start.Add(time.Duration(index) * reportInterval)
		if ok, _ := l.begin("tester"); !ok {
			t.Fatalf("report %d refused", index)
		}
		l.finish("tester", true)
	}
	now = now.Add(reportInterval)
	ok, wait := l.begin("tester")
	if ok {
		t.Fatal("report over the daily cap admitted")
	}
	if want := start.Add(reportWindow).Sub(now); wait != want {
		t.Fatalf("wait %s, want %s", wait, want)
	}
	now = start.Add(reportWindow + time.Second)
	if ok, _ := l.begin("tester"); !ok {
		t.Fatal("oldest report must leave the window")
	}
}
