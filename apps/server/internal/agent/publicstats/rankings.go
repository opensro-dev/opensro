/*
===========================================================================

rankings.go - GET /public/v1/rankings: every visible player, ranked

The /players page lists the whole shard, not a top board: by level, by
unique kills or by gear, optionally one race, fifty a page. Each ordering
is built once per shared snapshot (the roster and the kill list) for each
(by, race) and every page slices it, so a crawl of every page costs one
sort. Hidden characters are left out entirely, as on the leaderboard.

Port-only, not native: the original has no public read API.

===========================================================================
*/
package publicstats

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"opensro.online/server/internal/domain"
)

// rankingPageSize is the rows of one page.
const rankingPageSize = 50

// The orderings a request may ask for.
const (
	rankByLevel   = "level"
	rankByUniques = "uniques"
	rankByGear    = "gear"
)

/*
================
rankingKey
================
*/
type rankingKey struct {
	by, race string
}

/*
================
rankingSet

One ordering and the snapshot it was built from.
================
*/
type rankingSet struct {
	rosterUntil, killsUntil time.Time
	rows                    []RankingRow
}

/*
================
rankings

The ranked page. by defaults to level, page to 1; an unknown by, race or a
page below 1 answers 400, a page past the end an empty list.
================
*/
func (s *Service) rankings(r *http.Request) (any, error) {
	query := r.URL.Query()
	by := query.Get("by")
	if by == "" {
		by = rankByLevel
	}
	if by != rankByLevel && by != rankByUniques && by != rankByGear {
		return nil, fmt.Errorf("%w: by must be level, uniques or gear", errBadRequest)
	}
	race := query.Get("race")
	if race != "" && race != "chinese" && race != "european" {
		return nil, fmt.Errorf("%w: race must be chinese or european", errBadRequest)
	}
	page := 1
	if raw := query.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: page must be a whole number from 1", errBadRequest)
		}
		page = n
	}
	rows, err := s.rankedRows(rankingKey{by: by, race: race})
	if err != nil {
		return nil, err
	}
	out := RankingsResponse{By: by, Page: page, Total: len(rows), Pages: max(1, (len(rows)+rankingPageSize-1)/rankingPageSize),
		GeneratedAt: s.now().UTC().Format(time.RFC3339), Rows: []RankingRow{}}
	if race != "" {
		out.Race = &race
	}
	// Past the last page is empty; the bound comes first, so a huge page
	// number never reaches the multiplication.
	if page <= out.Pages {
		start := (page - 1) * rankingPageSize
		out.Rows = append(out.Rows, rows[start:min(start+rankingPageSize, len(rows))]...)
	}
	return out, nil
}

/*
================
rankedRows

The full ordering for key, rebuilt only when the shared roster or kill
list has been refreshed since it was built. Called only while cached holds
s.mu.
================
*/
func (s *Service) rankedRows(key rankingKey) ([]RankingRow, error) {
	kills, err := s.allKills(0)
	if err != nil {
		return nil, err
	}
	r := s.loadRoster()
	if set, ok := s.snap.rankings[key]; ok && set.rosterUntil.Equal(s.snap.rosterUntil) && set.killsUntil.Equal(s.snap.killsUntil) {
		return set.rows, nil
	}
	killsBy := map[int64]int{}
	for _, kill := range kills {
		killsBy[kill.KillerCharID]++
	}
	rows := make([]RankingRow, 0, len(r.all))
	reached := map[string]int64{}
	for _, c := range r.all {
		if c.PublicHidden {
			continue
		}
		race, gender := raceGender(c.ModelCodename)
		if key.race != "" && race != key.race {
			continue
		}
		row := RankingRow{Name: c.Name, Race: race, Gender: gender, Level: level(c), UniqueKills: killsBy[c.ID],
			ExpPercent: s.expPercent(c), Gear: gearScore(c)}
		if job := jobName(c.Job.Type); job != "none" {
			row.Job = job
		}
		if s.src.GuildOf != nil {
			row.Guild, _ = s.src.GuildOf(c)
		}
		if lvl := row.Level; lvl > 0 && lvl <= math.MaxUint8 {
			if at, ok := c.LevelReachedAt[uint8(lvl)]; ok {
				row.ReachedAt = optionalTime(at)
				reached[c.Name] = at
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rankedBefore(key.by, rows[i], rows[j], reached) })
	for i := range rows {
		rows[i].Rank = i + 1
	}
	if s.snap.rankings == nil {
		s.snap.rankings = map[rankingKey]rankingSet{}
	}
	s.snap.rankings[key] = rankingSet{rosterUntil: s.snap.rosterUntil, killsUntil: s.snap.killsUntil, rows: rows}
	return rows, nil
}

/*
================
rankedBefore

The orderings the site asked for, each ending on the name so equal rows
keep one order between snapshots. A level reached earlier ranks first; a
level with no recorded time (reached before tracking) ranks after.
================
*/
func rankedBefore(by string, a, b RankingRow, reached map[string]int64) bool {
	switch by {
	case rankByUniques:
		if a.UniqueKills != b.UniqueKills {
			return a.UniqueKills > b.UniqueKills
		}
		if a.Level != b.Level {
			return a.Level > b.Level
		}
	case rankByGear:
		if a.Gear.PlusTotal != b.Gear.PlusTotal {
			return a.Gear.PlusTotal > b.Gear.PlusTotal
		}
		if a.Gear.BestPlus != b.Gear.BestPlus {
			return a.Gear.BestPlus > b.Gear.BestPlus
		}
		if a.Level != b.Level {
			return a.Level > b.Level
		}
	default:
		if a.Level != b.Level {
			return a.Level > b.Level
		}
		if a.ExpPercent != b.ExpPercent {
			return a.ExpPercent > b.ExpPercent
		}
		at, aok := reached[a.Name]
		bt, bok := reached[b.Name]
		if aok != bok {
			return aok
		}
		if aok && at != bt {
			return at < bt
		}
	}
	return a.Name < b.Name
}

/*
================
expPercent

Progress to the next level, 0..100 to one decimal, from the level table's
requirement; 0 when the table has no row for the level.
================
*/
func (s *Service) expPercent(c *domain.Character) float64 {
	if s.src.ExpRequired == nil || c.Experience == nil {
		return 0
	}
	required, ok := s.src.ExpRequired(level(c))
	if !ok || required <= 0 {
		return 0
	}
	percent := float64(*c.Experience) * 100 / float64(required)
	return math.Round(min(max(percent, 0), 100)*10) / 10
}

/*
================
gearScore

The plus of every worn piece (equipment sockets 0..12): their sum and the
highest.
================
*/
func gearScore(c *domain.Character) Gear {
	var g Gear
	for _, row := range c.MissionInventory {
		if row.Slot >= 0 && row.Slot < equipmentSlots && row.RefObjID != 0 {
			g.PlusTotal += int(row.Plus)
			g.BestPlus = max(g.BestPlus, int(row.Plus))
		}
	}
	return g
}
