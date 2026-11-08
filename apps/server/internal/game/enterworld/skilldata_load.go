/*
===========================================================================

skilldata_load.go - the skill table's load lane: shard index and shared parse

The loader side of skilldata.go: which files make up the table and how a
TextdataSkills adopts the process-wide parse of them. The row surface
(SkillRow, the accessors and the parser itself) stays in skilldata.go.

===========================================================================
*/

package enterworld

import (
	"opensro.online/server/internal/data/texttable"
	"path/filepath"
	"strings"
)

/*
==================
load

load adopts the process-wide parse of the same files. The parse is
immutable once published: rows, compiled plans and the codename index are
only read afterwards, and UseBoundedCache replaces fields, never contents.
==================
*/
func (t *TextdataSkills) load() {
	shards := skillShards(t.dir)
	// skilleffect.txt feeds AssistsCompanions (skillassist.go).
	paths := []string{filepath.Join(t.dir, "skilldata.txt"), filepath.Join(t.dir, "skilleffect.txt")}
	for _, shard := range shards {
		paths = append(paths, filepath.Join(t.dir, shard))
	}
	t.sharedKey = texttable.Fingerprint(paths...)
	if t.tomb {
		t.sharedKey += "+qinshi_tomb"
	}
	// The rank index is a pure function of the rows, so it is built once
	// with the parse and shared read-only: decoding every resident row per
	// loader cost each test binary tens of seconds.
	parsed := sharedSkillParses.Get(t.sharedKey, func() *TextdataSkills {
		p := &TextdataSkills{dir: t.dir, tomb: t.tomb}
		p.parse(shards)
		p.indexLearnedRanks()
		return p
	})
	t.shared = parsed
	t.rows, t.plans, t.byCodename, t.loadErr = parsed.rows, parsed.plans, parsed.byCodename, parsed.loadErr
	t.byRank = parsed.byRank
}

/*
==================
skillShards

skillShards reads skilldata.txt, an INDEX naming the shard files; the
client feeds it and skilldata_virtual.txt through the same parser
(Client_LoadGameDataTables @0x00722f78 then @0x00722fc0), so the server
loads the same set.
==================
*/
func skillShards(dir string) []string {
	shards := []string{}
	for _, fields := range readTextdataFile(filepath.Join(dir, "skilldata.txt")) {
		if len(fields) == 1 && fields[0] != "" {
			// The server projection canonicalizes textdata filenames to lowercase;
			// the authored index retains Windows casing (SkillData_5000.txt).
			shards = append(shards, strings.ToLower(fields[0]))
		}
	}
	return append(shards, "skilldata_virtual.txt")
}
