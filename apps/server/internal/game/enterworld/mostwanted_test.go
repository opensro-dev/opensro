/*
===========================================================================

mostwanted_test.go - world entry re-raises a suited streak holder's state

Port-only, not native (#1849): the packet exists only with the rule on,
a suit worn and a streak saved; every other entry is byte-identical to
today's.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
suitedCharacter

A character wearing a thief suit over the given streak.
================
*/
func suitedCharacter(streak int64) *Character {
	c := &Character{}
	c.MissionInventory = []InventoryRow{{Slot: JobSuitSlot, TypeFlags: wire.PackTypeFlags(3, 1, 7, 2)}}
	if streak > 0 {
		c.MostWanted = &domain.MostWanted{Streak: streak}
	}
	return c
}

/*
================
TestMostWantedPacketsAtEntry
================
*/
func TestMostWantedPacketsAtEntry(t *testing.T) {
	on := &Deps{MostWantedEnabled: true}
	packets := mostWantedPackets(on, suitedCharacter(47))
	if len(packets) != 1 || packets[0].NativeOpcode != wire.OpMostWantedState ||
		!bytes.Equal(packets[0].Payload, []byte{4, 7, 10, 2, 1}) {
		t.Fatalf("packets %+v", packets)
	}
	// Below the first level the progress still reports (no row yet).
	if packets := mostWantedPackets(on, suitedCharacter(3)); len(packets) != 1 ||
		!bytes.Equal(packets[0].Payload, []byte{0, 3, 10, 2, 0}) {
		t.Fatalf("low streak packets %+v", packets)
	}
	for name, deps := range map[string]*Deps{"off": {MostWantedEnabled: false}, "nil": nil} {
		if packets := mostWantedPackets(deps, suitedCharacter(47)); packets != nil {
			t.Fatalf("%s: packets %+v", name, packets)
		}
	}
	unsuited := suitedCharacter(47)
	unsuited.MissionInventory = nil
	if packets := mostWantedPackets(on, unsuited); packets != nil {
		t.Fatalf("unsuited packets %+v", packets)
	}
	if packets := mostWantedPackets(on, suitedCharacter(0)); packets != nil {
		t.Fatalf("no-streak packets %+v", packets)
	}
	if packets := mostWantedPackets(on, nil); packets != nil {
		t.Fatalf("nil character packets %+v", packets)
	}
}
