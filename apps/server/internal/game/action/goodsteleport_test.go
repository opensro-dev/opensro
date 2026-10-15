/*
===========================================================================

goodsteleport_test.go - town teleport with a trade transport, both ways

===========================================================================
*/
package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestJobGoodsTeleportFromEnv
================
*/
func TestJobGoodsTeleportFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvJobGoodsTeleport, value)
		if got := JobGoodsTeleportFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}

/*
================
TestTownGateAdmitsATransportOnlyWithTheRule

513A70 kind 2: natively a near transport is refused (0x10); with the rule
it passes. A far transport is refused either way (0x13): it could not
come along.
================
*/
func TestTownGateAdmitsATransportOnlyWithTheRule(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	town := portalDestination{code: "GATE_CH"}
	restricted := portalLink{conditions: []portalCondition{{kind: portalConditionTransport}}}
	for _, tc := range []struct {
		label     string
		traveller portalTraveller
		want      byte
	}{
		{"native near transport", portalTraveller{transportCOS: true}, 0x10},
		{"rule near transport", portalTraveller{transportCOS: true, transportAdmitted: true}, 0},
		{"rule far transport", portalTraveller{transportCOS: true, transportFar: true, transportAdmitted: true}, 0x13},
		{"rule without a transport", portalTraveller{transportAdmitted: true}, 0},
	} {
		if got := portalAdmission(c, town, restricted, tc.traveller); got != tc.want {
			t.Errorf("%s: %#x, want %#x", tc.label, got, tc.want)
		}
	}
}

/*
================
summonTestTransport

Gives the player a summoned trade transport (TID band 2).
================
*/
func summonTestTransport(rt *Runtime, c *enterworld.Character) {
	items := testItems()
	for codename, ref := range rt.deps.(*enterworld.Deps).Items.(staticItemSource) {
		items[codename] = ref
	}
	rt.deps.(*enterworld.Deps).Items = cosTestItemSource{staticItemSource: items, characters: map[string]*enterworld.CharacterRef{
		"COS_FIXTURE": {RefObjID: 3914, Codename: "COS_FIXTURE", TidWord: 0x1c6 | (2 << 11), RunSpeed: 40, WalkSpeed: 20}}}
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_FIXTURE", Summoned: true, CurrentHP: 100}
}

/*
================
TestReturnScrollWithATransportOnlyWithTheRule

4A0380: natively a transport refuses the scroll (0x5E) and nothing starts;
with the rule the cast starts and completes, and the transport stays out.
================
*/
func TestReturnScrollWithATransportOnlyWithTheRule(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, c, clock, ref := returnFixture(t, 5000)
		items := testItems()
		items[ref.Codename] = ref
		rt.deps.(*enterworld.Deps).Items = items
		summonTestTransport(rt, c)
		rt.JobGoodsTeleport = on
		out := useReturn(rt, c)
		if !on {
			if len(out.Frames) == 0 || out.Frames[0].Payload[0] != 2 || out.Frames[0].Payload[1] != 0x5e || c.NativeTeleportMode != 0 {
				t.Fatalf("native: a transport return was admitted: %+v", out.Frames)
			}
			continue
		}
		if c.NativeTeleportMode != 1 {
			t.Fatalf("rule: the return did not start: %+v", out.Frames)
		}
		clock.Advance(5 * time.Second)
		rt.advanceReturnScrolls(clock.NowMs())
		if c.NativeTeleportMode != 0 || c.ActiveCOS == nil || !c.ActiveCOS.Summoned {
			t.Fatalf("rule: return did not complete with the transport out (mode %d, cos %+v)", c.NativeTeleportMode, c.ActiveCOS)
		}
	}
}

/*
================
TestReverseReturnWithATransportOnlyWithTheRule

4F2B50 case 5: natively a transport refuses the guide's reverse return
(0x10); with the rule the player travels to where they died.
================
*/
func TestReverseReturnWithATransportOnlyWithTheRule(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, c := guideFixture(t)
		died := simulation.Spawn{RegionID: 25000, X: 812, Y: 30, Z: 1204}
		c.World.LastDeathPoint = &domain.WorldPoint{WorldSpawn: *worldSpawnFromMission(died)}
		summonTestTransport(rt, c)
		if !rt.hasSummonedTransportCOS(c) {
			t.Fatal("fixture: the COS is not a transport")
		}
		rt.JobGoodsTeleport = on
		out := guideReturn(rt, c, reverseReturnLastDeath)
		refused := len(out.Frames) > 0 && bytes.Equal(out.Frames[0].Payload, portalFailure(errCodeGuideTransport).Frames[0].Payload)
		if refused == on {
			t.Fatalf("rule %v: answered %+v", on, out.Frames)
		}
		if on && (len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient) {
			t.Fatalf("rule: the reverse return did not re-enter the world: %+v", out.Frames)
		}
	}
}
