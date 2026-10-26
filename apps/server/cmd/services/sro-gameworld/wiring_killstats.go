/*
===========================================================================

wiring_killstats.go - the kill and EXP per online hour rollup (#1639)

Port-only, not native (SRO_KILL_STATS, owner-approved from the #1604
P10 vote): with the flag on, the gameplay runtime counts kills, EXP
and online time into a name-free (day, level band) rollup, this wiring
drains it into the economy history every few minutes and once at
shutdown, and the economy service serves it to the staff console
(/economy/v1/killstats). Off (default) nothing counts and the route
answers 404. The counting and banding live with the settlements
(game/action/killstats.go); the serving and privacy floor live with
the aggregates (platform/economy/killstats.go).

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/platform/economy"
)

/*
================
wireKillStats
================
*/
func (game *gameplayPlane) wireKillStats(application *gameWorldApplication) {
	if !action.KillStatsFromEnv() {
		return
	}
	if application.economy == nil {
		log.Warnf("kill stats: %s is on but the economy aggregates are off (%s=0); nothing will count",
			action.EnvKillStats, economy.EnvSnapshotHours)
		return
	}
	game.items.KillStats = true
	game.items.KillStatsFlush = func(rows []action.KillStatRow) error {
		flushed := make([]economy.KillStatRow, len(rows))
		for i, row := range rows {
			flushed[i] = economy.KillStatRow{DayS: row.DayS, Band: row.Band, Kills: row.Kills,
				Exp: row.Exp, SkillExp: row.SkillExp, OnlineMs: row.OnlineMs, Characters: row.Characters}
		}
		return application.economy.RecordKillStats(flushed)
	}
	application.economy.SetKillStatsEnabled(true)
	application.killStatsFlush = game.items.FlushKillStats
	log.Infof("kill stats: kills and EXP per online hour by level band counted (%s, port-only, not native)", action.EnvKillStats)
}
