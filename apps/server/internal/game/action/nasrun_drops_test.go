/*
===========================================================================

nasrun_drops_test.go - tests for nasrun_drops.go

Through the token kill (nasrun_tokens_test.go): a zero roll wins every
earner's Spirit and jewel into their bag with a notice; a high roll
wins nothing; a middle roll wins the 2 % jewel and misses the 0.25 %
Spirit; off, the same kill pays nothing. The odds themselves are
game/nasrun/drops_test.go's.

===========================================================================
*/
package action

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
)

// tokenKillRoll's rolls are draws of one value in DropRoll's domain
// (32768): easy Spirit odds (0.25 %) take a draw under 82, the jewel
// (2 %) under 656.
const (
	dropRollWinsAll  = 0
	dropRollJewel    = 100
	dropRollWinsNone = 30_000
)

// The prizes, as scripts/nasrun_import.py stages them.
var dropPrizeRefs = map[string]*enterworld.ItemRef{
	nasrunDropSpiritMale:   {RefObjID: 24405, Codename: nasrunDropSpiritMale, TypeIDs: [4]int64{3, 1, 14, 1}},
	nasrunDropSpiritFemale: {RefObjID: 24406, Codename: nasrunDropSpiritFemale, TypeIDs: [4]int64{3, 1, 14, 1}},
	nasrunDropJewel: {RefObjID: 24417, Codename: nasrunDropJewel, TypeIDs: [4]int64{3, 3, 10, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 250})},
}

/*
================
dropNotices

The lucky-drop notices among frames ("Lucky drop", UTF-16 on the wire).
================
*/
func dropNotices(frames []wire.Frame) int {
	n := 0
	for _, frame := range frames {
		if frame.Opcode == chat.OpChatBroadcast && strings.Contains(string(frame.Payload), "L\x00u\x00c\x00k\x00y\x00 \x00d\x00r\x00o\x00p\x00") {
			n++
		}
	}
	return n
}

/*
================
TestNasrunLuckyDropsWonOnAZeroRoll
================
*/
func TestNasrunLuckyDropsWonOnAZeroRoll(t *testing.T) {
	actor, partner, result := tokenKillRoll(t, true, dropRollWinsAll, dropPrizeRefs, nil)
	for _, c := range []*enterworld.Character{actor, partner} {
		if got := bagCount(c, nasrunDropSpiritMale); got != 1 {
			t.Fatalf("%s holds %d Spirits, want 1", c.Name, got)
		}
		if got := bagCount(c, nasrunDropJewel); got != 1 {
			t.Fatalf("%s holds %d jewels, want 1", c.Name, got)
		}
	}
	if got := dropNotices(result.Frames); got != 2 {
		t.Fatalf("%d actor drop notices, want 2", got)
	}
	if got := dropNotices(partnerFrames(result, partner.ID)); got != 2 {
		t.Fatalf("%d partner drop notices, want 2", got)
	}
}

/*
================
TestNasrunLuckyDropsMissOnAHighRoll
================
*/
func TestNasrunLuckyDropsMissOnAHighRoll(t *testing.T) {
	actor, partner, result := tokenKillRoll(t, true, dropRollWinsNone, dropPrizeRefs, nil)
	for _, c := range []*enterworld.Character{actor, partner} {
		if bagCount(c, nasrunDropSpiritMale) != 0 || bagCount(c, nasrunDropJewel) != 0 {
			t.Fatalf("%s won on a high roll: %+v", c.Name, c.MissionInventory)
		}
	}
	if got := dropNotices(result.Frames); got != 0 {
		t.Fatalf("%d actor drop notices, want 0", got)
	}
}

/*
================
TestNasrunLuckyDropsJewelOnlyInTheMiddle
================
*/
func TestNasrunLuckyDropsJewelOnlyInTheMiddle(t *testing.T) {
	actor, _, _ := tokenKillRoll(t, true, dropRollJewel, dropPrizeRefs, nil)
	if got := bagCount(actor, nasrunDropSpiritMale); got != 0 {
		t.Fatalf("the actor holds %d Spirits on a jewel-only roll", got)
	}
	if got := bagCount(actor, nasrunDropJewel); got != 1 {
		t.Fatalf("the actor holds %d jewels on a jewel-only roll", got)
	}
}

/*
================
TestNasrunLuckyDropsOffNative
================
*/
func TestNasrunLuckyDropsOffNative(t *testing.T) {
	actor, _, result := tokenKillRoll(t, false, dropRollWinsAll, dropPrizeRefs, nil)
	if bagCount(actor, nasrunDropSpiritMale) != 0 || bagCount(actor, nasrunDropJewel) != 0 {
		t.Fatalf("off, the kill paid a drop: %+v", actor.MissionInventory)
	}
	if got := dropNotices(result.Frames); got != 0 {
		t.Fatalf("off, %d actor drop notices", got)
	}
}
