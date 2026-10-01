/*
===========================================================================

npc_headings.go - exact-placement facing for the authored NPC roster

The client position table has no heading column. Preserve the older server
matches and supplement missing placements with reviewed ISRO-R evidence.
No identity-only or nearest-position fallback is allowed. Unknown placements
return found=false; the published-roster test requires complete coverage.

===========================================================================
*/
package simulation

import "math"

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
npcPlacementHeading

Keep existing vSRO matches ahead of the supplement: the later database changes
some headings even at unchanged positions (Salihap and Aryoan). A later row is
not automatically better evidence for the v1.150 world.
================
*/
func npcPlacementHeading(code string, region uint16, x, y, z float64) (uint16, bool) {
	key := npcHeadingKey{code, region, npcHeadingTenth(x), npcHeadingTenth(y), npcHeadingTenth(z)}
	if heading, found := recoveredNPCHeadings[key]; found {
		return heading, true
	}
	heading, found := supplementalNPCHeadings[key]
	return heading, found
}
