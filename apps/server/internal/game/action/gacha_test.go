/*
===========================================================================

gacha_test.go - Magic Pop roll and Gori voucher exchange behavior tests

Covers machine opening (0x7338), ticket rolls (0x7053), voucher exchange
(0x71BE), and the 725720 equipment property roll (+1..+5 OptLevel, 5-bit
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
			if reason == "" || len(again) != 0 || draws != wantDraws || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatal("replayed ticket mutated state")
			}
		})
	}
}

/*
================
TestGachaAdmissionFailuresPreserveTicket
================
*/
func TestGachaAdmissionFailuresPreserveTicket(t *testing.T) {
	for _, kind := range []string{"catalog", "random missing", "random failure", "random range", "selection", "outside scope", "disabled NPC", "entry", "slot", "ticket", "delete pending", "truncated", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, p := gachaFixture(t)
			rt.GachaRoll = func() (uint32, error) { return 0, nil }
			switch kind {
			case "catalog":
				rt.GachaCatalog = nil
			case "random missing":
				rt.GachaRoll = nil
			case "random failure":
				rt.GachaRoll = func() (uint32, error) { return 0, errors.New("entropy unavailable") }
			case "random range":
				rt.GachaRoll = func() (uint32, error) { return 10000, nil }
			case "selection":
				rt.Selected.Set(testDivision, c.Name, 18)
			case "outside scope":
				rt.NpcRoster[0].Spawn.RegionID = 0
			case "disabled NPC":
				rt.NpcSpawn.Enabled = false
			case "entry":
				p[4] = 99
			case "slot":
				p[8] = 1
			case "ticket":
				c.MissionInventory[0].RefObjID = 999
			case "unlisted pair":
				// Resolvable and in range, but no prize row names it: a pair no roll writes.
				c.MissionInventory[0].MagicOptions = []uint64{uint64(rt.GachaCatalog.Card.RefObjID), 1}
			case "stacked":
				c.MissionInventory[0].StackCount = 2
			case "delete pending":
				c.DeletePending = true
			case "truncated":
				p = p[:8]
			case "trailing":
				p = append(p, 0)
			}
			before := c.Snapshot()
			frames, reason := rt.HandleGachaRoll(testDivision, c, p)
			if reason == "" || len(frames) != 0 || !reflect.DeepEqual(c.Snapshot(), before) {
				t.Fatalf("refusal changed state: %s %+v", reason, frames)
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
		if reason == "" || len(frames) != 0 || calls != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatalf("failed waste draw committed: %v %s", frames, reason)
		}
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
					t.Fatal("voucher left by a row delete, which 7654B0 does not turn into the exchange notice")
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
	if frames, reason := rt.HandleGachaExchange(testDivision, c, p); reason == "" || len(frames) != 0 ||
		!reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("the blank card paid out the voucher's reward a second time")
	}
}

/*
================
TestGachaExchangeRefusalsPreserveTheVoucher
================
*/
func TestGachaExchangeRefusalsPreserveTheVoucher(t *testing.T) {
	for _, kind := range []string{"selection", "machine", "outside scope", "no voucher", "bad pair", "unlisted pair", "stacked", "delete pending", "truncated", "trailing", "catalog", "drop random missing", "drop random failure"} {
		t.Run(kind, func(t *testing.T) {
			rt, c, p := operatorFixture(t, 0)
			switch kind {
			case "selection":
				rt.Selected.Set(testDivision, c.Name, 17)
			case "machine":
				rt.Selected.Set(testDivision, c.Name, 17)
				p = wire.NewWriter(4).U32(17).Payload()
			case "outside scope":
				rt.NpcRoster[1].Spawn.RegionID = 0
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
		})
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
	if frames, reason := rt.HandleGachaExchange(testDivision, c, p); reason == "" || len(frames) != 0 ||
		!reflect.DeepEqual(c.Snapshot(), before) {
		t.Fatal("replay granted a second reward")
	}
}
