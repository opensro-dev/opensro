/*
===========================================================================

ground.go - owns ground behavior and its checked data boundaries

===========================================================================
*/
// Package domain owns persisted world records shared by gameplay and storage.
package domain

/*
================================================================================
Ground item records

The live registry owns allocation, expiry, and concurrency. These value types
are the neutral snapshot exchanged with the authority store; neither side
depends on the other's implementation.
================================================================================
*/

// GroundSnapshotVersion identifies the current snapshot shape.
const GroundSnapshotVersion = 1

const (
	// GroundGoldAmountMax is the native per-heap gold ceiling.
	GroundGoldAmountMax uint32 = 0x05f5e100
	// GroundCodenameMaxBytes bounds persisted diagnostic/reference text.
	GroundCodenameMaxBytes = 255
)

// GroundItemRecord is one ground item in persisted form. VarianceBits is a
// decimal string so the JSON shape remains exact across languages.
/*
================
GroundItemRecord
================
*/
type GroundItemRecord struct {
	TradeOwner string        `json:"tradeOwner,omitempty"`
	Summon     *CharacterCOS `json:"summon,omitempty"`
	RecordID   uint64        `json:"recordId,omitempty,string"`
	// PopulationWorld identifies the owning world. Process-local population
	// generations are deliberately excluded from persistence.
	PopulationWorld uint32   `json:"populationWorld,omitempty"`
	MagicOptions    []uint64 `json:"magicOptions,omitempty"`
	// TransformRefObjID survives a dropped monster capsule (4B2B26).
	TransformRefObjID uint32  `json:"transformRefObjId,omitempty"`
	Gid               uint32  `json:"gid"`
	RefObjID          uint32  `json:"refObjId"`
	Codename          string  `json:"codename,omitempty"`
	TypeFlags         uint16  `json:"typeFlags"`
	GoldAmount        uint32  `json:"goldAmount,omitempty"`
	Plus              uint8   `json:"plus,omitempty"`
	VarianceBits      string  `json:"varianceBits,omitempty"`
	Durability        uint32  `json:"durability,omitempty"`
	StackCount        uint16  `json:"stackCount,omitempty"`
	RegionID          uint16  `json:"regionId"`
	X                 float32 `json:"x"`
	Y                 float32 `json:"y"`
	Z                 float32 `json:"z"`
	Heading           uint16  `json:"heading,omitempty"`
	OwnerJID          uint32  `json:"ownerJid,omitempty"`
	DroppedBy         string  `json:"droppedBy,omitempty"`
	DroppedAtMs       int64   `json:"droppedAtMs,omitempty"`
}

// GroundSnapshot is the complete ground registry at one instant, including
// the allocation watermark that prevents entity-id reuse after restart.
/*
================
GroundSnapshot
================
*/
type GroundSnapshot struct {
	Version    int                           `json:"version"`
	GidCounter uint32                        `json:"gidCounter"`
	Divisions  map[string][]GroundItemRecord `json:"divisions"`
}

// ItemCount returns the number of records across all divisions.
/*
================
ItemCount
================
*/
func (s GroundSnapshot) ItemCount() int {
	total := 0
	for _, rows := range s.Divisions {
		total += len(rows)
	}
	return total
}
