/*
===========================================================================

forge.go - a GM's /FORGE: forge any item, with any attributes, by name

Port-only, not native (owner decision 2026-11-07, issue #1980): the
v1.150 client composes no forge command - its GM console ends at
/VERIFY - so the port's client composes /FORGE onto the GM lane's
0xF2 subcommand and this owner executes it. A GM names an item
codename and any of the attributes that item's body can carry
(key=value; forgekeys.go derives the legal set from the item
reference and the alchemy catalogue, and /forge help <CODENAME>
prints exactly that set).

Safety, per the owner's ruling: the lane refuses unless
SRO_GM_ITEM_FORGE is enabled (off is the native default; test shards
enable it, live needs the owner's go); every forged item is born
bound (ForgeBound - it refuses trade, NPC sale, stall offers and
player drops; the doors are enforced where those lanes move items,
not here); the whole command validates before anything is
built, so an unknown key or an out-of-range value forges nothing;
and each GM is rate-limited, with every forge audit-logged with its
full key set.

===========================================================================
*/
package action

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvGMItemForge turns /FORGE on ("on", "1", "true"). Default off.
const EnvGMItemForge = "SRO_GM_ITEM_FORGE"

// One GM's /FORGE allowance: at most forgeRateMax forged items per
// rolling forgeRateWindow (help lookups are read-only and unmetered).
// The cap exists so a scripted GM client cannot mint ground drops
// faster than a person can test them.
const (
	forgeRateMax    = 20
	forgeRateWindow = time.Minute
)

// forgeRateWindowState is one GM's recent forge stamps.
type forgeRateWindowState struct {
	mu     sync.Mutex
	stamps []time.Time
}

/*
================
GMItemForgeFromEnv

The /FORGE switch (port-only, issue #1980): "on", "1" or "true"
enable; everything else - unset, "off", "0", "false", or a typo -
reads off. Off is the native direction, so a mistyped value fails
safe as a silent no-op and, unlike the raising companion switch
(PetInventoryPagesEnabled), needs no boot error.
================
*/
func GMItemForgeFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGMItemForge))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
forgeRateAllow

Record one forge against the GM's rolling window; false when the
window already holds forgeRateMax stamps inside forgeRateWindow.
================
*/
func (rt *Runtime) forgeRateAllow(name string, now time.Time) bool {
	state, _ := rt.forgeStamps.LoadOrStore(strings.ToLower(name), &forgeRateWindowState{})
	window := state.(*forgeRateWindowState)
	window.mu.Lock()
	defer window.mu.Unlock()
	cutoff := now.Add(-forgeRateWindow)
	kept := window.stamps[:0]
	for _, stamp := range window.stamps {
		if stamp.After(cutoff) {
			kept = append(kept, stamp)
		}
	}
	if len(kept) >= forgeRateMax {
		window.stamps = kept
		return false
	}
	window.stamps = append(kept, now)
	return true
}

/*
================
ForgeGMItem

Called by the authenticated GM dispatcher (gmcommand's SubForge arm)
with the typed tail: "CODENAME key=value ..." or "help CODENAME".
Returns the guide lines to answer (one for a forged item, one per key
for help) and whether the command was honored; a refusal answers the
lane's bare failure ack and forges nothing.

Privilege is rechecked inside the store door, as MakeGMItem does;
the drop is MakeGMItem's own door (gm-ground-drop posture) so the
forged item lands at the GM's feet for the pickup loop, bound from
birth so only its owner-protection pickup can ever take it.
================
*/
func (rt *Runtime) ForgeGMItem(division, name, text string) ([]string, bool) {
	if rt == nil || rt.deps == nil || rt.Ground == nil {
		return nil, false
	}
	if !rt.GMItemForge {
		return nil, false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return nil, false
	}
	source, ok := rt.deps.ItemReferences().(interface {
		ItemRefByCodename(string) (*enterworld.ItemRef, bool)
	})
	if !ok {
		return nil, false
	}
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return nil, false
	}
	if strings.EqualFold(tokens[0], "help") {
		if len(tokens) != 2 {
			return nil, false
		}
		ref, found := forgeItemRef(source, tokens[1])
		if !found {
			return nil, false
		}
		return forgeHelpLines(ref, rt.Alchemy, rt.maxStackFor(ref.TypeFlags(), ref.Codename)), true
	}
	ref, found := forgeItemRef(source, tokens[0])
	if !found {
		return nil, false
	}
	builder := rt.newForgeBuilder(ref)
	for _, token := range tokens[1:] {
		if !builder.apply(token) {
			log.Debugf("forge: %s refused (bad key): %s", c.Name, text)
			return nil, false
		}
	}
	item, built := builder.assemble(rt.Now())
	if !built {
		log.Debugf("forge: %s refused (spec): %s", c.Name, text)
		return nil, false
	}
	// The allowance meters forged items, not attempts: a refused spec
	// costs nothing, so the stamp lands only once the item is real.
	if !rt.forgeRateAllow(c.Name, rt.Now()) {
		log.Debugf("forge: %s refused (rate limit): %s", c.Name, text)
		return nil, false
	}
	var added grounditem.Item
	committed := rt.deps.Update(c, "gm-item-forge", func() bool {
		if c.DeletePending || !c.GMPrivilege {
			return false
		}
		now := rt.Now()
		at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now.UnixMilli())
		added = rt.addCharacterGround(division, c, PlanItemDrop(item, item.Quantity, at, c.Name, now))
		return added.Gid != 0
	})
	if !committed || added.Gid == 0 {
		return nil, false
	}
	frames := append(rt.groundReferences([]grounditem.Item{added}), wire.DropBroadcastFrames(added.SpawnRow(true))...)
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, frames)
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, frames)
	}
	log.Infof("forge: %s forged %s [%s]", c.Name, item.Codename, text)
	return []string{forgeSuccessLine(item)}, true
}

/*
================
forgeSuccessLine

The one-line answer for a forged item: what it is, its headline
attributes, and the bound mark every forged item carries.
================
*/
func forgeSuccessLine(item inventory.Item) string {
	line := "forged " + item.Codename
	if item.Plus > 0 {
		line += fmt.Sprintf(" +%d", item.Plus)
	}
	if item.Quantity > 1 {
		line += fmt.Sprintf(" x%d", item.Quantity)
	}
	return line + " (bound)"
}
