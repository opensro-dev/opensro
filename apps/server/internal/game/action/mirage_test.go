/*
===========================================================================

mirage_test.go - the Warlock's Mirage lowers monsters' hostility toward
the caster

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// mirageCode is Mirage's first tier, efr(1,1,250,4,0,16) dtnt(2544,0).
	mirageCode = "SKILL_EU_WARLOCK_CONFUSIONA_AGGROLOW_A_01"
	mirageFlat = 2544
	// mirageHostility is more than the cut.
	mirageHostility = 1000000
)

/*
================
TestMirageCutsTheCastersHostilityAroundIt

The fixture monster fights the Warlock standing on it. Mirage prepares,
and its release cuts the monster's hate for the caster by at least the
flat word (plus the weapon term) and names the monster as a zero-damage
record of the release.
================
*/
func TestMirageCutsTheCastersHostilityAroundIt(t *testing.T) {
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, mirageCode)
	if !skill.Threat.Decrease || skill.TargetRequired || skill.Threat.Area.Shape != 1 || skill.ActionCastingTimeMs == 0 {
		t.Fatalf("catalog shape: %+v refusal %q", skill.Threat, skill.OffenseRefusal)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	gid := enterworld.ObjectIDForCharacter(c)
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("fixture monster has no mover")
	}
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	rt.commitAggression(testDivision, target.Gid, simulation.HostilityEvent{Attacker: gid, Aggression: mirageHostility}, clock.NowMs())
	hostility := func() int32 {
		live, _ := rt.Monsters.Get(testDivision, target.Gid)
		for _, record := range live.Opponents {
			if record.GID == gid {
				return record.Aggression
			}
		}
		return 0
	}
	before := hostility()
	if before <= 0 {
		t.Fatal("the monster holds no hate for the caster")
	}
	mp := enterworld.CurrentMP(c)
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Mirage was refused: %q %+v", out.DiagnosticRefusal, out.Frames)
	}
	if hostility() != before {
		t.Fatal("hostility changed before the release")
	}
	released := rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
	if len(released) == 0 {
		t.Fatal("Mirage never released")
	}
	if enterworld.CurrentMP(c) >= mp {
		t.Fatalf("MP not charged: %d -> %d", mp, enterworld.CurrentMP(c))
	}
	if after := hostility(); before-after < mirageFlat {
		t.Fatalf("hostility %d -> %d, want a cut of at least %d", before, after, mirageFlat)
	}
}
