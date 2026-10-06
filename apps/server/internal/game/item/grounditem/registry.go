/*
===========================================================================

registry.go - owns registry behavior and its checked data boundaries

===========================================================================
*/
// Package grounditem holds the server-authoritative registry of items lying on
// the ground, plus the distance and approach rules that decide when a pickup
// may execute.
//
// Ground items belong to a division and one population lifetime. The neutral snapshot
// values in domain feed the authority store, which commits them in the same
// SQLite transaction as the character half of a drop or pickup. Gameplay
// lookups and bootstrap rows filter the owning population; maintenance
// publications use the object's admitted scene scope.
//
// The rules mirror the (retired) Node launcher-api's server.mjs (source
// deleted at the Go cutover).
package grounditem

import (
	"sort"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// GidBase is the bottom of the ground-item entity id band. Player entities
// live at 100000+ and NPCs at 200000+, so drops start above both.
const GidBase uint32 = domain.GroundItemGIDBase

// Ground-item lifetime: FIXTURE POLICY, *NOT* NATIVE TRUTH. The client binary
// carries no ground-item expiry to recover - CIItem_Draw has no lifetime or
// fade clock, and the 0x36AB handler only despawns when the SERVER says to.
// Retail's drop lifetime lives in the server binary, which this project does
// not have, so the duration below is INVENTED for the fixture and must never
// be cited as reversed truth. The sweep emits the same 0x36AB despawn the
// pickup path already uses, so the client needs no new case.
const (
	FixtureLifetime = 180 * time.Second
	SweepInterval   = 5 * time.Second
	// OwnerLifetime is native server truth: CGObjItem_UpdateDropOwnership
	// releases CGObjItem+0x150 after +0x158 reaches 30.0 seconds.
	OwnerLifetime = 30 * time.Second
)

// Item is one item lying on the ground.
/*
================
Item
================
*/
type Item struct {
	TradeOwner string
	Summon     *domain.CharacterCOS
	RecordID   uint64 // item record identity, separate from ground runtime Gid
	Population Population
	Gid        uint32
	RefObjID   uint32
	Codename   string
	// TypeFlags is the itemdata type-flag word; it selects the conditional
	// fields of the spawn row.
	TypeFlags uint16
	// GoldAmount is non-zero only for a gold heap.
	GoldAmount   uint32
	Plus         uint8
	VarianceBits uint64
	Durability   uint32
	// MagicOptions are the encoded magic-option u64 params the item's
	// CSOItem body carries (wire.ItemBody.MagicOptions): server state
	// so a pickup restores them onto the granted row - the ground spawn
	// wire itself never carries them.
	MagicOptions      []uint64
	TransformRefObjID uint32 // monster capsule Data
	// StackCount is the heap's count for a stackable ETC drop. It never
	// reaches the CIItem spawn wire - the native ground label carries no
	// count for non-gold items - but it is server state so a pickup can
	// restore the stack, and an over-cap pickup writes the remainder back.
	// Zero reads as 1.
	StackCount uint16
	Position   Point
	Y          float32
	Heading    uint16
	// OwnerJID is the temporary native pickup reservation. Zero is public.
	OwnerJID uint32
	// DroppedBy records the character that dropped or earned it.
	DroppedBy string
	// DroppedAt is when the drop hit the ground; the fixture-policy expiry
	// sweep keys off it. The zero value means "never expires", matching the
	// fixture, which skips entries without a timestamp.
	DroppedAt time.Time
}

// IsGold reports whether this entry is a gold heap rather than an item.
/*
================
IsGold
================
*/
func (i Item) IsGold() bool {
	return i.GoldAmount > 0
}

// SpawnRow builds the CIItem spawn row for this entry. withAppearTail selects
// the single-object 0x30D7 form, which a fresh drop uses; object-list chunks
// omit the tail.
/*
================
SpawnRow
================
*/
func (i Item) SpawnRow(withAppearTail bool) wire.GroundItemRow {
	row := wire.GroundItemRow{
		RefObjID:   i.RefObjID,
		TypeFlags:  i.TypeFlags,
		Codename:   i.Codename,
		GoldAmount: i.GoldAmount,
		Gid:        i.Gid,
		Position: wire.Position{
			RegionID: i.Position.RegionID,
			X:        i.Position.X,
			Y:        i.Y,
			Z:        i.Position.Z,
			Heading:  i.Heading,
		},
		WithAppearTail: withAppearTail,
	}
	if i.OwnerJID != 0 {
		row.HasOwner = 1
		row.OwnerJID = i.OwnerJID
	}
	if withAppearTail {
		row.AppearFlag = 1
	}
	return row
}

// Registry holds the ground items of every division.
//
// It is safe for concurrent use: the gateway serves each session on its own
// goroutine, and a division's drops are shared across all of them.
//
// The registry owns the live in-memory lifecycle only. Mutation sites run
// registry calls inside the store commit door; the store reads Snapshot
// under the documented lock order and writes the character and ground rows
// in one transaction. There is no registry-local write path.
/*
================
Registry
================
*/
type Registry struct {
	mu         sync.Mutex
	byDivision map[string]map[uint32]Item
	counter    uint32
	// revision counts content mutations (Add/Remove/SetStackCount/
	// ExpireItems/Clear/Restore). The authority store's commit door reads
	// it to skip re-persisting ground rows on commits that never touched
	// the ground plane.
	revision uint64
}

// Revision reports the mutation counter. Two equal reads with no
// mutation in between guarantee identical Snapshot content.
/*
================
Revision
================
*/
func (r *Registry) Revision() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.revision
}

// NewRegistry returns an empty Registry.
/*
================
NewRegistry
================
*/
func NewRegistry() *Registry {
	return &Registry{byDivision: make(map[string]map[uint32]Item)}
}

// division returns the division's map, creating it on first use. Callers must
// hold the lock.
/*
================
division
================
*/
func (r *Registry) division(divisionID string) map[uint32]Item {
	items, ok := r.byDivision[divisionID]
	if !ok {
		items = make(map[uint32]Item)
		r.byDivision[divisionID] = items
	}
	return items
}

// Add registers a drop, assigns it the next entity id and returns the stored
// entry. Any Gid already set on item is ignored - the registry is the sole
// allocator, so two concurrent drops cannot collide.
/*
================
Add
================
*/
func (r *Registry) Add(divisionID string, item Item) Item {
	if !item.Population.Valid() {
		return Item{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.counter >= domain.MaxGroundItemGIDCounter {
		return Item{}
	}
	r.counter++
	r.revision++
	item.Gid = GidBase + r.counter
	item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
	item.Summon = domain.CloneCOS(item.Summon)
	r.division(divisionID)[item.Gid] = item
	item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
	item.Summon = domain.CloneCOS(item.Summon)
	return item
}

// Get returns the entry with the given entity id.
/*
================
Get
================
*/
func (r *Registry) Get(divisionID string, gid uint32) (Item, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	item, ok := r.byDivision[divisionID][gid]
	item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
	item.Summon = domain.CloneCOS(item.Summon)
	return item, ok
}

// Remove takes the entry out of the registry and returns it.
//
// The second result is false when the entry was already gone, which is the
// signal to answer the native "cannot be picked" notice: two players racing
// for the same drop both reach here, and only one gets true.
/*
================
Remove
================
*/
func (r *Registry) Remove(divisionID string, gid uint32) (Item, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items, ok := r.byDivision[divisionID]
	if !ok {
		return Item{}, false
	}
	item, ok := items[gid]
	if !ok {
		return Item{}, false
	}
	delete(items, gid)
	r.revision++
	return item, true
}

// SetStackCount rewrites a live entry's stack count in place, which is how an
// over-cap pickup leaves the remainder on the ground: the heap keeps its gid
// and its rendered entity, so no despawn or respawn is involved. The result
// is false when the entry is gone.
/*
================
SetStackCount
================
*/
func (r *Registry) SetStackCount(divisionID string, gid uint32, count uint16) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	items, ok := r.byDivision[divisionID]
	if !ok {
		return false
	}
	item, ok := items[gid]
	if !ok {
		return false
	}
	item.StackCount = count
	items[gid] = item
	r.revision++
	return true
}

// ExpireItems removes and returns the division's entries whose fixture
// lifetime has elapsed: DroppedAt is set and now-DroppedAt >= lifetime. The
// caller emits one 0x36AB despawn per returned entry, exactly like the pickup
// path. Entries without a timestamp never expire. Results are ordered by
// entity id so the despawn burst is deterministic.
/*
================
ExpireItems
================
*/
func (r *Registry) ExpireItems(divisionID string, now time.Time, lifetime time.Duration) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := r.byDivision[divisionID]
	var expired []Item
	for gid, item := range items {
		if item.DroppedAt.IsZero() || now.Sub(item.DroppedAt) < lifetime {
			continue
		}
		expired = append(expired, item)
		delete(items, gid)
	}
	if len(expired) > 0 {
		r.revision++
	}
	sort.Slice(expired, func(a, b int) bool { return expired[a].Gid < expired[b].Gid })
	return expired
}

// ReleaseExpiredOwners makes reserved drops public at the native 30-second
// boundary without replacing their entity identity. The caller broadcasts
// 0x31E2 for each returned row; subsequent calls return nothing.
/*
================
ReleaseExpiredOwners
================
*/
func (r *Registry) ReleaseExpiredOwners(divisionID string, now time.Time, lifetime time.Duration) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := r.byDivision[divisionID]
	var released []Item
	for gid, item := range items {
		if item.OwnerJID == 0 || item.DroppedAt.IsZero() || now.Sub(item.DroppedAt) < lifetime {
			continue
		}
		item.OwnerJID = 0
		items[gid] = item
		item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
		item.Summon = domain.CloneCOS(item.Summon)
		released = append(released, item)
	}
	if len(released) > 0 {
		r.revision++
	}
	sort.Slice(released, func(a, b int) bool { return released[a].Gid < released[b].Gid })
	return released
}

// DivisionIDs returns every division that currently holds entries, sorted,
// so the sweep can walk them deterministically.
/*
================
DivisionIDs
================
*/
func (r *Registry) DivisionIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.byDivision))
	for divisionID, items := range r.byDivision {
		if len(items) == 0 {
			continue
		}
		out = append(out, divisionID)
	}
	sort.Strings(out)
	return out
}

// All returns the division's entries ordered by entity id, so an object list
// built from them is deterministic.
/*
================
All
================
*/
func (r *Registry) All(divisionID string) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := r.byDivision[divisionID]
	out := make([]Item, 0, len(items))
	for _, item := range items {
		item.MagicOptions = append([]uint64(nil), item.MagicOptions...)
		item.Summon = domain.CloneCOS(item.Summon)
		out = append(out, item)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Gid < out[b].Gid })
	return out
}

// Count reports how many entries a division holds.
/*
================
Count
================
*/
func (r *Registry) Count(divisionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.byDivision[divisionID])
}

// Clear drops every entry of a division.
/*
================
Clear
================
*/
func (r *Registry) Clear(divisionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if items, ok := r.byDivision[divisionID]; ok && len(items) > 0 {
		r.revision++
	}
	delete(r.byDivision, divisionID)
}

// PendingKey is the key a pending pickup approach is tracked under. The
// character name is folded to lower case because the fixture matches character
// names case-insensitively.
/*
================
PendingKey
================
*/
func PendingKey(divisionID, characterName string) string {
	return divisionID + ":" + strings.ToLower(characterName)
}
