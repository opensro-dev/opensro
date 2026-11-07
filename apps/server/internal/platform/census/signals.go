/*
===========================================================================

signals.go - bot signals per session, from data the server keeps

Three signals are computable from what the server already retains -
the connection journal's spans and the moderation chat log's counts:

  - long-session: one unbroken entered span of eight hours or more.
  - always-on: the account was entered for at least twenty of the
    trailing twenty-four hours.
  - chat-silent: a span of two hours or more in which the character
    said nothing the chat log accepted. The count must have read
    clean; damaged evidence silences the signal, never the player.

Movement cadence, identical path loops and grinding in one spot are
not here: the server retains no movement history to compute them
from, and inventing one is a journal change of its own, not a read.
Each signal is shown, never acted on (opensro#2717): nothing in this
package feeds a kick, a flag or a store write.

===========================================================================
*/
package census

import (
	"sort"
	"strings"
)

const (
	// longSessionMs: an unbroken span at least this long signals.
	longSessionMs = 8 * 60 * 60 * 1000
	// alwaysOnMs: entered time in the trailing day at least this.
	alwaysOnMs = 20 * 60 * 60 * 1000
	// chatSilenceMs: a span at least this long with no accepted
	// chat signals.
	chatSilenceMs = 2 * 60 * 60 * 1000
	// signalsWindowMs is the window answers cover: the trailing day.
	signalsWindowMs = windowDayMs
	// signalsMax caps one answer's sessions, open first.
	signalsMax = 200
)

/*
================
Signal

One fired signal: what kind, and the measured value that fired it.
================
*/
type Signal struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

/*
================
SessionSignals

One session in the signals window with the signals it fired
(possibly none: an ordinary session listed so staff see the field).
================
*/
type SessionSignals struct {
	Session   string   `json:"session"`
	Account   string   `json:"account"`
	Character string   `json:"character"`
	Started   int64    `json:"started"`
	Ended     int64    `json:"ended"`
	Open      bool     `json:"open"`
	Estimated bool     `json:"estimated"`
	Signals   []Signal `json:"signals"`
}

/*
================
Signals

The signals answer: the trailing day's sessions, open first then
newest, capped; ChatOK says whether chat evidence read clean (when
it did not, no chat-silent signal fires anywhere in the answer).
================
*/
type Signals struct {
	SchemaVersion int              `json:"schemaVersion"`
	GeneratedAt   int64            `json:"generatedAt"`
	ChatOK        bool             `json:"chatOk"`
	Sessions      []SessionSignals `json:"sessions"`
	Truncated     bool             `json:"truncated"`
}

/*
================
Service.Signals
================
*/
func (s *Service) Signals() (Signals, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Signals{}, err
	}
	now := s.nowMs()
	from := now - signalsWindowMs
	answer := Signals{SchemaVersion: SchemaVersion, GeneratedAt: now, ChatOK: true, Sessions: []SessionSignals{}}

	chatCounts := map[string]int{}
	if s.chat != nil {
		counts, err := s.chat(from, now)
		if err != nil {
			answer.ChatOK = false
		} else {
			chatCounts = counts
		}
	} else {
		answer.ChatOK = false
	}

	// Entered time per account inside the window, overlapping spans
	// unioned: two clients of one account are not forty-eight hours.
	presence := map[string]int64{}
	byAccount := map[string][]Span{}
	for _, span := range idx.spansOverlapping(from, now, now) {
		byAccount[span.Account] = append(byAccount[span.Account], span)
	}
	for account, spans := range byAccount {
		presence[account] = coveredMs(spans, from, now)
	}

	sessions := idx.spansOverlapping(from, now, now)
	sort.Slice(sessions, func(i, j int) bool {
		if (sessions[i].Ended == 0) != (sessions[j].Ended == 0) {
			return sessions[i].Ended == 0
		}
		return sessions[i].Started > sessions[j].Started
	})
	answer.Truncated = len(sessions) > signalsMax
	if answer.Truncated {
		sessions = sessions[:signalsMax]
	}
	for _, span := range sessions {
		entry := SessionSignals{Session: span.Session, Account: span.Account, Character: span.Character,
			Started: span.Started, Ended: span.Ended, Open: span.Ended == 0, Estimated: span.Estimated,
			Signals: []Signal{}}
		length := span.end(now) - span.Started
		if length >= longSessionMs {
			entry.Signals = append(entry.Signals,
				Signal{Kind: "long-session", Value: round1(float64(length) / 3600000.0), Unit: "hours"})
		}
		if presence[span.Account] >= alwaysOnMs {
			entry.Signals = append(entry.Signals,
				Signal{Kind: "always-on", Value: round1(float64(presence[span.Account]) / 3600000.0), Unit: "hours"})
		}
		if answer.ChatOK && length >= chatSilenceMs && chatCounts[strings.ToLower(span.Character)] == 0 {
			entry.Signals = append(entry.Signals,
				Signal{Kind: "chat-silent", Value: round1(float64(length) / 3600000.0), Unit: "hours"})
		}
		answer.Sessions = append(answer.Sessions, entry)
	}
	return answer, nil
}

// coveredMs is the union length of the spans clamped to [from, to).
func coveredMs(spans []Span, from, to int64) int64 {
	intervals := make([][2]int64, 0, len(spans))
	for _, span := range spans {
		start := max(span.Started, from)
		end := min(span.end(to), to)
		if end > start {
			intervals = append(intervals, [2]int64{start, end})
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i][0] < intervals[j][0] })
	var total, curStart, curEnd int64
	for _, interval := range intervals {
		if interval[0] > curEnd {
			total += curEnd - curStart
			curStart, curEnd = interval[0], interval[1]
			continue
		}
		if interval[1] > curEnd {
			curEnd = interval[1]
		}
	}
	return total + curEnd - curStart
}

// round1 keeps one decimal: signal values are read by people.
func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}
