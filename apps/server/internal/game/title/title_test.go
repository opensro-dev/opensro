/*
===========================================================================

title_test.go - the title table and frame contract (#3502 slice 1)

===========================================================================
*/
package title

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureTitles = "# opensro-titles-v1 fixture\n" +
	"1\tTITLE_NORMAL_GOLD_USE_01\t0\t<Big Spender> %s\n" +
	"5\tTITLE_UNIQUE_MOB_SNAKE_SLAYER\t2\t<Snake Slayer> %s\n"

/*
================
TestParseTitlesReadsGeneratedRows
================
*/
func TestParseTitlesReadsGeneratedRows(t *testing.T) {
	parsed, err := ParseTitles([]byte(fixtureTitles))
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed %d titles, want 2", len(parsed))
	}
	row := parsed[5]
	if row.Codename != "TITLE_UNIQUE_MOB_SNAKE_SLAYER" || row.Category != 2 || row.Format != "<Snake Slayer> %s" {
		t.Fatalf("row 5 = %+v", row)
	}
}

/*
================
TestParseTitlesRefusesMalformedRows
================
*/
func TestParseTitlesRefusesMalformedRows(t *testing.T) {
	for _, body := range []string{
		"1\tTITLE\t0\n",                           // short row
		"0\tTITLE\t0\t<Zero> %s\n",                // id zero is no title
		"x\tTITLE\t0\t<Bad> %s\n",                 // non-numeric id
		"1\tTITLE\t0\t<A> %s\n1\tT2\t0\t<B> %s\n", // duplicate id
		"1\t\t0\t<A> %s\n",                        // empty codename
		"1\tTITLE\t0\t\n",                         // empty format
		"1\tTITLE\t0\t<" + strings.Repeat("x", maxFormatBytes) + "> %s\n", // over the frame's bound
		"1\tTITLE\t0\t<\xff> %s\n", // not UTF-8
	} {
		if _, err := ParseTitles([]byte(body)); err == nil {
			t.Fatalf("ParseTitles(%q) succeeded, want refusal", body)
		}
	}
}

/*
================
TestPeerTitleFrameCarriesIDAndFormat
================
*/
func TestPeerTitleFrameCarriesIDAndFormat(t *testing.T) {
	if err := InitTitles(writeFixtureDir(t)); err != nil {
		t.Fatal(err)
	}
	body := EncodePeerTitle(100144, 5)
	if got := binary.LittleEndian.Uint32(body[0:4]); got != 100144 {
		t.Fatalf("gid = %d, want 100144", got)
	}
	if got := binary.LittleEndian.Uint32(body[4:8]); got != 5 {
		t.Fatalf("title id = %d, want 5", got)
	}
	if got := int(binary.LittleEndian.Uint16(body[8:10])); got != len("<Snake Slayer> %s") {
		t.Fatalf("format length = %d", got)
	}
	if got := string(body[10:]); got != "<Snake Slayer> %s" {
		t.Fatalf("format = %q", got)
	}
	// An id the table does not carry still frames, with an empty format.
	body = EncodePeerTitle(100144, 999)
	if got := int(binary.LittleEndian.Uint16(body[8:10])); got != 0 || len(body) != 10 {
		t.Fatalf("unknown id frame = %d bytes, format length %d", len(body), got)
	}
}

/*
================
TestInitTitlesToleratesAMissingFile
================
*/
func TestInitTitlesToleratesAMissingFile(t *testing.T) {
	if err := InitTitles(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, ok := FormatFor(1); ok {
		t.Fatal("a missing table resolved title 1")
	}
}

/*
================
writeFixtureDir
================
*/
func writeFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, titlesFile), []byte(fixtureTitles), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
