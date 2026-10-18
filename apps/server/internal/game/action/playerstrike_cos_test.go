/*
===========================================================================

playerstrike_cos_test.go - a player's hits on a COS, judged through its owner

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
assertRefusalCode
================
*/
func assertRefusalCode(t *testing.T, frames []wire.Frame, code uint16) {
	t.Helper()
	want := offensiveRefusal(code).Frames[0]
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && bytes.Equal(frame.Payload, want.Payload) {
			return
		}
	}
	t.Fatalf("frames %v did not include refusal %#x", frames, code)
}

/*
================
equipStrikeTestCOS

equipCombatTestPet over the runtime's own items: the combat fixture's
weapons stay, so the attacker keeps a valid loadout.
================
*/
func equipStrikeTestCOS(t *testing.T, rt *Runtime, character *enterworld.Character) *enterworld.CharacterRef {
	t.Helper()
	deps := rt.deps.(*enterworld.Deps)
	base, ok := deps.Items.(staticItemSource)
	if !ok {
		t.Fatalf("fixture items are %T, want staticItemSource", deps.Items)
	}
	ref := equipCombatTestPet(t, rt, character, cosBandTransport)
	deps.Items = cosTestItemSource{staticItemSource: base, characters: map[string]*enterworld.CharacterRef{ref.Codename: ref}}
	return ref
}

/*
================
TestPlayerBasicAttackStrikesAnotherPlayersCOS

A player's basic attack on another player's summoned COS lands on the COS's
own body: the COS loses HP, its owner does not, and the owner hears its
companion's private frames.
================
*/
func TestPlayerBasicAttackStrikesAnotherPlayersCOS(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	ref := equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: cos.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if cos.CurrentHP >= ref.MaxHP {
		t.Fatalf("COS HP %d, want below %d", cos.CurrentHP, ref.MaxHP)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("owner HP %d, want the untouched 100: the COS takes the hit", *owner.CurrentHP)
	}
}

/*
================
TestCOSTargetIsJudgedThroughItsOwner

5291D0 then 5293A0 on the owner: an attacker in body mode 2, a target COS in
body mode 2..4, or an owner in body mode 2..4 refuses with 0x3020 and the
COS keeps its HP; hidden modes 6..7 do not resolve as a target; a player's
own COS is never a target.
================
*/
func TestCOSTargetIsJudgedThroughItsOwner(t *testing.T) {
	for _, body := range []uint8{0, 1, 2, 3, 4, 5, 6, 7} {
		t.Run(fmt.Sprintf("cos body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			owner.ActiveCOS.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if body >= mercenaryBodyHiddenFirst && body <= mercenaryBodyHiddenLast {
				if ok {
					t.Fatalf("hidden COS body=%d resolved as a target", body)
				}
				return
			}
			if !ok || target.cos == nil {
				t.Fatalf("COS body=%d must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body >= mercenaryBodyProtectedFirst && body <= mercenaryBodyProtectedLast {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(cos body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("protected COS body=%d HP %d, want untouched %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			} else if owner.ActiveCOS.CurrentHP >= ref.MaxHP {
				t.Fatalf("unprotected COS body=%d HP %d, want below %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
			}
		})
	}
	for _, body := range []uint8{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("owner body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			owner.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if !ok || target.cos == nil {
				t.Fatalf("owner body=%d: the COS must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body >= mercenaryBodyProtectedFirst && body <= mercenaryBodyProtectedLast {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(owner body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("protected owner body=%d COS HP %d, want untouched %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			}
		})
	}
	for _, body := range []uint8{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("attacker body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			attacker.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if !ok || target.cos == nil {
				t.Fatalf("attacker body=%d: the COS must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body == untouchableBodyStatus {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(attacker body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("untouchable attacker COS HP %d, want untouched %d", owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			}
		})
	}
	t.Run("own COS", func(t *testing.T) {
		rt, _, attacker, _ := newPvpPair(t)
		equipStrikeTestCOS(t, rt, attacker)
		if _, ok := rt.resolveCombatTarget(testDivision, attacker, attacker.ActiveCOS.GID, 0); ok {
			t.Fatal("a player's own COS resolved as a target")
		}
	})
}

/*
================
TestThiefCannotStrikeALevelOneCaravanTransport

The 1-star caravan protection (52B760) is the owner's, so it holds for the
transport too: a dressed thief is refused against a dressed trader's
transport carrying a level-1 caravan (0x3024), and admitted once the
transport is empty.
================
*/
func TestThiefCannotStrikeALevelOneCaravanTransport(t *testing.T) {
	rt, _, trader := caravanFixture(t)
	thief := *trader
	thief.ID, thief.Name, thief.ActiveCOS = trader.ID+1, "thief", nil
	items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
	for i, c := range []*enterworld.Character{trader, &thief} {
		suit := &enterworld.ItemRef{RefObjID: uint32(9301 + i), Codename: []string{"ITEM_CH_M_TRADE_TRADER_05", "ITEM_CH_M_TRADE_THIEF_05"}[i],
			Country: 3, TypeIDs: [4]int64{3, 1, 7, int64(1 + i)}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
		items[suit.Codename] = suit
		c.MissionInventory = append(append([]enterworld.InventoryRow(nil), c.MissionInventory...), enterworld.InventoryRow{
			Slot: int64(jobSuitSlot), RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
	}
	for _, c := range []*enterworld.Character{trader, &thief} {
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) {
				w.Spawn = simulation.Spawn{RegionID: pvpRegion, X: 960, Y: 20, Z: 458}
				w.SpawnSet = true
			})
	}
	target, ok := rt.resolveCombatTarget(testDivision, &thief, trader.ActiveCOS.GID, 0)
	if !ok || target.cos == nil || target.player != trader {
		t.Fatalf("the ridden transport must resolve as a COS target owned by the trader: %+v", target)
	}
	if code := rt.cosTargetRefusal(testDivision, &thief, target, 0); code != 0x3024 {
		t.Fatalf("thief on a level-1 caravan's transport = %#x, want 0x3024", code)
	}
	// A target is a detached snapshot: re-resolve after the cargo leaves.
	trader.ActiveCOS.Container.Rows = nil
	if target, ok = rt.resolveCombatTarget(testDivision, &thief, trader.ActiveCOS.GID, 0); !ok {
		t.Fatal("the empty transport must still resolve")
	}
	if code := rt.cosTargetRefusal(testDivision, &thief, target, 0); code != 0 {
		t.Fatalf("thief on an empty transport = %#x, want admitted", code)
	}
}

/*
================
TestPlayerKillOfATransportScattersItsCargo

A player's fatal hit on a transport settles through the companion owner's
death, as a monster's does: 4C42F0 -> 4D1FD0 drops the goods unowned around
it and 52A000's kind-1 release deletes the record. The owner is untouched.
================
*/
func TestPlayerKillOfATransportScattersItsCargo(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	transport := owner.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	transport.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	transport.CurrentHP = 1
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if owner.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	ground := rt.Ground.All(testDivision)
	if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
		t.Fatalf("cargo %+v, want the 7 potions unowned", ground)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("owner HP %d, want 100", *owner.CurrentHP)
	}
}

/*
================
TestPlayerKillOfACOSEasesTheKillersPenalty

4C42F0 -> 4EB6B0 for a player's killing blow on a COS: a caster with a
murder penalty eases it by the gap to the companion's level and hears its
0x30F2; a blow that does not kill eases nothing.
================
*/
func TestPlayerKillOfACOSEasesTheKillersPenalty(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	transport := owner.ActiveCOS
	transport.Level = uint8(rewardLevel(attacker)) + 4
	attacker.PK = &domain.PKRecord{Penalty: 100}
	transport.CurrentHP = 1
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if attacker.PK.Penalty != 90 {
		t.Fatalf("penalty %d after the kill, want 90", attacker.PK.Penalty)
	}
	if _, ok := findFrame(out.Frames, wire.OpPKPenalty); !ok {
		t.Fatal("no 0x30F2 for the killer")
	}
}

/*
================
TestPlayerAttackOnAMountedPlayerStrikesTheRide

59B5F3: a player's single-target attack aimed at a player riding a
transport lands on the transport; the rider keeps every HP.
================
*/
func TestPlayerAttackOnAMountedPlayerStrikesTheRide(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	ref := equipStrikeTestCOS(t, rt, owner)
	ride := owner.ActiveCOS
	ride.Mounted = true
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(owner)}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if ride.CurrentHP >= ref.MaxHP {
		t.Fatalf("ride HP %d, want below %d: the mounted rider's hit lands on the ride", ride.CurrentHP, ref.MaxHP)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("rider HP %d, want the untouched 100", *owner.CurrentHP)
	}
}
