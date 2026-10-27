/*
===========================================================================

namefilter.go - the native name filter (AbuseFilter.txt)

The original server checks every player-chosen name (its v1.188
character-name check) against AbuseFilter.txt, which its name filter
loads once and applies to each name:

  - the text is lowercased in ASCII only;
  - every code point must lie in the locale's #ALLOW_ID_TABLE ranges (the
    loader fills a bitmap from them and then inverts it, so the code-point
    test refuses a code point outside them);
  - no context-1 word of type 2 may appear anywhere in it (substring test);
  - no context-1 word of type 1 may equal one of its space-separated
    words (whole-word test).

The file is UTF-16 with six columns per locale: the word (or
#ALLOW_ID_TABLE), then four context types, then a blank. Context 1 is
names; the others are chat and notices.

INFERENCE: the server picks its locale's column group from the shard
configuration, which the port does not have. The port takes the first
group, the one the browser client already applies to new names
(foundation/ui/character-create.ts), so the server and the client refuse
the same names.

The server's name check also refuses a space and anything the shard
database's rejected-text table matches. The port
has no copy of that table; the callers' name shape already refuses a space
and every quote.

===========================================================================
*/

package namefilter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// File is the projection's name filter, under textdata.
const File = "abusefilter.txt"

const (
	allowTableTag = "#allow_id_table"
	// columnsPerLocale is the server filter loader's six columns a locale group spans.
	columnsPerLocale = 6
	// The name context's word types (the loader's first context column).
	typeWholeWord = "1"
	typeSubstring = "2"
)

/*
================
Filter

One immutable parsed name filter, shared by character creation and gameplay.
================
*/
type Filter struct {
	allowed    map[rune]bool
	substrings []string
	wholeWords map[string]bool
}

/*
================
Load

A missing or malformed projection filter fails construction before any
player-chosen name can pass unchecked.
================
*/
func Load(textdataDir string) (*Filter, error) {
	data, err := os.ReadFile(filepath.Join(textdataDir, File))
	if err != nil {
		return nil, fmt.Errorf("name filter: %w", err)
	}
	filter, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("name filter: %w", err)
	}
	return filter, nil
}

/*
================
Parse

Reads the UTF-16LE file (a byte-order mark is optional) and keeps the
first locale group. A file with no #ALLOW_ID_TABLE row is refused: every
name would fail.
================
*/
func Parse(data []byte) (*Filter, error) {
	text, err := decodeUTF16(data)
	if err != nil {
		return nil, err
	}
	f := &Filter{allowed: map[rune]bool{}, wholeWords: map[string]bool{}}
	for number, line := range strings.Split(text, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) > columnsPerLocale {
			fields = fields[:columnsPerLocale]
		}
		word := lowerASCII(strings.TrimSpace(fields[0]))
		if word == "" || len(fields) < 2 {
			continue
		}
		if word == allowTableTag {
			if len(fields) < 3 {
				return nil, fmt.Errorf("namefilter: line %d: allow range needs two bounds", number+1)
			}
			lo, errLo := strconv.ParseUint(strings.TrimSpace(fields[1]), 0, 16)
			hi, errHi := strconv.ParseUint(strings.TrimSpace(fields[2]), 0, 16)
			if errLo != nil || errHi != nil || lo > hi {
				return nil, fmt.Errorf("namefilter: line %d: bad allow range %q..%q", number+1, fields[1], fields[2])
			}
			for code := lo; code <= hi; code++ {
				f.allowed[rune(code)] = true
			}
			continue
		}
		if strings.HasPrefix(word, "#") {
			continue
		}
		switch strings.TrimSpace(fields[1]) {
		case typeWholeWord:
			f.wholeWords[word] = true
		case typeSubstring:
			f.substrings = append(f.substrings, word)
		}
	}
	if len(f.allowed) == 0 {
		return nil, errors.New("namefilter: no #ALLOW_ID_TABLE row")
	}
	return f, nil
}

/*
================
Allowed

The server's name check for one name: allowed code points only, no listed substring, no
listed whole word.
================
*/
func (f *Filter) Allowed(name string) bool {
	text := lowerASCII(name)
	for _, r := range text {
		if !f.allowed[r] {
			return false
		}
	}
	for _, word := range f.substrings {
		if strings.Contains(text, word) {
			return false
		}
	}
	for _, word := range strings.Split(text, " ") {
		if f.wholeWords[word] {
			return false
		}
	}
	return true
}

/*
================
lowerASCII

Text_CopyLowercaseASCII: A-Z only; every other byte is kept.
================
*/
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

/*
================
decodeUTF16
================
*/
func decodeUTF16(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xff, 0xfe})
	if len(data)%2 != 0 {
		return "", errors.New("namefilter: odd UTF-16 length")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return string(utf16.Decode(units)), nil
}
