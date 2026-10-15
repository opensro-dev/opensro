/*
===========================================================================

rankings.go - GET /public/v1/rankings: every visible player, ranked

The /players page lists the whole shard, not a top board: by level, by
unique kills or by gear, optionally one race, fifty a page. Each ordering
is built once per shared snapshot (the roster and the kill list) for each
(by, race) and every page slices it, so a crawl of every page costs one
sort. Hidden characters are left out entirely, as on the leaderboard.

Port-only, not native: the original has no public read API.

The gear score is site-only too (not native, not in game): it leaves the
shield socket out, so a one-handed weapon with a shield and a two-handed
weapon score the same at equal enhancement and every weapon counts once
(owner decision 2026-10-15, Option 2 of the balance advisor's choice).

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
	lastLogin := map[string]int64{}
	for _, c := range r.all {
		if c.PublicHidden {
			continue
		}
		race, gender := raceGender(c.ModelCodename)
		if key.race != "" && race != key.race {
			continue
		}
		row := RankingRow{Name: c.Name, Race: race, Gender: gender, Level: level(c), UniqueKills: killsBy[c.ID],
			ExpPercent: s.expPercent(c), Gear: s.gearScore(c)}
		if job := jobName(c.Job.Type); job != "none" {
			row.Job = job
		}
		if s.src.GuildOf != nil {
			row.Guild, _ = s.src.GuildOf(c)
		}
		if lvl := row.Level; lvl > 0 && lvl <= math.MaxUint8 {
			if at, ok := c.LevelReachedAt[uint8(lvl)]; ok {
				row.ReachedAt = optionalTime(at)
			}
		}
		lastLogin[c.Name] = c.LastLoginAtMs
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rankedBefore(key.by, rows[i], rows[j], lastLogin) })
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
keep one order between snapshots. By level, players of one level rank by
their latest login, the most recent first (owner 2026-10-15: the board
shows who is active at the top, not who got there first); a character
that never logged in since tracking (0) ranks after. The login time only
orders: it is not in the response.
================
*/
func rankedBefore(by string, a, b RankingRow, lastLogin map[string]int64) bool {
	switch by {
	case rankByUniques:
		if a.UniqueKills != b.UniqueKills {
			return a.UniqueKills > b.UniqueKills
		}
		if a.Level != b.Level {
			return a.Level > b.Level
		}
	case rankByGear:
		if a.Gear.Points != b.Gear.Points {
			return a.Gear.Points > b.Gear.Points
		}
		if a.Gear.best != b.Gear.best {
			return a.Gear.best > b.Gear.best
		}
		if a.Level != b.Level {
			return a.Level > b.Level
		}
	default:
		if a.Level != b.Level {
			return a.Level > b.Level
		}
		if at, bt := lastLogin[a.Name], lastLogin[b.Name]; at != bt {
			return at > bt
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

// Gear points (owner decision 2026-10-14, BUG-082; website only): each
// degree is worth gearPointsPerDegree, a seal adds its tier's bonus (a Star
// about one degree, as players value it), and each plus one point, so a
// +10 is worth a degree too.
const gearPointsPerDegree = 10

// gearSealBonus by seal tier, in ItemGrade's order: Star, Moon, Sun.
var gearSealBonus = [...]int{10, 15, 20}

/*
================
gearPoints

One worn piece's points: 10 x degree + seal bonus + plus.
================
*/
func gearPoints(degree, tier int, sealed bool, plus int) int {
	bonus := 0
	if sealed && tier >= 0 && tier < len(gearSealBonus) {
		bonus = gearSealBonus[tier]
	}
	return gearPointsPerDegree*max(degree, 0) + bonus + plus
}

// gearShieldSocket is the worn shield socket (inventory.SocketShield, 7),
// which the gear score leaves out; arrows and bolts worn there are
// ammunition, not gear, either way.
const gearShieldSocket = 7

/*
================
Service.gearScore

Every worn piece (equipment sockets 0..12) but the shield: the plus sum
and highest, and the gear points the ranking orders by, with the
strongest piece's points as its first tie-break. Leaving the shield out
scores a one-handed build the same as a two-handed one.
================
*/
func (s *Service) gearScore(c *domain.Character) Gear {
	var g Gear
	for _, row := range c.MissionInventory {
		if row.Slot < 0 || row.Slot >= equipmentSlots || row.Slot == gearShieldSocket || row.RefObjID == 0 {
			continue
		}
		plus := int(row.Plus)
		g.PlusTotal += plus
		g.BestPlus = max(g.BestPlus, plus)
		degree, tier, sealed := 0, 0, false
		if s.src.ItemGrade != nil {
			degree, tier, sealed = s.src.ItemGrade(row.RefObjID)
		}
		points := gearPoints(degree, tier, sealed, plus)
		g.Points += points
		g.best = max(g.best, points)
	}
	return g
}
