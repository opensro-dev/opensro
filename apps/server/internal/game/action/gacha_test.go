/*
===========================================================================

gacha_test.go - Magic Pop roll and Gori voucher exchange behavior tests

Covers machine opening (0x7338), ticket rolls (0x7053), voucher exchange
(0x71BE), and the native equipment property roll (+1..+5 OptLevel, 5-bit
white-stat variance + durability, and blue magic options).

===========================================================================
*/

package action

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
gachaFixture
================
*/
func gachaFixture(t *testing.T) (*Runtime, *enterworld.Character, []byte) {
	t.Helper()
	// The retail-lane fixture: the port-only pool stays off unless a test
	// opts in (gachaPoolV2Fixture sets it per case).
	t.Setenv(EnvGachaPoolV2, "off")
	c := testCharacter()
	items := testItems()
	for i, name := range []string{gacha.CardCodename, gacha.WinCardCodename, gacha.LoseCardCodename} {
		variant := int64(2)
		if i == 0 {
			variant = 1
		}
		items[name] = &enterworld.ItemRef{RefObjID: uint32(900 + i), Codename: name, TypeIDs: [4]int64{3, 3, 14, variant}}
	}
	items[gacha.CardCodename].ParamDescriptions = [20]string{gacha.LoseCardCodename, gacha.WinCardCodename}
	rt, _ := newTestRuntime(c, gmItemSource{items})
	dir := t.TempDir()
	for name, value := range map[string]string{"gachaitemset.txt": "1 1 11459 5000 1 1\n1 2 11460 10000 1 2\n", "gachanpcmap.txt": "1 9251 1 2\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.ConfigureGacha(dir); err != nil {
		t.Fatal(err)
	}
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 9251, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.Selected.Set(testDivision, c.Name, 17)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: 900, Codename: gacha.CardCodename, TypeFlags: items[gacha.CardCodename].TypeFlags(), StackCount: 1}}
	return rt, c, wire.NewWriter(9).U32(17).U32(1).U8(13).Payload()
}

/*
================
assertGachaRefusal

A refused roll or exchange answers exactly one [2][u8 code] frame on its
reply opcode; the byte on the wire is the stage code's low byte. Before
#1897 it answered nothing and stranded the client.
================
*/
func assertGachaRefusal(t *testing.T, frames []wire.Frame, opcode uint16, code uint16) {
	t.Helper()
	if len(frames) != 1 || frames[0].Opcode != opcode {
		t.Fatalf("frames %+v, want one 0x%04X refusal", frames, opcode)
	}
	if want := []byte{2, byte(code)}; !reflect.DeepEqual(frames[0].Payload, want) {
		t.Fatalf("refusal payload %v, want %v", frames[0].Payload, want)
	}
}

/*
================
TestGachaCommitResultOrderAndReplay
================
*/
func TestGachaCommitResultOrderAndReplay(t *testing.T) {
	for _, roll := range []uint32{0, 4999, 5000, 9999} {
		t.Run(string(rune('A'+roll%26)), func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			draws := 0
			rt.GachaRoll = func() (uint32, error) { draws++; return roll, nil }
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason != "" {
				t.Fatal(reason)
			}
			assertOpcodes(t, frames, 0x3645, 0xb053)
			win := roll < 5000
			want := uint32(902)
			code := byte(0)
			if win {
				want = 901
				code = 1
			}
			if c.MissionInventory[0].RefObjID != want || !reflect.DeepEqual(frames[1].Payload, []byte{1, code}) {
				t.Fatalf("wrong result: %+v %+v", c.MissionInventory, frames)
			}
			reward := uint64(11460)
			wantDraws := 2
			if win {
				reward = 11459
				wantDraws = 1
			}
			if draws != wantDraws {
				t.Fatalf("draws=%d want %d", draws, wantDraws)
			}
			if !reflect.DeepEqual(c.MissionInventory[0].MagicOptions, []uint64{reward, 1}) {
				t.Fatal("reward lost")
			}
			before := c.Snapshot()
			again, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason == "" || draws != wantDraws || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatal("replayed ticket mutated state")
			}
			// The spent row is a result card now, not a ticket: 0x1806.
			assertGachaRefusal(t, again, gacha.OpResult, 0x1806)
		})
	}
}

/*
================
TestGachaAdmissionFailuresPreserveTicket

Every refusal answers the one native [2][u8 code] frame on 0xB053 and
leaves the character untouched. The codes follow the native order:
dispatcher (object, scope, range), then service, bag limit, empty slot,
ticket identity, entry. They are literals on purpose: the test pins the
wire values, not the server's names for them.
================
*/
func TestGachaAdmissionFailuresPreserveTicket(t *testing.T) {
	for _, tc := range []struct {
		kind string
		code uint16
	}{
		{"catalog", 5},
		{"random missing", 5},
		{"random failure", 5},
		{"random range", 5},
		{"no such gid", 0},
		{"outside scope", 3},
		{"disabled NPC", 0},
		{"too far", 4},
		{"entry", 2},
		{"slot", 0x1806},
		{"empty slot", 0x1809},
		{"ticket", 0x1806},
		{"delete pending", 5},
		{"truncated", 0x1806},
		{"trailing", 0x1806},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			rt.GachaRoll = func() (uint32, error) { return 0, nil }
			switch tc.kind {
			case "catalog":
				rt.GachaCatalog = nil
			case "random missing":
				rt.GachaRoll = nil
			case "random failure":
				rt.GachaRoll = func() (uint32, error) { return 0, errors.New("entropy unavailable") }
			case "random range":
				rt.GachaRoll = func() (uint32, error) { return 10000, nil }
			case "no such gid":
				p[0] = 99
			case "outside scope":
				rt.NpcRoster[0].Spawn.RegionID = 0
			case "disabled NPC":
				rt.NpcSpawn.Enabled = false
			case "too far":
				// Height only: sector visibility reads X/Z, the hit range is 3D.
				rt.NpcRoster[0].Spawn.Y += 400
			case "entry":
				p[4] = 99
			case "slot":
				p[8] = 1
			case "empty slot":
				c.MissionInventory = nil
			case "ticket":
				c.MissionInventory[0].RefObjID = 999
			case "delete pending":
				c.DeletePending = true
			case "truncated":
				p = p[:8]
			case "trailing":
				p = append(p, 0)
			}
			before := c.Snapshot()
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason == "" || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatalf("refusal changed state: %s %+v", reason, frames)
			}
			assertGachaRefusal(t, frames, gacha.OpResult, tc.code)
		})
	}
}

/*
================
TestGachaRollIgnoresTheSelection

The #1897 sequence: the player opens the machine, closes the NPC talk
window (0x74B3 releases the selection) and then rolls. The reference server
never consults the selection on the roll, so the roll still lands; before
the fix the port refused it and sent nothing, stranding the window.
================
*/
func TestGachaRollIgnoresTheSelection(t *testing.T) {
	for _, kind := range []string{"released", "moved"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			rt.GachaRoll = func() (uint32, error) { return 0, nil }
			switch kind {
			case "released":
				rt.Selected.Clear(testDivision, c.Name)
			case "moved":
				rt.Selected.Set(testDivision, c.Name, 18)
			}
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason != "" {
				t.Fatal(reason)
			}
			assertOpcodes(t, frames, 0x3645, 0xb053)
			if c.MissionInventory[0].RefObjID != rt.GachaCatalog.WinCard.RefObjID {
				t.Fatalf("roll did not land: %+v", c.MissionInventory)
			}
		})
	}
}

/*
================
TestGachaOpeningUsesWorldAdmission
================
*/
func TestGachaOpeningUsesWorldAdmission(t *testing.T) {
	rt, c, _ := gachaFixture(t)
	p := wire.NewWriter(8).U32(17).U32(0x10000).Payload()
	frames, reason := rt.HandleGachaNpcAction(testDivision, c, p)
	if reason != "" {
		t.Fatal(reason)
	}
	assertOpcodes(t, frames, 0xb338)
	rt.NpcRoster[0].Spawn.RegionID = 0
	if frames, reason = rt.HandleGachaNpcAction(testDivision, c, p); reason == "" || len(frames) != 0 {
		t.Fatal("out-of-scope machine admitted")
	}
}

/*
================
TestGachaRewardsAreAdmittedToWorldReferences
================
*/
func TestGachaRewardsAreAdmittedToWorldReferences(t *testing.T) {
	rt, _, _ := gachaFixture(t)
	counts := map[string]int{}
	for _, name := range rt.StaticRefItemCodenames() {
		counts[name]++
	}
	for _, name := range rt.GachaCatalog.RewardCodenames() {
		if counts[name] != 1 {
			t.Fatalf("reward %s admitted %d times", name, counts[name])
		}
	}
}

/*
================
TestPublishedRewardsSendNoGroundReferenceDelta
================
*/
func TestPublishedRewardsSendNoGroundReferenceDelta(t *testing.T) {
	rt, _, _ := gachaFixture(t)
	rewards := rt.GachaCatalog.RewardCodenames()
	if len(rewards) == 0 {
		t.Fatal("fixture has no rewards")
	}
	published := grounditem.Item{RefObjID: 70001, Codename: rewards[0], TypeFlags: wire.PackTypeFlags(3, 3, 1, 1)}
	if frames := rt.groundReferences([]grounditem.Item{published}); len(frames) != 0 {
		t.Fatalf("published reward resent as %d reference frames", len(frames))
	}
	unpublished := grounditem.Item{RefObjID: 50080, Codename: "ITEM_CH_SWORD_09_B", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2)}
	if frames := rt.groundReferences([]grounditem.Item{unpublished}); len(frames) != 1 {
		t.Fatalf("unpublished drop produced %d reference frames, want 1", len(frames))
	}
}

/*
================
TestGachaFailedWasteDrawPreservesTicket
================
*/
func TestGachaFailedWasteDrawPreservesTicket(t *testing.T) {
	for _, bad := range []bool{false, true} {
		rt, c, p := gachaFixture(t)
		calls := 0
		rt.GachaRoll = func() (uint32, error) {
			calls++
			if calls == 1 {
				return 5000, nil
			}
			if bad {
				return 10000, nil
			}
			return 0, errors.New("waste entropy unavailable")
		}
		before := c.Snapshot()
		frames, reason := rt.HandleGachaRoll(testDivision, c, p)
		if reason == "" || calls != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatalf("failed waste draw committed: %v %s", frames, reason)
		}
		assertGachaRefusal(t, frames, gacha.OpResult, 5)
	}
}

// operatorFixture is the roll fixture with Gori beside the machine and a voucher in the bag.
/*
================
operatorFixture
================
*/
func operatorFixture(t *testing.T, roll uint32) (*Runtime, *enterworld.Character, []byte) {
	t.Helper()
	rt, c, p := gachaFixture(t)
	rt.GachaRoll = func() (uint32, error) { return roll, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	rt.NpcRoster = append(rt.NpcRoster, simulation.NpcDef{
		ObjectID: 18, RefObjID: 9252, Codename: gachaOperatorCodename, AuthoredSpawn: true,
		Spawn: simulation.SeedWorldState(c).Spawn,
	})
	rt.Selected.Set(testDivision, c.Name, 18)
	return rt, c, wire.NewWriter(4).U32(18).Payload()
}

/*
================
TestGachaExchangeSwapsTheVoucherForItsRecordedReward
================
*/
func TestGachaExchangeSwapsTheVoucherForItsRecordedReward(t *testing.T) {
	for _, tc := range []struct {
		name   string
		roll   uint32
		reward uint32
	}{{"win", 0, 11459}, {"loss", 9999, 11460}} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, p := operatorFixture(t, tc.roll)
			frames, reason := rt.HandleGachaExchange(testDivision, c, p)
			if reason != "" {
				t.Fatal(reason)
			}
			rewardSlot := int64(-1)
			for _, row := range c.MissionInventory {
				if rt.GachaCatalog.IsResultCard(row.RefObjID) {
					t.Fatalf("voucher survived the exchange: %+v", row)
				}
				if row.RefObjID == tc.reward && row.StackCount == 1 {
					rewardSlot = row.Slot
				}
			}
			// The voucher's freed slot is the first free one, so the reward takes it.
			if rewardSlot != 13 {
				t.Fatalf("reward %d at slot %d, want the voucher's slot 13: %+v", tc.reward, rewardSlot, c.MissionInventory)
			}
			quantity := -1
			for index, frame := range frames {
				if frame.Opcode == 0x3645 {
					quantity = index
					if !reflect.DeepEqual(frame.Payload, []byte{13, 8, 0, 0}) {
						t.Fatalf("voucher update %v", frame.Payload)
					}
				}
				if frame.Opcode == wire.OpItemMoveResponse && reflect.DeepEqual(frame.Payload, []byte{1, 15, 13, 0}) {
					t.Fatal("voucher left by a row delete, which client 7654B0 does not turn into the exchange notice")
				}
				// A frame writing slot 13 before the 0x3645 would let the client zero the reward.
				if frame.Opcode == wire.OpItemMoveResponse && len(frame.Payload) > 2 && frame.Payload[2] == 13 && quantity < 0 {
					t.Fatalf("frame %d writes slot 13 before the voucher's 0x3645: %+v", index, frames)
				}
			}
			if quantity != 0 {
				t.Fatalf("the voucher's 0x3645 is frame %d, want first: %+v", quantity, frames)
			}
		})
	}
}

/*
================
TestGachaExchangeEquipmentRollsNativeSpecialProperties
================
*/
func TestGachaExchangeEquipmentRollsNativeSpecialProperties(t *testing.T) {
	for _, tc := range []struct {
		name     string
		plusDraw [2]uint32
		wantPlus int64
		varDraw  uint32
		wantTier uint8
	}{
		{"min_of_4_and_2", [2]uint32{3, 1}, 2, 16384, 15},
		{"max_plus_5", [2]uint32{4, 4}, 5, 32767, 31},
		{"min_plus_1", [2]uint32{0, 4}, 1, 8192, 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, p := operatorFixture(t, 0)
			durMin := int64(55)
			sword := rt.deps.ItemReferences().(gmItemSource).staticItemSource["ITEM_CH_SWORD_01_A_RARE"]
			sword.VarianceIntMin1c0 = &durMin
			sword.MaxDurability = 69

			// 2 plus draws, then 21 weapon variance draws (7 fields * 3 draws),
			// then magic option count (4 draws of 0 -> 1 slot) and pool/million/value draws.
			seq := []uint32{tc.plusDraw[0], tc.plusDraw[1]}
			for i := 0; i < 21; i++ {
				seq = append(seq, tc.varDraw)
			}
			idx := 0
			rt.DropRoll = func() (uint32, error) {
				if idx < len(seq) {
					v := seq[idx]
					idx++
					return v, nil
				}
				return 0, nil
			}

			frames, reason := rt.HandleGachaExchange(testDivision, c, p)
			if reason != "" {
				t.Fatal(reason)
			}
			var granted *enterworld.InventoryRow
			for i := range c.MissionInventory {
				if c.MissionInventory[i].RefObjID == 11459 {
					granted = &c.MissionInventory[i]
					break
				}
			}
			if granted == nil {
				t.Fatalf("missing granted equipment row: %+v", c.MissionInventory)
			}
			if granted.Plus != tc.wantPlus {
				t.Fatalf("Plus = %d, want %d", granted.Plus, tc.wantPlus)
			}
			var wantBits uint64
			for field := 0; field < 7; field++ {
				wantBits |= uint64(tc.wantTier) << (field * 5)
			}
			gotBits, err := strconv.ParseUint(granted.VarianceBits, 10, 64)
			if err != nil || gotBits != wantBits {
				t.Fatalf("VarianceBits = %q (%d), want %d (err=%v)", granted.VarianceBits, gotBits, wantBits, err)
			}
			wantDur := int64(combat.DurabilityFromVariance(sword, tc.wantTier))
			if granted.Durability != wantDur {
				t.Fatalf("Durability = %d, want %d", granted.Durability, wantDur)
			}
			if len(granted.MagicOptions) == 0 {
				t.Fatalf("Rarity == 2 equipment granted without magic options: %+v", *granted)
			}

			wantBody := (&inventory.Item{
				Slot:         uint8(granted.Slot),
				RefObjID:     granted.RefObjID,
				Codename:     granted.Codename,
				TypeFlags:    granted.TypeFlags,
				Quantity:     uint16(granted.StackCount),
				Plus:         uint8(granted.Plus),
				VarianceBits: gotBits,
				Durability:   uint32(granted.Durability),
				MagicOptions: granted.MagicOptions,
			}).Body().Encode()
			wantGrantPayload := append([]byte{1, 14, uint8(granted.Slot), 0}, wantBody...)
			foundGrantFrame := false
			for _, frame := range frames {
				if frame.Opcode == wire.OpItemMoveResponse && bytes.Equal(frame.Payload, wantGrantPayload) {
					foundGrantFrame = true
					break
				}
			}
			if !foundGrantFrame {
				t.Fatalf("OpItemMoveResponse grant frame did not encode rolled equipment body: %+v", frames)
			}
		})
	}
}

/*
================
TestGachaExchangeNonEquipmentSkipsSpecialPropertyRolls
================
*/
func TestGachaExchangeNonEquipmentSkipsSpecialPropertyRolls(t *testing.T) {
	t.Setenv(EnvGachaPoolV2, "off")
	c := testCharacter()
	items := testItems()
	for i, name := range []string{gacha.CardCodename, gacha.WinCardCodename, gacha.LoseCardCodename} {
		variant := int64(2)
		if i == 0 {
			variant = 1
		}
		items[name] = &enterworld.ItemRef{RefObjID: uint32(900 + i), Codename: name, TypeIDs: [4]int64{3, 3, 14, variant}}
	}
	items[gacha.CardCodename].ParamDescriptions = [20]string{gacha.LoseCardCodename, gacha.WinCardCodename}
	rt, _ := newTestRuntime(c, gmItemSource{items})
	dir := t.TempDir()
	for name, value := range map[string]string{
		"gachaitemset.txt": "1 1 11459 5000 1 1\n1 2 3630 10000 3 2\n",
		"gachanpcmap.txt":  "1 9251 1 2\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.ConfigureGacha(dir); err != nil {
		t.Fatal(err)
	}
	rt.NpcSpawn.Enabled = true
	spawn := simulation.SeedWorldState(c).Spawn
	rt.NpcRoster = []simulation.NpcDef{
		{ObjectID: 17, RefObjID: 9251, AuthoredSpawn: true, Spawn: spawn},
		{ObjectID: 18, RefObjID: 9252, Codename: gachaOperatorCodename, AuthoredSpawn: true, Spawn: spawn},
	}
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot:         13,
		RefObjID:     rt.GachaCatalog.LoseCard.RefObjID,
		Codename:     rt.GachaCatalog.LoseCard.Codename,
		TypeFlags:    rt.GachaCatalog.LoseCard.TypeFlags,
		StackCount:   1,
		MagicOptions: []uint64{3630, 3},
	}}
	rt.Selected.Set(testDivision, c.Name, 18)
	// Non-equipment rewards must not touch DropRoll at all.
	rt.DropRoll = nil

	frames, reason := rt.HandleGachaExchange(testDivision, c, wire.NewWriter(4).U32(18).Payload())
	if reason != "" || len(frames) == 0 {
		t.Fatalf("non-equipment exchange failed: %q %+v", reason, frames)
	}
	var granted *enterworld.InventoryRow
	for i := range c.MissionInventory {
		if c.MissionInventory[i].RefObjID == 3630 {
			granted = &c.MissionInventory[i]
			break
		}
	}
	if granted == nil || granted.StackCount != 3 || granted.Plus != 0 || granted.Durability != 0 || len(granted.MagicOptions) != 0 {
		t.Fatalf("unexpected non-equipment grant row: %+v", granted)
	}
}

/*
================
TestGachaExchangeTakesTheLowestSlotVoucherFirst
================
*/
func TestGachaExchangeTakesTheLowestSlotVoucherFirst(t *testing.T) {
	rt, c, p := operatorFixture(t, 0)
	extra := c.MissionInventory[0]
	extra.Slot = 20
	extra.MagicOptions = []uint64{11460, 1}
	extra.RefObjID, extra.Codename = rt.GachaCatalog.LoseCard.RefObjID, rt.GachaCatalog.LoseCard.Codename
	c.MissionInventory = append(c.MissionInventory, extra)
	if _, reason := rt.HandleGachaExchange(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	for _, row := range c.MissionInventory {
		if row.RefObjID == 11459 {
			return
		}
	}
	t.Fatalf("slot 13's win reward was not granted first: %+v", c.MissionInventory)
}

/*
================
TestGachaExchangeConsumesTheVoucherItRead
================
*/
func TestGachaExchangeConsumesTheVoucherItRead(t *testing.T) {
	rt, c, p := operatorFixture(t, 0)
	// The paid voucher sits at slot 20; a same-codename card without a pair
	// (an operator grant, say) sits below it at slot 13.
	c.MissionInventory[0].Slot = 20
	blank := c.MissionInventory[0]
	blank.Slot = 13
	blank.MagicOptions = nil
	c.MissionInventory = append(c.MissionInventory, blank)
	if _, reason := rt.HandleGachaExchange(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	rewards := 0
	var blankKept, voucherKept bool
	for _, row := range c.MissionInventory {
		switch {
		case row.RefObjID == 11459:
			rewards += int(row.StackCount)
		case row.Slot == 13 && row.RefObjID == blank.RefObjID && len(row.MagicOptions) == 0:
			blankKept = true
		case row.Slot == 20 && rt.GachaCatalog.IsResultCard(row.RefObjID):
			voucherKept = true
		}
	}
	if rewards != 1 || !blankKept || voucherKept {
		t.Fatalf("rewards=%d blank kept=%v voucher kept=%v: %+v", rewards, blankKept, voucherKept, c.MissionInventory)
	}
	before := c.Snapshot()
	frames, reason := rt.HandleGachaExchange(testDivision, c, p)
	if reason == "" || !reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("the blank card paid out the voucher's reward a second time")
	}
	// No exchangeable voucher is left: the native refusal 5 on 0xB1BE.
	assertGachaRefusal(t, frames, gacha.OpExchangeResult, 5)
}

/*
================
TestGachaExchangeRefusalsPreserveTheVoucher

Every refusal answers the one native [2][u8 code] frame on 0xB1BE and
leaves the character untouched. The codes are literals on purpose: the
test pins the wire values, not the server's names for them.
================
*/
func TestGachaExchangeRefusalsPreserveTheVoucher(t *testing.T) {
	for _, tc := range []struct {
		kind string
		code uint16
	}{
		{"machine", 0x1C0F},
		{"no such gid", 0},
		{"outside scope", 3},
		{"too far", 4},
		{"no voucher", 5},
		{"bad pair", 5},
		{"unlisted pair", 5},
		{"stacked", 5},
		{"delete pending", 5},
		{"truncated", 0x1806},
		{"trailing", 0x1806},
		{"catalog", 5},
		{"drop random missing", 5},
		{"drop random failure", 5},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			rt, c, p := operatorFixture(t, 0)
			switch tc.kind {
			case "machine":
				p = wire.NewWriter(4).U32(17).Payload()
			case "no such gid":
				p = wire.NewWriter(4).U32(99).Payload()
			case "outside scope":
				rt.NpcRoster[1].Spawn.RegionID = 0
			case "too far":
				// Height only: sector visibility reads X/Z, the hit range is 3D.
				rt.NpcRoster[1].Spawn.Y += 400
			case "no voucher":
				c.MissionInventory = nil
			case "bad pair":
				c.MissionInventory[0].MagicOptions = []uint64{11459, 0}
			case "unlisted pair":
				// Resolvable and in range, but no prize row names it: a pair no roll writes.
				c.MissionInventory[0].MagicOptions = []uint64{uint64(rt.GachaCatalog.Card.RefObjID), 1}
			case "stacked":
				c.MissionInventory[0].StackCount = 2
			case "delete pending":
				c.DeletePending = true
			case "truncated":
				p = p[:3]
			case "trailing":
				p = append(p, 0)
			case "catalog":
				rt.GachaCatalog = nil
			case "drop random missing":
				rt.DropRoll = nil
			case "drop random failure":
				rt.DropRoll = func() (uint32, error) { return 0, errors.New("drop entropy unavailable") }
			}
			before := c.Snapshot()
			frames, reason := rt.HandleGachaExchange(testDivision, c, p)
			if reason == "" || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatalf("refusal changed state: %q %+v", reason, frames)
			}
			assertGachaRefusal(t, frames, gacha.OpExchangeResult, tc.code)
		})
	}
}

/*
================
TestGachaExchangeIgnoresTheSelection

The exchange names its operator by gid and checks range and service only;
a selection left on the machine (or released) does not refuse it.
================
*/
func TestGachaExchangeIgnoresTheSelection(t *testing.T) {
	rt, c, p := operatorFixture(t, 0)
	rt.Selected.Set(testDivision, c.Name, 17)
	frames, reason := rt.HandleGachaExchange(testDivision, c, p)
	if reason != "" {
		t.Fatal(reason)
	}
	if len(frames) == 0 {
		t.Fatal("exchange answered nothing")
	}
	for _, row := range c.MissionInventory {
		if rt.GachaCatalog.IsResultCard(row.RefObjID) {
			t.Fatalf("voucher survived the exchange: %+v", row)
		}
	}
}

/*
================
TestGachaExchangeReplayFindsNoVoucher
================
*/
func TestGachaExchangeReplayFindsNoVoucher(t *testing.T) {
	rt, c, p := operatorFixture(t, 0)
	if _, reason := rt.HandleGachaExchange(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	before := c.Snapshot()
	frames, reason := rt.HandleGachaExchange(testDivision, c, p)
	if reason == "" || !reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("replay granted a second reward")
	}
	assertGachaRefusal(t, frames, gacha.OpExchangeResult, 5)
}

/*
================
TestGachaPoolV2FromEnv

Unset and anything but an explicit off leaves the port-only pool on;
"off" restores the retail pool.
================
*/
func TestGachaPoolV2FromEnv(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want bool
	}{{"", true}, {"on", true}, {"ON", true}, {"1", true}, {"off", false}, {" Off ", false}, {"0", false}, {"false", false}, {"no", false}} {
		t.Setenv(EnvGachaPoolV2, tc.env)
		if got := GachaPoolV2FromEnv(); got != tc.want {
			t.Fatalf("%s=%q: %v, want %v", EnvGachaPoolV2, tc.env, got, tc.want)
		}
	}
}

// gachaPoolV2Fixture is the roll fixture over the shipped-shaped pool:
// set-1 entries 1-6 at 9/8 and the 1800-weight grain rows 15/16.
func gachaPoolV2Fixture(t *testing.T) (*Runtime, *enterworld.Character, []byte) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	for i, name := range []string{gacha.CardCodename, gacha.WinCardCodename, gacha.LoseCardCodename} {
		variant := int64(2)
		if i == 0 {
			variant = 1
		}
		items[name] = &enterworld.ItemRef{RefObjID: uint32(900 + i), Codename: name, TypeIDs: [4]int64{3, 3, 14, variant}}
	}
	items[gacha.CardCodename].ParamDescriptions = [20]string{gacha.LoseCardCodename, gacha.WinCardCodename}
	for name, id := range map[string]uint32{
		"WEAPON_A": 4140, "WEAPON_B": 4035, "WEAPON_C": 4104,
		"WEAPON_D": 4071, "WEAPON_E": 4176, "WEAPON_F": 4215,
		"GRAIN_A": 9, "GRAIN_B": 16, "FILLER": 3823,
		"ITEM_CH_TBLADE_09_A_RARE": 4146, "ITEM_CH_SWORD_09_A_RARE": 4038,
		"ITEM_CH_SPEAR_09_A_RARE": 4110, "ITEM_CH_BLADE_09_A_RARE": 4074,
		"ITEM_CH_BOW_09_A_RARE": 4182, "ITEM_CH_SHIELD_09_A_RARE": 4218,
		"ITEM_MALL_HWAN": 9259,
	} {
		items[name] = &enterworld.ItemRef{RefObjID: id, Codename: name, TypeIDs: [4]int64{3, 1, 6, 2}}
	}
	// The shipped Hwan is no weapon: TID 3/3/13/13 with Param1 1 (its
	// item-use family), as itemdata_10000.txt row 9259 carries it.
	items["ITEM_MALL_HWAN"].TypeIDs = [4]int64{3, 3, 13, 13}
	items["ITEM_MALL_HWAN"].NativeFields = enterworld.NewNativeFields(map[string]float64{"itemParam1_29c": 1})
	rt, _ := newTestRuntime(c, gmItemSource{items})
	dir := t.TempDir()
	prizes := "1 1 4140 9 1 1\n1 1 4035 8 1 2\n1 1 4104 9 1 3\n1 1 4071 8 1 4\n" +
		"1 1 4176 9 1 5\n1 1 4215 8 1 6\n1 2 9 1800 50 15\n1 2 16 1800 50 16\n1 2 3823 6400 1 26\n"
	for name, value := range map[string]string{"gachaitemset.txt": prizes, "gachanpcmap.txt": "1 9251 1 2\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.ConfigureGacha(dir); err != nil {
		t.Fatal(err)
	}
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 9251, AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}}
	rt.Selected.Set(testDivision, c.Name, 17)
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 13, RefObjID: 900, Codename: gacha.CardCodename, TypeFlags: items[gacha.CardCodename].TypeFlags(), StackCount: 1}}
	return rt, c, wire.NewWriter(9).U32(17).U32(1).U8(13).Payload()
}

func TestGachaPoolV2RollPaysTheStarWeapon(t *testing.T) {
	t.Setenv(EnvGachaPoolV2, "on")
	rt, c, p := gachaPoolV2Fixture(t)
	if !rt.GachaPoolV2 {
		t.Fatal("pool v2 not marked active")
	}
	// Roll 29 beats the v2 chance of 30 but loses to the retail 9.
	rt.GachaRoll = func() (uint32, error) { return 29, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	if !reflect.DeepEqual(c.MissionInventory[0].MagicOptions, []uint64{4146, 1}) {
		t.Fatalf("v2 win recorded %+v, want the TBLADE Star 4146 x1", c.MissionInventory[0].MagicOptions)
	}
}

func TestGachaPoolOffRollKeepsTheRetailPool(t *testing.T) {
	t.Setenv(EnvGachaPoolV2, "off")
	rt, c, p := gachaPoolV2Fixture(t)
	if rt.GachaPoolV2 {
		t.Fatal("pool v2 marked active while off")
	}
	prize, ok := rt.GachaCatalog.PrizeForNpc(9251, 1)
	if !ok || prize.RewardRefObjID != 4140 || prize.ChancePer10000 != 9 {
		t.Fatalf("retail entry 1 = %#v ok=%v", prize, ok)
	}
	rt.GachaRoll = func() (uint32, error) { return 8, nil }
	if _, reason := rt.HandleGachaRoll(testDivision, c, p); reason != "" {
		t.Fatal(reason)
	}
	if !reflect.DeepEqual(c.MissionInventory[0].MagicOptions, []uint64{4140, 1}) {
		t.Fatalf("retail win recorded %+v, want 4140 x1", c.MissionInventory[0].MagicOptions)
	}
}
