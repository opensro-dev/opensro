/*
===========================================================================

agentselect_test.go - selecting a caravan agent (port-only, #3704)

An agent gid resolves on the select lane like a monster: the grant is
the monster writer's HP answer (the client's target gauge), recorded
as the selection. Out of the viewer's ring, or dead, it refuses with
the same [2][3] answer an unknown gid gets.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestObjectSelectAgentGrantCarriesHP

Selecting the thief agent answers with its current HP and records the
selection; after it is wounded the grant tracks the new HP.
================
*/
func TestObjectSelectAgentGrantCarriesHP(t *testing.T) {
	rt, _, a, _ := newPvpPair(t)
	dir := newAgentDirectoryForTest(12000)
	rt.Agents = dir

	outcome := rt.HandleObjectSelect(testDivision, a, selectBody(agentGID))
	if outcome.Refusal != "" {
		t.Fatalf("agent gid refused: %s", outcome.Refusal)
	}
	want := wire.EncodeMonsterObjectSelectResult(agentGID, 12000)
	if len(outcome.Frames) != 1 || !bytes.Equal(outcome.Frames[0].Payload, want) {
		t.Fatalf("agent select frames = %+v, want the HP grant %v", outcome.Frames, want)
	}
	if gid, ok := rt.Selected.Get(testDivision, a.Name); !ok || gid != agentGID {
		t.Fatalf("selection = %d/%v, want the agent %d recorded", gid, ok, agentGID)
	}

	dir.ApplyAgentDamage(testDivision, "[AI]Thief_Viper", "someone", 4500, 0)
	outcome = rt.HandleObjectSelect(testDivision, a, selectBody(agentGID))
	want = wire.EncodeMonsterObjectSelectResult(agentGID, 7500)
	if len(outcome.Frames) != 1 || !bytes.Equal(outcome.Frames[0].Payload, want) {
		t.Fatalf("wounded agent select frames = %+v, want the HP grant %v", outcome.Frames, want)
	}
}

/*
================
TestObjectSelectAgentOutOfScopeAndDead

An agent standing outside the viewer's region ring, and a dead agent,
both refuse with the [2][3] not-found answer.
================
*/
func TestObjectSelectAgentOutOfScopeAndDead(t *testing.T) {
	rt, _, a, _ := newPvpPair(t)
	dir := newAgentDirectoryForTest(12000)
	dir.target.Pose = simulation.Spawn{RegionID: 0x1000, X: 963, Y: 20, Z: 458}
	rt.Agents = dir

	outcome := rt.HandleObjectSelect(testDivision, a, selectBody(agentGID))
	if outcome.Refusal == "" {
		t.Fatal("an out-of-scope agent was selectable")
	}
	if len(outcome.Frames) != 1 || !bytes.Equal(outcome.Frames[0].Payload, []byte{2, 3}) {
		t.Fatalf("out-of-scope select frames %+v, want the [2][3] answer", outcome.Frames)
	}

	dir.target.Pose = simulation.Spawn{RegionID: pvpRegion, X: 963, Y: 20, Z: 458}
	dir.ApplyAgentDamage(testDivision, "[AI]Thief_Viper", "someone", 999999, 0)
	if outcome := rt.HandleObjectSelect(testDivision, a, selectBody(agentGID)); outcome.Refusal == "" {
		t.Fatal("a dead agent remained selectable")
	}
}
