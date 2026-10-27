/*
===========================================================================

mostwanted_test.go - the wanted-state frame's byte layout

Port-only, not native (#1849): the browser client's decoder reads these
five bytes in this order, so the layout is pinned.

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestEncodeMostWantedStateLayout
================
*/
func TestEncodeMostWantedStateLayout(t *testing.T) {
	got := EncodeMostWantedState(MostWantedState{Level: 4, KillsIntoLevel: 7, KillsPerLevel: 10, Job: 2, DamagePercent: 1})
	if want := []byte{4, 7, 10, 2, 1}; !bytes.Equal(got, want) {
		t.Fatalf("state %v, want %v", got, want)
	}
	if got := EncodeMostWantedState(MostWantedState{}); !bytes.Equal(got, []byte{0, 0, 0, 0, 0}) {
		t.Fatalf("cleared state %v, want five zero bytes", got)
	}
	if OpMostWantedState != 0x3FB2 {
		t.Fatalf("opcode %#x, want 0x3fb2", OpMostWantedState)
	}
}
