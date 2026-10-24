/*
===========================================================================

mostwantednotice_test.go - the killer's own Most Wanted progress line

Port-only, not native: checked with SRO_MOST_WANTED_STREAK_NOTICE off (no
notice until a level, as before) and on (one notice per counted kill, to
the killer alone).

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
)

/*
================
TestMostWantedStreakText

Kills toward the next level, in kills under the test pacing's step, and
the cap.
================
*/
func TestMostWantedStreakText(t *testing.T) {
	for _, tc := range []struct {
		streak, step int64
		want         string
	}{
		{1, 1, "Most Wanted: 1/10 to LV 1"},
		{9, 1, "Most Wanted: 9/10 to LV 1"},
		{10, 1, "Most Wanted: 0/10 to LV 2"},
		{37, 1, "Most Wanted: 7/10 to LV 4"},
		{6, 2, "Most Wanted: 3/5 to LV 1"},
		{99, 1, "Most Wanted: 9/10 to LV 10"},
		{100, 1, "Most Wanted: LV 10 (max)"},
		{140, 2, "Most Wanted: LV 10 (max)"},
	} {
		if got := mostWantedStreakText(tc.streak, tc.step); got != tc.want {
			t.Fatalf("streak %d step %d: %q, want %q", tc.streak, tc.step, got, tc.want)
		}
	}
}

/*
================
TestMostWantedStreakNoticeFollowsTheFlag

A counted job kill under SRO_MOST_WANTED: off, the killer's frames carry no
notice; on, exactly the progress notice, and the victim hears nothing. A
kill that does not count (a related victim) sends nothing either way.
================
*/
func TestMostWantedStreakNoticeFollowsTheFlag(t *testing.T) {
	for _, on := range []bool{false, true} {
		rt, _, killer, victim := newPvpPair(t)
		rt.MostWanted, rt.MostWantedStreakNotice = true, on
		killer.MostWanted = &domain.MostWanted{Streak: 3}
		kill := playerKill{kind: pk.DeathJob, mostWantedStrangers: true}.withVictim(killer, victim)
		actor, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 0)
		if killer.MostWanted.Streak != 4 {
			t.Fatalf("on=%v: streak %d, want 4", on, killer.MostWanted.Streak)
		}
		want := wire.JobKillNotificationFrame("Most Wanted: 4/10 to LV 1")
		if got := countFrame(actor, want); got != map[bool]int{false: 0, true: 1}[on] {
			t.Fatalf("on=%v: %d progress notices in %+v", on, got, actor)
		}
		kill.mostWantedStrangers = false
		related, _, _ := rt.payPlayerKillInDoor(testDivision, killer, victim, kill, 1)
		for _, f := range related {
			if f.Opcode == want.Opcode {
				t.Fatalf("on=%v: a kill that did not count sent %+v", on, f)
			}
		}
	}
}

/*
================
countFrame
================
*/
func countFrame(frames []wire.Frame, want wire.Frame) int {
	n := 0
	for _, f := range frames {
		if f.Opcode == want.Opcode && reflect.DeepEqual(f.Payload, want.Payload) {
			n++
		}
	}
	return n
}
