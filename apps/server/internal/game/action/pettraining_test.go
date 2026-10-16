/*
===========================================================================

pettraining_test.go - attack-pet skills and gear through the real commands,
strikes and drops

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/chat"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
trainingCommand
================
*/
func trainingCommand(gid uint32, operation, arg uint8) []byte {
	return wire.NewWriter(petTrainingLength).U32(gid).U8(petTrainingTag).U8(operation).U8(arg).Payload()
}

/*
================
makeTrainedPet

Turns the fixture's attack pet into one of the four design pets at a pet
level, with bonus points and every pet gear row known to the item source.
================
*/
func makeTrainedPet(rt *Runtime, c *enterworld.Character, codename string, level uint8) {
	c.BattleUntilMs = 0
	items := rt.deps.ItemReferences().(cosTestItemSource)
	ref := items.characters["PET"]
	delete(items.characters, "PET")
	ref.Codename = codename
	items.characters[codename] = ref
	for _, d := range companion.D.Gear {
		for _, item := range d.Items {
			items.staticItemSource[item.Codename] = &enterworld.ItemRef{RefObjID: item.ID, Codename: item.Codename,
				TypeIDs: [4]int64{3, 3, 9, 0}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
		}
	}
	c.ActiveCOS.Codename = codename
	c.ActiveCOS.Level = level
	c.ActiveCOS.Training = domain.PetTraining{BonusPoints: 30}
}

/*
================
designSkill
================
*/
func designSkill(t *testing.T, name string) companion.SkillDef {
	t.Helper()
	for _, s := range companion.D.Skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no skill %q", name)
	return companion.SkillDef{}
}

/*
================
train
================
*/
func train(rt *Runtime, c *enterworld.Character, operation uint8, args ...uint8) OpResult {
	var result OpResult
	for _, arg := range args {
		result = rt.HandleCosCommand(testDivision, c, trainingCommand(c.ActiveCOS.GID, operation, arg))
	}
	return result
}

/*
================
learn

Learns a skill up to a level (the pet level must allow it).
================
*/
func learn(t *testing.T, rt *Runtime, c *enterworld.Character, name string, level int) companion.SkillDef {
	t.Helper()
	s := designSkill(t, name)
	for range level {
		train(rt, c, petTrainingLearn, s.ID)
	}
	if got := companion.Level(c.ActiveCOS, s); int(got) != level {
		t.Fatalf("%s learned to %d, want %d", name, got, level)
	}
	return s
}

/*
================
bagItem
================
*/
func bagItem(c *enterworld.Character, codename string) (enterworld.InventoryRow, bool) {
	for _, row := range c.MissionInventory {
		if row.Codename == codename && row.Slot >= int64(inventory.EquipmentSlotEnd) {
			return row, true
		}
	}
	return enterworld.InventoryRow{}, false
}

/*
================
grantGear

Puts pet gear with blue options into the bag.
================
*/
func grantGear(t *testing.T, rt *Runtime, c *enterworld.Character, id uint32, options ...uint64) {
	t.Helper()
	if !rt.deps.Update(c, "grant", func() bool {
		rows, _, err := rt.planBagChange(c, func(inv *inventory.Inventory) error {
			ref, _ := rt.deps.ItemReferences().ItemRefByID(id)
			_, fault := inv.GrantStack(inventory.Item{RefObjID: id, Codename: ref.Codename, TypeFlags: ref.TypeFlags(),
				Quantity: 1, MagicOptions: options}, 1)
			if fault != nil {
				return fmt.Errorf("grant refused")
			}
			return nil
		})
		c.MissionInventory = rows
		return err == nil
	}) {
		t.Fatal("gear grant refused")
	}
}

/*
================
stateSkills

The 0x3FB1 push among result frames: free points and the skill count.
================
*/
func stateSkills(t *testing.T, result OpResult) (points uint8, skills int) {
	t.Helper()
	for _, frame := range result.Frames {
		if frame.Opcode != wire.OpPetTrainingState {
			continue
		}
		p := frame.Payload
		at := 9
		for range 2 {
			at += 6 + 1 + int(p[at+6])
		}
		at += 3
		return p[4], int(p[at])
	}
	t.Fatalf("no training state in %d frames", len(result.Frames))
	return 0, 0
}

/*
================
TestPetTrainingAdmission
================
*/
func TestPetTrainingAdmission(t *testing.T) {
	rt, _, c, m := newPetCombatRuntime(t, 10000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_030", 30)
	bite := designSkill(t, "Rending Bite")
	gid := c.ActiveCOS.GID
	train(rt, c, petTrainingLearn, bite.ID)
	if companion.Level(c.ActiveCOS, bite) != 0 {
		t.Fatal("off flag mutated the pet")
	}
	rt.AttackPetExpansion = true
	for _, payload := range [][]byte{trainingCommand(gid+1, petTrainingLearn, bite.ID), trainingCommand(gid, petTrainingSlot+1, bite.ID),
		append(trainingCommand(gid, petTrainingLearn, bite.ID), 0)} {
		rt.HandleCosCommand(testDivision, c, payload)
	}
	if companion.Level(c.ActiveCOS, bite) != 0 {
		t.Fatal("foreign/malformed command mutated the pet")
	}
	points, skills := stateSkills(t, train(rt, c, petTrainingLearn, bite.ID))
	if companion.Level(c.ActiveCOS, bite) != 1 || int(points) != companion.TrainingPoints(c.ActiveCOS) || skills != 15 {
		t.Fatalf("learn: level %d points %d skills %d", companion.Level(c.ActiveCOS, bite), points, skills)
	}
	rt.HandleCosCommand(testDivision, c, petAttackOrder(gid, m.Gid))
	train(rt, c, petTrainingLearn, bite.ID)
	if companion.Level(c.ActiveCOS, bite) != 1 {
		t.Fatal("combat allowed training")
	}
	train(rt, c, petTrainingUse, bite.ID)
	if c.ActiveCOS.Training.Queued != bite.ID {
		t.Fatal("an action could not be used in combat")
	}
}

/*
================
TestPetGearKeepsItsBlues

A worn piece keeps its blue options, comes back to the bag with them,
swaps with a second piece of its slot, and other items are refused.
================
*/
func TestPetGearKeepsItsBlues(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 10000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_030", 30)
	rt.AttackPetExpansion = true
	d := companion.D.Gear[0]
	blue := companion.EncodeAffix(companion.D.Affixes[0].Code, 7)
	grantGear(t, rt, c, d.Items[0].ID, blue)
	row, _ := bagItem(c, d.Items[0].Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	worn := c.ActiveCOS.Training.Gear[0]
	if worn.RefObjID != d.Items[0].ID || worn.Options[0] != blue {
		t.Fatalf("worn %+v", worn)
	}
	if _, inBag := bagItem(c, d.Items[0].Codename); inBag {
		t.Fatal("the collar stayed in the bag")
	}
	grantGear(t, rt, c, d.Items[0].ID)
	row, _ = bagItem(c, d.Items[0].Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	back, ok := bagItem(c, d.Items[0].Codename)
	if !ok || len(back.MagicOptions) != 1 || back.MagicOptions[0] != blue || c.ActiveCOS.Training.Gear[0].Options[0] != 0 {
		t.Fatalf("swap lost the blues: bag %+v worn %+v", back, c.ActiveCOS.Training.Gear[0])
	}
	train(rt, c, petTrainingUnequip, 0)
	if c.ActiveCOS.Training.Gear[0].RefObjID != 0 {
		t.Fatal("unequip left the collar on")
	}
	train(rt, c, petTrainingEquip, 0)
	if c.ActiveCOS.Training.Gear[0].RefObjID != 0 {
		t.Fatal("an equipment slot was worn")
	}
}

/*
================
TestPetGearSixBluesEquipAndRemove

The sixth option must survive the bag, worn state and training wire.
================
*/
func TestPetGearSixBluesEquipAndRemove(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 10000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	d := companion.D.Gear[8]
	options := make([]uint64, 6)
	for i := range options {
		options[i] = companion.EncodeAffix(companion.D.Affixes[i].Code, uint32(i+1))
	}
	grantGear(t, rt, c, d.Items[0].ID, options...)
	row, _ := bagItem(c, d.Items[0].Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	if got := c.ActiveCOS.Training.Gear[0].Options; got[5] != options[5] {
		t.Fatalf("equipping lost sixth blue: %v", got)
	}
	train(rt, c, petTrainingUnequip, 0)
	back, ok := bagItem(c, d.Items[0].Codename)
	if !ok || len(back.MagicOptions) != 6 {
		t.Fatalf("unequipping lost blues: %+v", back)
	}
	for i, value := range options {
		if back.MagicOptions[i] != value {
			t.Fatalf("blue %d changed", i)
		}
	}
}

/*
================
attackUntil
================
*/
func attackUntil(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, m monster.Instance, done func() bool) {
	t.Helper()
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, m.Gid))
	tickPetCombat(t, rt, clock, 100, done)
}

/*
================
TestWolfRendingBiteBleedsAndSharesItsRow
================
*/
func TestWolfRendingBiteBleedsAndSharesItsRow(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_030", 30)
	rt.AttackPetExpansion = true
	bite := learn(t, rt, c, "Rending Bite", 1)
	attackUntil(t, rt, clock, c, m, func() bool {
		hit, _ := rt.Monsters.Get(testDivision, m.Gid)
		return hit.CurrentHP < m.CurrentHP
	})
	if c.ActiveCOS.Training.Rows[bite.Row].ReadyAtMs != 0 {
		t.Fatal("an action fired without the owner using it")
	}
	train(rt, c, petTrainingUse, bite.ID)
	attackUntil(t, rt, clock, c, m, func() bool { return c.ActiveCOS.Training.Rows[bite.Row].ReadyAtMs > clock.NowMs() })
	deadline := c.ActiveCOS.Training.Rows[bite.Row].ReadyAtMs
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	if live.Abnormal == nil || !live.Abnormal.Slots[abnormal.Bleeding].Active {
		t.Fatal("Rending Bite did not bleed")
	}
	if deadline != clock.NowMs()+int64(bite.CooldownAt(1)*1000) {
		t.Fatalf("row cooldown %d", deadline-clock.NowMs())
	}
	for range 30 {
		clock.now = clock.now.Add(100 * time.Millisecond)
		rt.TickHook()(clock.NowMs())
	}
	if c.ActiveCOS.Training.Rows[bite.Row].ReadyAtMs != deadline || c.ActiveCOS.Training.Queued != 0 {
		t.Fatal("a later attack bypassed the row cooldown or kept the queue")
	}
}

/*
================
TestBearGroundSlamStuns
================
*/
func TestBearGroundSlamStuns(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	slam := designSkill(t, "Ground Slam")
	makeTrainedPet(rt, c, "COS_P_BEAR_030", slam.UnlockLevel)
	rt.AttackPetExpansion = true
	learn(t, rt, c, "Ground Slam", 1)
	train(rt, c, petTrainingUse, slam.ID)
	attackUntil(t, rt, clock, c, m, func() bool {
		return c.ActiveCOS.Training.Rows[slam.Row].Left > 0 || c.ActiveCOS.Training.Rows[slam.Row].ReadyAtMs > 0
	})
	live, _ := rt.Monsters.Get(testDivision, m.Gid)
	if live.Abnormal == nil || !live.Abnormal.Slots[abnormal.Stun].Active {
		t.Fatal("Ground Slam did not stun")
	}
}

/*
================
TestExecuteAndShatterAdjustDamage
================
*/
func TestExecuteAndShatterAdjustDamage(t *testing.T) {
	rt := &Runtime{CombatRoll: func() (uint32, error) { return 9999, nil }}
	target := monster.Instance{CurrentHP: 500, Ref: monster.MonsterRef{MaxHP: 10000}}
	blocked := []combat.Result{{Blocked: true}}
	rt.petTrainedDamage(&petTrainedStrike{execute: 10}, target, blocked)
	if blocked[0].Damage != 0 {
		t.Fatal("a blocked hit executed the monster")
	}
	formulas := []combat.Result{{Damage: 100}}
	rt.petTrainedDamage(&petTrainedStrike{execute: 5}, target, formulas)
	if formulas[0].Damage != 500 {
		t.Fatalf("Execute below 5%%: damage %d", formulas[0].Damage)
	}
	formulas = []combat.Result{{Damage: 100}}
	rt.petTrainedDamage(&petTrainedStrike{execute: 3}, target, formulas)
	if formulas[0].Damage != 100 {
		t.Fatal("Execute above its threshold")
	}
	target.Abnormal = &abnormal.Block{}
	target.Abnormal.Slots[abnormal.Frostbite].Active = true
	formulas = []combat.Result{{Damage: 100}}
	rt.petTrainedDamage(&petTrainedStrike{shatter: 50}, target, formulas)
	if formulas[0].Damage != 150 {
		t.Fatalf("Shatter: damage %d", formulas[0].Damage)
	}
}

/*
================

# TestManualPetBuffCastsWithoutAttack

A counter starts at its full cost and each subsequent committed attack
pushes the reduced count, including the final ready state.
================
*/
func TestManualPetBuffCastsWithoutAttack(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_040", 40)
	rt.AttackPetExpansion = true
	skill := learn(t, rt, c, "Iron Hide", 1)
	command := append(trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID), 0, 0, 0, 0)
	result := rt.HandleCosCommand(testDivision, c, command)
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	if len(result.Frames) == 0 || state == nil || state.trained.ironHideUntil <= clock.NowMs() ||
		companion.RowReady(&c.ActiveCOS.Training, skill.Row, clock.NowMs()) || c.ActiveCOS.Training.Queued != 0 {
		t.Fatal("manual buff did not apply and spend cooldown without an attack")
	}
	until := state.trained.ironHideUntil
	rt.HandleCosCommand(testDivision, c, command)
	if state.trained.ironHideUntil != until {
		t.Fatal("cooldown allowed a second manual cast")
	}
}

/*
================
TestManualRoarAddsPetThreatWithoutDamageOrTarget
================
*/
func TestManualRoarAddsPetThreatWithoutDamageOrTarget(t *testing.T) {
	rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_040", 40)
	rt.AttackPetExpansion = true
	companion.UpgradeTree(c.ActiveCOS)
	if msg := companion.Learn(c.ActiveCOS, 16); msg != "" {
		t.Fatal(msg)
	}
	before, _ := rt.Monsters.Get(testDivision, mob.Gid)
	rt.HandleCosCommand(testDivision, c, append(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 16), 0, 0, 0, 0))
	after, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if companion.RowReady(&c.ActiveCOS.Training, 0, clock.NowMs()) || after.CurrentHP != before.CurrentHP || after.Opponents[0].GID != c.ActiveCOS.GID {
		t.Fatalf("roar did not taunt without damage: HP %d/%d opponents %+v", before.CurrentHP, after.CurrentHP, after.Opponents)
	}
}

/*
================
TestPetCounterPushAfterAttack
================
*/
func TestPetCounterPushAfterAttack(t *testing.T) {
	rt, clock, c, m := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_060", 60)
	rt.AttackPetExpansion = true
	frenzy := learn(t, rt, c, "Frenzy", 1)
	train(rt, c, petTrainingUse, frenzy.ID)
	attackUntil(t, rt, clock, c, m, func() bool { return c.ActiveCOS.Training.Rows[frenzy.Row].Left > 0 })
	if got := c.ActiveCOS.Training.Rows[frenzy.Row].Left; got != uint16(frenzy.CooldownAt(1)) {
		t.Fatalf("cast consumed its own recharge: %d", got)
	}
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	c.ActiveCOS.Training.Rows[frenzy.Row].Left = 1
	strike := &petTrainedStrike{passive: -1}
	ref, _ := rt.cosReference(c.ActiveCOS)
	step := petCombatStep{pet: c.ActiveCOS, state: state, ref: ref, nowMs: clock.NowMs()}
	rt.petTrainedCommit(step, c, c.ActiveCOS, strike, 1, false, false)
	if c.ActiveCOS.Training.Rows[frenzy.Row].Left != 0 || len(strike.ownerState) == 0 {
		t.Fatal("counter completion did not publish ready state")
	}
	strike = &petTrainedStrike{passive: -1, procUsed: true}
	rt.petTrainedCommit(step, c, c.ActiveCOS, strike, 0, false, false)
	if state.trained.procReadyAt != step.nowMs+companion.D.Rules.ProcInternalCooldownMs {
		t.Fatal("committed proc on a blocked hit did not spend its cooldown")
	}
}

/*
================
TestMonstersDropPetGearWithBlues
================
*/
func TestMonstersDropPetGearWithBlues(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 10000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_030", 30)
	rt.DropRoll = func() (uint32, error) { return 0, nil }
	rt.PetGearDropFactor = 0.5
	ref := &enterworld.ItemRef{RefObjID: 50000, Codename: "TEST_NATIVE_GEAR", TypeIDs: [4]int64{3, 1, 6, 2}, NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 1})}
	rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[ref.Codename] = ref
	parent := grounditem.Item{RefObjID: ref.RefObjID}
	at := simulation.Spawn{RegionID: 1}
	if _, ok := rt.preparePetGearDrop(c, parent, at, time.Now()); ok {
		t.Fatal("dropped with the expansion off")
	}
	rt.AttackPetExpansion = true
	drop, ok := rt.preparePetGearDrop(c, parent, at, time.Now())
	if !ok || !companion.IsGearRef(drop.RefObjID) || len(drop.MagicOptions) != 1 {
		t.Fatalf("drop %+v", drop)
	}
	rt.DropRoll = func() (uint32, error) { return dropRollDomain - 1, nil }
	if _, ok := rt.preparePetGearDrop(c, parent, at, time.Now()); ok {
		t.Fatal("a failed chance roll dropped gear")
	}
}

/*
================
TestPetGearBodyCarriesItsBlues
================
*/
func TestPetGearBodyCarriesItsBlues(t *testing.T) {
	blue := companion.EncodeAffix(companion.D.Affixes[0].Code, 9)
	body := wire.ItemBody{RefObjID: companion.D.Gear[0].Items[0].ID, TypeFlags: 3<<2 | 3<<5 | 9<<7, Quantity: 1, MagicOptions: []uint64{blue}}
	encoded := body.Encode()
	if len(encoded) != body.EncodedSize() || binary.LittleEndian.Uint64(encoded[len(encoded)-8:]) != blue || encoded[len(encoded)-9] != 1 {
		t.Fatalf("pet gear body % x", encoded)
	}
}

/*
================
TestPetReceiptsFitTheChatLimit
================
*/
func TestPetReceiptsFitTheChatLimit(t *testing.T) {
	message := strings.Repeat("Recall the pet and leave combat before changing its skills or gear. ", 4)
	result := petTrainingReceipt(message)
	var words []string
	for _, frame := range result.Frames {
		chatType, sender, line, ok := chat.DecodeChatBroadcastNamed(frame.Payload)
		if !ok || chatType != chat.ChatTypeWhisper || sender != "Pet trainer" || len(utf16.Encode([]rune(line))) > maxReceiptLine {
			t.Fatalf("receipt line %q (ok %t)", line, ok)
		}
		words = append(words, strings.Fields(line)...)
	}
	if len(result.Frames) < 2 || strings.Join(words, " ") != strings.Join(strings.Fields(message), " ") {
		t.Fatalf("receipt split into %d lines lost text", len(result.Frames))
	}
}
