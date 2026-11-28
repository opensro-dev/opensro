/*
===========================================================================

exchange_test.go - a request, offers and the swap between two players

===========================================================================
*/
package action

import (
	"bytes"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
exchangeFixture

Two players side by side; the first carries a tradable sword in slot 20.
================
*/
func exchangeFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.Character, map[string][]wire.Frame) {
	t.Helper()
	a := testCharacter()
	b := testCharacter()
	b.ID, b.Name, b.MissionInventory = 4, "peer", nil
	items := testItems()
	sword := *items["ITEM_CH_SWORD_01_A_RARE"]
	sword.NativeFields = sword.NativeFields.With("canTrade", 1)
	items["ITEM_CH_SWORD_01_A_RARE"] = &sword
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {a, b}},
		Items:      items,
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, nil)
	clock := &fakeClock{}
	clock.now = clock.now.Add(1)
	rt.Now = clock.Now
	pushed := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) { pushed[name] = append(pushed[name], frames...) }
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	return rt, a, b, pushed
}

/*
================
lastFrame
================
*/
func lastFrame(t *testing.T, frames []wire.Frame, opcode uint16) wire.Frame {
	t.Helper()
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].Opcode == opcode {
			return frames[i]
		}
	}
	t.Fatalf("no 0x%04X among %d frames", opcode, len(frames))
	return wire.Frame{}
}

/*
================
TestExchangeSwapsOffers

The requested player accepts, the sword and 1000 gold go on the table,
both confirm and approve, and the sword lands in the partner's first
empty bag slot while the gold moves the other way.
================
*/
func TestExchangeSwapsOffers(t *testing.T) {
	rt, a, b, pushed := exchangeFixture(t)
	gidA, gidB := enterworld.ObjectIDForCharacter(a), enterworld.ObjectIDForCharacter(b)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(gidB).Payload())
	if prompt := lastFrame(t, pushed[b.Name], 0x3393); !bytes.Equal(prompt.Payload, wire.NewWriter(5).U8(1).U32(gidA).Payload()) {
		t.Fatalf("prompt % X", prompt.Payload)
	}
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	if opened := lastFrame(t, pushed[a.Name], wire.OpExchangeRequestResult); !bytes.Equal(opened.Payload, wire.EncodeExchangeResult(0, wire.EncodeExchangeGid(gidB)...)) {
		t.Fatalf("requester heard % X", opened.Payload)
	}
	lastFrame(t, pushed[b.Name], wire.OpExchangeOpened)

	put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 20})
	if len(put.Frames) != 1 || !bytes.Equal(put.Frames[0].Payload, []byte{1, 4, 20, 0}) {
		t.Fatalf("put answered %+v", put.Frames)
	}
	if offer := lastFrame(t, pushed[b.Name], wire.OpExchangeOffer); offer.Payload[4] != 1 {
		t.Fatalf("partner's list % X", offer.Payload)
	}
	// The partner's client decodes the listed sword through its own
	// reference table, so the sword's reference arrives before the list.
	if frames := pushed[b.Name]; len(frames) < 2 || !referencesName(frames[len(frames)-2], stallSlotItem(t, a, 20)) {
		t.Fatalf("partner heard no sword reference before the list: %+v", frames)
	}
	gold := rt.HandleItemMove(testDivision, b, wire.NewWriter(5).U8(wire.MoveTypeExchangeGold).U32(1000).Payload())
	if len(gold.Frames) != 1 || !bytes.Equal(lastFrame(t, pushed[a.Name], wire.OpExchangePartnerGold).Payload, []byte{2, 0xe8, 3, 0, 0}) {
		t.Fatalf("gold answered %+v", gold.Frames)
	}
	if moved := rt.HandleItemMove(testDivision, a, []byte{0, 20, 21, 1, 0}); len(moved.Frames) != 1 || moved.Frames[0].Payload[0] != 2 {
		t.Fatalf("a bag move during the exchange answered %+v", moved.Frames)
	}

	if early := rt.HandleExchangeApprove(testDivision, a, nil); early.Frames[0].Payload[0] != 2 {
		t.Fatal("approval before both confirmed")
	}
	rt.HandleExchangeConfirm(testDivision, a, nil)
	lastFrame(t, pushed[b.Name], wire.OpExchangePartnerLocked)
	rt.HandleExchangeConfirm(testDivision, b, nil)
	if first := rt.HandleExchangeApprove(testDivision, a, nil); !bytes.Equal(first.Frames[0].Payload, []byte{1}) {
		t.Fatalf("first approval %+v", first.Frames)
	}
	rt.HandleExchangeApprove(testDivision, b, nil)
	lastFrame(t, pushed[a.Name], wire.OpExchangeSucceeded)
	lastFrame(t, pushed[b.Name], wire.OpExchangeSucceeded)
	if len(a.MissionInventory) != 0 || len(b.MissionInventory) != 1 || b.MissionInventory[0].Slot != 13 {
		t.Fatalf("bags after the swap: %+v / %+v", a.MissionInventory, b.MissionInventory)
	}
	if goldOf(a) != 6000 || goldOf(b) != 4000 {
		t.Fatalf("gold after the swap: %d / %d", goldOf(a), goldOf(b))
	}
	if rt.Exchanges.Trading(testDivision, a.Name) {
		t.Fatal("the session outlived the swap")
	}
}

/*
================
TestExchangeRefusals

An untradable item stays off the table, a cancel tells the partner, and a
refused request tells the requester.
================
*/
func TestExchangeRefusals(t *testing.T) {
	rt, a, b, pushed := exchangeFixture(t)
	gidB := enterworld.ObjectIDForCharacter(b)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(gidB).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 2)
	if refused := lastFrame(t, pushed[a.Name], wire.OpExchangeRequestResult); !bytes.Equal(refused.Payload, []byte{2, wire.ExchangeErrDenied}) {
		t.Fatalf("refusal % X", refused.Payload)
	}
	if self := rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(a)).Payload()); !bytes.Equal(self.Frames[0].Payload, []byte{2, wire.ExchangeErrInvalidTarget}) {
		t.Fatalf("self request %+v", self.Frames)
	}
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(gidB).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	rt.deps.(*enterworld.Deps).Items.(staticItemSource)["ITEM_CH_SWORD_01_A_RARE"].NativeFields =
		rt.deps.(*enterworld.Deps).Items.(staticItemSource)["ITEM_CH_SWORD_01_A_RARE"].NativeFields.With("canTrade", 0)
	if put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 20}); !bytes.Equal(put.Frames[0].Payload, []byte{2, wire.ExchangeErrCannotTrade}) {
		t.Fatalf("an untradable item answered %+v", put.Frames)
	}
	if out := rt.HandleExchangeCancel(testDivision, a, nil); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("cancel %+v", out.Frames)
	}
	if failed := lastFrame(t, pushed[b.Name], wire.OpExchangeFailed); !bytes.Equal(failed.Payload, []byte{wire.ExchangeErrCancelledByPeer}) {
		t.Fatalf("partner heard % X", failed.Payload)
	}
}

/*
================
TestExchangeSummonedPetItemRefused

#4490 U18-01: a summoned pet's item offered to the table answers the
pet refusal (the native word 0x18B6 in its low byte), not the
generic cannot-trade byte an untradable item gets.
================
*/
func TestExchangeSummonedPetItemRefused(t *testing.T) {
	rt, a, b, _ := exchangeFixture(t)
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	rt.ExchangeConsent().ApplyConsent(nil, testDivision, b, 1, 1)
	for i := range a.MissionInventory {
		if a.MissionInventory[i].Slot == 20 {
			a.MissionInventory[i].Summon = &domain.CharacterCOS{Summoned: true}
		}
	}
	put := rt.HandleItemMove(testDivision, a, []byte{wire.MoveTypeExchangePut, 20})
	if len(put.Frames) != 1 || !bytes.Equal(put.Frames[0].Payload, []byte{2, wire.ExchangeErrSummonedPet}) {
		t.Fatalf("a summoned pet's item answered %+v, want the pet refusal", put.Frames)
	}
}

/*
================
TestExchangeAskInteractionStates

#4490 U18-10, the asker's own interaction state at the request door:
a stall visitor is refused with the stall byte, a dead asker and one
keeping a stall of their own with the asker-busy byte, and an asker
already inside an exchange hears nothing at all. The asked side in a
stall answers the asked busy byte.
================
*/
func TestExchangeAskInteractionStates(t *testing.T) {
	ask := func(rt *Runtime, from, to *enterworld.Character) OpResult {
		return rt.HandleExchangeRequest(testDivision, from, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(to)).Payload())
	}
	refused := func(t *testing.T, out OpResult, code uint8) {
		t.Helper()
		if len(out.Frames) != 1 || !bytes.Equal(out.Frames[0].Payload, []byte{2, code}) {
			t.Fatalf("ask answered %+v, want [02 %02x]", out.Frames, code)
		}
	}

	t.Run("visitor asker", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		if err := rt.Stalls.Open(testDivision, b.Name, "stall", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Stalls.Enter(testDivision, a.Name, b.Name); err != nil {
			t.Fatal(err)
		}
		refused(t, ask(rt, a, b), wire.ExchangeErrStallVisit)
	})

	t.Run("dead asker", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		zero := int64(0)
		a.CurrentHP = &zero
		refused(t, ask(rt, a, b), wire.ExchangeErrAskerBusy)
	})

	t.Run("keeper asker", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		if err := rt.Stalls.Open(testDivision, a.Name, "stall", 0); err != nil {
			t.Fatal(err)
		}
		refused(t, ask(rt, a, b), wire.ExchangeErrAskerBusy)
	})

	t.Run("trading asker hears nothing", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		rt.Exchanges.Open(testDivision, a.Name, b.Name)
		if out := ask(rt, a, b); len(out.Frames) != 0 {
			t.Fatalf("a trading asker was answered %+v, want silence", out.Frames)
		}
	})

	t.Run("visitor asked", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		keeper := testCharacter()
		keeper.ID, keeper.Name = 9, "keeper"
		if err := rt.Stalls.Open(testDivision, keeper.Name, "stall", 0); err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Stalls.Enter(testDivision, b.Name, keeper.Name); err != nil {
			t.Fatal(err)
		}
		refused(t, ask(rt, a, b), wire.ExchangeErrBusy)
	})

	t.Run("keeper asked", func(t *testing.T) {
		rt, a, b, _ := exchangeFixture(t)
		if err := rt.Stalls.Open(testDivision, b.Name, "stall", 0); err != nil {
			t.Fatal(err)
		}
		refused(t, ask(rt, a, b), wire.ExchangeErrBusy)
	})
}

/*
================
TestExchangeReaskCancelsTheEarlierAsk

#4490 U18-10: an asker with an ask still out cancels that ask and
asks again - the first target's slot is free, the second target
holds the live ask and got the prompt.
================
*/
func TestExchangeReaskCancelsTheEarlierAsk(t *testing.T) {
	rt, a, b, pushed := exchangeFixture(t)
	third := testCharacter()
	third.ID, third.Name, third.MissionInventory = 9, "third", nil
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters = enterworld.StaticCharacterSource{testDivision: {a, b, third}}

	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(b)).Payload())
	if !rt.Exchanges.Pending(testDivision, b.Name, rt.Now().UnixMilli()) {
		t.Fatal("the first ask was not parked")
	}
	rt.HandleExchangeRequest(testDivision, a, wire.NewWriter(4).U32(enterworld.ObjectIDForCharacter(third)).Payload())
	if rt.Exchanges.Pending(testDivision, b.Name, rt.Now().UnixMilli()) {
		t.Fatal("the first ask survived the re-ask")
	}
	if !rt.Exchanges.Pending(testDivision, third.Name, rt.Now().UnixMilli()) {
		t.Fatal("the second ask was not parked")
	}
	if prompt := lastFrame(t, pushed[third.Name], 0x3393); !bytes.Equal(prompt.Payload, wire.NewWriter(5).U8(1).U32(enterworld.ObjectIDForCharacter(a)).Payload()) {
		t.Fatalf("prompt % X", prompt.Payload)
	}
}

/*
================
stallSlotItem

The RefObjID a character holds in a bag slot.
================
*/
func stallSlotItem(t *testing.T, c *enterworld.Character, slot int) uint32 {
	t.Helper()
	for _, row := range c.MissionInventory {
		if int(row.Slot) == slot {
			return row.RefObjID
		}
	}
	t.Fatalf("%s holds nothing in slot %d", c.Name, slot)
	return 0
}

/*
================
referencesName

Whether a frame is an item reference delta that describes refObjID.
================
*/
func referencesName(frame wire.Frame, refObjID uint32) bool {
	return frame.Opcode == opCommerceItemReferences &&
		bytes.Contains(frame.Payload, []byte(fmt.Sprintf(`"refObjId":%d,`, refObjID)))
}
