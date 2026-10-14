/*
===========================================================================

main.go - sro-swap-sun: trade named characters' degree-9 Sun gear for Moon

An operator's one-off maintenance tool (owner decision 2026-10-14): with
SRO_SUN_EVENT_ONLY degree-9 Seal of Sun gear became event-only, and the
balance advisor's own characters give theirs back for the matching Moon.
It is not a native mechanic.

Run it in the maintenance window, with the GameWorld stopped and the store
backed up: store.Open takes the authority's writer lock, so a running
GameWorld makes it refuse. It previews by default and writes only with
-apply. Every container the named characters reach is covered (bag and
equipped, avatars, companions, account storage, their guild's storage).

A Sun row keeps its slot, plus, variance word (the white-stat rolls),
magic options, stack and record id; only its reference changes, to the
Moon of the same item (codename _C_RARE -> _B_RARE, checked to be the
degree-9 Moon of the same type IDs). Whites are fractions of the
reference's ranges, so they keep their percentages and follow the Moon's
numbers, printed before and after. Current durability is clamped to the
Moon's maximum at that variance and those options, never raised. A row
that maps to nothing refuses the whole run.

===========================================================================
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/gamedata"
)

// sunSuffix and moonSuffix are the seal tiers' codename markers (A Star,
// B Moon, C Sun); an honor package's item adds _HONOR after them.
const (
	sunSuffix  = "_C_RARE"
	moonSuffix = "_B_RARE"
)

/*
================
swapper

The reference sources a swap reads, and what it reports per row.
================
*/
type swapper struct {
	items enterworld.ItemRefSource
	magic enterworld.MagicOptionSource
	notes []string
}

/*
================
nativeInt
================
*/
func nativeInt(ref *enterworld.ItemRef, field string) int {
	value, _ := ref.NativeFields.Lookup(field)
	return int(value)
}

/*
================
swapper.moonOf

The Moon reference a Sun reference trades for, or an error naming why
there is none.
================
*/
func (w *swapper) moonOf(sun *enterworld.ItemRef) (*enterworld.ItemRef, error) {
	if !strings.Contains(sun.Codename, sunSuffix) {
		return nil, fmt.Errorf("no %s in the codename", sunSuffix)
	}
	name := strings.Replace(sun.Codename, sunSuffix, moonSuffix, 1)
	moon, ok := w.items.ItemRefByCodename(name)
	if !ok || moon == nil {
		return nil, fmt.Errorf("no item %s", name)
	}
	if moon.TypeIDs != sun.TypeIDs || !progression.IsEventOnlyMoon(moon.TypeIDs, nativeInt(moon, "rarity"), nativeInt(moon, "itemClass")) {
		return nil, fmt.Errorf("%s is not the degree-9 Moon of the same type", name)
	}
	return moon, nil
}

/*
================
whites

A row's white stats as one line, for the before/after report.
================
*/
func whites(ref *enterworld.ItemRef, row domain.InventoryRow) string {
	bits, err := strconv.ParseUint(row.VarianceBits, 10, 64)
	if err != nil && row.VarianceBits != "" {
		return "variance " + row.VarianceBits + " unreadable"
	}
	s, err := combat.ItemWhiteStats(ref, bits, uint8(row.Plus))
	if err != nil {
		return err.Error()
	}
	var parts []string
	add := func(name string, value float64) {
		if value != 0 {
			parts = append(parts, fmt.Sprintf("%s %g", name, value))
		}
	}
	if s.PhysicalAttackMax != 0 {
		parts = append(parts, fmt.Sprintf("phy atk %g-%g", s.PhysicalAttackMin, s.PhysicalAttackMax))
	}
	if s.MagicalAttackMax != 0 {
		parts = append(parts, fmt.Sprintf("mag atk %g-%g", s.MagicalAttackMin, s.MagicalAttackMax))
	}
	add("hit", s.HitRate)
	add("crit", s.CriticalRate)
	add("block", s.BlockRate)
	add("phy def", s.PhysicalDefense)
	add("mag def", s.MagicalDefense)
	add("evasion", s.EvasionRate)
	add("parry", s.ParryRate)
	add("mag parry", s.MagicalParry)
	return strings.Join(parts, ", ")
}

/*
================
swapper.swap

store.ItemSwapFunc: a degree-9 Sun row becomes its Moon; anything else is
left alone.
================
*/
func (w *swapper) swap(row domain.InventoryRow) (domain.InventoryRow, bool, error) {
	sun, ok := w.items.ItemRefByCodename(row.Codename)
	if !ok || sun == nil || !progression.IsEventOnlySun(sun.TypeIDs, nativeInt(sun, "rarity"), nativeInt(sun, "itemClass")) {
		return row, false, nil
	}
	moon, err := w.moonOf(sun)
	if err != nil {
		return row, false, err
	}
	next := row
	next.MagicOptions = append([]uint64(nil), row.MagicOptions...)
	next.RefObjID, next.Codename, next.TypeFlags = moon.RefObjID, moon.Codename, moon.TypeFlags()
	maximum, err := combat.EquipmentMaxDurability(moon, next, w.magic)
	if err != nil {
		return row, false, err
	}
	note := fmt.Sprintf("  %s +%d -> %s\n    whites before: %s\n    whites after:  %s",
		row.Codename, row.Plus, moon.Codename, whites(sun, row), whites(moon, next))
	if next.Durability > int64(maximum) {
		note += fmt.Sprintf("\n    durability clamped %d -> %d (Moon max)", next.Durability, maximum)
		next.Durability = int64(maximum)
	}
	w.notes = append(w.notes, note)
	return next, true, nil
}

/*
================
main
================
*/
func main() {
	dir := flag.String("authority-dir", "", "stopped authority directory (default: the shard's)")
	shard := flag.String("shard", "", "owning shard")
	names := flag.String("characters", "", "comma-separated exact character names")
	textdata := flag.String("textdata", "", "server textdata directory (default: the verified game-data projection)")
	apply := flag.Bool("apply", false, "write the swap (default: preview only)")
	flag.Parse()
	if flag.NArg() != 0 || *shard == "" || *names == "" {
		fail("-shard and -characters are required")
	}
	if *textdata == "" {
		paths, err := gamedata.Resolve()
		if err != nil {
			fail("server game data: %v", err)
		}
		*textdata = paths.TextdataDir
	}
	if *dir == "" {
		*dir = store.DirForShardFromEnv(*shard)
	}
	// store.Open's writer lock refuses while the GameWorld holds the store.
	s, err := store.Open(*dir, store.Options{RequireStore: true})
	if err != nil {
		fail("open authority (is the GameWorld stopped?): %v", err)
	}
	defer s.Close()
	w := &swapper{items: enterworld.NewTextdataItems(*textdata), magic: enterworld.NewTextdataMagicOptions(*textdata)}
	swaps, err := s.SwapItemsOffline(*shard, strings.Split(*names, ","), w.swap, *apply)
	if err != nil {
		fail("refused, nothing written: %v", err)
	}
	for i, swap := range swaps {
		fmt.Printf("%s %s slot %d\n%s\n", swap.Character, swap.Container, swap.Slot, w.notes[i])
	}
	switch {
	case len(swaps) == 0:
		fmt.Println("No degree-9 Sun gear found; nothing to do.")
	case *apply:
		fmt.Printf("SWAPPED %d items.\n", len(swaps))
	default:
		fmt.Printf("Preview only: %d items would change; use -apply to write.\n", len(swaps))
	}
}

/*
================
fail
================
*/
func fail(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
