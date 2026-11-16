/*
===========================================================================

caravan_agent.go - persistent memory and route danger rows for AI caravan agents

Bounded relationship and route heat records stored in the shard SQLite
database when SRO_AI_CARAVANS is enabled.

===========================================================================
*/
package domain

const (
	MaxCaravanMemoriesPerAgent = 256
	MaxCaravanTotalMemories    = 4096
	MaxCaravanRouteHeatRegions = 256
	MaxCaravanMemoryNoteBytes  = 160

	CaravanTrustMin      = -100
	CaravanTrustMax      = 100
	CaravanVendettaScore = -50
	CaravanAllyScore     = 50
)

/*
================
CaravanAgentMemoryRow

One persistent relationship record between a named AI caravan agent and a
player character in the same division.
================
*/
type CaravanAgentMemoryRow struct {
	Division    string
	AgentName   string
	PlayerName  string
	TrustScore  int
	Kills       int
	Deaths      int
	GoldStolen  int64
	LastRegion  uint16
	LastNote    string
	UpdatedAtMs int64
}

/*
================
CaravanRouteHeatRow

Per-region ambush danger score learned by caravan agents along trade highways.
================
*/
type CaravanRouteHeatRow struct {
	Division    string
	RegionID    uint16
	DangerScore int
	Ambushes    int
	UpdatedAtMs int64
}

/*
================
ClampCaravanTrust

Clamp a relationship trust score to the [-100, +100] range.
================
*/
func ClampCaravanTrust(score int) int {
	if score < CaravanTrustMin {
		return CaravanTrustMin
	}
	if score > CaravanTrustMax {
		return CaravanTrustMax
	}
	return score
}

/*
================
TruncateCaravanNote

Bound an encounter memory note to MaxCaravanMemoryNoteBytes UTF-8 bytes so
persistent rows cannot grow unbounded.
================
*/
func TruncateCaravanNote(note string) string {
	if len(note) <= MaxCaravanMemoryNoteBytes {
		return note
	}
	cut := MaxCaravanMemoryNoteBytes
	for cut > 0 && (note[cut]&0xC0) == 0x80 {
		cut--
	}
	return note[:cut]
}
