/*
===========================================================================

storage_endings_test.go - every way an open warehouse ends or refuses

Native warehouse moves need the player's warehouse data and its one
interaction state (the reference server refuses 0x1834 unless it is the warehouse's 6),
and item moves the keeper in range. Each test below drives one
ending through the path players take.

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
goldDeposit

One gold deposit's answer byte: 1 accepted, 2 refused.
================
*/
func goldDeposit(t *testing.T, rt *Runtime, c *enterworld.Character, amount uint32) byte {
	t.Helper()
	return trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageGoldDeposit, GoldAmount: amount}).Frames[0].Payload[0]
}

/*
================
TestStorageItemMovesNeedTheKeeperInRange

The reference server's storage moves check the packet's keeper with the NPC range check and answer
its code (4, beyond the range); gold has no range test.
================
*/
func TestStorageItemMovesNeedTheKeeperInRange(t *testing.T) {
	rt, c, authority := storageFixture(t)
	moveMerchantAway(rt, npcHitRange+10)
	slot := uint8(potionRow(t, c).Slot)
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageDeposit, SourceSlot: slot, DestSlot: 0, NpcGID: 17})
	if !bytes.Equal(r.Frames[0].Payload, []byte{2, hitRangeTooFar}) || len(authority.room.Rows) != 0 {
		t.Fatalf("far deposit = %+v", r.Frames)
	}
	if goldDeposit(t, rt, c, 10) != 1 || authority.room.Gold != 10 {
		t.Fatalf("far gold deposit refused; room %d", authority.room.Gold)
	}
}

/*
================
TestStorageEndsWhenAShopOpens

7046 writes the shop's interaction state 5 over the warehouse's 6.
================
*/
func TestStorageEndsWhenAShopOpens(t *testing.T) {
	rt, c, _ := storageFixture(t)
	rt.NpcRoster[0].TalkFlags |= simulation.NpcTalkFlagShop
	if _, refusal := rt.HandleNpcAction(testDivision, c, wire.NewWriter(8).U32(17).U32(uint32(simulation.NpcTalkFlagShop)).Payload()); refusal != "" {
		t.Fatalf("shop open refused: %s", refusal)
	}
	if goldDeposit(t, rt, c, 10) != 2 {
		t.Fatal("the warehouse outlived a shop opening")
	}
}

/*
================
TestStorageEndsWithDeath

The reference server's death job-state clear writes the interaction state 0.
================
*/
func TestStorageEndsWithDeath(t *testing.T) {
	rt, c, _ := storageFixture(t)
	rt.deps.Update(c, "test-death", func() bool {
		rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, rt.Now().UnixMilli())
		return true
	})
	if _, open := rt.storageSession(testDivision, c.Name); open {
		t.Fatal("the warehouse outlived its user's death")
	}
}

/*
================
TestStorageEndsWhenAnExchangeOpens

An exchange takes both players' interaction state.
================
*/
func TestStorageEndsWhenAnExchangeOpens(t *testing.T) {
	rt, a, b, _ := exchangeFixture(t)
	rt.openStorageSession(testDivision, a.Name, 17)
	rt.openStorageSession(testDivision, b.Name, 17)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	if !rt.Exchanges.Trading(testDivision, a.Name) {
		t.Fatal("fixture: the exchange did not open")
	}
	for _, c := range []*enterworld.Character{a, b} {
		if _, open := rt.storageSession(testDivision, c.Name); open {
			t.Fatalf("%s's warehouse outlived the exchange", c.Name)
		}
	}
}

/*
================
TestStorageEndsWhenAStallOpens

The reference server's stall state writes the interaction state 4.
================
*/
func TestStorageEndsWhenAStallOpens(t *testing.T) {
	rt, owner, _, _ := stallFixture(t)
	rt.openStorageSession(testDivision, owner.Name, 17)
	if out := rt.HandleStallCreate(testDivision, owner, wire.NewWriter(16).WStr("Potions").Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("stall create %+v", out.Frames)
	}
	if _, open := rt.storageSession(testDivision, owner.Name); open {
		t.Fatal("the warehouse outlived the stall's opening")
	}
}

/*
================
TestStorageEndsWhenItsWindowCloses

The client closes the warehouse through its keeper's release (0x74B3,
native close-NPC handling ends the interaction).
================
*/
func TestStorageEndsWhenItsWindowCloses(t *testing.T) {
	rt, c, _ := storageFixture(t)
	if out := rt.HandleTargetRelease(testDivision, c, wire.NewWriter(4).U32(17).Payload()); len(out.Frames) == 0 {
		t.Fatalf("release = %+v", out)
	}
	if goldDeposit(t, rt, c, 10) != 2 {
		t.Fatal("gold went into a closed warehouse")
	}
}

/*
================
TestStorageRefusesAKeeperThatIsGone

A despawned keeper is not a live warehouse.
================
*/
func TestStorageRefusesAKeeperThatIsGone(t *testing.T) {
	rt, c, _ := storageFixture(t)
	rt.NpcRoster = nil
	if goldDeposit(t, rt, c, 10) != 2 {
		t.Fatal("gold went into a vanished keeper's warehouse")
	}
}

/*
================
TestStorageEndsWhenTheCharacterLeavesTheWorld

A logout, a disconnect past its grace, or a shard change forgets the
character (forgetCharacterLocked).
================
*/
func TestStorageEndsWhenTheCharacterLeavesTheWorld(t *testing.T) {
	rt, c, _ := storageFixture(t)
	rt.forgetCharacterLocked(testDivision, c.Name)
	if _, open := rt.storageSession(testDivision, c.Name); open {
		t.Fatal("the warehouse outlived its user leaving")
	}
}

/*
================
TestStorageEndsWithARelocation

A teleport or GM move rebuilds the scene (relocateCharacter).
================
*/
func TestStorageEndsWithARelocation(t *testing.T) {
	f := newGMTeleportFixture(t)
	f.rt.openStorageSession(testDivision, f.gm.Name, 17)
	if !f.rt.GoTownGM(testDivision, f.gm.Name) {
		t.Fatal("fixture: the GM did not go to town")
	}
	if _, open := f.rt.storageSession(testDivision, f.gm.Name); open {
		t.Fatal("the warehouse outlived a relocation")
	}
}
