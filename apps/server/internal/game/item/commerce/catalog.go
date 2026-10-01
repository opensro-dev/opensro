/*
===========================================================================

catalog.go - authored gold merchandise admission and package definitions

NPC and mall catalogues share template decoding. Their independent currency
and purchase policies remain in the respective catalogue owners.

===========================================================================
*/
package commerce

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"os"
	"path/filepath"
	"strconv"
)

/*
================
Content
================
*/
type Content struct {
	Ref      *enterworld.ItemRef
	Stack    uint16
	Plus     uint8
	Variance uint64
	Data     uint32
	Magic    []uint64
}

/*
================
Offer
================
*/
type Offer struct {
	Slot     uint8
	Ref      *enterworld.ItemRef
	Price    uint64
	Stack    uint16
	Contents []Content
}

/*
================
Catalog
================
*/
type Catalog struct {
	Tabs  map[int32][]Offer
	Magic enterworld.MagicOptionSource
}

// Load admits authored gold packages and preserves every item template.
// Conditional and multi-currency policies still require their own authorities.
/*
================
Load
================
*/
func Load(dir string, refs enterworld.ItemRefSource) (*Catalog, error) {
	tables := map[string][][]string{}
	for _, name := range []string{"refshoptab", "refshopgoods", "refscrapofpackageitem", "refpricepolicyofitem", "refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		path := filepath.Join(dir, name+".txt")
		rows := enterworld.ReadTextdataFile(path)
		info, err := os.Stat(path)
		if err != nil || rows == nil && info.Size() > 2 {
			return nil, fmt.Errorf("commerce: missing %s", name)
		}
		tables[name] = rows
	}
	c := &Catalog{Tabs: map[int32][]Offer{}, Magic: enterworld.NewTextdataMagicOptions(dir)}
	tabIDs := map[string]int32{}
	for _, r := range tables["refshoptab"] {
		if len(r) >= 6 && r[0] == "1" {
			n, e := strconv.ParseInt(r[2], 10, 32)
			if e != nil {
				return nil, e
			}
			tabIDs[r[3]] = int32(n)
		}
	}
	blocked := map[string]bool{}
	for _, name := range []string{"refconditiontosellpackageitem", "refrewardpolicytosellpackageitem"} {
		for _, r := range tables[name] {
			if len(r) > 2 && r[0] == "1" {
				blocked[r[2]] = true
			}
		}
	}
	prices := map[string]uint64{}
	for _, r := range tables["refpricepolicyofitem"] {
		if len(r) < 5 || r[0] != "1" {
			continue
		}
		n, e := strconv.ParseUint(r[4], 10, 32)
		if e != nil || r[3] != "1" || n == 0 || prices[r[2]] != 0 {
			blocked[r[2]] = true
		}
		prices[r[2]] = n
	}
	scraps := map[string][][]string{}
	for _, r := range tables["refscrapofpackageitem"] {
		if len(r) < 20 || r[0] != "1" {
			continue
		}
		scraps[r[2]] = append(scraps[r[2]], r)
	}
	seen := map[string]bool{}
	for _, r := range tables["refshopgoods"] {
		if len(r) < 5 || r[0] != "1" {
			continue
		}
		tab, ok := tabIDs[r[2]]
		if !ok {
			return nil, fmt.Errorf("commerce: missing tab %s", r[2])
		}
		slot, e := strconv.ParseUint(r[4], 10, 8)
		if e != nil {
			return nil, e
		}
		key := r[2] + ":" + r[4]
		if seen[key] {
			return nil, fmt.Errorf("commerce: duplicate slot %s", key)
		}
		seen[key] = true
		rows := scraps[r[3]]
		if blocked[r[3]] || len(rows) == 0 || prices[r[3]] == 0 || refs == nil {
			continue
		}
		contents, err := packageContents(rows, refs)
		if err != nil {
			continue
		}
		admitted := true
		for _, content := range contents {
			ref := content.Ref
			if (ref.TypeIDs[1] != 1 && ref.TypeIDs[1] != 3) || ref.TypeIDs[1] == 3 && (ref.TypeIDs[2] == 5 || ref.TypeIDs[2] == 8) {
				admitted = false
				break
			}
		}
		if !admitted {
			continue
		}
		first := contents[0]
		c.Tabs[tab] = append(c.Tabs[tab], Offer{Slot: uint8(slot), Ref: first.Ref, Price: prices[r[3]], Stack: first.Stack, Contents: contents})

	}
	return c, nil
}
