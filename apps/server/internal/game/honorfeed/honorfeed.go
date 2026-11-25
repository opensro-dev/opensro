/*
===========================================================================

honorfeed.go - the Job Honor ranking feed (port-only, not native)

#3530: with SRO_JOB_HONOR on, every entered session is kept told of the
job honor board and its own standing for the in-game Job Honor Rank
window. A poll composes the board from the ledger's standings (the same
rows the public board publishes, platform/jobhonor's BoardRows), adds
the viewing character's own standing, and sends one snapshot
(wire.OpJobHonorSnapshot) to a session whose bytes changed - on first
sight, when its points move, and when the board does.

Off (the default, and the native state: neither original publishes any
such feed), wiring builds no Service, installs no tick hook and sends
no frame.

===========================================================================
*/
package honorfeed

import (
	"bytes"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/platform/jobhonor"
)

// pollIntervalMs paces the composition: the board moves at sale speed.
const pollIntervalMs = 10_000

/*
================
Sources

Everything the feed reads, wired to the shard's authorities. Current
and Previous are the ledger's standings; Alias shows a character's job
alias (false hides the row, as the public board hides it); Account
names the viewing character's account (false: no own standing); Points
is the ledger's per-character sum over honor days; Used is the
account's kept points today, against the ledger's daily cap.
================
*/
type Sources struct {
	Current  func() ([]jobhonor.Standing, error)
	Previous func() ([]jobhonor.Standing, time.Time, error)
	Alias    func(characterID int64) (string, bool)
	Account  func(characterID int64) (string, bool)
	Points   func(characterID int64, from, to time.Time) (map[int]int64, error)
	Used     func(account string) int
	Now      func() time.Time
}

/*
================
Session

One entered session and the character it plays.
================
*/
type Session struct {
	ID          string
	CharacterID int64
}

/*
================
Service

The feed's memory: the payload each session last received, so a poll
sends only what changed. Not safe for concurrent use: the mission tick
is its only caller.
================
*/
type Service struct {
	src        Sources
	nextPollMs int64
	sent       map[string][]byte
}

/*
================
New
================
*/
func New(sources Sources) *Service {
	return &Service{src: sources, sent: map[string][]byte{}}
}

/*
================
board

One period's wire rows from its standings.
================
*/
func (s *Service) board(standings []jobhonor.Standing) []wire.JobHonorRow {
	rows := jobhonor.BoardRows(standings, 0, wire.JobHonorMaxRows, s.src.Alias)
	out := make([]wire.JobHonorRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, wire.JobHonorRow{Job: row.Job, Alias: row.Alias,
			Points: row.Points, Tier: row.Tier, Tonight: row.Tonight})
	}
	return out
}

/*
================
own

The viewing character's own standing: the place and tiers of its
strongest standing, its account's cap left, and its points today and
this month per job. A failed points read leaves the sums at zero; the
board still goes out, as the unique feed's snapshots do.
================
*/
func (s *Service) own(characterID int64, standings []jobhonor.Standing,
	board []jobhonor.BoardRow, now time.Time) wire.JobHonorOwn {
	account, ok := s.src.Account(characterID)
	if !ok {
		return wire.JobHonorOwn{}
	}
	own := wire.JobHonorOwn{Present: true,
		CapLeft: max(0, jobhonor.DailyCap-s.src.Used(account))}
	var best *jobhonor.Standing
	for i := range standings {
		row := &standings[i]
		if row.CharacterID != characterID {
			continue
		}
		if best == nil || row.Points > best.Points {
			best = row
		}
	}
	if best != nil {
		own.Job, own.Tier, own.Tonight = best.Job, best.Held, best.Tier
		for _, row := range board {
			if row.CharacterID == characterID && row.Job == best.Job {
				own.Place = row.Rank
				break
			}
		}
	}
	today := jobhonor.DayStart(now)
	tomorrow := today.AddDate(0, 0, 1)
	if points, err := s.src.Points(characterID, today, tomorrow); err == nil {
		for job, pts := range points {
			if job >= jobhonor.JobTrader && job <= jobhonor.JobHunter {
				own.Today[job-1] = pts
			}
		}
	}
	month := time.Date(now.Year(), now.Month(), 1, jobhonor.TierHourUTC, 0, 0, 0, time.UTC)
	if points, err := s.src.Points(characterID, month, tomorrow); err == nil {
		for job, pts := range points {
			if job >= jobhonor.JobTrader && job <= jobhonor.JobHunter {
				own.Month[job-1] = pts
			}
		}
	}
	return own
}

/*
================
Tick

One paced pass: a snapshot payload for each session whose composed
bytes changed since the last pass (every session on first sight), and
nothing between polls. A failed board read sends nothing and keeps
the last bytes, so the next clean poll diffs against what the client
actually holds. Sessions that vanish are forgotten.
================
*/
func (s *Service) Tick(nowMs int64, sessions []Session) map[string][]byte {
	if nowMs < s.nextPollMs {
		return nil
	}
	s.nextPollMs = nowMs + pollIntervalMs
	now := s.src.Now()
	standings, err := s.src.Current()
	if err != nil {
		return nil
	}
	previous, periodEnd, err := s.src.Previous()
	if err != nil {
		return nil
	}
	currentBoard := jobhonor.BoardRows(standings, 0, wire.JobHonorMaxRows, s.src.Alias)
	currentRows, previousRows := s.board(standings), s.board(previous)
	var periodEndMs int64
	if !periodEnd.IsZero() {
		periodEndMs = periodEnd.UnixMilli()
	}
	nextChangeMs := jobhonor.NextChange(now).UnixMilli()
	present := make(map[string]bool, len(sessions))
	var out map[string][]byte
	for _, session := range sessions {
		present[session.ID] = true
		payload := wire.EncodeJobHonorSnapshot(now.UnixMilli(), nextChangeMs, periodEndMs,
			currentRows, previousRows,
			s.own(session.CharacterID, standings, currentBoard, now))
		if bytes.Equal(s.sent[session.ID], payload) {
			continue
		}
		s.sent[session.ID] = payload
		if out == nil {
			out = map[string][]byte{}
		}
		out[session.ID] = payload
	}
	for id := range s.sent {
		if !present[id] {
			delete(s.sent, id)
		}
	}
	return out
}
