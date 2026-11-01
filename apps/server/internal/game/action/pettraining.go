/*
===========================================================================

pettraining.go - attack-pet skill and gear commands and the state push

Port-only, not native. COS tag F0 is [gid:u32, tag:u8, op:u8, arg:u8]: op
0 asks for state (arg2 upgrades sequential books), 1 learns the next rank,
2 queues the legacy skill or casts with an appended target u32, 3 wears the item in bag slot arg, 4 returns gear slot arg
to the bag, 5 refunds every skill, 6 slots passive arg. The server checks
ownership, serializes changes with normal pet orders and answers with the
0x3FB1 state push (item/wire/pettraining.go). Gear moves between the
owner's bag and the pet with its blue options, in the transaction of the
pet's state (planBagChange). A refusal is a short private whisper.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	petTrainingTag uint8 = 0xf0
	// petTrainingLength is the whole command: gid, tag, op, arg.
	petTrainingLength = 7
	// maxReceiptLine is the longest chat text the client accepts (UTF-16 units).
	maxReceiptLine = 100
)

// Training operations (the command's op byte).
const (
	petTrainingState uint8 = iota
	petTrainingLearn
	petTrainingUse
	petTrainingEquip
	petTrainingUnequip
	petTrainingRefund
	petTrainingSlot
)

/*
================
petTrainingReceipt

One whisper per line of at most maxReceiptLine UTF-16 units, wrapped at
spaces: the client refuses a longer chat text (its CHAT_TEXT_LIMIT, the
native input limit) and ends the session over it.
================
*/
func petTrainingReceipt(message string) OpResult {
	var frames []wire.Frame
	line := ""
	flush := func() {
		if line != "" {
			frames = append(frames, wire.Frame{Opcode: chat.OpChatBroadcast,
				Payload: chat.EncodeChatBroadcastNamed(chat.ChatTypeWhisper, "Pet trainer", line)})
		}
		line = ""
	}
	for _, word := range strings.Fields(message) {
		// shortcut: a single word over the limit is cut; receipts have none.
		for len(utf16.Encode([]rune(word))) > maxReceiptLine {
			flush()
			units := utf16.Encode([]rune(word))
			line, word = string(utf16.Decode(units[:maxReceiptLine])), string(utf16.Decode(units[maxReceiptLine:]))
		}
		next := word
		if line != "" {
			next = line + " " + word
		}
		if len(utf16.Encode([]rune(next))) > maxReceiptLine {
			flush()
			next = word
		}
		line = next
	}
	flush()
	return OpResult{Frames: frames}
}

/*
================
handlePetTraining

Called with the division lock. Persist before acknowledging; malformed or
foreign GIDs never modify any pet. The state request is allowed in combat.
================
*/
func (rt *Runtime) handlePetTraining(division string, c *enterworld.Character, payload []byte) OpResult {
	if (len(payload) != petTrainingLength && len(payload) != petTrainingLength+4) || payload[4] != petTrainingTag || payload[5] > petTrainingSlot ||
		len(payload) == petTrainingLength+4 && payload[5] != petTrainingUse {
		return OpResult{}
	}
	if !rt.AttackPetExpansion {
		return petTrainingReceipt("Attack-pet training is disabled on this server.")
	}
	r := wire.NewReader(payload)
	gid, err := r.U32()
	if err != nil {
		return OpResult{}
	}
	snapshot, ref := rt.commandCOSSnapshot(division, c, gid)
	if snapshot == nil || ref.TidWord>>11 != attackPetBand || companion.PetKind(ref.Codename) == "" {
		return OpResult{}
	}
	operation, arg := payload[5], payload[6]
	if operation == petTrainingState && arg == 2 && snapshot.CompanionByGID(gid).Training.TreeVersion < 2 {
		if !rt.deps.Update(c, "pet-tree-upgrade", func() bool {
			live := c.CompanionByGID(gid)
			if live == nil {
				return false
			}
			companion.UpgradeTree(live)
			return true
		}) {
			return petTrainingReceipt("The skill tree could not be upgraded.")
		}
		snapshot = rt.characterSnapshot(division, c)
	}
	nowMs := rt.Now().UnixMilli()
	if operation == petTrainingState {
		frames := rt.petTrainingStateFrames(snapshot.CompanionByGID(gid), nowMs)
		if state := rt.petSessionFor(division, c.Name, gid); state != nil {
			rt.deps.Read(division, func() {
				if counter := petSkillCounterFrame(state, gid); len(counter.Payload) > 0 {
					frames = append(frames, counter)
				}
			})
		}
		return OpResult{Frames: frames}
	}
	state := rt.petSessionFor(division, c.Name, gid)
	if operation == petTrainingUse && len(payload) == petTrainingLength {
		if skill, ok := companion.PetSkill(snapshot.CompanionByGID(gid), arg); ok && (skill.EffectName() == "Rejuvenating Plume" || skill.EffectName() == companion.BroodSkill) {
			return rt.manualPetSkill(division, c, snapshot, gid, arg, 0, nowMs)
		}
	}
	if operation == petTrainingUse && len(payload) == petTrainingLength+4 {
		return rt.manualPetSkill(division, c, snapshot, gid, arg, binary.LittleEndian.Uint32(payload[7:]), nowMs)
	}
	if operation != petTrainingUse && operation != petTrainingSlot &&
		(snapshot.BattleUntilMs > nowMs || state != nil && state.combat != nil) {
		return petTrainingReceipt("Recall the pet and leave combat before changing its skills or gear.")
	}
	gear := operation == petTrainingEquip || operation == petTrainingUnequip
	if gear && (rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name)) {
		return petTrainingReceipt("Close the stall or exchange before moving pet gear.")
	}
	refusal := "Training could not be saved."
	var frames []wire.Frame
	// updated is captured inside the training door; the state frames
	// below never re-read the companion unlocked (checklist item 2).
	var updated *domain.CharacterCOS
	if !rt.deps.Update(c, "pet-training", func() bool {
		pet := c.CompanionByGID(gid)
		if pet == nil || pet.BroodStage != 0 || !pet.Summoned || pet.CurrentHP == 0 {
			return false
		}
		defer func() {
			copy := *pet
			updated = &copy
		}()
		switch operation {
		case petTrainingLearn:
			refusal = companion.Learn(pet, arg)
		case petTrainingUse:
			refusal = companion.Queue(pet, arg, nowMs)
		case petTrainingSlot:
			refusal = companion.Slot(pet, arg)
		case petTrainingEquip:
			refusal, frames = rt.wearPetGear(c, gid, uint8(arg))
		case petTrainingUnequip:
			refusal, frames = rt.removePetGear(c, gid, int(arg))
		default:
			companion.Refund(pet)
			refusal = ""
		}
		if refusal == "" && pet.ResolveStacks > 0 && state != nil {
			skill, _, learned := companion.Spirit(pet)
			if !learned || skill.ID != state.trained.resolveBook {
				rt.setPetSpirit(pet, 0)
				reset := []wire.Frame{petSpiritFrame(pet, nowMs, nowMs), {Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, 0, pet.CurrentHP)}}
				frames = append(frames, reset...)
				state.public = append(state.public, reset...)
			}
		}
		return refusal == ""
	}) {
		result := petTrainingReceipt(refusal)
		result.Frames = append(result.Frames, rt.petTrainingStateFrames(updated, nowMs)...)
		return result
	}
	return OpResult{Frames: append(frames, rt.petTrainingStateFrames(updated, nowMs)...)}
}

/*
================
petEffectiveMaxHP

The maximum a pet-HP writer must respect: the effective reference
(gear included under the expansion), never the bare catalog row.
Zero when the pet does not resolve, so callers keep their own base.
================
*/
func (rt *Runtime) petEffectiveMaxHP(pet *domain.CharacterCOS) uint32 {
	if ref, ok := rt.cosReference(pet); ok && ref != nil {
		return ref.MaxHP
	}
	return 0
}

/*
================
petEffectiveMaxFrames

The client draws a pet's HP bar and text against the max this state
frame carries; until one arrives it divides by the native catalog max,
which the attack-pet profiles deliberately lower, so under the
expansion a fully healed pet reads as nearly empty on a default
client (#1896). Push the state unasked wherever an attack pet's
vitals first stand or its max can move: summon, entry restore,
potion and level growth. With the expansion off nothing is pushed -
the frame stays training-window traffic, as natively.
================
*/
func (rt *Runtime) petEffectiveMaxFrames(pet *domain.CharacterCOS, nowMs int64) []wire.Frame {
	if !rt.AttackPetExpansion || pet == nil {
		return nil
	}
	if ref, ok := rt.cosReference(pet); !ok || ref == nil || ref.TidWord>>11 != attackPetBand {
		return nil
	}
	return rt.petTrainingStateFrames(pet, nowMs)
}

/*
================
petTrainingStateFrames

The worn gear's item references, then the 0x3FB1 state push.
================
*/
func (rt *Runtime) petTrainingStateFrames(pet *domain.CharacterCOS, nowMs int64) []wire.Frame {
	if pet == nil {
		return nil
	}
	t := &pet.Training
	m := companion.ModifiersFor(pet)
	p := companion.Profile(pet.Codename)
	physical, magical := companion.AttackScales(pet.Codename, m)
	state := wire.PetTrainingState{
		PetGID:              pet.GID,
		FreePoints:          uint8(min(companion.TrainingPoints(pet), 255)),
		AttackPercent:       uint16(100 * physical),
		DefensePercent:      uint16(p.DefensePercent * (100 + m.DefensePct) / 100),
		MagicPercent:        uint16(100 * magical),
		MagicDefensePercent: uint16(p.MagicDefensePercent * (100 + m.DefensePct) / 100),
		MagicPrimary:        p.MagicPrimary,
		Slotted:             t.Slotted,
		GearStats:           [6]uint32{uint32(m.GearStats.PhysicalAttack), uint32(m.GearStats.MagicalAttack), uint32(m.GearStats.PhysicalDefense), uint32(m.GearStats.MagicalDefense), uint32(m.GearStats.HitRatio), uint32(m.GearStats.ParryRatio)},
		CooldownPercent:     uint16(m.CooldownPct),
	}
	for row := companion.ActiveRowCount; row < companion.RowCount; row++ {
		state.Slotted[row-companion.ActiveRowCount] = 0
		if active, ok := companion.SlottedPassive(pet, row); ok {
			state.Slotted[row-companion.ActiveRowCount] = uint8(active.Option)
		}
	}
	if ref, ok := rt.cosReference(pet); ok {
		state.MaxHP = ref.MaxHP
	}
	for row := range t.Rows {
		r := companion.AdvanceCharges(t.Rows[row], nowMs)
		if r.Capacity == 0 {
			for _, def := range companion.SkillsForPet(pet) {
				if def.Row == row && def.EffectName() == companion.BroodSkill && companion.Level(pet, def) > 0 {
					r = companion.BroodRow(r, def, companion.EffectiveLevel(pet, def, m), nowMs, m)
				}
			}
		}
		if r.Capacity > 0 {
			r.Left = uint16(r.Charges)
			r.Kind = companion.ChargeKind
		}
		state.Rows[row] = wire.PetRowState{TimerMs: uint32(max(0, r.ReadyAtMs-nowMs)), Left: r.Left, Kind: r.Kind}
	}
	for _, s := range companion.SkillsForPet(pet) {
		lvl := companion.Level(pet, s)
		eff := companion.EffectiveLevel(pet, s, m)
		bonus := m.SkillBonus
		if pet.Training.TreeVersion >= 2 {
			bonus += m.RowSkillBonus[s.Row]
		}
		row := wire.PetSkillRow{ID: s.ID, Level: lvl, EffectiveLevel: eff,
			NextPetLevel: companion.NextLevelRequirement(s, lvl),
			Value:        uint16(max(0, s.ValueAt(max(eff, 1))*10)),
			NextValue:    uint16(max(0, s.ValueAt(uint8(min(255, int(min(lvl+1, s.MaxLevel))+bonus)))*10))}
		if s.Active {
			row.Flags |= wire.PetSkillAction
			if t.Queued == s.ID {
				row.Flags |= wire.PetSkillQueued
			}
		} else if lvl > 0 && state.Slotted[s.Row-companion.ActiveRowCount] == uint8(s.Option) {
			row.Flags |= wire.PetSkillSlotted
		}
		if s.Live() {
			row.Flags |= wire.PetSkillLive
		}
		if companion.LearnRefusal(pet, s.ID) == "" {
			row.Flags |= wire.PetSkillLearnable
		}
		state.Skills = append(state.Skills, row)
	}
	var refs []uint32
	for _, g := range t.Gear {
		var options []uint64
		for _, o := range g.Options {
			if o != 0 {
				options = append(options, o)
			}
		}
		state.Gear = append(state.Gear, wire.PetGearState{RefObjID: g.RefObjID, Options: options})
		refs = append(refs, g.RefObjID)
	}
	frames := rt.itemReferencesByID(refs)
	return append(frames, wire.Frame{Opcode: wire.OpPetTrainingState, Payload: wire.EncodePetTrainingState(state)})
}

/*
================
petGearItem

A worn piece as a bag item with its blue options; false for an empty slot.
================
*/
func (rt *Runtime) petGearItem(g domain.PetGear) (inventory.Item, bool) {
	if g.RefObjID == 0 {
		return inventory.Item{}, false
	}
	ref, found := rt.deps.ItemReferences().ItemRefByID(g.RefObjID)
	if !found || ref == nil {
		return inventory.Item{}, false
	}
	item := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1}
	for _, o := range g.Options {
		if o != 0 {
			item.MagicOptions = append(item.MagicOptions, o)
		}
	}
	return item, true
}

/*
================
wearPetGear

Inside the character transaction: wear the gear item in a bag slot,
returning a piece already in its gear slot to the bag. The bag plan
commits whole or not at all.
================
*/
func (rt *Runtime) wearPetGear(c *enterworld.Character, gid uint32, bagSlot uint8) (string, []wire.Frame) {
	var taken inventory.Item
	pet := c.CompanionByGID(gid)
	slot := -1
	var degree companion.GearDegree
	rows, frames, err := rt.planBagChange(c, func(inv *inventory.Inventory) error {
		if bagSlot < inventory.EquipmentSlotEnd {
			return fmt.Errorf("not a bag slot")
		}
		item, fault := inv.DropQuantity(bagSlot, 1)
		if fault != nil {
			return fmt.Errorf("empty bag slot")
		}
		d, s, ok := companion.GearByID(item.RefObjID)
		if !ok {
			return fmt.Errorf("not pet gear")
		}
		taken, slot, degree = item, s, d
		if previous, worn := rt.petGearItem(pet.Training.Gear[s]); worn {
			if grant, fault := inv.GrantStack(previous, 1); fault != nil || grant.GroundRemainder != 0 {
				return fmt.Errorf("bag full")
			}
		}
		return nil
	})
	if err != nil {
		return "The pet cannot wear that item.", nil
	}
	if pet.Level < degree.MinPetLevel {
		return fmt.Sprintf("Degree %d gear needs pet level %d.", degree.Degree, degree.MinPetLevel), nil
	}
	c.MissionInventory = rows
	// The bag rows were rebuilt; address the pet through its owner again.
	pet = c.CompanionByGID(gid)
	if pet == nil {
		return "Training could not be saved.", nil
	}
	worn := domain.PetGear{RefObjID: taken.RefObjID}
	copy(worn.Options[:], taken.MagicOptions)
	pet.Training.Gear[slot] = worn
	// A swap can lower the effective max (the returned piece carried
	// the HP bonus): clamp like removePetGear does (#1896).
	if ref, ok := rt.cosReference(pet); ok && ref != nil && pet.CurrentHP > ref.MaxHP {
		pet.CurrentHP = ref.MaxHP
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, 0, pet.CurrentHP)})
	}
	return "", frames
}

/*
================
removePetGear
================
*/
func (rt *Runtime) removePetGear(c *enterworld.Character, gid uint32, slot int) (string, []wire.Frame) {
	pet := c.CompanionByGID(gid)
	if slot >= len(pet.Training.Gear) {
		return "Unknown pet gear slot.", nil
	}
	item, worn := rt.petGearItem(pet.Training.Gear[slot])
	if !worn {
		return "Nothing is worn there.", nil
	}
	rows, frames, err := rt.planBagChange(c, func(inv *inventory.Inventory) error {
		grant, fault := inv.GrantStack(item, 1)
		if fault != nil || grant.GroundRemainder != 0 {
			return fmt.Errorf("bag full")
		}
		return nil
	})
	if err != nil {
		return "Make room in the bag to remove this gear.", nil
	}
	c.MissionInventory = rows
	pet = c.CompanionByGID(gid)
	if pet == nil {
		return "Training could not be saved.", nil
	}
	pet.Training.Gear[slot] = domain.PetGear{}
	if ref, ok := rt.cosReference(pet); ok && pet.CurrentHP > ref.MaxHP {
		pet.CurrentHP = ref.MaxHP
		frames = append(frames, wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.HPRefreshPayload(gid, 0, pet.CurrentHP)})
	}
	return "", frames
}
