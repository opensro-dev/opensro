/*
===========================================================================

chatfeed.go - the website's public chat transcript

The community site shows the shard's latest public lines (owner,
2026-10-13): ordinary beta chat (already shard-wide Global, chat.go
ClosedBetaGlobalChat), the Global Chatting item and operator notices.
Nothing private ever enters: whispers, party, guild and union lines have
no path here. A character a GM bans loses every retained line at once
(Purge), and a line whose author is muted when read is left out
(Lines' hidden test), so moderation reaches the site without a delay.

Each line carries two names: the display name the game shows (the job
alias while the author is suited) and, apart from it, the author's
character name. Moderation only ever matches the character name: a job
suit must not hide its wearer from a mute or a ban. The character name
never reaches the public API either; resolving a suited author's line to
their character would defeat the suit's anonymity.

One Feed per GameWorld, written by the chat owner, the item owner, the
notice bridge and the gameplay announcer, read by the public API. It is
memory only: a restart starts it empty, as the in-game history does.

Two streams, each with its own bound. The transcript holds what people
say (chat, Global, operator notices). The events stream holds the
automated gameplay announcements (seal drops, enhancement advances, job
kills, breaks). They were one ring once, and on a busy shard a seal
drop every minute pushed every human line off the site (owner,
2026-10-28): a machine producer must never decide how long people's
words stay up.

Port-only, not native: the original has no website.

===========================================================================
*/
package chatfeed

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// MaxLines is how many transcript lines the feed keeps per division.
const MaxLines = 10

// MaxEvents is how many gameplay announcements the feed keeps per division.
const MaxEvents = 20

// Line kinds, as the public API names them. KindEvent lines go to the
// events stream; every other kind is transcript.
const (
	KindChat   = "chat"
	KindGlobal = "global"
	KindNotice = "notice"
	KindEvent  = "event"
)

/*
================
Line

One retained public line. Character is the display name the line carries
in game (the job alias while the author is suited) and is empty for a
notice. Author is the author's stable character name, the identity
moderation keys on; it is empty for a notice and is never serialized to
the public API.
================
*/
type Line struct {
	At        time.Time
	Kind      string
	Character string
	Author    string
	Text      string
}

/*
================
Feed
================
*/
type Feed struct {
	mu     sync.Mutex
	lines  map[string][]Line
	events map[string][]Line
	now    func() time.Time
}

/*
================
New
================
*/
func New() *Feed {
	return &Feed{lines: map[string][]Line{}, events: map[string][]Line{}, now: time.Now}
}

/*
================
appendBounded

rows with line appended, the oldest dropped past limit.
================
*/
func appendBounded(rows []Line, line Line, limit int) []Line {
	if len(rows) == limit {
		copy(rows, rows[1:])
		rows = rows[:limit-1]
	}
	return append(rows, line)
}

/*
================
printable

The text without control characters or bidirectional formatting (U+200E,
U+200F, U+202A-U+202E, U+2066-U+2069): a reversed or embedded run would
let one line restyle the site's rendering of the lines around it. The site
renders text only; this keeps the API's answer safe for any reader.
================
*/
func printable(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == 0x200E, r == 0x200F,
			r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			return -1
		}
		return r
	}, text)
}

/*
================
Record

Appends a line to its stream: a KindEvent line to the events (bound
MaxEvents), any other to the transcript (bound MaxLines), dropping the
stream's oldest. author is the author's character name (empty for a
notice or an event), character the display name the line shows. Text is
made printable first; nothing left is not a line. A nil feed records
nothing, so wiring may leave it out.
================
*/
func (f *Feed) Record(division, kind, author, character, text string) {
	if f == nil {
		return
	}
	text = printable(text)
	if strings.TrimSpace(text) == "" {
		return
	}
	line := Line{At: f.now(), Kind: kind, Character: character, Author: author, Text: text}
	f.mu.Lock()
	defer f.mu.Unlock()
	if kind == KindEvent {
		f.events[division] = appendBounded(f.events[division], line, MaxEvents)
		return
	}
	f.lines[division] = appendBounded(f.lines[division], line, MaxLines)
}

/*
================
Events

The division's gameplay announcements, oldest first, as a detached copy.
They have no author, so no mute or ban filter applies.
================
*/
func (f *Feed) Events(division string) []Line {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Line(nil), f.events[division]...)
}

/*
================
Purge

Removes every retained line the character wrote (a GM ban), under
whatever display name it was recorded with. Names compare
case-insensitively, as the shard's character names do.
================
*/
func (f *Feed) Purge(division, character string) {
	if f == nil || character == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.lines[division][:0]
	for _, line := range f.lines[division] {
		if !strings.EqualFold(line.Author, character) {
			rows = append(rows, line)
		}
	}
	clear(f.lines[division][len(rows):])
	f.lines[division] = rows
}

/*
================
Lines

The transcript as a detached copy (never the events), oldest first,
without the lines whose author hidden
reports (a muted character). hidden receives the author's character
name, never the display name. hidden may be nil.
================
*/
func (f *Feed) Lines(division string, hidden func(author string) bool) []Line {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	rows := append([]Line(nil), f.lines[division]...)
	f.mu.Unlock()
	if hidden == nil {
		return rows
	}
	out := rows[:0]
	for _, line := range rows {
		if line.Author != "" && hidden(line.Author) {
			continue
		}
		out = append(out, line)
	}
	return out
}
