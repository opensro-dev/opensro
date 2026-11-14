/*
===========================================================================

monstervitals_test.go - a monster's vitals reach the players that hold it

A monster's HP never rides a public frame: once per tick, a changed
monster sends its holders (the players that selected it) a private 0x33A6
with the tick's cause word, the HP and the status. These tests pin who
receives it, when, what it carries, and when a holder lets go.

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

/*
================
heldVitals

One decoded private vitals frame: the character it routes to, the monster,
the cause word, the HP and the status mask.
================
*/
type heldVitals struct {
	characterID int64
	gid         uint32
	cause       uint16
	hp          uint32
	mask        uint32
}

/*
================
monsterVitalsRoutes

Every private 0x33A6 for gid in routed, decoded; fails the test on a
public one or a body that is not flags 5.
================
*/
func monsterVitalsRoutes(t *testing.T, routed []simulation.DivisionFrames, gid uint32) []heldVitals {
	t.Helper()
	var out []heldVitals
	for _, route := range routed {
		for _, frame := range route.Frames {
			p := frame.Payload
			if frame.Opcode != simulation.OpVitalsUpdate || len(p) < 4 || binary.LittleEndian.Uint32(p) != gid || p[6]&1 == 0 {
				continue
			}
			if route.OnlyCharacterID == 0 {
				t.Fatalf("monster %#x HP routed publicly: % X", gid, p)
			}
			if p[6] != 5 || len(p) < 15 {
				t.Fatalf("monster %#x vitals body % X, want flags 5 with HP and status", gid, p)
			}
			out = append(out, heldVitals{characterID: route.OnlyCharacterID, gid: gid,
				cause: binary.LittleEndian.Uint16(p[4:]), hp: binary.LittleEndian.Uint32(p[7:]), mask: binary.LittleEndian.Uint32(p[11:])})
		}
	}
	return out
}

/*
================
assertNoMonsterHP

frames carry no 0x33A6 with gid's HP.
================
*/
func assertNoMonsterHP(t *testing.T, label string, frames []wire.Frame, gid uint32) {
	t.Helper()
	for _, frame := range frames {
		p := frame.Payload
		if frame.Opcode == simulation.OpVitalsUpdate && len(p) > 6 && binary.LittleEndian.Uint32(p) == gid && p[6]&1 != 0 {
			t.Fatalf("%s carry monster %#x's HP: % X", label, gid, p)
		}
	}
}

/*
================
TestSelectedMonsterVitalsReachTheHolderOncePerTick

A player that selected a monster receives its HP and status privately,
once per tick, with the tick's cause word; a bystander that did not select
it receives nothing, and nothing is sent again until the vitals move.
================
*/
func TestSelectedMonsterVitalsReachTheHolderOncePerTick(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	bystander := nearbyCharacter(rt, c, 21, "bystander", 5)
	selected := rt.HandleObjectSelect(testDivision, c, binary.LittleEndian.AppendUint32(nil, target.Gid))
	if selected.Refusal != "" || selected.Selected != target.Gid {
		t.Fatalf("select refused: %+v", selected)
	}
	rt.Monsters.DrainChangedVitals(nil)
	if routed := rt.publishMonsterVitals(clock.NowMs()); len(routed) != 0 {
		t.Fatalf("unchanged monster published %+v", routed)
	}

	// Two hits in one tick publish once, with the HP both left.
	for _, damage := range []uint32{300, 200} {
		live, _ := rt.Monsters.Get(testDivision, target.Gid)
		plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: live.CurrentHP, Damage: damage}
		if len(rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, live.CurrentHP, []simulation.MonsterDamagePlan{plan})) != 1 {
			t.Fatal("hit refused")
		}
	}
	held := monsterVitalsRoutes(t, rt.publishMonsterVitals(clock.NowMs()), target.Gid)
	if len(held) != 1 || held[0].characterID != c.ID || held[0].characterID == bystander.ID {
		t.Fatalf("holders = %+v, want only %d", held, c.ID)
	}
	if held[0].hp != target.CurrentHP-500 || held[0].cause != uint16(simulation.VitalsSourceCombatDamage) || held[0].mask != 0 {
		t.Fatalf("vitals = %+v, want HP %d, cause 4, no status", held[0], target.CurrentHP-500)
	}
	if routed := rt.publishMonsterVitals(clock.NowMs()); len(routed) != 0 {
		t.Fatalf("drained vitals published again: %+v", routed)
	}

	// A released target is held no more.
	rt.Selected.Clear(testDivision, c.Name)
	live, _ := rt.Monsters.Get(testDivision, target.Gid)
	plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: live.CurrentHP, Damage: 100}
	rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, live.CurrentHP, []simulation.MonsterDamagePlan{plan})
	if routed := rt.publishMonsterVitals(clock.NowMs()); len(routed) != 0 {
		t.Fatalf("released target published %+v", routed)
	}
}

/*
================
TestStrikeWithoutSelectionHoldsNothing

A strike binds no relation: a player that strikes a monster it did not
select receives none of the monster's HP, on the strike or on the tick.
Once it selects the monster, as the client does on every click, the
engage's next strike reaches it on that strike's tick.
================
*/
func TestStrikeWithoutSelectionHoldsNothing(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	result := rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	result = assertAndSeparateActionSession(t, result)
	if _, damage, fatal := assertSkillDamageOpen(t, result.Frames, 2, enterworld.ObjectIDForCharacter(c), target.Gid); damage == 0 || fatal {
		t.Fatalf("strike damage %d fatal %v, want a surviving damaged target", damage, fatal)
	}
	assertNoMonsterHP(t, "striker frames", result.Frames, target.Gid)
	assertNoMonsterHP(t, "observer frames", result.Broadcast, target.Gid)
	if held := monsterVitalsRoutes(t, rt.TickHook()(clock.NowMs()), target.Gid); len(held) != 0 {
		t.Fatalf("a striker that did not select received %+v", held)
	}
	if _, selected := rt.Selected.Get(testDivision, c.Name); selected {
		t.Fatal("the strike bound a selection")
	}

	if selected := rt.HandleObjectSelect(testDivision, c, binary.LittleEndian.AppendUint32(nil, target.Gid)); selected.Refusal != "" {
		t.Fatalf("select refused: %+v", selected)
	}
	rt.TickHook()(clock.At(testBasicAttackActionDuration).UnixMilli())
	routed := rt.TickHook()(clock.At(testBasicAttackActionDuration + time.Millisecond).UnixMilli())
	after, _ := rt.Monsters.Get(testDivision, target.Gid)
	held := monsterVitalsRoutes(t, routed, target.Gid)
	if len(held) != 1 || held[0].characterID != c.ID || held[0].hp != after.CurrentHP ||
		held[0].cause != uint16(simulation.VitalsSourceCombatDamage) {
		t.Fatalf("selected striker vitals = %+v, want one frame to %d with HP %d", held, c.ID, after.CurrentHP)
	}
}

/*
================
TestMovingMonsterRetiresDistantHolder

A monster that walks past its holder's range (910) leaves the holder in
silence, and its next HP reaches nobody; nearer, or resting beyond the
range, it stays held.
================
*/
func TestMovingMonsterRetiresDistantHolder(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	rt.Selected.Set(testDivision, c.Name, target.Gid)
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("target mover missing")
	}
	// 960 along both axes, inside the region: about 1358 at the end.
	away := func(at float64) float64 {
		if at < simulation.NativeRegionSize/2 {
			return at + 960
		}
		return at - 960
	}
	mover.From, mover.To = mover.Pose, mover.Pose
	mover.To.X, mover.To.Z = away(mover.Pose.X), away(mover.Pose.Z)
	mover.DepartMs, mover.ArriveMs = clock.NowMs(), clock.At(10*time.Second).UnixMilli()
	if !rt.Monsters.CommitMover(testDivision, target.Gid, mover) {
		t.Fatal("commit the walk")
	}
	held := func() bool {
		gid, ok := rt.Selected.Get(testDivision, c.Name)
		return ok && gid == target.Gid
	}

	rt.retireDistantHolders(clock.At(time.Second).UnixMilli())
	if !held() {
		t.Fatal("a monster walking within the range was let go")
	}
	late := clock.At(9 * time.Second).UnixMilli()
	rt.retireDistantHolders(late)
	if held() {
		t.Fatal("a monster that walked past the range is still held")
	}
	live, _ := rt.Monsters.Get(testDivision, target.Gid)
	plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: live.CurrentHP, Damage: 100}
	rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, live.CurrentHP, []simulation.MonsterDamagePlan{plan})
	if routed := rt.publishMonsterVitals(late); len(routed) != 0 {
		t.Fatalf("a let-go monster published %+v", routed)
	}

	// At rest beyond the range, the monster stays held.
	mover, _ = rt.Monsters.Mover(testDivision, target.Gid)
	mover.Pose = mover.To
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	if !rt.Monsters.CommitMover(testDivision, target.Gid, mover) {
		t.Fatal("commit the rest")
	}
	rt.Selected.Set(testDivision, c.Name, target.Gid)
	rt.retireDistantHolders(late)
	if !held() {
		t.Fatal("a resting monster beyond the range was let go")
	}
}

/*
================
TestReleaseKeepsASelectionMadeSince

A holder list is a copy: by the time a holder is let go, its player may
have selected something else, and that selection stays.
================
*/
func TestReleaseKeepsASelectionMadeSince(t *testing.T) {
	store := NewSelectionStore()
	store.Set(testDivision, "holder", 400001)
	copied := store.holders(nil)
	store.Set(testDivision, "holder", 400002)
	store.release(copied[0].division, copied[0].name, copied[0].gid)
	if gid, ok := store.Get(testDivision, "holder"); !ok || gid != 400002 {
		t.Fatalf("selection = %d %v, want the one made since the copy", gid, ok)
	}
	store.release(testDivision, "holder", 400002)
	if _, ok := store.Get(testDivision, "holder"); ok {
		t.Fatal("the named selection was not released")
	}
}

/*
================
TestStatusChangeJoinsTheTickCause

A hit that also applies a status ORs the status cause (0x100) into the
tick's word and carries the new mask, while the observers' frames carry
the status alone (flags 4), never the HP.
================
*/
func TestStatusChangeJoinsTheTickCause(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 100000)
	rt.Selected.Set(testDivision, c.Name, target.Gid)
	rt.Monsters.DrainChangedVitals(nil)
	record := abnormal.Record{Status: abnormal.Slow, Grade: 1, DurationMs: 600000, SourceGID: enterworld.ObjectIDForCharacter(c), SourceName: c.Name}
	plan := simulation.MonsterDamagePlan{GID: target.Gid, ExpectedHP: target.CurrentHP, Damage: 50, CreditGID: record.SourceGID,
		Abnormal: []abnormal.Record{record}, AbnormalSources: rt.Monsters.PrepareAbnormalSources(testDivision, []abnormal.Record{record})}
	impacts := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, target.CurrentHP, []simulation.MonsterDamagePlan{plan})
	if len(impacts) != 1 || impacts[0].Instance.Abnormal == nil {
		t.Fatal("slow not admitted")
	}
	public := rt.monsterImpactAbnormalFrames(testDivision, target.Gid, impacts)
	assertNoMonsterHP(t, "status frames", public, target.Gid)
	status := 0
	for _, frame := range public {
		if frame.Opcode == simulation.OpVitalsUpdate && frame.Payload[6] == 4 {
			status++
		}
	}
	if status != 1 {
		t.Fatalf("status frames = %04X, want one flags-4 status frame", opcodesOf(public))
	}
	held := monsterVitalsRoutes(t, rt.publishMonsterVitals(clock.NowMs()), target.Gid)
	want := uint16(simulation.VitalsSourceCombatDamage | simulation.VitalsSourceStatus)
	if len(held) != 1 || held[0].cause != want || held[0].hp != target.CurrentHP-50 || held[0].mask == 0 {
		t.Fatalf("holder vitals = %+v, want cause %#x, HP %d and the slow's mask", held, want, target.CurrentHP-50)
	}
}
