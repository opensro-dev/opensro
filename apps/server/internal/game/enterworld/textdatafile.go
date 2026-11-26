/*
===========================================================================

textdatafile.go - reading one shipped textdata file

One decoder for every lane that reads the client's tab-separated
textdata (itemrefs.go's item and character rows, internal/game/quest's
tables): the encodings Silkroad releases ship, blank and comment lines
skipped, cells split by texttable. The row builders live beside their
tables; this file only turns bytes into rows.

===========================================================================
*/

package enterworld

import (
	"encoding/binary"
	"os"
	"strings"
	"unicode/utf16"

	"opensro.online/server/internal/data/texttable"
)

/*
==================
ReadTextdataFile

ReadTextdataFile is the exported face of readTextdataFile for sibling
lanes that load their own shipped tables (internal/game/quest's questdata /
questcontentsdata catalog) - one decoder, so a media re-encode can
never split the lanes' readings of the same file.
==================
*/
func ReadTextdataFile(path string) [][]string {
	return readTextdataFile(path)
}

/*
==================
readTextdataFile

readTextdataFile reads one tab-separated textdata file, tolerant of the
encodings Silkroad releases ship (UTF-16LE with BOM, a null-heavy UTF-16LE
body, or UTF-8), skipping blank and // comment lines.
==================
*/
func readTextdataFile(path string) [][]string {
	buffer, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := decodeTextdata(buffer)
	cells := make(texttable.Cells)
	var rows [][]string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, "\r\t ")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		rows = append(rows, cells.Split(line))
	}
	return rows
}

/*
================
decodeTextdata
================
*/
func decodeTextdata(buffer []byte) string {
	if len(buffer) >= 2 && buffer[0] == 0xff && buffer[1] == 0xfe {
		return decodeUTF16LE(buffer[2:])
	}
	sampled := len(buffer)
	if sampled > 512 {
		sampled = 512
	}
	nulls := 0
	for i := 0; i < sampled; i++ {
		if buffer[i] == 0x00 {
			nulls++
		}
	}
	if sampled > 0 && float64(nulls)/float64(sampled) > 0.2 {
		return decodeUTF16LE(buffer)
	}
	return string(buffer)
}

/*
================
decodeUTF16LE
================
*/
func decodeUTF16LE(buffer []byte) string {
	units := make([]uint16, 0, len(buffer)/2)
	for i := 0; i+1 < len(buffer); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(buffer[i:i+2]))
	}
	return string(utf16.Decode(units))
}
