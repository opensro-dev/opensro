/*
===========================================================================

gacha_pool_reference.go - the published v1.696 Magic POP prize pool

The v1.696 Magic POP window lists thousands of prizes, filtered by its
select window's category tree, so the browser reads the pool from the
server's public references instead of a hard-coded list. The server owns
the RefObjIDs (the import remaps the later client's ids), so it is the
one publisher. Published only while the v1.696 catalog is active.

===========================================================================
*/

package enterworld

import "fmt"

// The browser's decodeGachaPool bounds (apps/client-next/src/engine/
// foundation/gameplay/gacha-pool.ts): the pool must decode there, so the
// server refuses what the reader would throw on.
const (
	gachaPoolMaxOptions  = 5
	gachaPoolMaxTreeRows = 4096
	gachaPoolMaxLabel    = 128
)

/*
================
GachaPoolReference

One machine's pool: the visible set in table order, the level cap the
client gates rows by (the v1.696 client shows a row only while the cap
reaches its column 16), and the select window's tree.
================
*/
type GachaPoolReference struct {
	Npc        uint32            `json:"npc"`
	VisibleSet uint8             `json:"visibleSet"`
	LevelCap   int64             `json:"levelCap"`
	Entries    []GachaPoolEntry  `json:"entries"`
	Tree       []GachaPoolBranch `json:"tree"`
}

// GachaPoolEntry is one prize row as the window displays it: the spec's
// O token is the plus level and each M token a blue option line.
type GachaPoolEntry struct {
	Entry       uint32            `json:"entry"`
	RefObjID    uint32            `json:"refObjId"`
	Quantity    uint32            `json:"quantity"`
	MinLevelCap uint32            `json:"minLevelCap"`
	Plus        uint8             `json:"plus"`
	Options     []GachaPoolOption `json:"options"`
}

// GachaPoolOption is one M token: the option's short name and value, and the
// magicoption row the server resolves from the token's tag and degree (the
// id the exchange writes into the option word). A fixture runtime with no
// magic option table publishes none, and the browser draws no option line.
type GachaPoolOption struct {
	Name    string `json:"name"`
	Value   int64  `json:"value"`
	ParamID uint16 `json:"paramId,omitempty"`
}

// GachaPoolBranch is one refgachatreeforclientuidata.txt row.
type GachaPoolBranch struct {
	TypeID     [4]uint8 `json:"tid"`
	Group      uint32   `json:"group"`
	GroupLabel string   `json:"groupLabel"`
	Sub        uint32   `json:"sub"`
	SubLabel   string   `json:"subLabel"`
	Degrees    uint8    `json:"degrees"`
}

/*
================
validateGachaPool

Every prize must be a published item row, or the window would show a
blank slot. The rest of the shape is held to the reader's own bounds
(decodeGachaPool), so a pool the server publishes is one the browser can
decode rather than one it throws on.
================
*/
func validateGachaPool(pool *GachaPoolReference, itemIDs map[uint32]bool) error {
	if pool == nil {
		return nil
	}
	if len(pool.Entries) == 0 || len(pool.Entries) > maxPublicReferenceRows {
		return fmt.Errorf("invalid gacha pool size: %d entries", len(pool.Entries))
	}
	if len(pool.Tree) > gachaPoolMaxTreeRows {
		return fmt.Errorf("invalid gacha pool tree size: %d rows", len(pool.Tree))
	}
	if pool.Npc == 0 || pool.LevelCap < 1 || pool.LevelCap > 255 {
		return fmt.Errorf("invalid gacha pool machine %d at cap %d", pool.Npc, pool.LevelCap)
	}
	seen := make(map[uint32]bool, len(pool.Entries))
	for _, entry := range pool.Entries {
		if entry.Entry == 0 || seen[entry.Entry] {
			return fmt.Errorf("gacha pool entry %d is zero or repeated", entry.Entry)
		}
		seen[entry.Entry] = true
		if entry.RefObjID == 0 || entry.Quantity == 0 || entry.MinLevelCap > 255 {
			return fmt.Errorf("gacha pool entry %d has invalid reward %d x%d at cap %d",
				entry.Entry, entry.RefObjID, entry.Quantity, entry.MinLevelCap)
		}
		if !itemIDs[entry.RefObjID] {
			return fmt.Errorf("gacha pool entry %d names unpublished item %d", entry.Entry, entry.RefObjID)
		}
		if len(entry.Options) > gachaPoolMaxOptions {
			return fmt.Errorf("gacha pool entry %d has %d options", entry.Entry, len(entry.Options))
		}
		for _, option := range entry.Options {
			if !validGachaOptionName(option.Name) || option.Value < 0 || option.Value > 0xffffffff {
				return fmt.Errorf("gacha pool entry %d has invalid option %q=%d", entry.Entry, option.Name, option.Value)
			}
		}
	}
	for _, branch := range pool.Tree {
		for _, label := range []string{branch.GroupLabel, branch.SubLabel} {
			if len(label) == 0 || len(label) > gachaPoolMaxLabel {
				return fmt.Errorf("gacha pool tree row %d/%d has an invalid label %q", branch.Group, branch.Sub, label)
			}
		}
	}
	return nil
}

/*
================
validGachaOptionName

The reader's `/^[a-z0-9]{1,4}$/i`: the magicoption short name the M token
carries.
================
*/
func validGachaOptionName(name string) bool {
	if len(name) == 0 || len(name) > 4 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
