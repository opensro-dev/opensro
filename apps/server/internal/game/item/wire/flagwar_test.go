/*
===========================================================================

flagwar_test.go - the Capture the Flag event encoders

Every 0x30A9 shape the match sends is pinned byte for byte: the
v1.150 client handler (0x4B2530) switches on these exact layouts.

===========================================================================
*/

package wire

import (
	"bytes"
	"testing"
)

/*
================
TestFlagWarEventEncoders
================
*/
func TestFlagWarEventEncoders(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want []byte
	}{
		{"mission start team 1", EncodeFlagWarMissionStart(1), []byte{0x08, 0x01, 0x00}},
		{"mission start team 0", EncodeFlagWarMissionStart(0), []byte{0x08, 0x00, 0x00}},
		{"carrier", EncodeFlagWarCarrier(0x01020304), []byte{0x0A, 0x04, 0x03, 0x02, 0x01}},
		{"elapsed", EncodeFlagWarElapsed(61000), []byte{0xFF, 0x0E, 0x48, 0xEE, 0x00, 0x00}},
		{"pickup ours", EncodeFlagWarNamed(FlagWarPickupOurs, "Bo"),
			[]byte{0xFF, 0x09, 0x02, 0x00, 'B', 'o'}},
		{"master key", EncodeFlagWarMasterKey(0x0A0B0C0D, 1, "Al"),
			[]byte{0xFF, 0x0B, 0x0D, 0x0C, 0x0B, 0x0A, 0x01, 0x02, 0x00, 'A', 'l'}},
		{"mission result", EncodeFlagWarMissionResult(1, "Bo"),
			[]byte{0xFF, 0x10, 0x01, 0x02, 0x00, 'B', 'o'}},
	}
	for _, tc := range cases {
		if !bytes.Equal(tc.got, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
