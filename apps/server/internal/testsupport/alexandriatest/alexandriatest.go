/*
===========================================================================

alexandriatest.go - a licensed textdata copy with an Alexandria-style overlay

build_alexandria_variant.py only appends rows to the native tables: NPC
placements in its own regions, teleport gates, their buildings and links to
native gates. Variant reproduces that shape over the licensed retail
textdata, with one row of each kind, and writes the server overlay
(alexandria-overlay.json), so a loader can be compared against the native
tables with SRO_ALEXANDRIA off and on.

===========================================================================
*/
package alexandriatest

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"opensro.online/server/internal/testsupport/licensed"
)

const (
	// Region is a sector VSRO's Alexandria uses and v1.150 does not (0x5A30).
	Region = 23088
	// TeleportID and BuildingRef are the VSRO ids the variant keeps.
	TeleportID  = 175
	BuildingRef = 27753
	// TeleportCode and BuildingCode name the added gate and its structure.
	TeleportCode = "GATE_SD1"
	BuildingCode = "STORE_SD_GATE1"
)

/*
================
Fixture

The variant directory and what was appended to it.
================
*/
type Fixture struct {
	Dir string
	// NativeDir is the untouched licensed textdata.
	NativeDir string
	// LinkedTeleport is the native gate both added links join.
	LinkedTeleport string
}

/*
================
Variant

Copies the licensed textdata into a temporary directory, appends one
Alexandria row of each kind and writes the overlay; npcRefID is the native
NPC placed a second time, inside Region. Skips without game data.
================
*/
func Variant(t testing.TB, npcRefID string) Fixture {
	t.Helper()
	licensed.RequireGameData(t)
	native := licensed.RetailTextdataDir(t)
	dir := t.TempDir()
	entries, err := os.ReadDir(native)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(native, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, strings.ToLower(entry.Name())), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fixture := Fixture{Dir: dir, NativeDir: native}

	npc := firstRow(t, dir, "npcpos.txt", func(row []string) bool { return len(row) >= 5 && row[0] == npcRefID })
	npc[1] = strconv.Itoa(Region)
	appendRow(t, dir, "npcpos.txt", npc)

	building := firstRow(t, dir, "teleportbuilding.txt", func(row []string) bool {
		return len(row) == 58 && row[0] == "1" && row[41] != "0"
	})
	building[1], building[2], building[41] = strconv.Itoa(BuildingRef), BuildingCode, strconv.Itoa(Region)
	appendRow(t, dir, "teleportbuilding.txt", building)

	teleport := firstRow(t, dir, "teleportdata.txt", func(row []string) bool {
		return len(row) >= 13 && row[0] == "1" && row[3] != "0"
	})
	teleport[1], teleport[2], teleport[3], teleport[5] = strconv.Itoa(TeleportID), TeleportCode,
		strconv.Itoa(BuildingRef), strconv.Itoa(Region)
	appendRow(t, dir, "teleportdata.txt", teleport)

	link := firstRow(t, dir, "teleportlink.txt", func(row []string) bool { return len(row) == 21 && row[0] == "1" })
	fixture.LinkedTeleport = link[1]
	out := append([]string(nil), link...)
	out[1], out[2] = strconv.Itoa(TeleportID), fixture.LinkedTeleport
	appendRow(t, dir, "teleportlink.txt", out)
	back := append([]string(nil), link...)
	back[1], back[2] = fixture.LinkedTeleport, strconv.Itoa(TeleportID)
	appendRow(t, dir, "teleportlink.txt", back)

	overlay, err := json.Marshal(map[string]any{
		"format":    "sro-server-alexandria-overlay",
		"version":   1,
		"regions":   []int{Region},
		"teleports": []string{TeleportCode},
		"buildings": []string{BuildingCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alexandria-overlay.json"), overlay, 0o644); err != nil {
		t.Fatal(err)
	}
	return fixture
}

/*
================
AppendRow

Appends one row to a fixture table in the file's own encoding, as the
variant builder does.
================
*/
func AppendRow(t testing.TB, dir, name string, row []string) {
	t.Helper()
	appendRow(t, dir, name, row)
}

/*
================
SetLinkCondition

Rewrites the first condition triple of the teleport link row joining
source to target (the remaining triples are zeroed), in the file's own
encoding, so a test can stage a condition the native rows never carry.
================
*/
func SetLinkCondition(t testing.TB, dir, source, target string, kind, a, b uint32) {
	t.Helper()
	path := filepath.Join(dir, "teleportlink.txt")
	text, wide := decode(t, path)
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		row := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(row) != 21 || row[1] != source || row[2] != target {
			continue
		}
		row[6] = strconv.FormatUint(uint64(kind), 10)
		row[7] = strconv.FormatUint(uint64(a), 10)
		row[8] = strconv.FormatUint(uint64(b), 10)
		for j := 9; j < len(row); j++ {
			row[j] = "0"
		}
		lines[i] = strings.Join(row, "\t")
		if strings.HasSuffix(line, "\r") {
			lines[i] += "\r"
		}
		found = true
	}
	if !found {
		t.Fatalf("teleportlink has no %s -> %s row", source, target)
	}
	text = strings.Join(lines, "\n")
	payload := []byte(text)
	if wide {
		units := utf16.Encode([]rune(text))
		payload = make([]byte, 2+2*len(units))
		payload[0], payload[1] = 0xff, 0xfe
		for index, unit := range units {
			binary.LittleEndian.PutUint16(payload[2+2*index:], unit)
		}
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

/*
================
firstRow

A copy of the first data row of name that keep accepts.
================
*/
func firstRow(t testing.TB, dir, name string, keep func([]string) bool) []string {
	t.Helper()
	text, _ := decode(t, filepath.Join(dir, name))
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		row := strings.Split(line, "\t")
		if keep(row) {
			return row
		}
	}
	t.Fatalf("%s has no usable row", name)
	return nil
}

/*
================
appendRow

Appends one row in the file's own encoding, as the variant builder does.
================
*/
func appendRow(t testing.TB, dir, name string, row []string) {
	t.Helper()
	path := filepath.Join(dir, name)
	_, wide := decode(t, path)
	line := "\r\n" + strings.Join(row, "\t") + "\r\n"
	payload := []byte(line)
	if wide {
		units := utf16.Encode([]rune(line))
		payload = make([]byte, 2*len(units))
		for index, unit := range units {
			binary.LittleEndian.PutUint16(payload[2*index:], unit)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
}

/*
================
decode

The file's text, and whether it is UTF-16LE with a byte-order mark.
================
*/
func decode(t testing.TB, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xfe {
		return strings.TrimPrefix(string(data), "\ufeff"), false
	}
	units := make([]uint16, 0, (len(data)-2)/2)
	for index := 2; index+1 < len(data); index += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[index:]))
	}
	return string(utf16.Decode(units)), true
}
