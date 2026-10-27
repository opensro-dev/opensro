/*
===========================================================================

wiring_sealpity.go - the seal pity daily rollup sink (#1690)

Port-only, not native (SRO_SEAL_PITY, owner decision 2026-10-27): with
the rule counting (shadow or on, wired in wiring_gameplay.go), the
gameplay runtime accrues debt, natural and forced drops per (UTC day,
degree, tier); this wiring drains the rollup into the economy history
every few minutes and once at shutdown, and the economy service serves
it to the staff console (/economy/v1/sealpity). Off (default) nothing
counts and the route answers 404. The counters themselves persist on
the characters, so a missing sink loses only the measurement, never a
player's debt.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/platform/economy"
)

/*
================
wireSealPityStats
================
*/
func (game *gameplayPlane) wireSealPityStats(application *gameWorldApplication) {
	if !game.items.SealPity.Enabled() {
		return
	}
	if application.economy == nil {
		log.Warnf("seal pity: %s is %s but the economy aggregates are off (%s=0); the counters persist on characters but nothing will be measured",
			progression.EnvSealPity, game.items.SealPity.Mode().Name(), economy.EnvSnapshotHours)
		return
	}
	game.items.SealPityStatsFlush = func(rows []action.SealPityStatRow) error {
		flushed := make([]economy.SealPityStatRow, len(rows))
		for i, row := range rows {
			flushed[i] = economy.SealPityStatRow{DayS: row.DayS, Degree: row.Degree, Tier: row.Tier,
				DebtAccrued: row.DebtAccrued, NaturalDrops: row.NaturalDrops, ForcedDrops: row.ForcedDrops,
				DailyCapHits: row.DailyCapHits, OverThreshold: row.OverThreshold}
		}
		return application.economy.RecordSealPityStats(flushed)
	}
	application.economy.SetSealPityStatsEnabled(true)
	application.sealPityFlush = game.items.FlushSealPityStats
	log.Infof("seal pity: debt and drop rollup by day, degree and tier counted (%s, port-only, not native)", progression.EnvSealPity)
}
