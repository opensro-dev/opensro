/*
===========================================================================

tradeprogress_test.go - the port-only trade progress body

===========================================================================
*/
package wire

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

/*
================
TestEncodeTradeProgressLayout
================
*/
func TestEncodeTradeProgressLayout(t *testing.T) {
	got := EncodeTradeProgress(3, 2424)
	want := binary.LittleEndian.AppendUint32([]byte{3}, 2424)
	if !bytes.Equal(got, want) {
		t.Fatalf("body % X, want % X", got, want)
	}
	if got := EncodeTradeProgress(5, 0); !bytes.Equal(got, []byte{5, 0, 0, 0, 0}) {
		t.Fatalf("five-star body % X, want a zero gap", got)
	}
	if got := EncodeTradeProgress(1, math.MaxUint64); binary.LittleEndian.Uint32(got[1:]) != math.MaxUint32 {
		t.Fatalf("gap %d, want the uint32 cap", binary.LittleEndian.Uint32(got[1:]))
	}
}
