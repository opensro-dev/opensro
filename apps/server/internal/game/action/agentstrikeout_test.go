/*
===========================================================================

agentstrikeout_test.go - caravan agent strikes on players (port-only)

The agent as a synthetic attacker: its weapon ranges and skill row
feed the monster strike's formula, and the hit commits through the
shared player-victim door - the victim loses real HP, sees its own
frames, and a lethal strike defeats it with no killer booked.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
newStrikingAgentForTest

A thief agent standing on the PvP fixture's victim, armed with the
fixture's basic skill and a sword-shaped weapon range.
================
*/
func newStrikingAgentForTest() AgentTarget {
	return AgentTarget{
		GID:               agentGID,
		Name:              "[AI]Thief_Viper",
		JobType:           domain.JobThief,
		Pose:              simulation.Spawn{RegionID: pvpRegion, X: 963, Y: 20, Z: 458},
		BodyRadius:        4,
		Level:             65,
		HP:                11000,
		MaxHP:             11000,
		SkillID:           2,
		PhysicalAttackMin: 15,
		PhysicalAttackMax: 18,
		MagicalAttackMin:  25,
		MagicalAttackMax:  31,
	}
}

/*
================
TestAgentStrikeHitsPlayer

The strike commits: the victim's HP drops, the cast result names the
agent as caster in the room's frames, and the victim's own frames are
addressed to it.
================
*/
func TestAgentStrikeHitsPlayer(t *testing.T) {
	rt, clock, _, v := newPvpPair(t)
	before := *v.CurrentHP

	result := rt.StrikePlayerByAgent(testDivision, newStrikingAgentForTest(), v.Name, clock.NowMs())
	if !result.Committed {
		t.Fatal("the agent strike did not commit")
	}
	if *v.CurrentHP >= before {
		t.Fatalf("victim HP %d -> %d, want real damage", before, *v.CurrentHP)
	}
	if _, ok := findFrame(result.Public, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result in the room's frames: %+v", result.Public)
	}
	if result.Victim.CharacterID != v.ID {
		t.Fatalf("victim frames addressed to %d, want the victim %d", result.Victim.CharacterID, v.ID)
	}
	if result.Defeated {
		t.Fatal("a full-HP victim fell to one strike")
	}
}

/*
================
TestAgentStrikeDefeatsPlayer

A lethal strike defeats the victim; nobody's PK record moves (the
killer is the world, as with an environmental death).
================
*/
func TestAgentStrikeDefeatsPlayer(t *testing.T) {
	rt, clock, a, v := newPvpPair(t)
	v.CurrentHP = testInt64(1)

	result := rt.StrikePlayerByAgent(testDivision, newStrikingAgentForTest(), v.Name, clock.NowMs())
	if !result.Committed || !result.Defeated {
		t.Fatalf("result = %+v, want a committed defeat", result)
	}
	if enterworld.CharacterAlive(v) {
		t.Fatal("the 1-HP victim survived the strike")
	}
	if a.PK != nil && a.PK.TotalCount != 0 {
		t.Fatalf("a bystander's PK record moved: %+v", a.PK)
	}
}

/*
================
TestAgentStrikeOutOfReach

A victim beyond the reach bound, and an unknown victim name, commit
nothing.
================
*/
func TestAgentStrikeOutOfReach(t *testing.T) {
	rt, clock, _, v := newPvpPair(t)
	agent := newStrikingAgentForTest()
	agent.Pose = simulation.Spawn{RegionID: pvpRegion, X: 1063, Y: 20, Z: 458}
	if result := rt.StrikePlayerByAgent(testDivision, agent, v.Name, clock.NowMs()); result.Committed {
		t.Fatal("a strike from 100 units away committed")
	}
	if result := rt.StrikePlayerByAgent(testDivision, newStrikingAgentForTest(), "noSuchPlayer", clock.NowMs()); result.Committed {
		t.Fatal("a strike on an unknown victim committed")
	}
	if *v.CurrentHP != 100 {
		t.Fatalf("victim HP = %d, want untouched", *v.CurrentHP)
	}
}
