/*
===========================================================================

namefilter_test.go - tests for namefilter.go

A small UTF-16 fixture pins the parser and the three refusals; the
projection's own file proves the shipped data parses and refuses its own
listed words.

===========================================================================
*/

package namefilter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
utf16File

lines as AbuseFilter.txt stores them: UTF-16LE with a byte-order mark,
CRLF between rows.
================
*/
func utf16File(lines ...string) []byte {
	units := utf16.Encode([]rune(strings.Join(lines, "\r\n")))
	out := []byte{0xff, 0xfe}
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

/*
================
fixture

Two locale groups: the second must be ignored.
================
*/
func fixture(t *testing.T) *Filter {
	t.Helper()
	f, err := Parse(utf16File(
		"#ALLOW_ID_TABLE\t0x0030\t0x0039\t\t\t\t#ALLOW_ID_TABLE\t0x0021\t0x0021\t\t\t",
		"#ALLOW_ID_TABLE\t0x0041\t0x005A\t\t\t\t\t\t\t\t\t",
		"#ALLOW_ID_TABLE\t0x005F\t0x005F\t\t\t\t\t\t\t\t\t",
		"#ALLOW_ID_TABLE\t0x0061\t0x007A\t\t\t\t\t\t\t\t\t",
		"badword\t2\t2\t2\t2\t\tother\t2\t2\t2\t2\t",
		"exact\t1\t0\t1\t0\t\t",
		"chatonly\t0\t2\t0\t0\t\t",
	))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

/*
================
TestAllowedNames
================
*/
func TestAllowedNames(t *testing.T) {
	f := fixture(t)
	for name, want := range map[string]bool{
		"Hero_01":     true,
		"exactly":     true,  // a whole-word entry is not a substring entry
		"chatonly":    true,  // a word listed only for chat
		"other":       true,  // a word of the second locale group
		"MyBadWordX":  false, // substring, matched case-insensitively
		"EXACT":       false, // whole word
		"Hero!":       false, // '!' is allowed only in the second group
		"Ünicode":     false, // outside every range
		"two words":   false, // a space is not in the table
		"":            true,  // the callers' shape refuses an empty name
		"0123456789Z": true,
	} {
		if got := f.Allowed(name); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", name, got, want)
		}
	}
}

/*
================
TestParseRefusesAFileWithoutAnAllowTable
================
*/
func TestParseRefusesAFileWithoutAnAllowTable(t *testing.T) {
	if _, err := Parse(utf16File("badword\t2\t2\t2\t2\t")); err == nil {
		t.Fatal("a filter without #ALLOW_ID_TABLE parsed")
	}
	if _, err := Parse([]byte{0xff, 0xfe, 0x41}); err == nil {
		t.Fatal("an odd-length file parsed")
	}
}

/*
================
TestLoad
================
*/
func TestLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data := utf16File(
		"#ALLOW_ID_TABLE\t0x0061\t0x007A",
		"badword\t2",
	)
	if err := os.WriteFile(filepath.Join(dir, File), data, 0o600); err != nil {
		t.Fatal(err)
	}
	filter, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Allowed("Hero") {
		t.Fatal("loaded filter refused an ordinary name")
	}
	if filter.Allowed("MyBadWordX") {
		t.Fatal("loaded filter admitted a listed substring")
	}
}

/*
================
TestLoadRefusesMissingFilter
================
*/
func TestLoadRefusesMissingFilter(t *testing.T) {
	t.Parallel()
	filter, err := Load(t.TempDir())
	if filter != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load missing filter = %v, %v; want no filter and a missing-file error", filter, err)
	}
}

/*
================
TestLoadRefusesMalformedFilter
================
*/
func TestLoadRefusesMalformedFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte{0xff, 0xfe, 0x41}, 0o600); err != nil {
		t.Fatal(err)
	}
	filter, err := Load(dir)
	if filter != nil || err == nil {
		t.Fatalf("Load malformed filter = %v, %v; want no filter and an error", filter, err)
	}
}

/*
================
TestProjectionFilter

The shipped file parses, keeps ordinary names, and refuses each of its
own listed name words (taken from the file, not written here).
================
*/
func TestProjectionFilter(t *testing.T) {
	tier.Long(t, "the shipped name filter refuses each of its own entries")
	data, err := os.ReadFile(filepath.Join(gamedatatest.TextdataDir(t), File))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Hero", "Kaplan667", "Night_Owl", "ZZtop"} {
		if !f.Allowed(name) {
			t.Errorf("ordinary name %q refused", name)
		}
	}
	if len(f.substrings) == 0 || len(f.wholeWords) == 0 {
		t.Fatalf("projection filter has %d substrings and %d whole words", len(f.substrings), len(f.wholeWords))
	}
	for _, word := range f.substrings {
		if !f.Allowed(word) {
			continue // a listed word outside the allowed characters fails earlier, as natively
		}
		t.Fatalf("listed substring %d characters long passed", len(word))
	}
	for word := range f.wholeWords {
		if f.Allowed(word) {
			t.Fatalf("listed whole word %d characters long passed", len(word))
		}
	}
}
