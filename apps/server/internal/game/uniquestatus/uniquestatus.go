/*
===========================================================================

uniquestatus.go - the Unique Status feed (port-only, not native)

#3531: with SRO_UNIQUE_STATUS on, every entered session is kept told of
every unique's state for the in-game Unique Status window: a snapshot
(wire.OpUniqueStatusSnapshot) when the session is first seen, then one
update (wire.OpUniqueStatusUpdate) per unique whose state changed.

The feed composes from the same two owners the public API's uniques
page reads - the monster state's live timeline (UniqueStates) and the
unique kill record (UniqueKillsAfter) - so the window and the site can
never disagree, and there is no second tracker to drift. A poll on the
mission tick paces the composition; unique spawns and deaths are rare,
so a second's lag buys a feed that costs nothing between events.

Off (the default, and the native state: neither original publishes any
such feed), wiring builds no Service, installs no tick hook and sends no
frame. When a dead unique returns stays secret (owner, 2026-10-11): the
rows carry no respawn delay, exactly as the public page's do.

===========================================================================
*/
package uniquestatus

import (
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvUniqueStatus enables the Unique Status feed.
const EnvUniqueStatus = "SRO_UNIQUE_STATUS"

// pollIntervalMs paces the composition: the window is a slow view.
const pollIntervalMs = 1000

/*
================
Enabled

The shard's word that the feed exists (SRO_UNIQUE_STATUS).
================
*/
func Enabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(EnvUniqueStatus))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Sources

Everything the feed reads, wired to the shard's authorities. States is
MonsterState.UniqueStates for the division; KillsAfter pages the unique
kill record from a sequence; UniqueName resolves a reference's retail
display name ("" falls back to the codename, as the public page does).
================
*/
type Sources struct {
	States     func() []simulation.UniqueState
	KillsAfter func(afterSeq int64) ([]domain.UniqueKill, error)
	KillPage   int
	UniqueName func(refObjID uint32) string
}

/*
================
Service

The feed's memory: the last kill per unique, the rows last composed,
and the sessions already holding a snapshot. Not safe for concurrent
use: the mission tick is its only caller.
================
*/
type Service struct {
	src        Sources
	nextPollMs int64
	started    bool
	lastSeq    int64
	lastKill   map[uint32]domain.UniqueKill
	rows       map[uint32]wire.UniqueStatusRow
	seen       map[string]bool
}

/*
================
New
================
*/
func New(sources Sources) *Service {
	return &Service{
		src:      sources,
		lastKill: map[uint32]domain.UniqueKill{},
		rows:     map[uint32]wire.UniqueStatusRow{},
		seen:     map[string]bool{},
	}
}

/*
================
catchUpKills

Appends every kill recorded since the last read, page by page, so the
last-kill map is complete however long the table grows.
================
*/
func (s *Service) catchUpKills() error {
	for {
		page, err := s.src.KillsAfter(s.lastSeq)
		if err != nil {
			return err
		}
		for _, kill := range page {
			if kill.Seq > s.lastSeq {
				s.lastSeq = kill.Seq
			}
			s.lastKill[kill.RefObjID] = kill
		}
		if len(page) == 0 || s.src.KillPage <= 0 || len(page) < s.src.KillPage {
			return nil
		}
	}
}

/*
================
compose

One row per unique the division's nests can spawn, in reference order:
the live state joined with the last recorded kill.
================
*/
func (s *Service) compose() []wire.UniqueStatusRow {
	states := s.src.States()
	out := make([]wire.UniqueStatusRow, 0, len(states))
	for _, state := range states {
		name := ""
		if s.src.UniqueName != nil {
			name = s.src.UniqueName(state.RefObjID)
		}
		if name == "" {
			name = state.Codename
		}
		row := wire.UniqueStatusRow{
			RefObjID: state.RefObjID,
			Name:     name,
			Level:    state.Level,
			RegionID: state.RegionID,
			Alive:    state.Alive,
		}
		if kill, ok := s.lastKill[state.RefObjID]; ok {
			row.DiedAtMs = kill.AtMs
			row.Killer = kill.KillerName
		}
		out = append(out, row)
	}
	return out
}

/*
================
Tick

One paced pass. It returns a snapshot payload for each session seen
for the first time and one update payload per unique whose row changed
since the last pass; both are nil between polls. The first pass only
learns the world as it is, so nobody is told of changes that predate
the feed. Sessions that vanish are forgotten: a session that returns
is new again and owed a snapshot.
================
*/
func (s *Service) Tick(nowMs int64, sessionIDs []string) (map[string][]byte, [][]byte) {
	if nowMs < s.nextPollMs {
		return nil, nil
	}
	s.nextPollMs = nowMs + pollIntervalMs
	// A failed kill read keeps the last map and the last rows: the rows
	// still compose and snapshots still go out, but the diff waits for a
	// clean read rather than absorbing a change it may have missed.
	killsErr := s.catchUpKills()
	rows := s.compose()
	var updates [][]byte
	if killsErr == nil {
		if s.started {
			for _, row := range rows {
				if prev, ok := s.rows[row.RefObjID]; !ok || prev != row {
					updates = append(updates, wire.EncodeUniqueStatusUpdate(row, nowMs))
				}
			}
		}
		s.started = true
		s.rows = make(map[uint32]wire.UniqueStatusRow, len(rows))
		for _, row := range rows {
			s.rows[row.RefObjID] = row
		}
	}
	present := make(map[string]bool, len(sessionIDs))
	var snapshots map[string][]byte
	for _, id := range sessionIDs {
		present[id] = true
		if !s.seen[id] {
			if snapshots == nil {
				snapshots = map[string][]byte{}
			}
			snapshots[id] = wire.EncodeUniqueStatusSnapshot(rows, nowMs)
		}
	}
	s.seen = present
	return snapshots, updates
}
