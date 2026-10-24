/*
===========================================================================

mostwantednotice.go - the killer's own Most Wanted progress line

Port-only, not native (owner decision 2026-10-24, after a player reported
that Most Wanted "isn't working": nothing shows until the tenth counted
kill). SRO_MOST_WANTED_STREAK_NOTICE sends the killer, and only the
killer, one notice for every counted kill:

	Most Wanted: N/10 to LV L     (N kills toward the next level L)
	Most Wanted: LV 10 (max)      (at the cap)

The notice is the job-kill channel's notification frame (the one the
shard's Most Wanted notices use), in the kill's actor frames: it reaches
no other session and no chat feed, and has no per-minute cap. Under the
test pacing the count is in kills, so it reads N/5.

It does nothing unless SRO_MOST_WANTED is on. Off, the default here, is
the current rule: a streak says nothing until it makes a level.

===========================================================================
*/
package action

import (
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// EnvMostWantedStreakNotice turns the killer's progress notice on ("on",
// "1", "true").
const EnvMostWantedStreakNotice = "SRO_MOST_WANTED_STREAK_NOTICE"

/*
================
MostWantedStreakNoticeFromEnv
================
*/
func MostWantedStreakNoticeFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvMostWantedStreakNotice))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
mostWantedStreakText

The progress line for a streak. step is what one counted kill adds, so
the count reads in kills under the test pacing as well.
================
*/
func mostWantedStreakText(streak, step int64) string {
	level := min(streak/mostWantedKillsPerLevel, mostWantedMaxLevel)
	if level >= mostWantedMaxLevel {
		return fmt.Sprintf("Most Wanted: LV %d (max)", mostWantedMaxLevel)
	}
	step = max(step, 1)
	return fmt.Sprintf("Most Wanted: %d/%d to LV %d",
		streak%mostWantedKillsPerLevel/step, mostWantedKillsPerLevel/step, level+1)
}

/*
================
Runtime.mostWantedStreakNotice

The killer's notice for a kill that just counted, or nothing with the
flag off.
================
*/
func (rt *Runtime) mostWantedStreakNotice(killer *enterworld.Character) []wire.Frame {
	if !rt.MostWantedStreakNotice || killer == nil || killer.MostWanted == nil {
		return nil
	}
	return []wire.Frame{wire.JobKillNotificationFrame(mostWantedStreakText(killer.MostWanted.Streak, rt.mostWantedStreakStep()))}
}
