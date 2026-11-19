/*
===========================================================================

storage_siblings_test.go - one open account warehouse per account

Port-only, not native: the port binds play per character, so two
characters of one account can stand at two keepers at once, while the
original holds one session per account. The account warehouse opens for
one of them at a time (storage.go accountStorageHolder); the other hears
the keeper's busy refusal until the first closes or leaves.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
siblingStorageFixture

The storage fixture's player, whose room is open, and a second character
of the same account at the same keeper. Both are in the world.
================
*/
func siblingStorageFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character) {
	t.Helper()
	rt, a, _ := storageFixture(t)
	b := testCharacter()
	b.ID, b.Name = 4, "sibling"
	a.AccountID, b.AccountID = "account-1", "account-1"
	rt.deps.(*enterworld.Deps).Characters = enterworld.StaticCharacterSource{testDivision: {a, b}}
	for _, c := range []*enterworld.Character{a, b} {
		rt.characterAdmissions.Store(simulation.WorldKey(testDivision, c.Name), populationAdmission{division: testDivision, name: c.Name})
	}
	// Reopen now that the account is known, as the player's open does.
	openStorageRoom(t, rt, a, 17)
	rt.Selected.Set(testDivision, b.Name, 17)
	return rt, a, b
}

/*
================
keeperBusy

True when frames are the keeper's busy refusal (0xB338 kind 2, code 0x0B).
================
*/
func keeperBusy(frames []wire.Frame) bool {
	return len(frames) == 1 && frames[0].Opcode == wire.OpNpcInteractionAck &&
		bytes.Equal(frames[0].Payload, wire.EncodeNpcInteractionRefusal(errCodeKeeperBusy))
}

/*
================
TestAccountWarehouseOpensForOneCharacterAtATime
================
*/
func TestAccountWarehouseOpensForOneCharacterAtATime(t *testing.T) {
	rt, a, b := siblingStorageFixture(t)
	list := wire.NewWriter(5).U32(17).U8(0).Payload()
	open := wire.NewWriter(8).U32(17).U32(wire.StorageFunctionMask).Payload()

	frames, refusal := rt.HandleStorageList(testDivision, b, list)
	if !keeperBusy(frames) || refusal == "" {
		t.Fatalf("the sibling listed the open warehouse: %+v / %q", frames, refusal)
	}
	frames, refusal = rt.HandleNpcAction(testDivision, b, open)
	if !keeperBusy(frames) || refusal == "" {
		t.Fatalf("the sibling opened the open warehouse: %+v / %q", frames, refusal)
	}
	if _, held := rt.storageSession(testDivision, b.Name); held {
		t.Fatal("a refused sibling holds a warehouse session")
	}

	// The first character closes its window: the sibling's visit lists.
	rt.closeStorageSession(testDivision, a.Name)
	if frames, refusal := rt.HandleStorageList(testDivision, b, list); refusal != "" || len(frames) != 3 {
		t.Fatalf("the sibling could not list a closed warehouse: %+v / %q", frames, refusal)
	}
	openStorageRoom(t, rt, b, 17)
	// Now the first character is the one refused.
	rt.Selected.Set(testDivision, a.Name, 17)
	if frames, _ := rt.HandleNpcAction(testDivision, a, open); !keeperBusy(frames) {
		t.Fatalf("the first character opened the sibling's warehouse: %+v", frames)
	}
}

/*
================
TestAccountWarehouseFreesWhenItsHolderLeaves

A holder no longer in the world (no admission) holds nothing.
================
*/
func TestAccountWarehouseFreesWhenItsHolderLeaves(t *testing.T) {
	rt, a, b := siblingStorageFixture(t)
	rt.characterAdmissions.Delete(simulation.WorldKey(testDivision, a.Name))
	openStorageRoom(t, rt, b, 17)
}

/*
================
TestOtherAccountsShareNoWarehouseLock

Characters of different accounts open their own warehouses together, and
a character with no account id (fixtures, tools) is never held.
================
*/
func TestOtherAccountsShareNoWarehouseLock(t *testing.T) {
	rt, _, b := siblingStorageFixture(t)
	b.AccountID = "account-2"
	openStorageRoom(t, rt, b, 17)
	b.AccountID = ""
	openStorageRoom(t, rt, b, 17)
}
