/*
===========================================================================

cosdeath_test.go - a dead COS, or a dead rider's vehicle, is released at once

The native COS death (its pet and summoned variants alike) and
the native player death both end in the same release: the COS
leaves the world. A pet's record stays on its item (kind 8); a vehicle's
is deleted (kind 1), and a vehicle's own death drops its cargo first.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
despawnsGID

Whether frames carry the despawn of gid.
================
*/
func despawnsGID(frames []wire.Frame, gid uint32) bool {
	want := wire.ObjectDespawn{Gid: gid}.Encode()
	for _, frame := range frames {
		if frame.Opcode == wire.OpObjectDespawn && string(frame.Payload) == string(want) {
			return true
		}
	}
	return false
}

/*
================
TestRiderDeathReleasesTheRiddenVehicle
================
*/
func TestRiderDeathReleasesTheRiddenVehicle(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 100)
	ride := &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true, StateFlags: cosStateSummoned | 1,
	}
	character.ActiveCOS = ride
	zero := int64(0)
	character.CurrentHP = &zero

	effects, _ := rt.settlePlayerDeathInDoor(testDivision, character, deathKiller{}, 1000)
	if ride.Summoned || ride.Mounted || ride.StateFlags&cosStateSummoned != 0 {
		t.Fatalf("the dead rider kept its vehicle: %+v", ride)
	}
	if ride.CurrentHP != 87829 {
		t.Fatal("releasing the vehicle changed its HP", ride.CurrentHP)
	}
	if character.ActiveCOS != nil {
		t.Fatal("the released vehicle kept its record (the native kind-1 release deletes it)")
	}
	dismount, found := findFrame(effects, wire.OpCosRideState)
	if !found || dismount.Payload[4] != 0 {
		t.Fatalf("no dismount before the release: %+v", effects)
	}
	if !despawnsGID(effects, ride.GID) {
		t.Fatalf("the released vehicle stayed in the world: %+v", effects)
	}
}

/*
================
TestRiderDeathKeepsAnUnriddenCompanion

Natively a player's death releases only the ridden vehicle (+0x30); a pet
beside its owner lives on.
================
*/
func TestRiderDeathKeepsAnUnriddenCompanion(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 100)
	pet := &enterworld.CharacterCOS{
		GID: 0x00C00004, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, StateFlags: cosStateSummoned | 1,
	}
	character.ActiveCOS = pet
	zero := int64(0)
	character.CurrentHP = &zero

	effects, _ := rt.settlePlayerDeathInDoor(testDivision, character, deathKiller{}, 1000)
	if !pet.Summoned || despawnsGID(effects, pet.GID) {
		t.Fatalf("an unridden companion died with its owner: %+v %+v", pet, effects)
	}
}

/*
================
TestStarvedPetIsReleased

A COS death (here hunger, through the shared fatal commit) publishes the
death, then releases the corpse; the record keeps its dead HP for revival.
================
*/
func TestStarvedPetIsReleased(t *testing.T) {
	items := shippedItems(t)
	c := testCharacter()
	rt, _ := newTestRuntime(c, items)
	equipShippedPet(t, rt, c, items, "COS_P_WOLF_002")
	pet := c.ActiveCOS
	pet.Satiety = 1
	pet.StateFlags = 3
	rt.BindPetSession(testDivision, c, 1)
	rt.storeCosAbnormal(testDivision, c.Name, pet.GID, &abnormal.Block{Mask: abnormal.Burn.Bit()})
	rt.advancePets(1000)
	output := rt.advancePets(70000)
	if pet.CurrentHP != 0 || pet.Summoned || pet.StateFlags&(1|cosStateSummoned) != 0 {
		t.Fatalf("the starved pet was not released: %+v", pet)
	}
	var frames []wire.Frame
	for _, batch := range output {
		for _, frame := range batch.Frames {
			frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
	}
	deadLife := wire.ObjectStateRefresh{Gid: pet.GID, StateType: wire.StateChannelLife, Value: wire.LifeStateDead}.Encode()
	died, released := -1, -1
	for i, frame := range frames {
		if frame.Opcode == wire.OpObjectStateRefresh && string(frame.Payload) == string(deadLife) {
			died = i
		}
		if despawnsGID(frames[i:i+1], pet.GID) {
			released = i
		}
	}
	if died < 0 || released < died {
		t.Fatalf("the corpse must publish its death before it leaves (dead %d, despawn %d): %+v", died, released, frames)
	}
	if !despawnsGID(frames, pet.GID) {
		t.Fatalf("the starved pet stayed in the world: %+v", frames)
	}
}

/*
================
TestTransportDeathDropsCargoAndDeletesRecord

Natively a transport killed in the field drops its goods
unowned around it, and the COS death's kind-1 release deletes its record; a pet
of the same death keeps its record for revival.
================
*/
func TestTransportDeathDropsCargoAndDeletesRecord(t *testing.T) {
	for _, band := range []uint16{cosBandTransport, cosBandAttackPet} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, character, band)
		pet := character.ActiveCOS
		potion := testItems()["ITEM_ETC_HP_POTION_01"]
		pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
			Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
		pet.CurrentHP = 5
		record := abnormal.Record{Status: abnormal.Burn, Level: 1, Grade: 1, DurationMs: 10000,
			PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		if !owner.block.Apply(owner, record, clock.NowMs()) {
			t.Fatal("status refused")
		}
		owner.block.Update(owner, clock.NowMs())
		owner.commit()
		frames := rt.cosAbnormalPublication(pet.GID, owner)
		ground := rt.Ground.All(testDivision)
		if band == cosBandAttackPet {
			if character.ActiveCOS != pet || len(ground) != 0 {
				t.Fatalf("a dead pet lost its record or dropped %d items", len(ground))
			}
			continue
		}
		if character.ActiveCOS != nil {
			t.Fatal("a dead transport kept its record")
		}
		if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
			t.Fatalf("cargo %+v", ground)
		}
		if !saw(frames, wire.OpObjectDespawn) {
			t.Fatal("the dead transport stayed in view")
		}
	}
}

/*
================
TestRiddenTransportDeathDropsCargo

Regression coverage for #2730: a transport that is itself killed while
its owner rides it drops its goods the same way an unridden one does -
the cargo scatters unowned, the kind-1 release deletes the record and
the corpse leaves the world.
================
*/
func TestRiddenTransportDeathDropsCargo(t *testing.T) {
	rt, clock, character, source := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, character, cosBandTransport)
	pet := character.ActiveCOS
	pet.Mounted = true
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	pet.CurrentHP = 5
	record := abnormal.Record{Status: abnormal.Burn, Level: 1, Grade: 1, DurationMs: 10000,
		PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10}
	owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
	owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
	if !owner.block.Apply(owner, record, clock.NowMs()) {
		t.Fatal("status refused")
	}
	owner.block.Update(owner, clock.NowMs())
	owner.commit()
	frames := rt.cosAbnormalPublication(pet.GID, owner)
	if character.ActiveCOS != nil {
		t.Fatal("a dead ridden transport kept its record")
	}
	ground := rt.Ground.All(testDivision)
	if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
		t.Fatalf("cargo %+v", ground)
	}
	if !saw(frames, wire.OpObjectDespawn) {
		t.Fatal("the dead ridden transport stayed in view")
	}
}

/*
================
dressTestCape

Register a free-battle cape of one group and wear it in slot eight,
the group playerRelationEquipment resolves.
================
*/
func dressTestCape(t *testing.T, rt *Runtime, c *enterworld.Character, index int, group int64) {
	t.Helper()
	cape := &enterworld.ItemRef{
		RefObjID: uint32(63200 + index), Codename: "TEST_CARGO_CAPE_" + string(rune('A'+index)),
		TypeIDs:      [4]int64{3, 1, 7, 5},
		NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: float64(group)}),
		Combat:       &enterworld.ItemCombatRef{},
	}
	switch items := rt.deps.ItemReferences().(type) {
	case staticItemSource:
		items[cape.Codename] = cape
	case cosTestItemSource:
		items.staticItemSource[cape.Codename] = cape
	default:
		t.Fatalf("unknown item source %T", items)
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(enterworld.JobSuitSlot),
		RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1})
}

/*
================
killLoadedTransport

The TestPlayerKillOfATransportScattersItsCargo strike: a loaded
transport at one HP, killed by the attacker's basic attack; the kill
must land and retire the record.
================
*/
func killLoadedTransport(t *testing.T, rt *Runtime, attacker, owner *enterworld.Character) {
	t.Helper()
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
}

/*
================
TestTransportKillCreditFollowsTheFreeBattleRelation

The kill credit's relation gate on the cargo: a fatal dealer whose
free-battle group opposes the transport owner's (or is the
everyone-opposes group) earns no credit at all, so the goods go with
the record; a team-mate's kill runs the credit and drops them. The
record dies either way.
================
*/
func TestTransportKillCreditFollowsTheFreeBattleRelation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		killerCape int64
		ownerCape  int64
		drops      bool
	}{
		{"opposing groups keep the cargo", 2, 1, false},
		{"the everyone-opposes group keeps the cargo", 5, 3, false},
		{"team mates drop the cargo", 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			dressTestCape(t, rt, attacker, 0, tc.killerCape)
			dressTestCape(t, rt, owner, 1, tc.ownerCape)
			killLoadedTransport(t, rt, attacker, owner)
			ground := rt.Ground.All(testDivision)
			if dropped := len(ground) == 1; dropped != tc.drops {
				t.Fatalf("killer cape %d, owner cape %d: cargo dropped %v (ground %+v), want drops %v",
					tc.killerCape, tc.ownerCape, dropped, ground, tc.drops)
			}
		})
	}
}

/*
================
TestJobOpponentKillOfATransportDropsItsCargo

A suited job opponent is not the relation gate: a thief striking a
trader's transport dead runs the full credit (his kill books as a job
kill), so the goods drop unowned like any credited kill's.
================
*/
func TestJobOpponentKillOfATransportDropsItsCargo(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	dressJobSuit(t, rt, attacker, domain.JobThief)
	dressJobSuit(t, rt, owner, domain.JobTrader)
	killLoadedTransport(t, rt, attacker, owner)
	ground := rt.Ground.All(testDivision)
	if len(ground) != 1 || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
		t.Fatalf("a thief's kill of a trader's transport left %+v, want the 7 potions unowned", ground)
	}
}

/*
================
TestAttackPetKillOfATransportKeepsCargoOnOpposingCapes

A companion dealer answers with its owner's relation: an attack pet
whose owner wears an opposing free-battle cape kills the transport,
and the skipped credit keeps the cargo with the record exactly as a
direct player strike on that relation does.
================
*/
func TestAttackPetKillOfATransportKeepsCargoOnOpposingCapes(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 100, attackPetBand)
	petGID := c.ActiveCOS.GID
	victim := *c
	victim.ID, victim.Name, victim.ActiveCOS = c.ID+1, "victim", nil
	ref := equipCombatTestPet(t, rt, &victim, cosBandTransport)
	transport := victim.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	transport.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	transport.CurrentHP = 1
	dressTestCape(t, rt, c, 0, 2)
	dressTestCape(t, rt, &victim, 1, 1)
	attackerRef, ok := rt.cosReference(c.ActiveCOS)
	if !ok {
		t.Fatal("the attack pet has no reference")
	}
	state := rt.petSessionFor(testDivision, c.Name, petGID)
	if state == nil || state.character != c {
		t.Fatal("the pet session does not hold its owner")
	}
	step := petCombatStep{key: petOwnerKey{division: testDivision, name: c.Name, gid: petGID},
		state: state, snapshot: c, pet: c.ActiveCOS, ref: attackerRef, percent: fullAreaPercent}
	target := petCombatTarget{
		combatTarget: combatTarget{gid: transport.GID, player: &victim, snapshot: &victim},
		cos:          transport, cosRef: ref,
	}
	skill := rt.deps.SkillData().(staticSkillSource)[2]
	result, committed := rt.petStrikeCOS(step, target, skill)
	if !committed || !result.fatal {
		t.Fatalf("the pet's hit on the transport did not commit fatally: %+v", result)
	}
	if victim.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	if ground := rt.Ground.All(testDivision); len(ground) != 0 {
		t.Fatalf("an opposing-cape pet kill dropped %+v, want the cargo kept with the record", ground)
	}
}

/*
================
TestMonsterKillOfATransportDropsItsCargo

A thief or any monster striking a loaded transport dead: its goods drop
unowned on the ground and its record goes (the same death a burn takes).
A second player's report said a dead transport dropped nothing; this pins
the monster lane.
================
*/
func TestMonsterKillOfATransportDropsItsCargo(t *testing.T) {
	rt, _, c, instance := newCombatTestRuntime(t, 100)
	instance.Ref.DefaultSkillIDs[0] = 2
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	skills := rt.deps.SkillData().(staticSkillSource)
	strike := skills[2]
	strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 1000, 1000, 100
	skills[2] = strike
	equipCombatTestPet(t, rt, c, cosBandTransport)
	pet := c.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	pet.CurrentHP = 1
	result := rt.MonsterBasicAttack(testDivision, instance, pet.GID, 2, rt.Now().UnixMilli())
	if !result.Accepted {
		t.Fatalf("the strike on the transport was refused: %+v", result)
	}
	ground := rt.Ground.All(testDivision)
	if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
		t.Fatalf("a transport killed by a monster left %+v on the ground, want the 7 potions unowned", ground)
	}
	if c.ActiveCOS != nil {
		t.Fatal("the dead transport kept its record")
	}
}

/*
================
TestBurnKillOfATransportFollowsTheFreeBattleRelation

A burn death settles through the abnormal path (cosabnormal.go
commit), which never saw a direct hit: the fatal periodic hit still
credits its source, so the cargo gate reads the burn dealer's cape
exactly as it reads a strike's. An opposing-cape burn keeps the
cargo with the record; a team-mate's burn drops it.
================
*/
func TestBurnKillOfATransportFollowsTheFreeBattleRelation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		killerCape int64
		drops      bool
	}{
		{"an opposing-cape burn keeps the cargo", 2, false},
		{"a team-mate burn drops the cargo", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, burner, owner := newPvpPair(t)
			// Equip first: it replaces the item source, and the cape
			// registration (dressTestCape) writes into that source.
			equipCombatTestPet(t, rt, owner, cosBandTransport)
			dressTestCape(t, rt, burner, 0, tc.killerCape)
			dressTestCape(t, rt, owner, 1, 1)
			transport := owner.ActiveCOS
			potion := testItems()["ITEM_ETC_HP_POTION_01"]
			transport.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
				Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
			transport.CurrentHP = 5
			record := abnormal.Record{Status: abnormal.Burn, Level: 1, Grade: 1, DurationMs: 10000,
				PeriodMs: 2000, SourceGID: enterworld.ObjectIDForCharacter(burner), Rate24: 10, Scale20: 1, Param38: 10}
			abnormalOwner := rt.newCosAbnormalOwner(testDivision, owner, clock.NowMs())
			abnormalOwner.sources = rt.captureAbnormalSources(testDivision, abnormalOwner.block, []abnormal.Record{record})
			if !abnormalOwner.block.Apply(abnormalOwner, record, clock.NowMs()) {
				t.Fatal("status refused")
			}
			abnormalOwner.block.Update(abnormalOwner, clock.NowMs())
			abnormalOwner.commit()
			if owner.ActiveCOS != nil {
				t.Fatal("the burned transport kept its record")
			}
			ground := rt.Ground.All(testDivision)
			if dropped := len(ground) == 1; dropped != tc.drops {
				t.Fatalf("burner cape %d, owner cape 1: cargo dropped %v (ground %+v), want drops %v",
					tc.killerCape, dropped, ground, tc.drops)
			}
		})
	}
}
