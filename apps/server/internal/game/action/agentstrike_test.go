/*
===========================================================================

agentstrike_test.go - player strikes on caravan agents (port-only, #3704)

The agent arm of the cast stage: an agent gid resolves through the
directory, the job-PvP refusal judges the pair, and a landed basic
attack sends its rolled damage through the directory's damage door.
With no directory wired, an agent gid resolves to nothing, exactly as
before this slice.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// fakeAgentHit records one damage-door call.
type fakeAgentHit struct {
	agent    string
	attacker string
	damage   int
}

// fakeAgentDirectory is the test AgentDirectory: one agent at a fixed
// pose, its HP behind the same door the real runtime owns.
type fakeAgentDirectory struct {
	gid    uint32
	target AgentTarget
	hp     int
	dead   bool
	hits   []fakeAgentHit
}

/*
================
fakeAgentDirectory.AgentByGID
================
*/
func (f *fakeAgentDirectory) AgentByGID(_ string, gid uint32) (AgentTarget, bool) {
	if f.dead || gid != f.gid {
		return AgentTarget{}, false
	}
	target := f.target
	target.HP = f.hp
	return target, true
}

/*
================
fakeAgentDirectory.ApplyAgentDamage
================
*/
func (f *fakeAgentDirectory) ApplyAgentDamage(_, agentName, attackerName string, damage int, _ int64) bool {
	f.hits = append(f.hits, fakeAgentHit{agent: agentName, attacker: attackerName, damage: damage})
	f.hp -= damage
	if f.hp <= 0 {
		f.dead = true
		return true
	}
	return false
}

// agentGID is the object gid of the first synthetic caravan identity.
var agentGID = simulation.PlayerObjectID(0x70000000)

/*
================
dressJobForTest

Puts a job suit of the given job on the character's job slot, the way
a dressed player's record reads (enterworld.DressedJob). The suit is
registered with the runtime's item source, as a real suit row is, so
the loadout lanes resolve it.
================
*/
func dressJobForTest(rt *Runtime, c *enterworld.Character, job uint8) {
	codename := fmt.Sprintf("TEST_JOB_SUIT_%d", job)
	suit := &enterworld.ItemRef{
		RefObjID: 9000 + uint32(job),
		Codename: codename,
		TypeIDs:  [4]int64{3, 1, 7, int64(job)},
	}
	if deps, ok := rt.deps.(*enterworld.Deps); ok {
		if items, ok := deps.Items.(staticItemSource); ok {
			items[codename] = suit
		}
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot:       enterworld.JobSuitSlot,
		RefObjID:   suit.RefObjID,
		Codename:   codename,
		TypeFlags:  suit.TypeFlags(),
		StackCount: 1,
	})
}

/*
================
newAgentDirectoryForTest

A thief agent standing three units from the PvP fixture's attacker.
================
*/
func newAgentDirectoryForTest(hp int) *fakeAgentDirectory {
	return &fakeAgentDirectory{
		gid: agentGID,
		hp:  hp,
		target: AgentTarget{
			Name:       "[AI]Thief_Viper",
			JobType:    domain.JobThief,
			Pose:       simulation.Spawn{RegionID: pvpRegion, X: 963, Y: 20, Z: 458},
			BodyRadius: 4,
			Level:      65,
			MaxHP:      hp,
		},
	}
}

/*
================
TestPlayerBasicAttackStrikesAgent

A trader's basic attack on a thief agent resolves, passes the job
refusal, and lands real damage through the directory; the cast result
is published for the attacker and the room.
================
*/
func TestPlayerBasicAttackStrikesAgent(t *testing.T) {
	rt, clock, a, _ := newPvpPair(t)
	dressJobForTest(rt, a, domain.JobTrader)
	dir := newAgentDirectoryForTest(100000)
	rt.Agents = dir

	target, ok := rt.resolveCombatTarget(testDivision, a, agentGID, clock.NowMs())
	if !ok || target.agent == nil {
		t.Fatalf("the agent gid did not resolve to an agent target: ok=%v target=%+v", ok, target)
	}
	if !rt.combatTargetExists(testDivision, a, agentGID) {
		t.Fatal("the agent does not exist for the cast-validity lane")
	}

	out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: agentGID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if len(dir.hits) == 0 {
		t.Fatal("no damage reached the agent directory")
	}
	total := 0
	for _, hit := range dir.hits {
		if hit.agent != "[AI]Thief_Viper" || hit.attacker != a.Name {
			t.Fatalf("damage door call = %+v, want the thief struck by %s", hit, a.Name)
		}
		total += hit.damage
	}
	if total <= 0 {
		t.Fatalf("the strike dealt %d total damage, want real rolled damage", total)
	}
}

/*
================
TestPlayerAttackRefusesNonHostileJobAgent

The port refusal: a trader may not open on a trader agent, and an
undressed player may not open on any agent - both answer 0x3006 and
no damage lands.
================
*/
func TestPlayerAttackRefusesNonHostileJobAgent(t *testing.T) {
	cases := []struct {
		name    string
		dress   uint8
		agentIs uint8
	}{
		{"same job", domain.JobTrader, domain.JobTrader},
		{"no suit", 0, domain.JobThief},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, a, _ := newPvpPair(t)
			if tc.dress != 0 {
				dressJobForTest(rt, a, tc.dress)
			}
			dir := newAgentDirectoryForTest(100000)
			dir.target.JobType = tc.agentIs
			rt.Agents = dir

			out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: agentGID}.Encode())
			refusal, ok := findFrame(out.Frames, wire.OpSkillCastResult)
			if !ok || len(refusal.Payload) != 2 || refusal.Payload[0] != 2 || refusal.Payload[1] != 0x06 {
				t.Fatalf("attack answer = %+v (%s), want the 0x3006 refusal", out.Frames, out.DiagnosticRefusal)
			}
			if len(dir.hits) != 0 {
				t.Fatalf("a refused attack dealt damage: %+v", dir.hits)
			}
		})
	}
}

/*
================
TestPlayerBasicAttackKillsAgent

A lethal strike reports the agent defeated through the door, the
death frame rides the broadcast, and the corpse no longer resolves.
================
*/
func TestPlayerBasicAttackKillsAgent(t *testing.T) {
	rt, clock, a, _ := newPvpPair(t)
	dressJobForTest(rt, a, domain.JobHunter)
	dir := newAgentDirectoryForTest(1)
	rt.Agents = dir

	out := rt.HandleTargetInteract(testDivision, a, wire.BasicAttackEngage{TargetGid: agentGID}.Encode())
	if !dir.dead {
		t.Fatalf("the 1-HP agent survived: hits=%+v (%s)", dir.hits, out.DiagnosticRefusal)
	}
	if _, ok := findFrame(out.Broadcast, wire.OpObjectStateRefresh); !ok {
		t.Fatalf("no death frame in the broadcast: %+v", out.Broadcast)
	}
	if _, ok := rt.resolveCombatTarget(testDivision, a, agentGID, clock.NowMs()); ok {
		t.Fatal("the dead agent still resolves as a target")
	}
}

/*
================
TestAgentGidResolvesToNothingWithoutDirectory

Flag off: with no directory wired, the synthetic gid is not a target,
as before this slice.
================
*/
func TestAgentGidResolvesToNothingWithoutDirectory(t *testing.T) {
	rt, clock, a, _ := newPvpPair(t)
	if _, ok := rt.resolveCombatTarget(testDivision, a, agentGID, clock.NowMs()); ok {
		t.Fatal("an agent gid resolved with no directory wired")
	}
	if rt.combatTargetExists(testDivision, a, agentGID) {
		t.Fatal("an agent gid exists with no directory wired")
	}
}
