/*
===========================================================================

bardaura_reentry_test.go - a running aura reaches members who come back

#2789: a party member who went to town (or arrived through any loading
screen) never got the running dance again until the Bard recast it. The
join walk installed the child the first tick the member's position read
in range - while its session was still scene-loading and absent from
the tick's delivery set - so the installation reached nobody and the
member was recorded as joined for good. The walk now waits for the
member's scene to go live (Runtime.RewardActorPresent), like the party
pickup and concealment lanes, and joins it on the next scan instead.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
danceInstallFor

The aura pass's installation frames addressed to one member (its own
attached-effect add, routed by the member's gid), or nil.
================
*/
func danceInstallFor(frames []simulation.DivisionFrames, member *enterworld.Character) []simulation.Frame {
	gid := enterworld.ObjectIDForCharacter(member)
	for _, routed := range frames {
		if routed.SourceGID != gid {
			continue
		}
		for _, frame := range routed.Frames {
			if frame.Opcode == wire.OpAttachedEffect {
				return routed.Frames
			}
		}
	}
	return nil
}

func tickFrames(rt *Runtime, clock *fakeClock, d time.Duration) []simulation.DivisionFrames {
	clock.Advance(d)
	return rt.TickHook()(clock.NowMs())
}

/*
================
TestDanceRejoinsMemberAfterTownTrip

A member holding the dance goes to town (a loading screen ends its
children) and returns to the training spot: the running dance must be
handed to it again without a recast.
================
*/
func TestDanceRejoinsMemberAfterTownTrip(t *testing.T) {
	rt, clock, c, b := dancingParty(t, danceOfManaID)
	mate := partyMate(rt, c, 99, "dance-mate", 150)
	setParty(rt, c, b, mate)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, danceOfManaID) {
		t.Fatal("the member did not join the dance")
	}

	// To town: the loading screen ends the member's children, and it lands
	// far outside the radius.
	rt.endPartyAurasForLoading(testDivision, mate)
	moveCharacter(rt, mate, c, auraRadius+500)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if hasSkillEffect(rt, mate.Name, danceOfManaID) {
		t.Fatal("the member kept the dance in town")
	}

	// Back at the training spot: the running dance must reach it again.
	moveCharacter(rt, mate, c, 150)
	bardTick(rt, clock, time.Millisecond)
	bardTick(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, danceOfManaID) {
		t.Fatal("the member did not get the dance back on return")
	}
}

/*
================
TestDanceWaitsForMemberStillLoading

#2789: a member whose scene is still loading (it teleported back into
the radius) is not handed the child yet: its session is absent from
the tick's delivery set, so installing then would record a join the
client never saw. The first scan after its scene goes live joins it.
================
*/
func TestDanceWaitsForMemberStillLoading(t *testing.T) {
	rt, clock, c, b := dancingParty(t, danceOfManaID)
	mate := partyMate(rt, c, 99, "loading-mate", 150)
	setParty(rt, c, b, mate)

	sceneLive := false
	rt.RewardActorPresent = func(division, name string) bool {
		return name != mate.Name || sceneLive
	}

	if frames := tickFrames(rt, clock, time.Millisecond); danceInstallFor(frames, mate) != nil {
		t.Fatal("the dance was installed on a member still loading its scene")
	}
	if hasSkillEffect(rt, mate.Name, danceOfManaID) {
		t.Fatal("a member still loading its scene holds the dance")
	}

	sceneLive = true
	frames := tickFrames(rt, clock, time.Millisecond)
	if !hasSkillEffect(rt, mate.Name, danceOfManaID) {
		t.Fatal("the member did not join once its scene went live")
	}
	if danceInstallFor(frames, mate) == nil {
		t.Fatal("the joined member was sent no installation")
	}
}
