/*
===========================================================================

title.go - the later client's title table (#3502): which title id prints
how on a name line

Later-native, not port-only: the title system is the later client's own
(build 1696), so it ships as the default with no switch (the owner's
later-native rule). titles.txt (scripts/title_import.py, cut from that
client's Media.pk2) carries one row per title the build localizes: id,
codename, category (0-4, the title window's tabs) and the name-line
format - the title's SN text, a printf-style string the character's
name prints through ("<Big Spender> %s"). No title is id 0 and prints
as plain "%s". Only the format rides to the client in this slice;
codenames and categories stay server-side for the title window slice.

The later client reads the id off the spawn stream (its spawn reader
reads a u32 and calls its title setter before building the display
name). The port carries the id on its own frame instead (wire.go): a
tab loaded before this release decodes the spawn row length-strictly,
so a grown row would fail it at the first player spawn, while a client
that predates the frame drops the unknown opcode.

InitTitles is the one load, at wiring time; a missing file leaves
every id unresolved and names print plain, as a native title lookup
miss does.

===========================================================================
*/
package title

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// titlesFile is the generated table's name in the server textdata root.
const titlesFile = "titles.txt"

// Title is one row of the later client's RefTitle with its resolved
// name-line format.
type Title struct {
	ID       uint32
	Codename string
	Category uint8
	Format   string
}

// table is the loaded title set, replaced whole by InitTitles before
// gameplay starts and read-only after (the loot catalogue's shape).
var table = map[uint32]Title{}

/*
================
InitTitles

Loads titles.txt from the server textdata root. A missing file is not
an error: the generated table is cut on the maintainer's machine, and
without it every title id is simply unresolved. A present file must
parse whole.
================
*/
func InitTitles(textdataDir string) error {
	data, err := os.ReadFile(filepath.Join(textdataDir, titlesFile))
	if os.IsNotExist(err) {
		log.Printf("titles: no %s; title ids resolve to plain names", titlesFile)
		table = map[uint32]Title{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("titles: %w", err)
	}
	parsed, err := ParseTitles(data)
	if err != nil {
		return fmt.Errorf("titles: %w", err)
	}
	table = parsed
	return nil
}

/*
================
ParseTitles

Parses the generated titles.txt: one title per line,
id <TAB> codename <TAB> category <TAB> format, '#' comments and blank
lines aside. Strict, because a half-read table would print some names
titled and some not: a malformed row, a duplicate id or an empty
format refuses the whole file.
================
*/
func ParseTitles(data []byte) (map[uint32]Title, error) {
	parsed := map[uint32]Title{}
	for lineNo, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("line %d: %d fields, want 4", lineNo+1, len(fields))
		}
		id, err := strconv.ParseUint(fields[0], 10, 32)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("line %d: bad title id %q", lineNo+1, fields[0])
		}
		category, err := strconv.ParseUint(fields[2], 10, 8)
		if err != nil {
			return nil, fmt.Errorf("line %d: bad category %q", lineNo+1, fields[2])
		}
		if fields[1] == "" || fields[3] == "" {
			return nil, fmt.Errorf("line %d: empty codename or format", lineNo+1)
		}
		if _, dup := parsed[uint32(id)]; dup {
			return nil, fmt.Errorf("line %d: duplicate title id %d", lineNo+1, id)
		}
		parsed[uint32(id)] = Title{
			ID:       uint32(id),
			Codename: fields[1],
			Category: uint8(category),
			Format:   fields[3],
		}
	}
	return parsed, nil
}

/*
================
FormatFor

The name-line format for a title id; false for id 0 and for ids the
table does not carry (unlocalized in this build), which print the
plain name.
================
*/
func FormatFor(id uint32) (string, bool) {
	row, ok := table[id]
	if !ok {
		return "", false
	}
	return row.Format, true
}

/*
================
Known

Whether the table carries a title id at all.
================
*/
func Known(id uint32) bool {
	_, ok := table[id]
	return ok
}
