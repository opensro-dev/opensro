/*
===========================================================================

attendancefeed.go - the attendance calendar feed (port-only, not native)

#3255: with SRO_ATTENDANCE on, every entered session is kept told of its
character's attendance month for the client's calendar window. A poll
reads the month from the ledger (platform/attendance) and sends one
snapshot (wire.OpAttendanceSnapshot) to a session whose bytes changed -
on first sight, after a check-in or claim, and when the 04:00 UTC day
turns. The trailing server clock is left out of the comparison, so a
quiet month sends nothing.

Off (the default, and the native state: neither original has an
attendance calendar), wiring builds no Service, installs no tick hook
and sends no frame.

===========================================================================
*/
package attendancefeed

import (
	"bytes"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/platform/attendance"
)

// pollIntervalMs paces the reads: a month moves at most once a day per
// character, plus its claims.
const pollIntervalMs = 10_000

/*
================
Sources

Everything the feed reads: Month is the ledger's current month for a
character, Now the clock the snapshot's day and countdown come from.
================
*/
type Sources struct {
	Month func(characterID int64) (attendance.Month, error)
	Now   func() time.Time
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
sends only what changed. Not safe for concurrent use: the tick is its
only caller.
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
Snapshot

The character's snapshot payload as of now. A missing ledger row is an
empty month, so a character that never checked in still sees the
calendar.
================
*/
func Snapshot(month attendance.Month, now time.Time) []byte {
	key, today := attendance.Today(now)
	if month.Key != key {
		month = attendance.Month{Key: key}
	}
	return wire.EncodeAttendanceSnapshot(wire.AttendanceSnapshot{
		MonthKey:            key,
		Today:               today,
		DaysInMonth:         attendance.DaysIn(key),
		Checked:             month.Checked,
		ContinuityClaimed:   month.ContinuityClaimed,
		AccumulationClaimed: month.AccumulationClaimed,
		ContinuityTiers:     attendance.ContinuityTiers[:],
		AccumulationTiers:   attendance.AccumulationTiers[:],
		NextResetMs:         attendance.NextReset(now).UnixMilli(),
		ServerNowMs:         now.UnixMilli(),
	})
}

/*
================
Tick

One paced pass: a snapshot payload for each session whose month bytes
changed since it was last sent one (every session on first sight), and
nothing between polls. A failed read skips that session and keeps its
last bytes, so the next clean poll diffs against what the client holds.
Sessions that vanish are forgotten.
================
*/
func (s *Service) Tick(nowMs int64, sessions []Session) map[string][]byte {
	if nowMs < s.nextPollMs {
		return nil
	}
	s.nextPollMs = nowMs + pollIntervalMs
	now := s.src.Now()
	present := make(map[string]bool, len(sessions))
	var out map[string][]byte
	for _, session := range sessions {
		present[session.ID] = true
		month, err := s.src.Month(session.CharacterID)
		if err != nil {
			continue
		}
		payload := Snapshot(month, now)
		if last, ok := s.sent[session.ID]; ok && sameMonth(last, payload) {
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

// sameMonth compares two payloads without their trailing server clock.
func sameMonth(a, b []byte) bool {
	if len(a) < wire.AttendanceClockBytes || len(b) < wire.AttendanceClockBytes {
		return false
	}
	return bytes.Equal(a[:len(a)-wire.AttendanceClockBytes], b[:len(b)-wire.AttendanceClockBytes])
}
