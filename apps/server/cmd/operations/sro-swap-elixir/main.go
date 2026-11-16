/*
===========================================================================

main.go - sro-swap-elixir: every plain elixir on the shard becomes the Intensifying one

An operator's one-off maintenance tool (owner 2026-11-16, #general: "one
elixir"). The game now hands out only the Intensifying (B) elixir of each
kind (opensro#3645), and while SRO_ALCHEMY_RATES is on both roll the same
table, so the plain (A) rows players still hold are traded for B one for
one. It is not a native mechanic.

Run it in the maintenance window, with the GameWorld stopped and the store
backed up: store.Open takes the authority's writer lock, so a running
GameWorld makes it refuse. It previews by default and writes only with
-apply. The whole shard is covered (store.SwapItemsOfflineShard): every
character's bag, avatars and companions, every account's warehouse,
every guild's storage, and the ground: each item and a dropped pet's
container (a stopped shard keeps its ground_items and restores them at
boot). A stall is never stored, so it holds nothing to swap.

A row keeps its slot, stack, trade owner and record id; only its reference
changes, to the B elixir of the same kind (codename _A -> _B, checked to
share the type IDs and to hold at least as many per slot). A row that cannot map refuses
the whole run, so nothing is written.

===========================================================================
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/gamedata"
)

// The four elixir kinds share one codename family; A is plain, B Intensifying.
const (
	elixirPrefix = "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_"
	plainSuffix  = "_A"
	strongSuffix = "_B"
)

/*
================
swapper

The item references a swap reads, and the count it changes per kind.
================
*/
type swapper struct {
	items  enterworld.ItemRefSource
	counts map[string]int64
}

/*
================
isPlainElixir
================
*/
func isPlainElixir(codename string) bool {
	return strings.HasPrefix(codename, elixirPrefix) && strings.HasSuffix(codename, plainSuffix)
}

/*
================
nativeMaxStack
================
*/
func nativeMaxStack(ref *enterworld.ItemRef) float64 {
	value, _ := ref.NativeFields.Lookup("maxStack")
	return value
}

/*
================
swapper.intensifyingOf

The B reference an A reference trades for, or an error naming why there
is none.
================
*/
func (w *swapper) intensifyingOf(plain *enterworld.ItemRef) (*enterworld.ItemRef, error) {
	name := strings.TrimSuffix(plain.Codename, plainSuffix) + strongSuffix
	strong, ok := w.items.ItemRefByCodename(name)
	if !ok || strong == nil {
		return nil, fmt.Errorf("no item %s", name)
	}
	if strong.TypeIDs != plain.TypeIDs {
		return nil, fmt.Errorf("%s is not the same kind of item", name)
	}
	// Both rows share the shard's stack rule (SRO_STACK_SIZES goes by kind),
	// so a B row that natively holds at least what the A row holds keeps
	// every stack a player could build valid.
	if nativeMaxStack(strong) < nativeMaxStack(plain) {
		return nil, fmt.Errorf("%s holds fewer per slot than %s", name, plain.Codename)
	}
	return strong, nil
}

/*
================
swapper.swap

store.ItemSwapFunc: a plain elixir row becomes the Intensifying one;
anything else is left alone.
================
*/
func (w *swapper) swap(row domain.InventoryRow) (domain.InventoryRow, bool, error) {
	if !isPlainElixir(row.Codename) {
		return row, false, nil
	}
	plain, ok := w.items.ItemRefByCodename(row.Codename)
	if !ok || plain == nil {
		return row, false, fmt.Errorf("no item %s", row.Codename)
	}
	strong, err := w.intensifyingOf(plain)
	if err != nil {
		return row, false, err
	}
	next := row
	next.RefObjID, next.Codename, next.TypeFlags = strong.RefObjID, strong.Codename, strong.TypeFlags()
	stack := row.StackCount
	if stack < 1 {
		stack = 1
	}
	w.counts[strong.Codename] += stack
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
	textdata := flag.String("textdata", "", "server textdata directory (default: the verified game-data projection)")
	apply := flag.Bool("apply", false, "write the swap (default: preview only)")
	flag.Parse()
	if flag.NArg() != 0 || *shard == "" {
		fail("-shard is required")
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
	w := &swapper{items: enterworld.NewTextdataItems(*textdata), counts: map[string]int64{}}
	swaps, err := s.SwapItemsOfflineShard(*shard, w.swap, *apply)
	if err != nil {
		fail("refused, nothing written: %v", err)
	}
	for _, swap := range swaps {
		fmt.Printf("%s %s slot %d: %s -> %s\n", swap.Character, swap.Container, swap.Slot, swap.From, swap.To)
	}
	kinds := make([]string, 0, len(w.counts))
	for kind := range w.counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		fmt.Printf("%s: %d elixirs\n", kind, w.counts[kind])
	}
	switch {
	case len(swaps) == 0:
		fmt.Println("No plain elixirs found; nothing to do.")
	case *apply:
		fmt.Printf("SWAPPED %d rows.\n", len(swaps))
	default:
		fmt.Printf("Preview only: %d rows would change; use -apply to write.\n", len(swaps))
	}
}

/*
================
fail
================
*/
func fail(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
