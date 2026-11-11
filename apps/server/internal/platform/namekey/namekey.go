/*
===========================================================================

namekey.go - the case-folded character identity key, folded once per name

Two hot lookups key on the same string - the transport hub's exclusive
session bindings (the social directory) and the live-world store - and
both used to fold the character name with strings.ToLower on every
call. On the tick path that is once per character per reward and
population pass, always for the same online names. Lower folds a name
once and memoizes the fold, and Key composes the shared key in its one
defining place and memoizes the composition, so the key's shape
(division id, a colon, the folded name) cannot drift between the two
owners. The fold is byte-identical to strings.ToLower for every input;
nothing but the cost changes.

===========================================================================
*/

package namekey

import (
	"strings"
	"sync"
	"sync/atomic"
)

// maxMemoizedFolds bounds the fold memo. A shard's character roster is
// far smaller; past the bound a name folds fresh each call (today's
// behaviour) rather than the memo growing without limit on names nobody
// will look up twice.
const maxMemoizedFolds = 16384

// foldMemo holds the folded form of names that contain capitals, the
// only folds that cost an allocation. Names without capitals never
// touch it, and neither do non-ASCII names, so exotic input cannot
// grow it either.
var foldMemo = struct {
	sync.Mutex
	byName map[string]string
}{byName: make(map[string]string)}

// maxMemoizedKeys bounds the composed-key memo (Key) the same way
// maxMemoizedFolds bounds the fold memo: a shard's roster is far
// smaller, and past the bound a key composes fresh rather than the
// memo growing without limit.
const maxMemoizedKeys = 16384

// composedKeys is one published snapshot of the Key memo: the composed
// key per division and raw character name. Two raw names that fold
// alike ("Jalk", "jalk") hold two entries with the same composed value;
// the memo changes only the cost, never the key. A snapshot is never
// mutated once stored: memoizeKey copies the touched division's inner
// map, so a reader walks plain maps and takes no lock at all. Key is
// called per lookup across the tick, under the world store's lock and
// the hub's; the leaf mutex it used to take on every hit serialized
// those callers against each other (#3126).
type composedKeys map[string]map[string]string

// keyMemo owns the memo's write side. insert serializes the rare
// first-touch of a name; count rides under it. The read side is the
// snapshot in current, replaced whole on each insert.
var keyMemo = struct {
	insert  sync.Mutex
	current atomic.Value // composedKeys
	count   int
}{}

/*
================
Lower

Lower folds a character name to lower case, byte-identical to
strings.ToLower: an ASCII name with no capitals returns unchanged (no
allocation), an ASCII name with capitals folds into one exact-size
buffer and is memoized, and any non-ASCII byte defers the whole string
to strings.ToLower, so exotic folds (dotted capitals, multi-rune
mappings) keep the exact behaviour of the call this replaces.
================
*/
func Lower(name string) string {
	upper := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 0x80 {
			return strings.ToLower(name)
		}
		if 'A' <= c && c <= 'Z' {
			upper = true
		}
	}
	if !upper {
		return name
	}
	foldMemo.Lock()
	folded, ok := foldMemo.byName[name]
	if !ok {
		folded = foldASCII(name)
		if len(foldMemo.byName) < maxMemoizedFolds {
			foldMemo.byName[name] = folded
		}
	}
	foldMemo.Unlock()
	return folded
}

/*
================
foldASCII

foldASCII folds a name already known to be pure ASCII with at least
one capital, into one exact-size buffer.
================
*/
func foldASCII(name string) string {
	buf := []byte(name)
	for i, c := range buf {
		if 'A' <= c && c <= 'Z' {
			buf[i] = c + ('a' - 'A')
		}
	}
	return string(buf)
}

/*
================
Key

Key is the identity key for one character in one division: the division
id, a colon, and the folded name. The hub's session bindings and the
live-world store both key on this string; its shape must never change
or the two owners stop finding each other's entries.

The composed key is memoized (keyMemo): the world-store lanes compose
the same few online names' keys on every tick (#2033: ~65 compositions
a tick, the tick's largest single allocator), and a shard's divisions
and roster are tiny next to the bound. Past the bound a key composes
fresh, exactly as before the memo. The memo's read side takes no lock
(#3126): the lanes call concurrently, and the leaf mutex each hit used
to take cost more than the two map reads it guarded.
================
*/
func Key(divisionID, characterName string) string {
	if snapshot := publishedKeys(); snapshot != nil {
		if key, ok := snapshot[divisionID][characterName]; ok {
			return key
		}
	}
	key := divisionID + ":" + Lower(characterName)
	memoizeKey(divisionID, characterName, key)
	return key
}

/*
================
publishedKeys

publishedKeys returns the memo's current snapshot, or nil before the
first insert. The snapshot's maps are immutable; reading one needs no
lock, so a tick lookup never waits on another lane's first-touch.
================
*/
func publishedKeys() composedKeys {
	if snapshot, ok := keyMemo.current.Load().(composedKeys); ok {
		return snapshot
	}
	return nil
}

/*
================
memoizeKey

memoizeKey publishes key as the composed key of one name, unless a
twin call already did or the memo is at its bound (past the bound the
key is composed fresh each call, as before the memo). Only the touched
division's inner map is copied; the rest are shared with the snapshot
they were published in, which stays valid for readers still walking it.
================
*/
func memoizeKey(divisionID, characterName, key string) {
	keyMemo.insert.Lock()
	defer keyMemo.insert.Unlock()
	current := publishedKeys()
	if _, ok := current[divisionID][characterName]; ok {
		return
	}
	if keyMemo.count >= maxMemoizedKeys {
		return
	}
	next := make(composedKeys, len(current)+1)
	for division, byName := range current {
		next[division] = byName
	}
	byName := make(map[string]string, len(current[divisionID])+1)
	for name, composed := range current[divisionID] {
		byName[name] = composed
	}
	byName[characterName] = key
	next[divisionID] = byName
	keyMemo.current.Store(next)
	keyMemo.count++
}
