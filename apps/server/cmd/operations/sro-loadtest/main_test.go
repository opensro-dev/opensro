/*
===========================================================================

main_test.go - the load runner's pure arithmetic

The staircase parser and the enter-latency summary are the report's
denominator: a step that silently accepted "0" sessions, or a median
computed over the wrong set, would understate the load the server
actually carried.

===========================================================================
*/

package main

import "testing"

/*
================
TestParseSteps
================
*/
func TestParseSteps(t *testing.T) {
	steps, err := parseSteps("50, 100,200,500")
	if err != nil {
		t.Fatalf("parseSteps: %v", err)
	}
	want := []int{50, 100, 200, 500}
	if len(steps) != len(want) {
		t.Fatalf("parseSteps = %v, want %v", steps, want)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Fatalf("parseSteps = %v, want %v", steps, want)
		}
	}
	for _, bad := range []string{"", "0", "-3", "fifty", "50,,100"} {
		if _, err := parseSteps(bad); err == nil {
			t.Fatalf("parseSteps(%q) succeeded, want error", bad)
		}
	}
}

/*
================
TestEnterStats
================
*/
func TestEnterStats(t *testing.T) {
	if p50, max := enterStats(nil); p50 != 0 || max != 0 {
		t.Fatalf("enterStats(nil) = %d/%d, want 0/0", p50, max)
	}
	if p50, max := enterStats([]int64{42}); p50 != 42 || max != 42 {
		t.Fatalf("enterStats(one) = %d/%d, want 42/42", p50, max)
	}
	// Unsorted input must not change the answer, and the caller's slice
	// keeps its order (rows are reported per bot, not per latency).
	in := []int64{900, 100, 500, 300, 700}
	if p50, max := enterStats(in); p50 != 500 || max != 900 {
		t.Fatalf("enterStats = %d/%d, want 500/900", p50, max)
	}
	if in[0] != 900 || in[4] != 700 {
		t.Fatalf("enterStats mutated its input: %v", in)
	}
}

/*
================
TestCountedOpcode

Only opcodes above the session-internal range occupy a stream
position; the bot's ack must mirror the server's ring rule or its
resume position drifts from what the server kept.
================
*/
func TestCountedOpcode(t *testing.T) {
	for _, op := range []uint16{0x0001, 0x0002, 0x0005} {
		if countedOpcode(op) {
			t.Fatalf("countedOpcode(0x%04X) = true, want false", op)
		}
	}
	for _, op := range []uint16{0x0006, 0x3012, 0xB245} {
		if !countedOpcode(op) {
			t.Fatalf("countedOpcode(0x%04X) = false, want true", op)
		}
	}
}
