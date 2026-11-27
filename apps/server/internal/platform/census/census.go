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
kinds and sharing counts. A session is named by a per-answer label
("Session 1", ...), never by the journal's session name, whose half
is a per-boot random hex id (sessionLabels).

Port-only, not native: the whole census runs behind
SRO_CONNECTION_JOURNAL (connjournal), default off. With the flag off
no journal exists, no service is built and the routes are never
installed (wiring_census.go).

===========================================================================
*/
package census

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/social/chatlog"
	"opensro.online/server/internal/platform/connjournal"
)

// SchemaVersion is the answer shape's version; the console checks it.
// Fields added to an answer (the no-user-agent signal and count, the
// person view's ticket hours, #3307) keep it: a reader of version 1
// ignores a field it does not know.
const SchemaVersion = 1

/*
================
sessionLabels

Names each session of one answer by its first-seen order: "Session 1",
"Session 2", ... Rows of one session share a label, and the journal's
session name (connjournal: a per-boot random hex id joined to a
transport counter) never leaves the package. The console's guard
refused a whole person answer for that hex at history[199] (console#35):
the id is an identifier, not evidence, so the answer carries only its
grouping. A label means nothing across answers; a reader that needs one
session asks by character and time.
================
*/
type sessionLabels map[string]string

func (labels sessionLabels) of(session string) string {
	if label, ok := labels[session]; ok {
		return label
	}
	label := "Session " + strconv.Itoa(len(labels)+1)
	labels[session] = label
	return label
}

// Path is the handler's route prefix on the operator listener.
const Path = "/internal/operations/census"

const (
	// windowDayMs is the journal's retention and the widest answer.
	windowDayMs = 24 * 60 * 60 * 1000
	// snapshotTTL bounds how stale one computed snapshot may be:
	// staff polling shares one load, like the observatory's capture.
	snapshotTTL = 10 * time.Second
	// publicCountsTTL bounds how stale the public pair may be. The
	// public status route is polled by every site visitor, and each
	// snapshot reloads the whole 30-day journal: on 2026-11-27 that
	// reload cost 6% of a core and 0.74 GB of allocation every 30 s
	// on the live GameWorld (#4587). Two counts on a website can lag
	// two minutes; staff readings keep snapshotTTL.
	publicCountsTTL = 2 * time.Minute
)

/*
================
Span

One entered session as the census reasons about it. Times are UTC
milliseconds; Ended 0 means still entered (its end is now). Client
is the client fingerprint the session presented (user agent, build,
screen), empty when it presented none - an absent fingerprint is no
signal, never a shared one. NoUserAgent is the one fact the absence
does carry: every browser sends a User-Agent on both transports, so
a session without one was not a browser (signals.go).
================
*/
type Span struct {
	Session     string
	Account     string
	Character   string
	Transport   string
	Started     int64
	Ended       int64
	Estimated   bool
	IPKey       string
	NetKey      string
	Client      string
	NoUserAgent bool
	// Activity is the span's traffic (connjournal/activity.go), nil while
	// it is open or when it closed without one.
	Activity *connjournal.Activity
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
				IPKey: row.IPKey, NetKey: row.NetKey, Client: clientFingerprint(row), NoUserAgent: row.UserAgent == "",
				Activity: row.Activity})
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
TicketHours

One account's world tickets per hour from sinceMs on (minted, and
refused by the pacer, of each kind): the journal's ticket buckets in
production (JournalTickets), a fixture in tests.
================
*/
type TicketHours func(account string, sinceMs int64) ([]connjournal.TicketHour, error)

/*
================
JournalTickets

The connection journal's ticket buckets as TicketHours.
================
*/
func JournalTickets(journal *connjournal.Journal) TicketHours {
	return journal.TicketHours
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
	source  RowSource
	chat    ChatCounts
	tickets TicketHours
	now     func() time.Time

	mu    sync.Mutex
	at    time.Time
	spans []Span
	index *index
	err   error

	// The public pair's own answer, kept for publicCountsTTL so the
	// public route never drives a snapshot reload by itself.
	publicMu     sync.Mutex
	publicAt     time.Time
	publicPeople int
	publicUnique int
}

// NewService builds the census over a row source; chat may be nil,
// which only silences the chat signal, and tickets may be nil, which
// leaves the person view's ticket hours empty.
func NewService(source RowSource, chat ChatCounts, tickets TicketHours, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{source: source, chat: chat, tickets: tickets, now: now}
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

/*
================
Service.PublicCounts

The two census numbers that may be public (console#9 part 4,
opensro-web#78): people online now, and unique people over the last
7 days, by this package's clustering - the same definitions the staff
readings use. Counts only: nothing about any person - no names, no
hashes - leaves through here, so the public status answer can carry
them beside the staff readings they match. One snapshot serves both
counts; the full readings (the online series, all three unique
windows) stay staff-side. An answer is reused for publicCountsTTL; a
failed read is never kept, so the next call retries.
================
*/
func (s *Service) PublicCounts() (peopleNow int, uniquePeople7d int, err error) {
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	if !s.publicAt.IsZero() && s.now().Sub(s.publicAt) < publicCountsTTL {
		return s.publicPeople, s.publicUnique, nil
	}
	idx, err := s.snapshot()
	if err != nil {
		return 0, 0, err
	}
	now := s.nowMs()
	peopleNow = idx.peopleCount(distinctAccounts(idx.openSpans(now)))
	uniquePeople7d = idx.peopleCount(distinctAccounts(idx.spansOverlapping(now-7*windowDayMs, now, now)))
	s.publicAt, s.publicPeople, s.publicUnique = s.now(), peopleNow, uniquePeople7d
	return peopleNow, uniquePeople7d, nil
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
