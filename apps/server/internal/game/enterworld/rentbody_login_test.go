/*
===========================================================================

rentbody_login_test.go - a rental's login row carries its deadline

BuildItemBody wrote every login row with no deadline: a rented avatar
read as permanent after each relog, while every live packet of the same
row sent its deadline. Both bodies now come from one rule.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestLoginRowCarriesTheRentDeadline

A rented row of a rental reference ends its login body with the
deadline in unix milliseconds, the same bytes its live body carries; a
permanent row of that reference ends with a zero leg.
================
*/
func TestLoginRowCarriesTheRentDeadline(t *testing.T) {
	wire.SetRentRefs([]uint32{29411})
	defer wire.SetRentRefs(nil)
	flags := wire.PackTypeFlags(3, 1, 13, 1)
	const deadline = int64(1_798_000_000)
	rented := WireItem{RefObjID: 29411, TypeFlags: flags, Plus: 1, Durability: 50, RentExpiresUnix: deadline}
	body := BuildItemBody(rented)
	if len(body) < 8 {
		t.Fatalf("login body = %x, too short for a rent leg", body)
	}
	if got := int64(binary.LittleEndian.Uint64(body[len(body)-8:])); got != deadline*1000 {
		t.Fatalf("login rent leg = %d, want %d", got, deadline*1000)
	}
	live := wire.ItemBody{RefObjID: 29411, TypeFlags: flags, Plus: 1, Durability: 50, RentExpiresUnixMs: deadline * 1000}
	if !bytes.Equal(body, live.Encode()) {
		t.Fatalf("login body = %x, live body = %x; want one encoding", body, live.Encode())
	}
	permanent := rented
	permanent.RentExpiresUnix = 0
	if leg := BuildItemBody(permanent); !bytes.Equal(leg[len(leg)-8:], make([]byte, 8)) || len(leg) != len(body) {
		t.Fatalf("permanent login body = %x, want the same length ending in a zero leg", leg)
	}
}
