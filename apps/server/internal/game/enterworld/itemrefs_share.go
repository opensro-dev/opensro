/*
===========================================================================

itemrefs_share.go - one shared item parse per process, private copies on change

Test binaries built many TextdataItems over the same directory and each
re-parsed every itemdata/characterdata table; the garbage alone was 39% of
the action package's CPU. load adopts the process-wide parse of the same
files (texttable.SharedParses); a loader that changes rows (the Apply*
shard settings) copies them first, and UseBoundedCache releases the entry
so production does not pin the rows it hands to the record cache.

===========================================================================
*/

package enterworld

import (
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/data/texttable"
)

// sharedItemParses deduplicates identical item projections per process, as
// sharedSkillParses does for skills: every package's tests built loaders over
// the same directory and re-parsed it each time.
var sharedItemParses texttable.SharedParses[TextdataItems]

/*
================
load

load adopts the process-wide parse of the same files. The parse is
immutable once published; a loader that changes rows copies them first
(ownRows), and UseBoundedCache releases the shared entry so production
does not pin the rows it hands to the record cache.
================
*/
func (t *TextdataItems) load() {
	t.sharedKey = itemTextdataKey(t.dir)
	parsed := sharedItemParses.Get(t.sharedKey, func() *TextdataItems {
		p := &TextdataItems{dir: t.dir}
		p.parse()
		return p
	})
	t.byCodename, t.byID, t.charactersByCodename = parsed.byCodename, parsed.byID, parsed.charactersByCodename
	t.summonableCharacters, t.refusedSummonRows = parsed.summonableCharacters, parsed.refusedSummonRows
	t.sharedRows = true
}

/*
================
itemTextdataKey

The files parse reads: textdataname.txt and every itemdata and characterdata
table, fingerprinted by path, size and modification time.
================
*/
func itemTextdataKey(dir string) string {
	paths := []string{filepath.Join(dir, "textdataname.txt")}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "absent:" + dir
	}
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name())
		if entry.IsDir() || !strings.HasSuffix(lower, ".txt") {
			continue
		}
		if strings.HasPrefix(lower, "itemdata") || strings.HasPrefix(lower, "characterdata") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return texttable.Fingerprint(paths...)
}

/*
================
ownRows

Gives this loader private copies of the shared rows before it changes any.
Rows are flat values (NativeFields is an immutable string), so a struct
copy is a full copy for every Apply* change.
================
*/
func (t *TextdataItems) ownRows() {
	if !t.sharedRows {
		return
	}
	t.sharedRows = false
	clones := make(map[*ItemRef]*ItemRef, len(t.byCodename))
	clone := func(ref *ItemRef) *ItemRef {
		if ref == nil {
			return nil
		}
		if c, ok := clones[ref]; ok {
			return c
		}
		c := *ref
		clones[ref] = &c
		return &c
	}
	byCodename := make(map[string]*ItemRef, len(t.byCodename))
	for name, ref := range t.byCodename {
		byCodename[name] = clone(ref)
	}
	byID := make(map[uint32]*ItemRef, len(t.byID))
	for id, ref := range t.byID {
		byID[id] = clone(ref)
	}
	characters := make(map[string]*CharacterRef, len(t.charactersByCodename))
	for name, ref := range t.charactersByCodename {
		if ref == nil {
			characters[name] = nil
			continue
		}
		c := *ref
		characters[name] = &c
	}
	t.byCodename, t.byID, t.charactersByCodename = byCodename, byID, characters
	t.summonableCharacters = append([]CharacterRef(nil), t.summonableCharacters...)
}
