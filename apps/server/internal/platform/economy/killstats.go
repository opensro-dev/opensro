/*
===========================================================================

killstats.go - kills and EXP per online hour by level band (port-only)

Port-only, not native (#1639, owner-approved from the #1604 P10 vote).
The GameWorld's kill-stat rollup (game/action) drains here; this owner
keeps the daily rows in its own history database (kill_stats) and
serves them on /economy/v1/killstats behind the same operator token.
Name-free end to end: a row is a (UTC day, 10-level band) of counts -
kills, character EXP, skill EXP, online milliseconds and a distinct
character count. No account or character identity is ever written.

Privacy floor: a band whose day saw fewer than MinKillStatCharacters
distinct characters is withheld whole (its counts could single out
one player's hours); the response reports how many were withheld.

The route exists only while the shard counts (SRO_KILL_STATS on);
off, it answers 404 like any unknown route.

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

// MinKillStatCharacters is the privacy floor: a (day, band) with fewer
// distinct characters than this is never served.
const MinKillStatCharacters = 3

// killStatMaxDays bounds one answer, like maxBuckets bounds the others.
const killStatMaxDays = 366

// killStatDaySeconds is the UTC day the rows are keyed by.
const killStatDaySeconds = 24 * 60 * 60

// killStatsSchema is the rollup table, added to every history database
// (existing ones gain it on Open).
const killStatsSchema = `
CREATE TABLE IF NOT EXISTS kill_stats (
  day_s    INTEGER NOT NULL,
  band     INTEGER NOT NULL,
  kills    INTEGER NOT NULL,
  exp      INTEGER NOT NULL,
  skill_exp INTEGER NOT NULL,
  online_ms INTEGER NOT NULL,
  chars    INTEGER NOT NULL,
  PRIMARY KEY (day_s, band)
);
`

/*
================
KillStatRow

One flushed (day, band) row: additive deltas since the last flush and
the band's distinct characters so far that day (a count, never an
identity).
================
*/
type KillStatRow struct {
	DayS       int64
	Band       int
	Kills      int64
	Exp        int64
	SkillExp   int64
	OnlineMs   int64
	Characters int
}

/*
================
Service.SetKillStatsEnabled

The wiring's word that the shard counts (SRO_KILL_STATS): only then
does the route exist.
================
*/
func (s *Service) SetKillStatsEnabled(enabled bool) {
	s.killStats = enabled
}

/*
================
Service.RecordKillStats

One flush: the additive columns accumulate; the character count is
the writer's distinct-so-far for the day and replaces (the single
GameWorld writer only ever grows it within a day).
================
*/
func (s *Service) RecordKillStats(rows []KillStatRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.history.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO kill_stats (day_s, band, kills, exp, skill_exp, online_ms, chars)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (day_s, band) DO UPDATE SET
			kills = kills + excluded.kills,
			exp = exp + excluded.exp,
			skill_exp = skill_exp + excluded.skill_exp,
			online_ms = online_ms + excluded.online_ms,
			chars = excluded.chars`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.Exec(row.DayS, row.Band, row.Kills, row.Exp, row.SkillExp, row.OnlineMs, row.Characters); err != nil {
			return fmt.Errorf("kill stats: %w", err)
		}
	}
	return tx.Commit()
}

//============================================================================

/*
================
killStatBandLabel

Band 0 is levels 1-10, band 1 is 11-20, and so on.
================
*/
func killStatBandLabel(band int) string {
	return fmt.Sprintf("%d-%d", band*10+1, band*10+10)
}

type killStatBandJSON struct {
	Band                  int     `json:"band"`
	Label                 string  `json:"label"`
	Characters            int     `json:"characters"`
	Kills                 int64   `json:"kills"`
	Exp                   int64   `json:"exp"`
	SkillExp              int64   `json:"skillExp"`
	OnlineHours           float64 `json:"onlineHours"`
	KillsPerOnlineHour    float64 `json:"killsPerOnlineHour"`
	ExpPerOnlineHour      float64 `json:"expPerOnlineHour"`
	SkillExpPerOnlineHour float64 `json:"skillExpPerOnlineHour"`
}

type killStatDayJSON struct {
	DayS  int64              `json:"day"`
	Bands []killStatBandJSON `json:"bands"`
}

/*
================
Service.serveKillStats

GET /economy/v1/killstats?from=<unix>&to=<unix>: whole UTC days from
the first day at or after from through the day holding to (both
default to the last 14 days). Bands under the privacy floor are
withheld and counted, never named.
================
*/
func (s *Service) serveKillStats(w http.ResponseWriter, q url.Values) {
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
	rows, err := s.history.Query(`SELECT day_s, band, kills, exp, skill_exp, online_ms, chars FROM kill_stats
		WHERE day_s >= ? AND day_s <= ? ORDER BY day_s, band`, firstDay, lastDay)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	days := []killStatDayJSON{}
	suppressed := 0
	var current *killStatDayJSON
	for rows.Next() {
		var dayS, kills, exp, skillExp, onlineMs int64
		var band, chars int
		if err := rows.Scan(&dayS, &band, &kills, &exp, &skillExp, &onlineMs, &chars); err != nil {
			s.fail(w, err)
			return
		}
		if chars < MinKillStatCharacters {
			suppressed++
			continue
		}
		if current == nil || current.DayS != dayS {
			days = append(days, killStatDayJSON{DayS: dayS, Bands: []killStatBandJSON{}})
			current = &days[len(days)-1]
		}
		hours := float64(onlineMs) / 3600000
		row := killStatBandJSON{Band: band, Label: killStatBandLabel(band), Characters: chars,
			Kills: kills, Exp: exp, SkillExp: skillExp, OnlineHours: hours}
		if onlineMs > 0 {
			row.KillsPerOnlineHour = float64(kills) / hours
			row.ExpPerOnlineHour = float64(exp) / hours
			row.SkillExpPerOnlineHour = float64(skillExp) / hours
		}
		current.Bands = append(current.Bands, row)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	sort.Slice(days, func(i, j int) bool { return days[i].DayS < days[j].DayS })
	writeJSON(w, map[string]any{"contract": contractVersion, "minCharacters": MinKillStatCharacters,
		"suppressed": suppressed, "days": days})
}
