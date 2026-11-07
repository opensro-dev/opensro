/*
===========================================================================

census.go - the player census read side: people, not sessions

Part 2 of the player census (opensro#2717, console#9). Part 1 (#2716)
records one journal row per entered session with its address held
only as keyed hashes. This package reads those rows and answers the
staff questions: how many people are online now, how many unique
people a window saw, which accounts are probably one person (and
why), what one person's history looks like, and which sessions carry
bot signals (signals.go). Signals are shown, never acted on.

A "person" is a cluster of accounts (clusters.go): accounts merge
only on two independent signals, so an address alone - a NAT, a
household, a VPN exit - never merges strangers. No raw address and
no hash value ever leaves this package: answers carry account and
character names, times and counts, and link evidence reduced to
kinds and sharing counts.

Port-only, not native: the whole census runs behind
SRO_CONNECTION_JOURNAL (connjournal), default off. With the flag off
no journal exists, no service is built and the routes are never
installed (wiring_census.go).

===========================================================================
*/
package census

import (
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/platform/connjournal"
)

// SchemaVersion is the answer shape's version; the console checks it.
const SchemaVersion = 1

// Path is the handler's route prefix on the operator listener.
const Path = "/internal/operations/census"

const (
	// windowDayMs is the journal's retention and the widest answer.
	windowDayMs = 24 * 60 * 60 * 1000
	// snapshotTTL bounds how stale one computed snapshot may be:
	// staff polling shares one load, like the observatory's capture.
	snapshotTTL = 10 * time.Second
)

/*
================
Span

One entered session as the census reasons about it. Times are UTC
milliseconds; Ended 0 means still entered (its end is now). Client
is the client fingerprint the session presented (user agent, build,
screen), empty when it presented none - an absent fingerprint is no
signal, never a shared one.
================
*/
type Span struct {
	Session   string
	Account   string
	Character string
	Transport string
	Started   int64
	Ended     int64
	Estimated bool
	IPKey     string
	NetKey    string
	Client    string
}

// end returns the span's effective end: its close, or now while open.
func (s Span) end(now int64) int64 {
	if s.Ended == 0 {
		return now
	}
	return s.Ended
}

// overlaps reports whether the span touches [from, to).
func (s Span) overlaps(from, to, now int64) bool {
	return s.Started < to && s.end(now) > from
}

/*
================
RowSource

Where spans come from: the journal in production (JournalRows), a
fixture in tests.
================
*/
type RowSource func(sinceMs int64) ([]Span, error)

/*
================
JournalRows

The connection journal as a RowSource. The fingerprint is the client
facts joined where correlation can compare them without this package
ever printing one.
================
*/
func JournalRows(journal *connjournal.Journal) RowSource {
	return func(sinceMs int64) ([]Span, error) {
		rows, err := journal.Rows(sinceMs)
		if err != nil {
			return nil, err
		}
		spans := make([]Span, 0, len(rows))
		for _, row := range rows {
			spans = append(spans, Span{Session: row.Session, Account: row.Account, Character: row.Character,
				Transport: row.Transport, Started: row.Started, Ended: row.Ended, Estimated: row.Estimated,
				IPKey: row.IPKey, NetKey: row.NetKey, Client: clientFingerprint(row)})
		}
		return spans, nil
	}
}

/*
================
clientFingerprint
================
*/
func clientFingerprint(row connjournal.Row) string {
	if row.UserAgent == "" {
		return ""
	}
	return row.UserAgent + "\x00" + row.Build + "\x00" + row.Screen
}

/*
================
ChatCounts

Accepted chat lines per folded character name over [fromMs, toMs).
An error means the count is unknown; chat-derived answers then stay
silent rather than guess (a torn log line must never read as a
player's silence).
================
*/
type ChatCounts func(fromMs, toMs int64) (map[string]int, error)

/*
================
ChatLogCounts

The moderation chat log (chatlog) as a ChatCounts. Any unreadable
line fails the count whole: partial evidence is not evidence.
================
*/
func ChatLogCounts(dir string) ChatCounts {
	return func(fromMs, toMs int64) (map[string]int, error) {
		counts, skipped, err := chatlog.CountByCharacter(dir,
			time.UnixMilli(fromMs).UTC(), time.UnixMilli(toMs).UTC())
		if err != nil {
			return nil, err
		}
		if skipped > 0 {
			return nil, errChatUnreadable
		}
		return counts, nil
	}
}

var errChatUnreadable = chatUnreadableError{}

type chatUnreadableError struct{}

func (chatUnreadableError) Error() string { return "chat log has unreadable lines" }

/*
================
Service

The census answers over one shared snapshot of the journal. Every
read recomputes at most once per snapshotTTL; the clustering and the
windows all derive from that one load, so two answers can never
disagree about the same instant.
================
*/
type Service struct {
	source RowSource
	chat   ChatCounts
	now    func() time.Time

	mu    sync.Mutex
	at    time.Time
	spans []Span
	index *index
	err   error
}

// NewService builds the census over a row source; chat may be nil,
// which only silences the chat signal.
func NewService(source RowSource, chat ChatCounts, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{source: source, chat: chat, now: now}
}

// nowMs is the service's one time form: UTC milliseconds.
func (s *Service) nowMs() int64 { return s.now().UTC().UnixMilli() }

/*
================
snapshot

The current spans and their index, loaded together. A failed load is
kept with its error so every route fails the same way until the next
load succeeds.
================
*/
func (s *Service) snapshot() (*index, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index != nil && s.now().Sub(s.at) < snapshotTTL {
		return s.index, s.err
	}
	now := s.now()
	spans, err := s.source(now.UTC().UnixMilli() - 30*windowDayMs)
	s.at, s.spans, s.err = now, spans, err
	if err != nil {
		s.index = nil
		return nil, err
	}
	s.index = buildIndex(spans, now.UTC().UnixMilli())
	return s.index, nil
}

/*
================
Online

People, accounts, characters and sessions entered right now, with a
sampled series over the window behind now: at each bucket start, who
was entered. People counts clusters, so one person on three accounts
is one.
================
*/
type Online struct {
	SchemaVersion int           `json:"schemaVersion"`
	GeneratedAt   int64         `json:"generatedAt"`
	People        int           `json:"people"`
	Accounts      int           `json:"accounts"`
	Characters    int           `json:"characters"`
	Sessions      int           `json:"sessions"`
	Series        []SeriesPoint `json:"series"`
}

// SeriesPoint is one sampled instant of the online series.
type SeriesPoint struct {
	T          int64 `json:"t"`
	People     int   `json:"people"`
	Characters int   `json:"characters"`
	Sessions   int   `json:"sessions"`
}

/*
================
Service.OnlineNow
================
*/
func (s *Service) OnlineNow(windowMs, bucketMs int64) (Online, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Online{}, err
	}
	now := s.nowMs()
	answer := Online{SchemaVersion: SchemaVersion, GeneratedAt: now, Series: []SeriesPoint{}}
	open := idx.openSpans(now)
	answer.Sessions = len(open)
	answer.Accounts = len(distinctAccounts(open))
	answer.Characters = len(distinctCharacters(open))
	answer.People = idx.peopleCount(distinctAccounts(open))
	for t := now - windowMs; t <= now; t += bucketMs {
		active := idx.activeSpans(t)
		answer.Series = append(answer.Series, SeriesPoint{T: t,
			People: idx.peopleCount(distinctAccounts(active)), Characters: len(distinctCharacters(active)),
			Sessions: len(active)})
	}
	return answer, nil
}

/*
================
Uniques

Unique people, accounts, characters and sessions per window: a span
counts when it overlaps the window, a person when any of their
accounts does. This is the definition the public "unique players"
number must use (console#9): people, over the window, by this
clustering.
================
*/
type Uniques struct {
	SchemaVersion int                    `json:"schemaVersion"`
	GeneratedAt   int64                  `json:"generatedAt"`
	Windows       map[string]WindowCount `json:"windows"`
}

// WindowCount is one window's uniques.
type WindowCount struct {
	People     int `json:"people"`
	Accounts   int `json:"accounts"`
	Characters int `json:"characters"`
	Sessions   int `json:"sessions"`
}

/*
================
Service.Uniques
================
*/
func (s *Service) Uniques() (Uniques, error) {
	idx, err := s.snapshot()
	if err != nil {
		return Uniques{}, err
	}
	now := s.nowMs()
	answer := Uniques{SchemaVersion: SchemaVersion, GeneratedAt: now, Windows: map[string]WindowCount{}}
	for name, windowMs := range map[string]int64{"24h": windowDayMs, "7d": 7 * windowDayMs, "30d": 30 * windowDayMs} {
		spans := idx.spansOverlapping(now-windowMs, now, now)
		answer.Windows[name] = WindowCount{
			People: idx.peopleCount(distinctAccounts(spans)), Accounts: len(distinctAccounts(spans)),
			Characters: len(distinctCharacters(spans)), Sessions: len(spans)}
	}
	return answer, nil
}

// distinctAccounts is the span set's account set.
func distinctAccounts(spans []Span) map[string]bool {
	accounts := make(map[string]bool, len(spans))
	for _, span := range spans {
		accounts[span.Account] = true
	}
	return accounts
}

// distinctCharacters is the span set's character set, folded: one
// character keeps one case in the journal, but the fold is free.
func distinctCharacters(spans []Span) map[string]bool {
	characters := make(map[string]bool, len(spans))
	for _, span := range spans {
		characters[strings.ToLower(span.Character)] = true
	}
	return characters
}
