/*
===========================================================================

interaction_test.go - fortress requests pinned to native packet write order

These bytes follow client 703130, independently of the production decoder.
They also guard the overlapping construction and ordinary request headers.

===========================================================================
*/
package siege

import (
	"encoding/hex"
	"fmt"
	"testing"
)

/*
================
TestInteractionNativeRequests
================
*/
func TestInteractionNativeRequests(t *testing.T) {
	for _, tc := range []struct {
		hex     string
		action  uint8
		service uint8
		ref     uint32
		word    uint16
		flag    uint8
		gold    int64
	}{
		{"443322110008070605", 0, 0x17, 0, 0, 0, 0},
		{"443322110108070605ecff", 1, 0x17, 0, 0xffec, 0, 0},
		{"443322110208070605ffffffffffffffff", 2, 0x17, 0, 0, 0, -1},
		{"443322110308070605", 3, 0x17, 0, 0, 0, 0},
		{"44332211040807060504", 4, 0x17, 0, 0, 4, 0},
		{"443322110508070605", 5, 0x17, 0, 0, 0, 0},
		{"4433221106", 6, 0x18, 0, 0, 0, 0},
		{"44332211070807060501", 7, 0x18, 0, 0, 1, 0},
		{"44332211080807060500", 8, 0x18, 0, 0, 0, 0},
		{"4433221109", 9, 0x19, 0, 0, 0, 0},
		{"443322110b080706054030201003", 11, 0x19, 0x10203040, 0, 3, 0},
		{"443322110c0807060540302010", 12, 0x19, 0x10203040, 0, 0, 0},
		{"443322110d08070605", 13, 0x1a, 0, 0, 0, 0},
		{"443322110e08070605403020100500", 14, 0x1a, 0x10203040, 5, 0, 0},
		{"443322110f0807060540302010", 15, 0x1a, 0x10203040, 0, 0, 0},
		{"443322111008070605403020100500", 16, 0x1a, 0x10203040, 5, 0, 0},
		{"443322111108070605", 17, 0x1b, 0, 0, 0, 0},
		{"443322111208070605403020100500", 18, 0x1b, 0x10203040, 5, 0, 0},
		{"44332211130807060540302010", 19, 0x1b, 0x10203040, 0, 0, 0},
		{"443322111408070605403020100500", 20, 0x1b, 0x10203040, 5, 0, 0},
		{"4433221115080706050200", 21, 0x1f, 0, 2, 0, 0},
		{"443322111608070605", 22, 0, 0, 0, 0, 0},
		{"443322111708070605", 23, 0, 0, 0, 0, 0},
		{"443322111808070605", 24, 0x19, 0, 0, 0, 0},
	} {
		t.Run(fmt.Sprintf("action_%02x", tc.action), func(t *testing.T) {
			payload, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			want := Interaction{
				Action: tc.action, Target: 0x11223344, Fortress: 0x05060708,
				Reference: tc.ref, Value16: tc.word, Value8: tc.flag, Gold: tc.gold,
			}
			if tc.action == 6 || tc.action == 9 {
				want.Fortress = 0
			}
			got, err := DecodeInteraction(payload)
			if err != nil || got != want {
				t.Fatalf("decode = %+v, %v; want %+v", got, err, want)
			}
			if got := InteractionService(tc.action); got != tc.service {
				t.Fatalf("service = %x, want %x", got, tc.service)
			}
			for n := range len(payload) {
				if _, err := DecodeInteraction(payload[:n]); err == nil {
					t.Fatalf("accepted truncation at %d", n)
				}
			}
			if _, err := DecodeInteraction(append(payload, 0)); err == nil {
				t.Fatal("accepted trailing data")
			}
		})
	}
}

/*
================
TestConstructionHeaderIsExplicit
================
*/
func TestConstructionHeaderIsExplicit(t *testing.T) {
	// Both interpretations are syntactically valid. The caller must use its
	// pending construction state, never payload[0], to choose the decoder.
	payload := []byte{10, 1, 0, 0, 0, 2, 0, 0, 0}
	ordinary, err := DecodeInteraction(payload)
	if err != nil || ordinary.Action != ActionTaxQuery || ordinary.Target != 266 {
		t.Fatalf("ordinary interpretation: %+v, %v", ordinary, err)
	}
	construction, err := DecodeConstruction(payload)
	if err != nil || construction != (Interaction{Action: ActionConstruct, Fortress: 1, Reference: 2}) {
		t.Fatalf("construction interpretation: %+v, %v", construction, err)
	}
	if InteractionService(ActionConstruct) != 0x19 {
		t.Fatal("construction did not require the aide service")
	}
	for n := range len(payload) {
		if _, err := DecodeConstruction(payload[:n]); err == nil {
			t.Fatalf("accepted construction truncation at %d", n)
		}
	}
	if _, err := DecodeConstruction(append(payload, 0)); err == nil {
		t.Fatal("accepted construction trailing bytes")
	}
	payload[0] = 0
	if _, err := DecodeConstruction(payload); err == nil {
		t.Fatal("accepted a different construction action")
	}
}

/*
================
TestLaterServerActionsAreOutsideClientScope
================
*/
func TestLaterServerActionsAreOutsideClientScope(t *testing.T) {
	for action := 25; action <= 255; action++ {
		if _, err := DecodeInteraction([]byte{1, 0, 0, 0, byte(action), 1, 0, 0, 0}); err == nil {
			t.Fatalf("accepted later action %x", action)
		}
		if InteractionService(uint8(action)) != 0 {
			t.Fatalf("granted later action %x", action)
		}
	}
}
