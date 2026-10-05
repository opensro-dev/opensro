/*
===========================================================================

temptation_test.go - the Bard's Temptation turns a monster on its own

The shipped Temptation row is cast through HandleTargetInteract; the
monsters then run the real monster leg (RunMonsterLeg through the action
owner's RunMonsterAction), as production drives them (Bard specification,
rule 6).

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// temptationCode is Temptation's last tier, ca(30000,80,8).
	temptationCode = "SKILL_EU_BARD_FORGETA_TARGET_A_06"
	// temptationDuration is ca's first word; a level-1 target sits below
	// grade 8, so 590680 keeps it unscaled.
	temptationDuration = 30000 * time.Millisecond
	// temptationTick is the monster leg's cadence in these tests.
	temptationTick = 100 * time.Millisecond
	// temptationTicks bounds one phase of the fight.
	temptationTicks = 100
	// temptationAttackSkill is the fixture's monster attack row.
	temptationAttackSkill = 2
	// temptationChampion is the champion spawn grade (Rarity 1).
	temptationChampion = 1
	// regularMonsterTID and regularMonsterTID4 make an ordinary MOB.
	regularMonsterTID  = 0xc6
	regularMonsterTID4 = 1
	// skillCastCasterOffset and skillCastTargetOffset locate the caster
	// and the target GID in a B245 success prefix.
	skillCastCasterOffset = 6
	skillCastTargetOffset = 14
)

/*
================
temptationPusher

Records what each viewer session receives from the monster leg.
================
*/
type temptationPusher struct {
	frames []simulation.Frame
}

func (p *temptationPusher) PushToSession(_ string, frames []simulation.Frame) {
	p.frames = append(p.frames, frames...)
}

func (p *temptationPusher) PushToDivision(string, []simulation.Frame, string) {}

/*
================
temptationFight

A Bard beside monster a, monster b ten units from a, both idle, and the
monster leg wired through the action owner.
================
*/
type temptationFight struct {
	rt     *Runtime
	clock  *fakeClock
	c      *enterworld.Character
	a, b   monster.Instance
	ops    *simulation.MonsterMoverOps
	viewer simulation.SessionSnapshot
	push   *temptationPusher
}

/*
================
newTemptationFight

rarity is monster a's spawn grade; b is always an ordinary monster.
================
*/
func newTemptationFight(t *testing.T, rarity uint8) temptationFight {
	t.Helper()
	rt, clock, c, original := newCombatTestRuntime(t, 1000)
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[temptationAttackSkill]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 1, 1, 100
	skills[temptationAttackSkill] = attack
	ref := original.Ref
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = temptationAttackSkill, 22, 8, 100
	ref.TidWord, ref.TypeID4 = regularMonsterTID, regularMonsterTID4
	tempted := ref
	tempted.RefObjID, tempted.MonsterType = ref.RefObjID+1, rarity
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref, tempted.RefObjID: tempted}, nil))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	state.SetAbnormalContext(monsterAbnormalContext{rt})
	rt.Monsters = state
	at := monster.Pose{RegionID: original.Spawn.RegionID, X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z}
	idleUntil := clock.NowMs() + int64(10*temptationDuration/time.Millisecond)
	a, err := state.DevelopmentCreateLeader(testDivision, tempted.RefObjID, at, idleUntil)
	if err != nil {
		t.Fatal(err)
	}
	at.X += 10
	b, err := state.DevelopmentCreateLeader(testDivision, ref.RefObjID, at, idleUntil)
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := state.ObjectPopulation(testDivision, a.Gid)
	if !ok {
		t.Fatal("missing population")
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: original.Spawn.RegionID, X: original.Spawn.X - 3, Y: original.Spawn.Y, Z: original.Spawn.Z}
			w.SpawnSet = true
		})
	*c.World.Spawn.X = original.Spawn.X - 3
	viewer := simulation.SessionSnapshot{SessionID: "temptation", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan,
		RunAction: rt.RunMonsterAction}
	return temptationFight{rt: rt, clock: clock, c: c, a: a, b: b, ops: ops, viewer: viewer, push: &temptationPusher{}}
}

/*
================
castTemptation

The Bard casts the shipped row on monster a with a certain roll and the
world tick admits the status.
================
*/
func (f temptationFight) castTemptation(t *testing.T) {
	t.Helper()
	skill := shippedOffense(t, temptationCode)
	if !skill.StatusCast || !skill.TargetRequired || !skill.Targets.EnemyM {
		t.Fatalf("catalog shape: status=%v refusal=%q", skill.StatusCast, skill.OffenseRefusal)
	}
	index, _ := abnormal.SourceIndex(0x6361)
	if args := skill.Abnormal.Params[index].Args; args[0] != 30000 || args[1] != 80 || args[2] != 8 {
		t.Fatalf("ca words %v, want 30000,80,8", args[:3])
	}
	equipStatusCastSkill(f.rt, f.c, skill, 0x6361, bardHarpKind)
	// A level-1 gauge holds less than the last tier's cost; Intellect
	// raises the maximum the cost is admitted against.
	f.c.Intellect = testInt64(2000)
	f.c.CurrentMP = testInt64(100000)
	out := f.rt.HandleTargetInteract(testDivision, f.c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: f.a.Gid}.Encode())
	if len(out.Frames) == 0 {
		t.Fatalf("Temptation was refused: %+v", out)
	}
	tick := f.clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1
	f.clock.Advance(time.Duration(tick-f.clock.NowMs()+5) * time.Millisecond)
	f.rt.advanceProjectileCasts(tick)
	f.rt.advanceMonsterAbnormals(tick)
}

/*
================
run

Step the monster leg until done reports true or the phase ends.
================
*/
func (f temptationFight) run(done func() bool) bool {
	for range temptationTicks {
		f.clock.Advance(temptationTick)
		f.rt.advanceMonsterAbnormals(f.clock.NowMs())
		f.ops.RunMonsterLeg(f.clock.NowMs(), []simulation.SessionSnapshot{f.viewer}, f.push)
		if done() {
			return true
		}
	}
	return false
}

/*
================
hp
================
*/
func (f temptationFight) hp(gid uint32) uint32 {
	instance, _ := f.rt.Monsters.Get(testDivision, gid)
	return instance.CurrentHP
}

/*
================
TestTemptationTurnsAMonsterOnItsNeighbour

The tempted monster leaves the Bard beside it alone, runs to the other
monster and strikes it with its own attack (a B245 whose caster and
target are both monsters reaches the viewer); the struck monster answers.
When Confusion ends, both drop each other and the fight stops.
================
*/
func TestTemptationTurnsAMonsterOnItsNeighbour(t *testing.T) {
	f := newTemptationFight(t, 0)
	f.castTemptation(t)
	tempted, _ := f.rt.Monsters.Get(testDivision, f.a.Gid)
	if tempted.Abnormal == nil || !tempted.Abnormal.Has(abnormal.Confusion) {
		t.Fatal("Temptation did not land Confusion on a regular monster")
	}
	playerHP := enterworld.CurrentHP(f.c)
	bHP, aHP := f.hp(f.b.Gid), f.hp(f.a.Gid)
	if !f.run(func() bool { return f.hp(f.b.Gid) < bHP && f.hp(f.a.Gid) < aHP }) {
		mover, _ := f.rt.Monsters.Mover(testDivision, f.a.Gid)
		t.Fatalf("no monster fight: a hp %d->%d b hp %d->%d, a mover %+v", aHP, f.hp(f.a.Gid), bHP, f.hp(f.b.Gid), mover)
	}
	if enterworld.CurrentHP(f.c) != playerHP {
		t.Fatalf("the tempted monster struck the player: HP %d -> %d", playerHP, enterworld.CurrentHP(f.c))
	}
	struck := false
	for _, frame := range f.push.frames {
		// SkillCastSuccess prefix: result, code, skill, caster, token, target.
		if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) < skillCastTargetOffset+4 {
			continue
		}
		caster := binary.LittleEndian.Uint32(frame.Payload[skillCastCasterOffset:])
		target := binary.LittleEndian.Uint32(frame.Payload[skillCastTargetOffset:])
		struck = struck || caster == f.a.Gid && target == f.b.Gid
	}
	if !struck {
		t.Fatal("the viewer never received the tempted monster's attack on its neighbour")
	}

	// Let Confusion run out, then the fight must stop.
	f.clock.Advance(temptationDuration)
	f.rt.advanceMonsterAbnormals(f.clock.NowMs())
	if live, _ := f.rt.Monsters.Get(testDivision, f.a.Gid); live.Abnormal != nil && live.Abnormal.Has(abnormal.Confusion) {
		t.Fatal("Confusion outlived its duration")
	}
	f.run(func() bool { return false })
	aHP, bHP = f.hp(f.a.Gid), f.hp(f.b.Gid)
	f.run(func() bool { return false })
	if f.hp(f.a.Gid) != aHP || f.hp(f.b.Gid) != bHP {
		t.Fatalf("the fight outlived Temptation: a %d->%d b %d->%d", aHP, f.hp(f.a.Gid), bHP, f.hp(f.b.Gid))
	}
	for _, gid := range []uint32{f.a.Gid, f.b.Gid} {
		mover, _ := f.rt.Monsters.Mover(testDivision, gid)
		if other := mover.TargetGID(); other == f.a.Gid || other == f.b.Gid {
			t.Fatalf("monster %d still targets monster %d", gid, other)
		}
	}
	if enterworld.CurrentHP(f.c) != playerHP {
		t.Fatalf("a monster struck the player: HP %d -> %d", playerHP, enterworld.CurrentHP(f.c))
	}
}

/*
================
TestTemptationLeavesAChampionAlone

Owner's rule: a champion is not a regular monster. The press answers the
invalid-target refusal (0x3006) and costs nothing, Confusion never lands
on it and it fights nobody.
================
*/
func TestTemptationLeavesAChampionAlone(t *testing.T) {
	f := newTemptationFight(t, temptationChampion)
	skill := shippedOffense(t, temptationCode)
	equipStatusCastSkill(f.rt, f.c, skill, confusionTag, bardHarpKind)
	f.c.Intellect = testInt64(2000)
	f.c.CurrentMP = testInt64(100000)
	_, _, _, mp := f.rt.playerKeeperVitals(testDivision, f.c)
	out := f.rt.HandleTargetInteract(testDivision, f.c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: f.a.Gid}.Encode())
	refused, ok := findFrame(out.Frames, wire.OpSkillCastResult)
	if !ok || len(refused.Payload) != 2 || refused.Payload[0] != 2 || refused.Payload[1] != 0x06 {
		t.Fatalf("Temptation on a champion was not refused as an invalid target: %+v", out)
	}
	if _, _, _, after := f.rt.playerKeeperVitals(testDivision, f.c); after != mp {
		t.Fatalf("the refused cast cost MP: %d -> %d", mp, after)
	}
	f.rt.advanceMonsterAbnormals(f.clock.NowMs())
	champion, _ := f.rt.Monsters.Get(testDivision, f.a.Gid)
	if champion.Rarity() != temptationChampion || champion.Abnormal != nil && champion.Abnormal.Has(abnormal.Confusion) {
		t.Fatalf("Confusion landed on a champion (rarity %d)", champion.Rarity())
	}
	bHP := f.hp(f.b.Gid)
	if f.run(func() bool { return f.hp(f.b.Gid) != bHP }) {
		t.Fatalf("the champion attacked its neighbour: HP %d -> %d", bHP, f.hp(f.b.Gid))
	}
	if mover, _ := f.rt.Monsters.Mover(testDivision, f.a.Gid); mover.TargetGID() == f.b.Gid {
		t.Fatal("the champion targets its neighbour")
	}
}
