/*
===========================================================================

views.go - the public read API's answers (P1-P6, P9)

Each builder reads the injected sources once and applies the privacy rule
(publicstats.go header) against the characters' current publicHidden.

===========================================================================
*/
package publicstats

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"opensro.online/server/internal/domain"
)

/*
================
roster

The shard's characters indexed by id and by lower-case name.
================
*/
type roster struct {
	byID   map[int64]*domain.Character
	byName map[string]*domain.Character
	all    []*domain.Character
}

/*
================
loadRoster
================
*/
func (s *Service) loadRoster() roster {
	out := roster{byID: map[int64]*domain.Character{}, byName: map[string]*domain.Character{}}
	if s.src.Characters == nil {
		return out
	}
	for _, c := range s.src.Characters() {
		if c == nil || c.DeletePending {
			continue
		}
		out.byID[c.ID] = c
		out.byName[strings.ToLower(c.Name)] = c
		out.all = append(out.all, c)
	}
	return out
}

/*
================
killerOf

The public name and guild of a kill's killer: "a hunter" (no guild) when
the character is hidden now or no longer exists.
================
*/
func (s *Service) killerOf(r roster, kill domain.UniqueKill) (name, guild string) {
	c := r.byID[kill.KillerCharID]
	if c == nil || c.PublicHidden {
		return HiddenName, ""
	}
	if s.src.GuildOf != nil {
		guild, _ = s.src.GuildOf(c)
	}
	return c.Name, guild
}

/*
================
uniqueName
================
*/
func (s *Service) uniqueName(ref uint32, codename string) string {
	if s.src.UniqueName != nil {
		if name := s.src.UniqueName(ref); name != "" {
			return name
		}
	}
	return codename
}

/*
================
allKills
================
*/
func (s *Service) allKills(sinceMs int64) ([]domain.UniqueKill, error) {
	if s.src.Kills == nil {
		return nil, nil
	}
	return s.src.Kills(sinceMs)
}

/*
================
uniques

P1: every unique with its live state, respawn window and last kill.
================
*/
func (s *Service) uniques(*http.Request) (any, error) {
	kills, err := s.allKills(0)
	if err != nil {
		return nil, err
	}
	r := s.loadRoster()
	last := map[uint32]domain.UniqueKill{}
	for _, kill := range kills {
		last[kill.RefObjID] = kill
	}
	out := UniquesResponse{ServerTime: s.now().UTC().Format("2006-01-02T15:04:05Z07:00"), Uniques: []Unique{}}
	if s.src.Uniques == nil {
		return out, nil
	}
	for _, state := range s.src.Uniques() {
		u := Unique{
			RefObjID: state.RefObjID, Name: s.uniqueName(state.RefObjID, state.Codename), Level: state.Level,
			RegionID: state.RegionID, Alive: state.Alive, SpawnedAt: optionalTime(state.SpawnedAtMs),
			Respawn: Respawn{MinSeconds: state.RespawnMinSec, MaxSeconds: state.RespawnMaxSec},
		}
		if !state.Alive && state.DiedAtMs > 0 {
			u.NextWin = &Window{
				OpensAt:  rfc3339(state.DiedAtMs + int64(state.RespawnMinSec)*1000),
				ClosesAt: rfc3339(state.DiedAtMs + int64(state.RespawnMaxSec)*1000),
			}
		}
		if kill, ok := last[state.RefObjID]; ok {
			name, guild := s.killerOf(r, kill)
			u.LastKill = &LastKill{At: rfc3339(kill.AtMs), Killer: name, Guild: guild}
		}
		out.Uniques = append(out.Uniques, u)
	}
	return out, nil
}

/*
================
uniqueCodenames

Reference id to codename, for kills of uniques the nests no longer list.
================
*/
func (s *Service) uniqueCodenames() map[uint32]string {
	out := map[uint32]string{}
	if s.src.Uniques != nil {
		for _, state := range s.src.Uniques() {
			out[state.RefObjID] = state.Codename
		}
	}
	return out
}

/*
================
kills

P2: the newest kills, newest first.
================
*/
func (s *Service) kills(req *http.Request) (any, error) {
	limit, err := boundedInt(req.URL.Query().Get("limit"), defaultKillLimit, maxKillLimit)
	if err != nil {
		return nil, err
	}
	kills, err := s.allKills(0)
	if err != nil {
		return nil, err
	}
	r, names := s.loadRoster(), s.uniqueCodenames()
	out := KillsResponse{Kills: []Kill{}}
	for i := len(kills) - 1; i >= 0 && len(out.Kills) < limit; i-- {
		kill := kills[i]
		name, guild := s.killerOf(r, kill)
		out.Kills = append(out.Kills, Kill{At: rfc3339(kill.AtMs), RefObjID: kill.RefObjID,
			Name: s.uniqueName(kill.RefObjID, names[kill.RefObjID]), Killer: name, Guild: guild})
	}
	return out, nil
}

/*
================
leaderboard

P3: the top unique hunters of all time or of the last seven days. Hidden
and deleted characters are left out, not renamed.
================
*/
func (s *Service) leaderboard(req *http.Request) (any, error) {
	period := req.URL.Query().Get("period")
	if period == "" {
		period = "all"
	}
	var sinceMs int64
	switch period {
	case "all":
	case "week":
		sinceMs = s.now().UnixMilli() - weekMs
	default:
		return nil, errBadRequest
	}
	kills, err := s.allKills(sinceMs)
	if err != nil {
		return nil, err
	}
	r := s.loadRoster()
	type tally struct {
		c        *domain.Character
		kills    int
		byUnique map[string]int
	}
	tallies := map[int64]*tally{}
	for _, kill := range kills {
		c := r.byID[kill.KillerCharID]
		if c == nil || c.PublicHidden {
			continue
		}
		t := tallies[c.ID]
		if t == nil {
			t = &tally{c: c, byUnique: map[string]int{}}
			tallies[c.ID] = t
		}
		t.kills++
		t.byUnique[strconv.FormatUint(uint64(kill.RefObjID), 10)]++
	}
	ranked := make([]*tally, 0, len(tallies))
	for _, t := range tallies {
		ranked = append(ranked, t)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].kills != ranked[j].kills {
			return ranked[i].kills > ranked[j].kills
		}
		return ranked[i].c.Name < ranked[j].c.Name
	})
	out := LeaderboardResponse{Period: period, Rows: []LeaderboardRow{}}
	for i, t := range ranked {
		if i == leaderboardSize {
			break
		}
		race, _ := raceGender(t.c.ModelCodename)
		guild := ""
		if s.src.GuildOf != nil {
			guild, _ = s.src.GuildOf(t.c)
		}
		out.Rows = append(out.Rows, LeaderboardRow{Rank: i + 1, Name: t.c.Name, Guild: guild, Level: level(t.c),
			Race: race, Kills: t.kills, ByUnique: t.byUnique})
	}
	return out, nil
}

/*
================
firsts

P4: the first character to reach each milestone level and the first to
kill each unique. A hidden winner keeps the slot as "a hunter".
================
*/
func (s *Service) firsts(*http.Request) (any, error) {
	r := s.loadRoster()
	out := FirstsResponse{Levels: []LevelFirst{}, Uniques: []UniqueFirst{}, Shard: s.src.Shard}
	for _, milestone := range domain.PublicLevelMilestones {
		var best *domain.Character
		var bestAt int64
		for _, c := range r.all {
			at, ok := c.LevelReachedAt[milestone]
			if !ok || at <= 0 {
				continue
			}
			if best == nil || at < bestAt || at == bestAt && c.ID < best.ID {
				best, bestAt = c, at
			}
		}
		if best == nil {
			continue
		}
		name := best.Name
		if best.PublicHidden {
			name = HiddenName
		}
		out.Levels = append(out.Levels, LevelFirst{Level: milestone, Name: name, At: rfc3339(bestAt)})
	}
	kills, err := s.allKills(0)
	if err != nil {
		return nil, err
	}
	names, seen := s.uniqueCodenames(), map[uint32]bool{}
	for _, kill := range kills {
		if seen[kill.RefObjID] {
			continue
		}
		seen[kill.RefObjID] = true
		killer, _ := s.killerOf(r, kill)
		out.Uniques = append(out.Uniques, UniqueFirst{RefObjID: kill.RefObjID, Name: s.uniqueName(kill.RefObjID, names[kill.RefObjID]),
			Killer: killer, At: rfc3339(kill.AtMs)})
	}
	return out, nil
}

/*
================
search

P5: characters whose name starts with q (case-insensitive), hidden ones
absent, in name order.
================
*/
func (s *Service) search(req *http.Request) (any, error) {
	query := req.URL.Query()
	prefix := strings.ToLower(strings.TrimSpace(query.Get("q")))
	if prefix == "" || utf8.RuneCountInString(prefix) > maxNameLength {
		return nil, errBadRequest
	}
	limit, err := boundedInt(query.Get("limit"), defaultSearchLimit, maxSearchLimit)
	if err != nil {
		return nil, err
	}
	r := s.loadRoster()
	var matches []*domain.Character
	for _, c := range r.all {
		if !c.PublicHidden && strings.HasPrefix(strings.ToLower(c.Name), prefix) {
			matches = append(matches, c)
		}
	}
	sort.Slice(matches, func(i, j int) bool { return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name) })
	out := CharactersResponse{Characters: []CharacterSummary{}}
	for _, c := range matches {
		if len(out.Characters) == limit {
			break
		}
		race, _ := raceGender(c.ModelCodename)
		guild := ""
		if s.src.GuildOf != nil {
			guild, _ = s.src.GuildOf(c)
		}
		out.Characters = append(out.Characters, CharacterSummary{Name: c.Name, Level: level(c), Race: race, Guild: guild})
	}
	return out, nil
}

/*
================
profile

P6: one character's public profile; 404 for a hidden or unknown name.
================
*/
func (s *Service) profile(req *http.Request) (any, error) {
	name := req.PathValue("name")
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return nil, errNotFound
	}
	r := s.loadRoster()
	c := r.byName[strings.ToLower(name)]
	if c == nil || c.PublicHidden {
		return nil, errNotFound
	}
	race, gender := raceGender(c.ModelCodename)
	p := Profile{
		Name: c.Name, Race: race, Gender: gender, Level: level(c),
		Job: jobName(c.Job.Type), JobLevel: c.Job.Grade, Masteries: []Mastery{},
		Look:      Look{BodyCodename: c.ModelCodename, Worn: []WornItem{}, Avatar: []WornItem{}},
		LastLogin: optionalTime(c.LastLoginAtMs),
	}
	if s.src.GuildOf != nil {
		if guild, rank := s.src.GuildOf(c); guild != "" {
			p.Guild = &Guild{Name: guild, Rank: rank}
		}
	}
	for _, m := range c.Masteries {
		p.Masteries = append(p.Masteries, Mastery{RefID: m.ID, Level: m.Level})
	}
	if c.Strength != nil {
		p.Stats.Str = *c.Strength
	}
	if c.Intellect != nil {
		p.Stats.Int = *c.Intellect
	}
	if s.src.Vitals != nil {
		p.Stats.HP, p.Stats.MP = s.src.Vitals(c)
	}
	if s.src.ModelRef != nil {
		p.Look.BodyRefObjID = s.src.ModelRef(c)
	}
	for _, row := range c.MissionInventory {
		if row.Slot >= 0 && row.Slot < equipmentSlots && row.RefObjID != 0 {
			p.Look.Worn = append(p.Look.Worn, WornItem{Slot: row.Slot, RefItemID: row.RefObjID, Plus: row.Plus})
		}
	}
	if c.AvatarInventory != nil {
		for _, row := range c.AvatarInventory.Rows {
			if row.RefObjID != 0 {
				p.Look.Avatar = append(p.Look.Avatar, WornItem{Slot: row.Slot, RefItemID: row.RefObjID})
			}
		}
	}
	sort.Slice(p.Look.Worn, func(i, j int) bool { return p.Look.Worn[i].Slot < p.Look.Worn[j].Slot })
	sort.Slice(p.Look.Avatar, func(i, j int) bool { return p.Look.Avatar[i].Slot < p.Look.Avatar[j].Slot })
	kills, err := s.allKills(0)
	if err != nil {
		return nil, err
	}
	for _, kill := range kills {
		if kill.KillerCharID == c.ID {
			p.UniqueKills++
		}
	}
	if s.src.Online != nil {
		p.Online = s.src.Online(c.ID)
	}
	return p, nil
}

// equipmentSlots are the worn sockets 0..12 a profile shows.
const equipmentSlots = 13

/*
================
rules

P9: the Original vs OpenSRO list, fixed at boot.
================
*/
func (s *Service) rules(*http.Request) (any, error) {
	out := s.src.Rules
	if out.Rules == nil {
		out.Rules = []Rule{}
	}
	if out.Deviations == nil {
		out.Deviations = []Deviation{}
	}
	return out, nil
}

/*
================
boundedInt

A query integer in 1..max, def when absent.
================
*/
func boundedInt(text string, def, max int) (int, error) {
	if text == "" {
		return def, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > max {
		return 0, errBadRequest
	}
	return n, nil
}
