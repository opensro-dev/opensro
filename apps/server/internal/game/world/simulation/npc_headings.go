/*
===========================================================================

npc_headings.go - exact-placement facing for the authored NPC roster

The client position table has no heading column. The operator's table
(NPCHeadingsFile, from their own server databases) supplies a heading per
exact placement: the older server's matches first, then reviewed later
placements, then the optional Alexandria city. No identity-only or
nearest-position fallback is allowed. Unknown placements return
found=false and face heading zero; without the table every NPC does.

===========================================================================
*/
package simulation

import (
	"encoding/json"
	"fmt"
	"math"

	"opensro.online/server/internal/gamedata"
)

// NPCHeadingsFile is the placement heading table in the derived data.
const NPCHeadingsFile = "simulation/npc-headings.json"

// npcHeadingsFormat names the table's layout: per section,
// [codename, region, x10, y10, z10, heading] rows.
const npcHeadingsFormat = "sro-npc-headings-v1"

/*
================
npcHeadingKey

Float32 coordinates are normalized to tenths, matching the evidence extractor.
This joins serialized coordinate precision, not nearby or relocated NPCs.
================
*/
type npcHeadingKey struct {
	Codename      string
	Region        uint16
	X10, Y10, Z10 int64
}

/*
================
npcHeadingTenth
================
*/
func npcHeadingTenth(value float64) int64 {
	return int64(math.Round(float64(float32(value)) * 10))
}

/*
================
NPCHeadings

The loaded table in precedence order. A nil table knows no placement.
================
*/
type NPCHeadings struct {
	// sections are consulted in order: recovered, supplement, alexandria.
	sections [3]map[npcHeadingKey]uint16
}

/*
================
LoadNPCHeadings

The table from derivedDir, or nil when there is none.
================
*/
func LoadNPCHeadings(derivedDir string) (*NPCHeadings, error) {
	data, err := gamedata.ReadDerived(derivedDir, NPCHeadingsFile)
	if err != nil || data == nil {
		return nil, err
	}
	var document struct {
		Format                            string
		Recovered, Supplement, Alexandria [][6]json.RawMessage
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("npc headings: %w", err)
	}
	if document.Format != npcHeadingsFormat {
		return nil, fmt.Errorf("npc headings: format %q, want %q", document.Format, npcHeadingsFormat)
	}
	headings := &NPCHeadings{}
	for i, rows := range [3][][6]json.RawMessage{document.Recovered, document.Supplement, document.Alexandria} {
		section := make(map[npcHeadingKey]uint16, len(rows))
		for n, row := range rows {
			var key npcHeadingKey
			var heading uint16
			if json.Unmarshal(row[0], &key.Codename) != nil || json.Unmarshal(row[1], &key.Region) != nil ||
				json.Unmarshal(row[2], &key.X10) != nil || json.Unmarshal(row[3], &key.Y10) != nil ||
				json.Unmarshal(row[4], &key.Z10) != nil || json.Unmarshal(row[5], &heading) != nil || key.Codename == "" {
				return nil, fmt.Errorf("npc headings: malformed row %d of section %d", n, i)
			}
			if _, exists := section[key]; exists {
				return nil, fmt.Errorf("npc headings: duplicate placement %s in section %d", key.Codename, i)
			}
			section[key] = heading
		}
		headings.sections[i] = section
	}
	return headings, nil
}

/*
================
placement

Keep existing vSRO matches ahead of the supplement: the later database changes
some headings even at unchanged positions (Salihap and Aryoan). A later row is
not automatically better evidence for the v1.150 world.
================
*/
func (h *NPCHeadings) placement(code string, region uint16, x, y, z float64) (uint16, bool) {
	if h == nil {
		return 0, false
	}
	key := npcHeadingKey{code, region, npcHeadingTenth(x), npcHeadingTenth(y), npcHeadingTenth(z)}
	for _, section := range h.sections {
		if heading, found := section[key]; found {
			return heading, true
		}
	}
	return 0, false
}
