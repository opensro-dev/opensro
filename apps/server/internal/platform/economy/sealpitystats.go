/*
===========================================================================

sealpitystats.go - the seal pity daily rollup (port-only)

Port-only, not native (#1690, owner decision 2026-10-27). The
GameWorld's pity rollup (game/action/sealpity.go) drains here; this
owner keeps the daily rows in its own history database (seal_pity_stats)
and serves them on /economy/v1/sealpity behind the same operator token.
Name-free end to end: a row is a (UTC day, degree, tier) of debt accrued
(in expected drops), natural drops, forced drops, daily-cap hits and
counters found resting at or past their threshold (always 0 unless a
forced drop could not be prepared). No account or character identity
is ever written, so unlike the kill stats no privacy floor applies.

The route exists only while the shard counts (SRO_SEAL_PITY shadow or
on); off, it answers 404 like any unknown route. Phase A of the #1690
plan reads calibration from these rows: natural drops over debt
accrued is the observed-to-expected ratio.

===========================================================================
*/
package economy

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

// sealPityStatsSchema is the rollup table, added to every history
// database (existing ones gain it on Open).
const sealPityStatsSchema = `
CREATE TABLE IF NOT EXISTS seal_pity_stats (
  day_s    INTEGER NOT NULL,
  degree   INTEGER NOT NULL,
  tier     INTEGER NOT NULL,
  accrued  REAL    NOT NULL,
  natural  INTEGER NOT NULL,
  forced   INTEGER NOT NULL,
  cap_hits INTEGER NOT NULL,
  over     INTEGER NOT NULL,
  PRIMARY KEY (day_s, degree, tier)
);
`

/*
================
SealPityStatRow

One flushed (day, degree, tier) row: additive deltas since the last
flush. Tier is Star, Moon, Sun in enterworld's order (0, 1, 2).
================
*/
type SealPityStatRow struct {
	DayS          int64
	Degree        int
	Tier          int
	DebtAccrued   float64
	NaturalDrops  int64
	ForcedDrops   int64
	DailyCapHits  int64
	OverThreshold int64
}

/*
================
Service.SetSealPityStatsEnabled

The wiring's word that the shard counts seal pity (SRO_SEAL_PITY
shadow or on): only then does the route exist.
================
*/
func (s *Service) SetSealPityStatsEnabled(enabled bool) {
	s.sealPityStats = enabled
}

/*
================
Service.RecordSealPityStats

One flush: every column accumulates (the single GameWorld writer's
deltas are disjoint by construction).
================
*/
func (s *Service) RecordSealPityStats(rows []SealPityStatRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.history.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT INTO seal_pity_stats (day_s, degree, tier, accrued, natural, forced, cap_hits, over)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (day_s, degree, tier) DO UPDATE SET
			accrued = accrued + excluded.accrued,
			natural = natural + excluded.natural,
			forced = forced + excluded.forced,
			cap_hits = cap_hits + excluded.cap_hits,
			over = over + excluded.over`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.Exec(row.DayS, row.Degree, row.Tier, row.DebtAccrued,
			row.NaturalDrops, row.ForcedDrops, row.DailyCapHits, row.OverThreshold); err != nil {
			return fmt.Errorf("seal pity stats: %w", err)
		}
	}
	return tx.Commit()
}

//============================================================================

type sealPityRowJSON struct {
	Degree        int     `json:"degree"`
	Tier          int     `json:"tier"`
	DebtAccrued   float64 `json:"debtAccrued"`
	NaturalDrops  int64   `json:"naturalDrops"`
	ForcedDrops   int64   `json:"forcedDrops"`
	DailyCapHits  int64   `json:"dailyCapHits"`
	OverThreshold int64   `json:"overThreshold"`
	// Calibration is natural drops over debt accrued: what phase A of
	// the #1690 plan compares against its 0.6-1.4 (Moon/Sun) and
	// 0.90-1.10 (degree-9 Star) bands. Zero while no debt accrued.
	Calibration float64 `json:"calibration,omitempty"`
}

type sealPityDayJSON struct {
	DayS int64             `json:"day"`
	Rows []sealPityRowJSON `json:"rows"`
}

/*
================
Service.serveSealPityStats

GET /economy/v1/sealpity?from=<unix>&to=<unix>: whole UTC days from
the first day at or after from through the day holding to (both
default to the last 14 days).
================
*/
func (s *Service) serveSealPityStats(w http.ResponseWriter, q url.Values) {
	toS := s.now().Unix()
	var err error
	if raw := q.Get("to"); raw != "" {
		if toS, err = strconv.ParseInt(raw, 10, 64); err != nil || toS < 0 {
			writeError(w, http.StatusBadRequest, queryError{"bad_bucket", "to is unix seconds"})
			return
		}
	}
	fromS := toS - 14*killStatDaySeconds
	if raw := q.Get("from"); raw != "" {
		if fromS, err = strconv.ParseInt(raw, 10, 64); err != nil || fromS < 0 {
			writeError(w, http.StatusBadRequest, queryError{"bad_bucket", "from is unix seconds"})
			return
		}
	}
	if fromS > toS {
		writeError(w, http.StatusBadRequest, queryError{"bad_bucket", "from is after to"})
		return
	}
	firstDay := fromS / killStatDaySeconds * killStatDaySeconds
	lastDay := toS / killStatDaySeconds * killStatDaySeconds
	if (lastDay-firstDay)/killStatDaySeconds+1 > killStatMaxDays {
		writeError(w, http.StatusBadRequest, queryError{"range_too_wide", "range holds more than 366 days"})
		return
	}
	rows, err := s.history.Query(`SELECT day_s, degree, tier, accrued, natural, forced, cap_hits, over FROM seal_pity_stats
		WHERE day_s >= ? AND day_s <= ? ORDER BY day_s, degree, tier`, firstDay, lastDay)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	days := []sealPityDayJSON{}
	var current *sealPityDayJSON
	for rows.Next() {
		var dayS, natural, forced, capHits, over int64
		var degree, tier int
		var accrued float64
		if err := rows.Scan(&dayS, &degree, &tier, &accrued, &natural, &forced, &capHits, &over); err != nil {
			s.fail(w, err)
			return
		}
		if current == nil || current.DayS != dayS {
			days = append(days, sealPityDayJSON{DayS: dayS, Rows: []sealPityRowJSON{}})
			current = &days[len(days)-1]
		}
		row := sealPityRowJSON{Degree: degree, Tier: tier, DebtAccrued: accrued,
			NaturalDrops: natural, ForcedDrops: forced, DailyCapHits: capHits, OverThreshold: over}
		if accrued > 0 {
			row.Calibration = float64(natural) / accrued
		}
		current.Rows = append(current.Rows, row)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	sort.Slice(days, func(i, j int) bool { return days[i].DayS < days[j].DayS })
	writeJSON(w, map[string]any{"contract": contractVersion, "days": days})
}
