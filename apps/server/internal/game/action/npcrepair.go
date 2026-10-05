/*
===========================================================================

npcrepair.go - smiths and armourers repair equipment for gold

The v1.150 shop window's Repair button arms a repair cursor; clicking an
item sends 0x746F [u32 npcGid][u8 1][u8 slot], and Repair All, after its
cost confirmation, 0x746F [u32 npcGid][u8 2] (6938x0,
CGInterface_SendNpcRepairRequest746F_758C). The answer is 0xB46F [1] or
[2, notice] in category 13 (CPSMission_OnNpcRepairResponse0xB46F); each
repaired item's durability rides 0x31E8 and the balance 0x30B3.

v1.188 is CGObjNPC_ApplyRepairRequest0x703E (4C7A10): the NPC must offer
service 4 (CGObjNPC_SpawnAndConfigureServices registers it on every town
smith and armourer and the fortress smiths), the player must hold gold,
mode 1 repairs one slot and mode 2 walks every inventory slot until the
gold runs out (CGObjNPC_RepairInventorySlot 4C7AF0 per slot).

===========================================================================
*/

package action

import (
	"math"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	opNpcRepairRequest  uint16 = 0x746F
	opNpcRepairResponse uint16 = 0xB46F

	repairOneSlot  uint8 = 1
	repairAllSlots uint8 = 2

	// The category-13 notices 0xB46F raises (v1.188 0x1Cxx low bytes).
	repairErrTooFar      uint8 = 0x04
	repairErrNotService  uint8 = 0x05
	repairErrNoGold      uint8 = 0x07
	repairErrNotRepaired uint8 = 0x11

	// errCodeNothingToRepair is the low byte of 49C4DB's 0x1888 (v1.150
	// notice 392, UIIT_MSG_STRGERR_THERE_IS_NO_ITEM_TO_REPAIR).
	errCodeNothingToRepair uint8 = 0x88
)

// repairNpcCodenames: the NPCs 4C6350 registers service 4 for.
var repairNpcCodenames = map[string]bool{
	"NPC_CH_SMITH": true, "NPC_CH_ARMOR": true, "NPC_WC_SMITH": true, "NPC_WC_ARMOR": true,
	"NPC_KT_SMITH": true, "NPC_KT_ARMOR": true, "NPC_EU_SMITH": true, "NPC_EU_ARMOR": true,
	"NPC_CA_SMITH": true, "NPC_CA_ARMOR": true,
	"NPC_SD_M_AREA_SMITH": true, "NPC_SD_M_AREA_ARMOR": true, "NPC_SD_T_AREA_SMITH": true, "NPC_SD_T_AREA_ARMOR": true,
	"NPC_CH_FORTRESS_SMITH1": true, "NPC_CH_FORTRESS_SMITH2": true, "NPC_WC_FORTRESS_SMITH1": true,
	"NPC_WC_FORTRESS_SMITH2": true, "NPC_KT_FORTRESS_SMITH": true, "NPC_EU_FORTRESS_SMITH": true,
	"NPC_CA_FORTRESS_SMITH1": true, "NPC_CA_FORTRESS_SMITH2": true,
}

/*
================
repairRefusal
================
*/
func repairRefusal(code uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opNpcRepairResponse, Payload: []byte{2, code}}}}
}

/*
================
HandleNpcRepair
================
*/
func (rt *Runtime) HandleNpcRepair(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	mode, e2 := r.U8()
	if c == nil || e != nil || e2 != nil || mode != repairOneSlot && mode != repairAllSlots {
		return repairRefusal(repairErrNotService)
	}
	slot := uint8(0)
	if mode == repairOneSlot {
		if slot, e = r.U8(); e != nil {
			return repairRefusal(repairErrNotService)
		}
	}
	if r.Done() != nil {
		return repairRefusal(repairErrNotService)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return repairRefusal(repairErrNotService)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || !repairNpcCodenames[npc.Codename] {
		return repairRefusal(repairErrNotService)
	}
	if !rt.npcWithinHitRange(division, c, npc) {
		return repairRefusal(repairErrTooFar)
	}
	result := repairRefusal(repairErrNoGold)
	rt.deps.Update(c, "npc-repair", func() bool {
		if c.DeletePending || goldOf(c) == 0 {
			return false
		}
		var repaired wearFrames
		refusal := uint8(0)
		if mode == repairOneSlot {
			repaired, refusal = rt.repairInventorySlot(division, c, slot)
		} else {
			for _, row := range append([]enterworld.InventoryRow(nil), c.MissionInventory...) {
				if row.Slot < 0 || row.Slot > 0xff {
					continue
				}
				taken, code := rt.repairInventorySlot(division, c, uint8(row.Slot))
				repaired.actor = append(repaired.actor, taken.actor...)
				repaired.public = append(repaired.public, taken.public...)
				if code == repairErrNoGold {
					break
				}
			}
		}
		if refusal != 0 {
			result = repairRefusal(refusal)
			return false
		}
		if len(repaired.actor) == 0 {
			return false
		}
		frames := append([]wire.Frame{{Opcode: opNpcRepairResponse, Payload: []byte{1}}}, repaired.actor...)
		result = OpResult{Frames: append(frames, goldFrame(c)), Broadcast: repaired.public}
		return true
	})
	return result
}

/*
================
repairInventorySlot

CGObjNPC_RepairInventorySlot (4C7AF0) inside the character's Update: the
item must be repairable equipment, the quote must find the gold, then
CGObjPC_RepairItemForGold (4E7210) restores the points, charges the gold
and spends a MATTR_REPAIR charge. Returns the frames and a refusal byte
(0 when repaired or nothing to repair).
================
*/
func (rt *Runtime) repairInventorySlot(division string, c *enterworld.Character, slot uint8) (wearFrames, uint8) {
	index := -1
	for i, row := range c.MissionInventory {
		if row.Slot == int64(slot) {
			index = i
			break
		}
	}
	refs := rt.deps.ItemReferences()
	if index < 0 || refs == nil {
		return wearFrames{}, 0
	}
	row := c.MissionInventory[index]
	ref, ok := refs.ItemRefByCodename(row.Codename)
	if !ok || ref == nil || !rt.itemRepairable(ref, row) {
		return wearFrames{}, repairErrNotRepaired
	}
	maximum := rt.equipmentMaxDurability(&row)
	costRepair, _ := ref.NativeFields.Lookup("repairCostB4")
	costRevive, _ := ref.NativeFields.Lookup("reviveCostB8")
	restored, cost, ok := combat.RepairQuote(uint32(max(row.Durability, 0)), maximum, int64(costRepair), int64(costRevive), int64(min(goldOf(c), math.MaxInt64)))
	if !ok {
		if row.Durability >= int64(maximum) {
			return wearFrames{}, 0
		}
		return wearFrames{}, repairErrNoGold
	}
	frames := rt.offsetItemDurability(division, c, slot, int32(int64(restored)-row.Durability))
	if len(frames.actor) == 0 {
		return wearFrames{}, 0
	}
	gold := int64(min(goldOf(c), math.MaxInt64)) - cost
	c.Gold = &gold
	rt.spendRepairCharge(c, index)
	return frames, 0
}

/*
================
itemRepairable

CGItemEquip_CanRepair (497340): equipment, not an accessory (family 5 or
12) or a job suit (7), itemdata CanRepair, and its options allow it.
================
*/
func (rt *Runtime) itemRepairable(ref *enterworld.ItemRef, row enterworld.InventoryRow) bool {
	family := row.TypeFlags >> 7 & 15
	if !isEquipmentItem(row.TypeFlags) || family == 5 || family == 12 || family == 7 {
		return false
	}
	if canRepair, ok := ref.NativeFields.Lookup("canRepair"); !ok || canRepair == 0 {
		return false
	}
	allowed, err := combat.RepairableByOptions(row, rt.deps.MagicOptionDefinitions())
	if err != nil {
		log.Warnf("action: repair of %s refused: %v", row.Codename, err)
		return false
	}
	return allowed
}

/*
================
spendRepairCharge

CGItemEquip_ConsumeRepairMagicCharge (491890): a MATTR_REPAIR count above 1
loses one.
================
*/
func (rt *Runtime) spendRepairCharge(c *enterworld.Character, index int) {
	source := rt.deps.MagicOptionDefinitions()
	if source == nil {
		return
	}
	row := &c.MissionInventory[index]
	for i, encoded := range row.MagicOptions {
		definition, ok := source.MagicOptionByParamID(uint32(encoded & 0xffff))
		if !ok || definition == nil || definition.Tag != 0x726570 {
			continue
		}
		if count := uint32(encoded >> 32); count > 1 {
			row.MagicOptions[i] = uint64(count-1)<<32 | encoded&0xffffffff
		}
	}
}

/*
================
hammerRepair

The repair hammer (49C2B0 case 6): every repairable item in the inventory
below its maximum is restored free (CGObjPC_OffsetItemDurability by the
missing points) and spends a MATTR_REPAIR charge. Runs inside the item
use's character Update.
================
*/
func (rt *Runtime) hammerRepair(division string, c *enterworld.Character) wearFrames {
	var out wearFrames
	refs := rt.deps.ItemReferences()
	if refs == nil {
		return out
	}
	for index := range c.MissionInventory {
		row := c.MissionInventory[index]
		ref, ok := refs.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || row.Slot < 0 || row.Slot > 0xff || !rt.itemRepairable(ref, row) {
			continue
		}
		maximum := rt.equipmentMaxDurability(&row)
		if row.Durability >= int64(maximum) {
			continue
		}
		taken := rt.offsetItemDurability(division, c, uint8(row.Slot), int32(int64(maximum)-row.Durability))
		if len(taken.actor) == 0 {
			continue
		}
		rt.spendRepairCharge(c, index)
		out.actor = append(out.actor, taken.actor...)
		out.public = append(out.public, taken.public...)
	}
	return out
}
