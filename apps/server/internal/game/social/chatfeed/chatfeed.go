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

One Feed per GameWorld, written by the chat owner, the item owner and the
notice bridge, read by the public API. It is memory only: a restart
starts it empty, as the in-game history does.

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

// MaxLines is how many lines the feed keeps per division.
const MaxLines = 10

// Line kinds, as the public API names them.
const (
	KindChat   = "chat"
	KindGlobal = "global"
	KindNotice = "notice"
)

/*
================
Line

One retained public line. Character is empty for a notice.
================
*/
type Line struct {
	At        time.Time
	Kind      string
	Character string
	Text      string
}

/*
================
Feed
================
*/
type Feed struct {
	mu    sync.Mutex
	lines map[string][]Line
	now   func() time.Time
}

/*
================
New
================
*/
func New() *Feed {
	return &Feed{lines: map[string][]Line{}, now: time.Now}
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

Appends a line, dropping the oldest past MaxLines. Text is made printable
first; nothing left is not a line. A nil feed records nothing, so wiring
may leave it out.
================
*/
func (f *Feed) Record(division, kind, character, text string) {
	if f == nil {
		return
	}
	text = printable(text)
	if strings.TrimSpace(text) == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.lines[division]
	if len(rows) == MaxLines {
		copy(rows, rows[1:])
		rows = rows[:MaxLines-1]
	}
	f.lines[division] = append(rows, Line{At: f.now(), Kind: kind, Character: character, Text: text})
}

/*
================
Purge

Removes every retained line the character wrote (a GM ban). Names compare
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
		if !strings.EqualFold(line.Character, character) {
			rows = append(rows, line)
		}
	}
	clear(f.lines[division][len(rows):])
	f.lines[division] = rows
}

/*
================
Lines

A detached copy, oldest first, without the lines whose author hidden
reports (a muted character). hidden may be nil.
================
*/
func (f *Feed) Lines(division string, hidden func(character string) bool) []Line {
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
		if line.Character != "" && hidden(line.Character) {
			continue
		}
		out = append(out, line)
	}
	return out
}
