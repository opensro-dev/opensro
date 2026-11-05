package enterworld

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

type cosBootstrapRefSource struct {
	character *CharacterRef
}

func (s cosBootstrapRefSource) ItemRefByCodename(string) (*ItemRef, bool) {
	return nil, false
}

func (s cosBootstrapRefSource) CharacterRefByCodename(codename string) (*CharacterRef, bool) {
	if s.character == nil || s.character.Codename != codename {
		return nil, false
	}
	return s.character, true
}

func (s cosBootstrapRefSource) SummonableCharacterRefs() []CharacterRef {
	if s.character == nil {
		return nil
	}
	return []CharacterRef{*s.character}
}

func TestBootstrapRehydratesCosBeforeObjectListAndRideAfterFinalize(t *testing.T) {
	ref := &CharacterRef{
		RefObjID: 3914, TidWord: 0x11C6, Codename: "COS_T_DHORSE3", Name: "Red Horse",
		WalkSpeed: 20, RunSpeed: 40, Scale: 100, MaxHP: 87829,
		MountedAttackCapability210: 3000,
	}
	deps := &Deps{Items: cosBootstrapRefSource{character: ref}}
	character := &Character{
		ID: 3, Name: "asd2",
		ActiveCOS: &CharacterCOS{
			GID: 0x00C00003, RefObjID: ref.RefObjID, Codename: ref.Codename,
			Name: ref.Name, CurrentHP: ref.MaxHP, Summoned: true, Mounted: true,
			NativeBodyStatus: 4,
		},
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}
	packets, err := buildBootstrapPackets(deps, "global-official", character, entry, nil, ObjectIDForCharacter(character))
	if err != nil {
		t.Fatal(err)
	}

	indexes := map[uint16]int{}
	for index, packet := range packets {
		indexes[packet.NativeOpcode] = index
	}
	createIndex, hasCreate := indexes[wire.OpCosRecordCreate]
	startIndex, hasStart := indexes[OpcodeObjectListStart]
	finalizeIndex, hasFinalize := indexes[OpcodeObjectListFinalize]
	rideIndex, hasRide := indexes[wire.OpCosRideState]
	if !hasCreate || !hasStart || !hasFinalize || !hasRide ||
		createIndex >= startIndex || startIndex >= finalizeIndex || finalizeIndex >= rideIndex {
		t.Fatalf("COS bootstrap order = create:%d/%v start:%d/%v finalize:%d/%v ride:%d/%v",
			createIndex, hasCreate, startIndex, hasStart, finalizeIndex, hasFinalize, rideIndex, hasRide)
	}
	if got := packets[startIndex].Payload; len(got) != 3 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("object-list start = %v, want one COS row", got)
	}
	if startIndex+2 != finalizeIndex || packets[startIndex+1].NativeOpcode != OpcodeObjectListChunk {
		t.Fatalf("COS object row is not the sole bracket body: start=%d finalize=%d packets=%+v", startIndex, finalizeIndex, packets)
	}
	row := packets[startIndex+1].Payload
	if len(row) <= 31 || row[31] != 4 {
		t.Fatalf("re-entry lost authoritative COS body status: %v", row)
	}
	if len(row) < 8 || binary.LittleEndian.Uint32([]byte{byte(row[0]), byte(row[1]), byte(row[2]), byte(row[3])}) != ref.RefObjID ||
		binary.LittleEndian.Uint32([]byte{byte(row[4]), byte(row[5]), byte(row[6]), byte(row[7])}) != character.ActiveCOS.GID {
		t.Fatalf("COS object row prefix = %v", row)
	}
	wantRide := wire.EncodeCosRideState(ObjectIDForCharacter(character), true, character.ActiveCOS.GID)
	gotRide := packets[rideIndex].Payload
	if len(gotRide) != len(wantRide) {
		t.Fatalf("B4B5 length = %d, want %d", len(gotRide), len(wantRide))
	}
	for index, want := range wantRide {
		if gotRide[index] != want {
			t.Fatalf("B4B5[%d] = 0x%02X, want 0x%02X", index, gotRide[index], want)
		}
	}
}

/*
================
TestBootstrapCosOwnerNameFollowsTheJobSuit

#1983: a relogging suited owner's rehydrated COS row carries the job
alias, not the real name; the disguise survives the relogin.
================
*/
func TestBootstrapCosOwnerNameFollowsTheJobSuit(t *testing.T) {
	ref := &CharacterRef{
		RefObjID: 3914, TidWord: 0x11C6, Codename: "COS_T_DHORSE3", Name: "Red Horse",
		WalkSpeed: 20, RunSpeed: 40, Scale: 100, MaxHP: 87829,
		MountedAttackCapability210: 3000,
	}
	deps := &Deps{Items: cosBootstrapRefSource{character: ref}}
	character := &Character{
		ID: 3, Name: "asd2",
		Job: domain.CharacterJob{Type: domain.JobTrader, Grade: 1, Alias: "Trader"},
		MissionInventory: []InventoryRow{
			{Slot: JobSuitSlot, RefObjID: 9101, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, 1), StackCount: 1},
		},
		ActiveCOS: &CharacterCOS{
			GID: 0x00C00003, RefObjID: ref.RefObjID, Codename: ref.Codename,
			Name: ref.Name, CurrentHP: ref.MaxHP, Summoned: true, Mounted: true,
			NativeBodyStatus: 4,
		},
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}
	packets, err := buildBootstrapPackets(deps, "global-official", character, entry, nil, ObjectIDForCharacter(character))
	if err != nil {
		t.Fatal(err)
	}
	alias := append([]byte{byte(len("Trader")), 0}, "Trader"...)
	realName := append([]byte{byte(len(character.Name)), 0}, character.Name...)
	found := false
	for _, packet := range packets {
		if packet.NativeOpcode != OpcodeObjectListChunk {
			continue
		}
		found = true
		if !bytes.Contains(packet.Payload, alias) {
			t.Fatalf("relogin COS row owner name is not the alias: %v", packet.Payload)
		}
		if bytes.Contains(packet.Payload, realName) {
			t.Fatalf("relogin COS row leaks the real name: %v", packet.Payload)
		}
	}
	if !found {
		t.Fatal("bootstrap published no COS object row")
	}
}
