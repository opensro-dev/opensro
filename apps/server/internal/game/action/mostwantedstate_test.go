/*
===========================================================================

mostwantedstate_test.go - the wanted-state frame's senders

Port-only, not native (#1849): with SRO_MOST_WANTED on, a counted kill
tells the killer its new state and a clear tells it the streak is gone;
off, neither frame exists, as today.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
wantedStateFrame

The one wanted-state frame in a frame list, or none.
================
*/
func wantedStateFrame(frames []wire.Frame) (wire.MostWantedState, bool) {
	for _, f := range frames {
		if f.Opcode == wire.OpMostWantedState {
			p := f.Payload
			return wire.MostWantedState{Level: p[0], KillsIntoLevel: p[1], KillsPerLevel: p[2], Job: p[3], DamagePercent: p[4]}, true
		}
	}
	return wire.MostWantedState{}, false
}

/*
================
TestMostWantedStateFrameReportsTheStreak
================
*/
func TestMostWantedStateFrameReportsTheStreak(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	c := testCharacter()
	c.MostWanted = &domain.MostWanted{Streak: 47}
	dressThief(c)
	if frames := rt.mostWantedStateFrame(c); frames != nil {
		t.Fatalf("flag off sent %v", frames)
	}
	rt.MostWanted = true
	state, ok := wantedStateFrame(rt.mostWantedStateFrame(c))
	if !ok || state != (wire.MostWantedState{Level: 4, KillsIntoLevel: 7, KillsPerLevel: 10, Job: domain.JobThief, DamagePercent: 1}) {
		t.Fatalf("state %+v (sent %v)", state, ok)
	}
	c.MostWanted = nil
	if state, _ := wantedStateFrame(rt.mostWantedStateFrame(c)); state.Level != 0 || state.KillsIntoLevel != 0 {
		t.Fatalf("a cleared streak reported %+v", state)
	}
}

/*
================
TestMostWantedKillSendsTheStateFrame

Through payPlayerKillInDoor: the counted kill that crosses into level 1
carries the killer's new state; with the rule off, no frame.
================
*/
func TestMostWantedKillSendsTheStateFrame(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted = on
		dressThief(killer)
		killer.MostWanted = &domain.MostWanted{Streak: 9}
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
		actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		state, sent := wantedStateFrame(actor)
		if sent != on {
			t.Fatalf("on=%v: state frame sent %v", on, sent)
		}
		if on && state != (wire.MostWantedState{Level: 1, KillsIntoLevel: 0, KillsPerLevel: 10, Job: domain.JobThief, DamagePercent: 0}) {
			t.Fatalf("on: state %+v", state)
		}
	}
}

/*
================
TestMostWantedClearSendsTheClearedFrame

Death tells the holder its streak is gone - also below level 1, where no
stat refresh follows - and with the rule off it hears nothing.
================
*/
func TestMostWantedClearSendsTheClearedFrame(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, actor, _ := newPvpPair(t)
		rt.MostWanted = on
		dressThief(actor)
		actor.MostWanted = &domain.MostWanted{Streak: 5}
		_, progression := rt.settlePlayerDeathInDoor(testDivision, actor, deathKiller{}, 0)
		state, sent := wantedStateFrame(progression)
		if sent != on {
			t.Fatalf("on=%v: cleared frame sent %v (streak %v)", on, sent, actor.MostWanted)
		}
		if on && (state.Level != 0 || state.KillsIntoLevel != 0) {
			t.Fatalf("on: cleared state %+v", state)
		}
	}
}

/*
================
TestMostWantedStateFrameLayout

The payload the client decodes, byte for byte.
================
*/
func TestMostWantedStateFrameLayout(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), testItems())
	rt.MostWanted = true
	c := testCharacter()
	c.MostWanted = &domain.MostWanted{Streak: 100}
	dressThief(c)
	frames := rt.mostWantedStateFrame(c)
	if len(frames) != 1 || !bytes.Equal(frames[0].Payload, []byte{10, 10, 10, 2, 3}) {
		t.Fatalf("frames %+v", frames)
	}
}
