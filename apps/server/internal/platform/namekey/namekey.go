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

// keyMemo holds the composed Key per division and raw character name.
// Two raw names that fold alike ("Jalk", "jalk") hold two entries with
// the same composed value; the memo changes only the cost, never the
// key. Guarded by its own mutex: Key is called under the world store's
// lock and the hub's, so this leaf lock is the only one it may take
// (Lower's foldMemo lock is taken after this one is released).
var keyMemo = struct {
	sync.Mutex
	byDivision map[string]map[string]string
	count      int
}{byDivision: make(map[string]map[string]string)}

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
fresh, exactly as before the memo.
================
*/
func Key(divisionID, characterName string) string {
	keyMemo.Lock()
	if byName, ok := keyMemo.byDivision[divisionID]; ok {
		key, ok := byName[characterName]
		keyMemo.Unlock()
		if ok {
			return key
		}
	} else {
		keyMemo.Unlock()
	}
	key := divisionID + ":" + Lower(characterName)
	keyMemo.Lock()
	if keyMemo.count < maxMemoizedKeys {
		byName, ok := keyMemo.byDivision[divisionID]
		if !ok {
			byName = make(map[string]string)
			keyMemo.byDivision[divisionID] = byName
		}
		if _, ok := byName[characterName]; !ok {
			byName[characterName] = key
			keyMemo.count++
		}
	}
	keyMemo.Unlock()
	return key
}
