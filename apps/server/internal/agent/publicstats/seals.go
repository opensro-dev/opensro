/*
===========================================================================

seals.go - the weekly seal board answer (#1641, #1604 P9)

GET /public/v1/leaderboards/seals: this week's sealed drops (Star, Moon
and Sun counted apart) by job alias. The counter (platform/sealboard)
knows only character ids; the alias join happens here, at read time,
against the shared roster snapshot: a deleted or public-hidden character
is absent, a character with no job alias has no row, and a midweek alias
change moves the character's counts under the new alias. The character
id never reaches the answer.

Port-only, not native. Unserved (404) while SRO_SEAL_BOARD is off.

===========================================================================
*/
package publicstats

import (
	"net/http"
	"sort"

	"opensro.online/server/internal/platform/sealboard"
)

/*
================
sealBoard

Builds the week's board: Sun first, then Moon, then Star, ties by alias,
at most leaderboardSize rows.
================
*/
func (s *Service) sealBoard(*http.Request) (any, error) {
	if s.src.SealCounts == nil {
		return nil, errNotFound
	}
	now := s.now()
	weekStart := sealboard.WeekStart(now)
	counts, err := s.src.SealCounts(weekStart)
	if err != nil {
		return nil, err
	}
	since := weekStart
	if s.src.SealCountingSince != nil {
		since, err = s.src.SealCountingSince(weekStart)
		if err != nil {
			return nil, err
		}
	}
	r := s.loadRoster()
	type tally struct {
		alias string
		tiers [sealboard.Tiers]int64
	}
	tallies := map[int64]*tally{}
	for _, count := range counts {
		if count.Tier < 0 || count.Tier >= sealboard.Tiers {
			continue
		}
		c := r.byID[count.CharacterID]
		if c == nil || c.PublicHidden || c.Job.Alias == "" {
			continue
		}
		t := tallies[c.ID]
		if t == nil {
			t = &tally{alias: c.Job.Alias}
			tallies[c.ID] = t
		}
		t.tiers[count.Tier] += count.Drops
	}
	ranked := make([]*tally, 0, len(tallies))
	for _, t := range tallies {
		ranked = append(ranked, t)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.tiers[sealboard.TierSun] != b.tiers[sealboard.TierSun] {
			return a.tiers[sealboard.TierSun] > b.tiers[sealboard.TierSun]
		}
		if a.tiers[sealboard.TierMoon] != b.tiers[sealboard.TierMoon] {
			return a.tiers[sealboard.TierMoon] > b.tiers[sealboard.TierMoon]
		}
		if a.tiers[sealboard.TierStar] != b.tiers[sealboard.TierStar] {
			return a.tiers[sealboard.TierStar] > b.tiers[sealboard.TierStar]
		}
		return a.alias < b.alias
	})
	out := SealBoardResponse{
		WeekStart:     rfc3339(weekStart.UnixMilli()),
		WeekEnd:       rfc3339(weekStart.AddDate(0, 0, 7).UnixMilli()),
		CountingSince: rfc3339(since.UnixMilli()),
		GeneratedAt:   rfc3339(now.UnixMilli()),
		Rows:          []SealBoardRow{},
	}
	for i, t := range ranked {
		if i == leaderboardSize {
			break
		}
		out.Rows = append(out.Rows, SealBoardRow{
			Rank:  i + 1,
			Alias: t.alias,
			Sun:   t.tiers[sealboard.TierSun],
			Moon:  t.tiers[sealboard.TierMoon],
			Star:  t.tiers[sealboard.TierStar],
		})
	}
	return out, nil
}
