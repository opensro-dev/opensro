/*
===========================================================================

fortress_repair_test.go - repair item admission and linked lifecycle

Exercise the inventory command against the existing fortress population.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

/*
================
structureRepairFixture
================
*/
func structureRepairFixture(t *testing.T, grade int) (*Runtime, *enterworld.Character, *fakeClock, monster.Instance, []byte) {
	t.Helper()
	rt, c, clock, _ := captureFixture(t)
	level := int64(1)
	c.Level = &level
	skill := shippedOffense(t, fmt.Sprintf("SKILL_FORT_REPAIR_KIT_%02d", grade))
	if !skill.StructureRepair.Pinned {
		t.Fatalf("uncompiled repair: %s %+v", skill.Codename, skill.StructureRepair)
	}
	ref := &enterworld.ItemRef{RefObjID: 900001, Codename: "TEST_REPAIR", Country: 3, TypeIDs: [4]int64{3, 3, 1, 10}, AssociatedSkillCodename: skill.Codename,
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam5_2ac": 3, "actionRange23c": 150})}
	items := testItems()
	items[ref.Codename] = ref
	rt.deps.(*enterworld.Deps).Items = items
	rt.deps.(*enterworld.Deps).Skills = namedItemSkills{staticSkillSource{skill.ID: skill}}
	rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 77}, members: []domain.GuildMemberRecord{{CharID: c.ID, FortressRole: 1}}}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	lease, _ := rt.Monsters.PopulationLease(testDivision, instance.Pack(2, 1))
	var target monster.Instance
	for _, row := range rt.Monsters.PopulationInstances(testDivision, lease, []uint16{0x62aa}) {
		if row.Ref.TypeID4 == structureKindGuardTower {
			target = row
			break
		}
	}
	if target.Gid == 0 || !rt.Monsters.RestoreStructure(testDivision, target.Gid, 100, 0) {
		t.Fatal("missing tower")
	}
	target, _ = rt.Monsters.Get(testDivision, target.Gid)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = rt.monsterSpawn(testDivision, target.Gid, clock.NowMs())
		w.MoveSegment = nil
	})
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3}}
	request := []byte{21, byte(ref.TypeFlags()), byte(ref.TypeFlags() >> 8), 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(request[3:], target.Gid)
	return rt, c, clock, target, request
}

/*
================
TestStructureRepairGradesAndPulseBoundaries
================
*/
func TestStructureRepairGradesAndPulseBoundaries(t *testing.T) {
	for grade := 1; grade <= 3; grade++ {
		t.Run(fmt.Sprint(grade), func(t *testing.T) {
			rt, c, clock, target, request := structureRepairFixture(t, grade)
			out := rt.HandleItemUse(testDivision, c, request)
			if !repairItemAccepted(out) || c.MissionInventory[0].StackCount != 2 || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
				t.Fatalf("repair refused: %+v", out)
			}
			now := clock.NowMs()
			for _, elapsed := range []int64{0, 2999, 3000, 5999, 6000, 9000, 12000, 12001, 15000} {
				rt.advancePeriodicEffects(now + elapsed)
				got, _ := rt.Monsters.Get(testDivision, target.Gid)
				pulses := min(elapsed/3000, 4)
				want := uint32(100) + uint32(pulses)*target.EffectiveMaxHP()*uint32(grade)/100
				if got.CurrentHP != want {
					t.Fatalf("at %d HP=%d want=%d", elapsed, got.CurrentHP, want)
				}
			}
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || len(rt.periodicEffects.Frame(now+15000)) != 0 {
				t.Fatal("repair survived expiry")
			}
			again := rt.HandleItemUse(testDivision, c, request)
			if !repairItemAccepted(again) || c.MissionInventory[0].StackCount != 1 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatal("native skill refusal did not consume exactly one kit")
			}
		})
	}
}

/*
================
TestStructureRepairRefusalOrder
================
*/
func TestStructureRepairRefusalOrder(t *testing.T) {
	for _, name := range []string{"war", "missing", "full", "destroyed", "role", "outside", "guild", "range", "line", "malformed"} {
		t.Run(name, func(t *testing.T) {
			rt, c, _, target, request := structureRepairFixture(t, 1)
			want := uint8(3)
			switch name {
			case "war":
				rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, false)
				binary.LittleEndian.PutUint32(request[3:], 0)
				want = repairOutsideWar
			case "missing":
				binary.LittleEndian.PutUint32(request[3:], 0)
			case "full":
				rt.Monsters.RestoreStructure(testDivision, target.Gid, target.EffectiveMaxHP(), 0)
				want = repairInvalidStructure
			case "destroyed":
				rt.Monsters.RestoreStructure(testDivision, target.Gid, 0, 1)
				want = repairInvalidStructure
			case "role":
				rt.Guilds = fortressGuilds{guild: domain.GuildRecord{ID: 77}}
				want = repairWrongRole
			case "outside":
				c.World.PackedInstance = nil
				want = repairOutsideFortress
			case "guild":
				def, _ := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
				id, _ := rt.Fortresses.ForWorld(def)
				rt.Fortresses.Occupy(testDivision, id, 88)
				want = repairWrongGuild
			case "range":
				rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 151 })
				want = repairOutOfRange
			case "line":
				rt.LineOfSight = func(simulation.Spawn, simulation.NavOwner, simulation.Spawn) bool { return false }
				want = repairInvalidStructure
			case "malformed":
				request = append(request, 0)
				want = wire.ErrCodeInvalidRequest
			}
			out := rt.HandleItemUse(testDivision, c, request)
			if itemUseFailureCode(out) != want || c.MissionInventory[0].StackCount != 3 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
				t.Fatalf("refusal=%+v want=%x", out, want)
			}
		})
	}
}

/*
================
TestStructureRepairStopsBeforeAnotherPulse
================
*/
func TestStructureRepairStopsBeforeAnotherPulse(t *testing.T) {
	for _, name := range []string{"source-retired", "voluntary", "damage", "death", "logout", "destroyed", "world"} {
		t.Run(name, func(t *testing.T) {
			rt, c, clock, target, request := structureRepairFixture(t, 1)
			out := rt.HandleItemUse(testDivision, c, request)
			if !repairItemAccepted(out) || len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
				t.Fatalf("repair refused: %+v", out)
			}
			switch name {
			case "source-retired":
				rows := rt.effects.Snapshot(testDivision, c.Name)
				rt.effects.RetireInstances(testDivision, c.Name, []uint32{rows[0].InstanceToken})
			case "voluntary":
				rows := rt.effects.Snapshot(testDivision, c.Name)
				rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: rows[0].SkillID}).Encode())
				if !rt.effects.Snapshot(testDivision, c.Name)[0].StopRequested {
					t.Fatal("native delay stop refused")
				}
			case "damage":
				rt.cancelEffectsOnDamage(testDivision, c, 1, clock.NowMs())
			case "world":
				c.World.PackedInstance = nil
			case "death":
				hp := int64(0)
				c.CurrentHP = &hp
			case "logout":
				rt.ForgetCharacter(testDivision, c.Name)
			case "destroyed":
				rt.Monsters.RestoreStructure(testDivision, target.Gid, 0, 1)
			}
			rt.advancePeriodicEffects(clock.NowMs() + 3000)
			got, _ := rt.Monsters.Get(testDivision, target.Gid)
			want := uint32(100)
			if name == "destroyed" {
				want = 0
			}
			if got.CurrentHP != want || len(rt.periodicEffects.Frame(clock.NowMs()+3001)) != 0 {
				t.Fatal("retired repair pulsed", got.CurrentHP)
			}
		})
	}
}

/*
================
repairItemAccepted
================
*/
func repairItemAccepted(out OpResult) bool {
	frame, ok := findFrame(out.Frames, wire.OpItemUseResponse)
	return ok && len(frame.Payload) > 0 && frame.Payload[0] == 1
}

/*
================
TestStructureRepairLatePulseClampsAndPersists
================
*/
func TestStructureRepairLatePulseClampsAndPersists(t *testing.T) {
	rt, c, clock, target, request := structureRepairFixture(t, 3)
	if out := rt.HandleItemUse(testDivision, c, request); !repairItemAccepted(out) {
		t.Fatal(out)
	}
	rt.Monsters.RestoreStructure(testDivision, target.Gid, 499, 0)
	rt.advancePeriodicEffects(clock.NowMs() + 12001)
	got, _ := rt.Monsters.Get(testDivision, target.Gid)
	if got.CurrentHP != 500 || len(rt.periodicEffects.Frame(clock.NowMs()+12002)) != 0 {
		t.Fatal("late pulse or clamp failed", got.CurrentHP)
	}
	store := &structureRowStore{rows: map[uint32]domain.FortressStructureRecord{}}
	rt.FortressStore = store
	rt.forceFortressSave(testDivision)
	rt.advanceFortressStructures(clock.NowMs() + 12002)
	if store.rows[85].HP != 500 {
		t.Fatal("repair HP not persisted", store.rows[85])
	}
}
