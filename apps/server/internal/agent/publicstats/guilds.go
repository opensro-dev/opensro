/*
===========================================================================

guilds.go - P10: every guild of the shard, ranked, with its union

GET /public/v1/guilds lists the shard's guilds 50 a page: by level, then
members, then name, optionally narrowed to names starting with q. Each row
names the master (HiddenName for a master who chose to hide, as everywhere
else), the other guilds of its union, and the fortress it holds. Nothing
private leaves: no notice, crest, storage, GP, member names beyond the
master, or votes (owner decision 2026-10-14: GP stays private).

===========================================================================
*/

package publicstats

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// guildPageSize is a guild page's length, as the rankings'.
const guildPageSize = 50

// guildMasterGrade is the master's member grade (domain.GuildLeaderGrade).
const guildMasterGrade = 0

/*
================
GuildEntry

One guild as the GameWorld wiring reads it: its members by character id
and grade, the other guilds of its union, and the fortress it holds ("" for
none; the fortress codename's town, lower case).
================
*/
type GuildEntry struct {
	ID       int64
	Name     string
	Level    uint8
	Members  []GuildEntryMember
	Union    []int64
	Fortress string
	// FoundedAtMs is when the guild was created (unix ms); 0 for a guild
	// founded before the store kept it.
	FoundedAtMs int64
}

/*
================
GuildEntryMember
================
*/
type GuildEntryMember struct {
	CharID int64
	Grade  uint8
}

/*
================
Service.guilds
================
*/
func (s *Service) guilds(r *http.Request) (any, error) {
	query := r.URL.Query()
	prefix := strings.ToLower(strings.TrimSpace(query.Get("q")))
	if len(prefix) > maxNameLength {
		return nil, fmt.Errorf("%w: q is at most %d characters", errBadRequest, maxNameLength)
	}
	page := 1
	if raw := query.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: page must be a whole number from 1", errBadRequest)
		}
		page = n
	}
	rows := []GuildRow{}
	for _, row := range s.loadGuilds() {
		if prefix == "" || strings.HasPrefix(strings.ToLower(row.Name), prefix) {
			rows = append(rows, row)
		}
	}
	out := GuildsResponse{Page: page, Total: len(rows), Pages: max(1, (len(rows)+guildPageSize-1)/guildPageSize),
		GeneratedAt: s.now().UTC().Format(time.RFC3339), Rows: []GuildRow{}}
	if prefix != "" {
		out.Query = &prefix
	}
	if page <= out.Pages {
		start := (page - 1) * guildPageSize
		out.Rows = append(out.Rows, rows[start:min(start+guildPageSize, len(rows))]...)
	}
	return out, nil
}

/*
================
Service.loadGuilds

The shared guild list: every guild, ranked, built at most once per
snapshotMaxAge from one Guilds() copy (a walk of every character and a
read per guild), whatever searches and pages the visitors ask for. The
rank is fixed here, before any name filter, so a search shows each guild's
place on the full list. Called only while cached holds s.mu.
================
*/
func (s *Service) loadGuilds() []GuildRow {
	now := s.now()
	if s.snap.guildsUntil.IsZero() || !now.Before(s.snap.guildsUntil) {
		s.snap.guilds = s.rankGuilds()
		s.snap.guildsUntil = now.Add(snapshotMaxAge)
	}
	return s.snap.guilds
}

/*
================
Service.rankGuilds
================
*/
func (s *Service) rankGuilds() []GuildRow {
	if s.src.Guilds == nil {
		return nil
	}
	entries := s.src.Guilds()
	names := make(map[int64]string, len(entries))
	for _, entry := range entries {
		names[entry.ID] = entry.Name
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Level != b.Level {
			return a.Level > b.Level
		}
		if len(a.Members) != len(b.Members) {
			return len(a.Members) > len(b.Members)
		}
		return a.Name < b.Name
	})
	r := s.loadRoster()
	rows := []GuildRow{}
	for i, entry := range entries {
		row := GuildRow{Rank: i + 1, Name: entry.Name, Level: int(entry.Level), Members: len(entry.Members),
			Union: []string{}}
		for _, member := range entry.Members {
			if member.Grade != guildMasterGrade {
				continue
			}
			if c := r.byID[member.CharID]; c != nil {
				row.Master = c.Name
				if c.PublicHidden {
					row.Master = HiddenName
				}
			}
		}
		for _, id := range entry.Union {
			if name := names[id]; name != "" {
				row.Union = append(row.Union, name)
			}
		}
		sort.Strings(row.Union)
		if entry.FoundedAtMs > 0 {
			founded := time.UnixMilli(entry.FoundedAtMs).UTC().Format(time.RFC3339)
			row.Founded = &founded
		}
		if entry.Fortress != "" {
			fortress := entry.Fortress
			row.Fortress = &fortress
		}
		rows = append(rows, row)
	}
	return rows
}
