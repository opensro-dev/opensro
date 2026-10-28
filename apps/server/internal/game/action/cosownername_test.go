/*
===========================================================================

cosownername_test.go - a suited owner's companions carry the job alias

#1983: the companion spawn writers carried the owner's real name even
while the owner wore a job suit, unmasking the disguise the player spawn
itself honours. Every companion owner-name writer takes the one display
name (enterworld.PartyMemberName), and a completed dress or undress
republishes already-summoned companions under the new label.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
wireName

A length-prefixed name as the COS spawn body carries it (U16 byte count,
then the bytes), so a test matches a whole field, never a substring of
some longer name.
================
*/
func wireName(name string) []byte {
	out := make([]byte, 2, 2+len(name))
	binary.LittleEndian.PutUint16(out, uint16(len(name)))
	return append(out, name...)
}

/*
================
TestSuitedOwnerCompanionsCarryTheJobAlias

A dressed owner's fresh pet summon, peer presentation and transport
summon all carry the job alias; the real name appears in none of them.
The undressed control carries the real name.
================
*/
func TestSuitedOwnerCompanionsCarryTheJobAlias(t *testing.T) {
	c, refs := persistentSummonFixture()
	dressTrader(c)
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	summon := rt.HandleItemUse(testDivision, c, wire.NewWriter(3).U8(23).U16(refs.staticItemSource["SUMMON_ATTACK"].TypeFlags()).Payload())
	if len(summon.Frames) == 0 || summon.Frames[0].Opcode != wire.OpItemUseResponse || summon.Frames[0].Payload[0] != 1 {
		t.Fatalf("suited summon refused: %+v", summon)
	}
	found := false
	for _, frame := range summon.Frames {
		if frame.Opcode != wire.OpSingleObjectSpawn {
			continue
		}
		found = true
		if !bytes.Contains(frame.Payload, wireName("Trader")) {
			t.Fatalf("fresh summon owner name is not the alias: %v", frame.Payload)
		}
		if bytes.Contains(frame.Payload, wireName(c.Name)) {
			t.Fatalf("fresh summon leaks the real name %q: %v", c.Name, frame.Payload)
		}
	}
	if !found {
		t.Fatal("summon published no spawn frame")
	}
	peers := rt.CompanionPresentations(testDivision, c.Name)
	if len(peers) != 1 || peers[0].Row.OwnerName != "Trader" {
		t.Fatalf("peer presentation owner names = %+v, want the alias", peers)
	}

	// The transport summon answers its owner with the same label.
	horseChar, horseRefs := persistentSummonFixture()
	dressTrader(horseChar)
	level := int64(10)
	horseChar.Level = &level
	horseRT, _ := newTestRuntime(horseChar, horseRefs)
	horse := horseRefs.staticItemSource["ITEM_COS_T_DHORSE3"]
	horseChar.MissionInventory = append(horseChar.MissionInventory, enterworld.InventoryRow{Slot: 22, RefObjID: horse.RefObjID, Codename: horse.Codename, TypeFlags: horse.TypeFlags(), StackCount: 1})
	horseRT.BindPetSession(testDivision, horseChar, 101)
	ride := horseRT.HandleItemUse(testDivision, horseChar, []byte{22, 0xec, 0x11})
	if len(ride.Frames) == 0 || ride.Frames[0].Payload[0] != 1 {
		t.Fatalf("suited transport summon refused: %+v", ride)
	}
	found = false
	for _, frame := range ride.Frames {
		if frame.Opcode != wire.OpSingleObjectSpawn {
			continue
		}
		found = true
		if !bytes.Contains(frame.Payload, wireName("Trader")) || bytes.Contains(frame.Payload, wireName(horseChar.Name)) {
			t.Fatalf("transport summon owner name is not the alias alone: %v", frame.Payload)
		}
	}
	if !found {
		t.Fatal("transport summon published no spawn frame")
	}

	// Undressed, the same peer lane carries the real name.
	bare, bareRefs := persistentSummonFixture()
	bareRT, _ := newTestRuntime(bare, bareRefs)
	bareRT.CompanionRoll = func() (uint32, error) { return 0, nil }
	bareRT.BindPetSession(testDivision, bare, 101)
	useSummonerFixture(t, bareRT, bare, 23, bareRefs.staticItemSource["SUMMON_ATTACK"])
	if peers := bareRT.CompanionPresentations(testDivision, bare.Name); len(peers) != 1 || peers[0].Row.OwnerName != bare.Name {
		t.Fatalf("undressed peer owner names = %+v, want the real name", peers)
	}
}

/*
================
TestRefreshCompanionOwnerLabelsBumpsOnlyTheOwnersPets

The dress-completion republish bumps the generation of every summoned
companion session of the dressed owner, and of nothing else: viewers
despawn and respawn those pets with the new label.
================
*/
func TestRefreshCompanionOwnerLabelsBumpsOnlyTheOwnersPets(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	other := testCharacter()
	other.ID, other.Name = 4, "other1"
	rt.BindPetSession(testDivision, other, 202)

	generationOf := func(character *enterworld.Character) (uint64, bool) {
		for _, pet := range rt.CompanionPresentations(testDivision, character.Name) {
			return pet.Generation, true
		}
		return 0, false
	}
	before, ok := generationOf(c)
	if !ok {
		t.Fatal("no companion presentation for the owner")
	}
	otherBefore, otherOK := generationOf(other)
	rt.refreshCompanionOwnerLabels(testDivision, c)
	after, _ := generationOf(c)
	if after != before+1 {
		t.Fatalf("owner pet generation = %d, want %d after the republish", after, before+1)
	}
	if otherOK {
		if got, _ := generationOf(other); got != otherBefore {
			t.Fatalf("another owner's pet generation moved %d -> %d", otherBefore, got)
		}
	}
}
