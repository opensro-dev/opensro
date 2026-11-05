/*
===========================================================================

mutes.go - the chat mute book: staff mutes that survive relogs and restarts

Package moderation owns staff penalties the GameWorld enforces (#2671). A
chat mute is the native chat command restriction (transport kind 4, the
record v1.150's chat gates already read through restriction.Report), so a
muted player sees the client's own "chat blocked until" notice. What the
native record lacks, this book adds: it is keyed by character, written to
the shard state directory on every change, reinstalled into the hub at
boot, and cleared by Reap when its time is up. The native design leaves
expiry to the installer (restriction.go: "Do not infer expiration from
the server clock"); this book is that installer.

The hub (transport) owns the live records; this package owns which mutes
exist and when they end. Nothing else installs chat restrictions.

===========================================================================
*/
package moderation

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/transport"
)

// MuteFileName is the book's file in the shard state directory.
const MuteFileName = "chat-mutes.json"

// MaxMuteDuration bounds one mute. Anything longer is a ban, which the
// Agent's account block owns (provisioning SetBlock).
const MaxMuteDuration = 30 * 24 * time.Hour

const (
	maxClauseBytes   = 16
	maxReasonRunes   = 500
	maxOperatorBytes = 80
	muteFileVersion  = 1
)

// ErrNoMute is returned when an unmute names a character with no mute.
var ErrNoMute = errors.New("moderation: character is not muted")

/*
================
RestrictionHub

What the book needs from the transport hub: the by-character install and
clear that reach every live session of the character and every session
bound to it later.
================
*/
type RestrictionHub interface {
	SetCharacterRestriction(divisionID, characterName string, kind uint8, until [8]uint16) bool
	ClearCharacterRestriction(divisionID, characterName string, kind uint8) bool
}

/*
================
Mute

One staff chat mute. Clause is the published rule's clause id the action
cites; Reason is the staff text; both land in the operator audit too.
================
*/
type Mute struct {
	Division  string `json:"division"`
	Character string `json:"character"`
	UntilMs   int64  `json:"untilMs"`
	AtMs      int64  `json:"atMs"`
	Operator  string `json:"operator"`
	Clause    string `json:"clause"`
	Reason    string `json:"reason"`
}

type muteFile struct {
	Version int    `json:"version"`
	Mutes   []Mute `json:"mutes"`
}

/*
================
Book
================
*/
type Book struct {
	mu    sync.Mutex
	path  string
	hub   RestrictionHub
	now   func() time.Time
	write func(path string, payload []byte) error
	mutes map[string]Mute
}

func key(division, character string) string {
	return division + ":" + strings.ToLower(character)
}

/*
================
OpenBook

Loads the book from path (a missing file is an empty book; write is the
durable atomic writer, store.WriteFileAtomic in the server), drops mutes
that ended while the server was down, and installs the rest into the hub.
A corrupt file refuses to start: silently forgetting every mute would
let the people staff silenced talk again without anyone noticing.
================
*/
func OpenBook(path string, hub RestrictionHub, now func() time.Time, write func(string, []byte) error) (*Book, error) {
	if hub == nil || now == nil || write == nil {
		return nil, errors.New("moderation: hub, clock and writer are required")
	}
	book := &Book{path: path, hub: hub, now: now, write: write, mutes: map[string]Mute{}}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("moderation: read %s: %w", path, err)
	}
	if err == nil {
		var file muteFile
		if err := json.Unmarshal(raw, &file); err != nil || file.Version != muteFileVersion {
			return nil, fmt.Errorf("moderation: %s is not a version %d mute book (%v)", path, muteFileVersion, err)
		}
		for _, mute := range file.Mutes {
			book.mutes[key(mute.Division, mute.Character)] = mute
		}
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	nowMs := now().UnixMilli()
	for k, mute := range book.mutes {
		if mute.UntilMs <= nowMs {
			delete(book.mutes, k)
			continue
		}
		hub.SetCharacterRestriction(mute.Division, mute.Character, transport.CommandRestrictionChat, systemTime(mute.UntilMs))
	}
	return book, book.saveLocked()
}

/*
================
Mute

Validates, saves, then installs: a mute the disk does not hold is never
announced as applied. A second mute on the same character replaces the
first (staff extend or shorten by muting again).
================
*/
func (book *Book) Mute(mute Mute, duration time.Duration) (Mute, error) {
	mute.Character = strings.TrimSpace(mute.Character)
	mute.Reason = strings.TrimSpace(mute.Reason)
	switch {
	case mute.Character == "":
		return Mute{}, errors.New("moderation: character required")
	case duration < time.Minute || duration > MaxMuteDuration:
		return Mute{}, fmt.Errorf("moderation: duration must be between 1 minute and %s", MaxMuteDuration)
	case !ValidClause(mute.Clause):
		return Mute{}, errors.New("moderation: clause id required (letters, digits, dot, dash; up to 16)")
	case len([]rune(mute.Reason)) < 5 || len([]rune(mute.Reason)) > maxReasonRunes:
		return Mute{}, errors.New("moderation: reason must be 5 to 500 characters")
	case mute.Operator == "" || len(mute.Operator) > maxOperatorBytes:
		return Mute{}, errors.New("moderation: operator required")
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	now := book.now()
	mute.AtMs = now.UnixMilli()
	mute.UntilMs = now.Add(duration).UnixMilli()
	k := key(mute.Division, mute.Character)
	previous, had := book.mutes[k]
	book.mutes[k] = mute
	if err := book.saveLocked(); err != nil {
		if had {
			book.mutes[k] = previous
		} else {
			delete(book.mutes, k)
		}
		return Mute{}, err
	}
	book.hub.SetCharacterRestriction(mute.Division, mute.Character, transport.CommandRestrictionChat, systemTime(mute.UntilMs))
	return mute, nil
}

/*
================
Unmute
================
*/
func (book *Book) Unmute(division, character string) (Mute, error) {
	book.mu.Lock()
	defer book.mu.Unlock()
	k := key(division, character)
	mute, ok := book.mutes[k]
	if !ok {
		return Mute{}, ErrNoMute
	}
	delete(book.mutes, k)
	if err := book.saveLocked(); err != nil {
		book.mutes[k] = mute
		return Mute{}, err
	}
	book.hub.ClearCharacterRestriction(mute.Division, mute.Character, transport.CommandRestrictionChat)
	return mute, nil
}

/*
================
Active

The character's mute, if one is running now.
================
*/
func (book *Book) Active(division, character string) (Mute, bool) {
	book.mu.Lock()
	defer book.mu.Unlock()
	mute, ok := book.mutes[key(division, character)]
	if !ok || mute.UntilMs <= book.now().UnixMilli() {
		return Mute{}, false
	}
	return mute, true
}

/*
================
Reap

Clears every mute whose time is up and returns them. The caller runs it
on a timer (wiring: every ReapInterval). A failed save keeps the mutes in
the book, still enforced, and retries on the next pass.
================
*/
func (book *Book) Reap() ([]Mute, error) {
	book.mu.Lock()
	defer book.mu.Unlock()
	nowMs := book.now().UnixMilli()
	var ended []Mute
	for k, mute := range book.mutes {
		if mute.UntilMs <= nowMs {
			ended = append(ended, mute)
			delete(book.mutes, k)
		}
	}
	if len(ended) == 0 {
		return nil, nil
	}
	if err := book.saveLocked(); err != nil {
		for _, mute := range ended {
			book.mutes[key(mute.Division, mute.Character)] = mute
		}
		return nil, err
	}
	for _, mute := range ended {
		book.hub.ClearCharacterRestriction(mute.Division, mute.Character, transport.CommandRestrictionChat)
	}
	return ended, nil
}

// ReapInterval is how often the wiring calls Reap: a mute ends at most
// this late, and the client's own notice already shows the end time.
const ReapInterval = 30 * time.Second

func (book *Book) saveLocked() error {
	file := muteFile{Version: muteFileVersion, Mutes: make([]Mute, 0, len(book.mutes))}
	for _, mute := range book.mutes {
		file.Mutes = append(file.Mutes, mute)
	}
	sort.Slice(file.Mutes, func(i, j int) bool {
		return key(file.Mutes[i].Division, file.Mutes[i].Character) < key(file.Mutes[j].Division, file.Mutes[j].Character)
	})
	payload, err := json.MarshalIndent(file, "", "\t")
	if err != nil {
		return err
	}
	if err := book.write(book.path, append(payload, '\n')); err != nil {
		return fmt.Errorf("moderation: save %s: %w", book.path, err)
	}
	return nil
}

/*
================
ValidClause

A clause id from the published rules: 1 to 16 of letters, digits, dot
and dash ("C1", "chat.2").
================
*/
func ValidClause(clause string) bool {
	if clause == "" || len(clause) > maxClauseBytes {
		return false
	}
	for _, r := range clause {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

/*
================
systemTime

The mute's end as the Win32 SYSTEMTIME the native record and notice carry
(year, month, weekday, day, hour, minute, second, ms), in UTC: the shard
keeps every stamp in UTC and the client prints the fields as given.
================
*/
func systemTime(ms int64) [8]uint16 {
	at := time.UnixMilli(ms).UTC()
	return [8]uint16{uint16(at.Year()), uint16(at.Month()), uint16(at.Weekday()), uint16(at.Day()),
		uint16(at.Hour()), uint16(at.Minute()), uint16(at.Second()), uint16(at.Nanosecond() / 1e6)}
}
