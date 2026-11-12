/*
===========================================================================

pacer_test.go - the keyed token bucket's allowance, refill and bound

===========================================================================
*/

package pacer

import (
	"testing"
	"time"
)

/*
================
testClock
================
*/
type testClock struct{ at time.Time }

func (clock *testClock) now() time.Time { return clock.at }

/*
================
TestKeyedSpendsBurstThenRefills

A key gets Burst calls at once, then one per Refill; the refusal names
the wait until the next token.
================
*/
func TestKeyedSpendsBurstThenRefills(t *testing.T) {
	clock := &testClock{at: time.Unix(1_000, 0)}
	pacer := New(Config{Burst: 3, Refill: 10 * time.Second, MaxKeys: 8, Now: clock.now})
	for i := range 3 {
		if !pacer.Allow("a") {
			t.Fatalf("call %d refused inside the burst", i)
		}
	}
	clock.at = clock.at.Add(4 * time.Second)
	allowed, wait := pacer.Take("a")
	if allowed || wait != 6*time.Second {
		t.Fatalf("empty bucket: allowed=%v wait=%s, want refused with 6s", allowed, wait)
	}
	if !pacer.Allow("b") {
		t.Fatal("another key shares the first key's bucket")
	}
	clock.at = clock.at.Add(6 * time.Second)
	if !pacer.Allow("a") {
		t.Fatal("no token after one refill period")
	}
	if pacer.Allow("a") {
		t.Fatal("two tokens after one refill period")
	}
	clock.at = clock.at.Add(time.Hour)
	for i := range 3 {
		if !pacer.Allow("a") {
			t.Fatalf("call %d refused after a full refill", i)
		}
	}
	if pacer.Allow("a") {
		t.Fatal("refill passed the burst")
	}
}

/*
================
TestKeyedForgetsTheOldestKeyAtTheBound

Past MaxKeys the least recently seen key is forgotten (a fresh
allowance), and the bound holds.
================
*/
func TestKeyedForgetsTheOldestKeyAtTheBound(t *testing.T) {
	clock := &testClock{at: time.Unix(1_000, 0)}
	pacer := New(Config{Burst: 1, Refill: time.Hour, MaxKeys: 2, Now: clock.now})
	pacer.Allow("old")
	clock.at = clock.at.Add(time.Second)
	pacer.Allow("mid")
	clock.at = clock.at.Add(time.Second)
	pacer.Allow("new")
	if len(pacer.buckets) != 2 {
		t.Fatalf("%d buckets, want the bound 2", len(pacer.buckets))
	}
	if pacer.Allow("mid") {
		t.Fatal("a recent key was forgotten instead of the oldest")
	}
	if !pacer.Allow("old") {
		t.Fatal("the oldest key kept its empty bucket")
	}
}
