/*
===========================================================================

targetinteract_lane_test.go - the target-interact path commits through its lane's doors

A held skill press is replayed by the simulation tick (advanceBasicAttackIntent's
Deferred branch) through the same handleTargetInteractLocked the packet handler
runs. The lane, never the call site, chooses the commit timing (#2036, #3099):
the replay must commit through the deferred doors it was handed, or every replayed
press commits synchronously inside the tick (#2150, live 2026-11-14: about 3.7 ms
of each tick at 104 sessions).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
recordingDoors

A lane's doors that count their commits and forward to the deferred pair.
================
*/
func recordingDoors(rt *Runtime, commits *int) characterDoors {
	tick := rt.tickDoors()
	return characterDoors{
		update: func(c *enterworld.Character, label string, update func() bool) bool {
			*commits++
			return tick.update(c, label, update)
		},
		updateMany: func(cs []*enterworld.Character, label string, update func() bool) bool {
			*commits++
			return tick.updateMany(cs, label, update)
		},
	}
}

/*
================
TestTargetInteractSkillCommitsThroughTheLaneDoors

An offensive skill press dispatched with a lane's doors commits through those
doors: the replayed press of a tick lane joins the tick's batch.
================
*/
func TestTargetInteractSkillCommitsThroughTheLaneDoors(t *testing.T) {
	rt, c, target, skill, _ := arrowFixture(t)
	commits := 0
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	result := rt.handleTargetInteractLocked(testDivision, c, cast.Encode(), recordingDoors(rt, &commits))
	if result.DiagnosticRefusal != "" || commandRefused(result) {
		t.Fatalf("the press was refused: %+v", result)
	}
	if commits == 0 {
		t.Fatal("the press committed outside the lane's doors (the handler's immediate door is hard-wired again)")
	}
}
