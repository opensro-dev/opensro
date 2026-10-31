/*
===========================================================================

statscache.go - each character's evaluated stats, shared while every input stands still

PlayerStatsWithModifiers rebuilds the whole keeper projection on every
call: per hit, per effect refresh, per recovery tick, usually over a
snapshot whose stat inputs did not move since the last call (live
profile, #1943: 0.43 GB/min cumulative). The result is a pure function
of the snapshot's stat content, the four reference sources, the effect
writes and the abnormal block, so one character's answer is computed
once and shared until any of that content moves.

The cache is keyed on CONTENT, like rankcache.go and
projectioncache.go: any writer of the character, the effects or the
block, in any package, misses on its next read. No writer has to
remember to invalidate anything.

Results handed out are shared and read-only: Stats is returned by
value, its keeper graph is never mutated after the build
(paramkeeper's Value is a pure read), and its remaining reference
fields are read-only snapshots, the same posture projectioncache.go
takes with the learned power dictionary. Callers keep setting scalar
fields on their own copy, as effectstats.go does with AbnormalMask.
The cache is safe for concurrent use; a miss computes outside the lock.

===========================================================================
*/
package combat

import (
	"container/list"
	"slices"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/paramkeeper"
)

// statsCacheMax bounds the memo; the least recently used entry leaves
// first. Production peaks at a few dozen characters.
const statsCacheMax = 2048

/*
================
statsCore

The scalar half of one memo key: the reference sources the projection
resolves through, and the snapshot scalars playerSnapshotCore and the
stats head read. Comparable, so a lookup compares it whole.
================
*/
type statsCore struct {
	itemsSource  uintptr
	skillsSource uintptr
	magicSource  uintptr
	setsSource   uintptr
	level        int64
	maxLevel     int64
	strength     int64
	intellect    int64
	bodyStatus   uint8
}

/*
================
statsMemoCore

The scalar key of a stats call, and whether the call may memoise at
all. A character without an ID, a core pointer the projection would
refuse, or a reference source without an identity is computed fresh,
exactly as the projection caches treat their sources.
================
*/
func statsMemoCore(character *domain.Character, catalogs Catalogs) (statsCore, bool) {
	var core statsCore
	if character == nil || character.ID == 0 {
		return core, false
	}
	core.itemsSource = sourceIdentity(catalogs.Items)
	core.skillsSource = sourceIdentity(catalogs.Skills)
	core.magicSource = sourceIdentity(catalogs.MagicOptions)
	if catalogs.Sets != nil {
		core.setsSource = sourceIdentity(catalogs.Sets)
		if core.setsSource == 0 {
			return core, false
		}
	}
	if core.itemsSource == 0 || core.skillsSource == 0 || core.magicSource == 0 {
		return core, false
	}
	if character.Level == nil || character.Strength == nil || character.Intellect == nil {
		return core, false
	}
	core.level = *character.Level
	core.maxLevel = core.level
	if character.MaxLevel != nil {
		core.maxLevel = *character.MaxLevel
	}
	core.strength = *character.Strength
	core.intellect = *character.Intellect
	core.bodyStatus = character.NativeBodyStatus
	return core, true
}

/*
================
statsEquipRow

The fields of one equipped row the projection reads: identity, plus,
variance, durability and its magic options. The stack count is not
keyed: ammunition contributes no stats, and its count moves per shot.
================
*/
type statsEquipRow struct {
	slot         int64
	refObjID     uint32
	codename     string
	typeFlags    uint16
	plus         int64
	varianceBits string
	durability   int64
	magicOptions []uint64
}

/*
================
statsAvatarRow

The fields of one avatar row the projection reads (avataroptions.go):
what it is, its magic options, and its socket, which names the keeper
source of the row's writes.
================
*/
type statsAvatarRow struct {
	slot         int64
	refObjID     uint32
	codename     string
	magicOptions []uint64
}

/*
================
statsEntry

One character's memoised stats: the content they were computed from,
and the result.
================
*/
type statsEntry struct {
	id        int64
	core      statsCore
	skills    []uint32
	masteries []domain.CharacterMastery
	equipment []statsEquipRow
	avatar    []statsAvatarRow
	modifiers []paramkeeper.Write
	hasBlock  bool
	block     [abnormal.MaxModifiers]abnormal.Modifier
	stats     Stats
	loadout   Loadout
	err       error
	at        *list.Element
}

/*
================
statsCache
================
*/
type statsCache struct {
	mu      sync.Mutex
	entries map[int64]*statsEntry
	order   *list.List
}

var playerStatsMemo = statsCache{entries: make(map[int64]*statsEntry), order: list.New()}

/*
================
statsCache.lookup

The memoised stats of an identical call. The comparison walks the
snapshot in place and allocates nothing.
================
*/
func (cache *statsCache) lookup(
	character *domain.Character,
	catalogs Catalogs,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
) (Stats, Loadout, error, bool) {
	core, ok := statsMemoCore(character, catalogs)
	if !ok {
		return Stats{}, Loadout{}, nil, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[character.ID]
	if !ok || entry.core != core ||
		!slices.Equal(entry.skills, character.Skills) ||
		!slices.Equal(entry.masteries, character.Masteries) ||
		!statsEquipmentEqual(entry.equipment, character.MissionInventory) ||
		!statsAvatarEqual(entry.avatar, character.AvatarInventory) ||
		!slices.Equal(entry.modifiers, modifiers) ||
		entry.hasBlock != (block != nil) ||
		(block != nil && entry.block != block.Modifiers) {
		return Stats{}, Loadout{}, nil, false
	}
	cache.order.MoveToFront(entry.at)
	return entry.stats, entry.loadout, entry.err, true
}

/*
================
statsCache.insert

Memoise one computed result under the content it came from. The key
slices are cloned: the caller's snapshot and writes stay theirs.
================
*/
func (cache *statsCache) insert(
	character *domain.Character,
	catalogs Catalogs,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
	stats Stats,
	loadout Loadout,
	err error,
) {
	core, ok := statsMemoCore(character, catalogs)
	if !ok {
		return
	}
	entry := &statsEntry{
		id:        character.ID,
		core:      core,
		skills:    slices.Clone(character.Skills),
		masteries: slices.Clone(character.Masteries),
		equipment: statsEquipmentKey(character.MissionInventory),
		avatar:    statsAvatarKey(character.AvatarInventory),
		modifiers: slices.Clone(modifiers),
		hasBlock:  block != nil,
		stats:     stats,
		loadout:   loadout,
		err:       err,
	}
	if block != nil {
		entry.block = block.Modifiers
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if old, ok := cache.entries[character.ID]; ok {
		at := old.at
		*old = *entry
		old.at = at
		cache.order.MoveToFront(old.at)
		return
	}
	entry.at = cache.order.PushFront(entry)
	cache.entries[character.ID] = entry
	for cache.order.Len() > statsCacheMax {
		oldest := cache.order.Back()
		cache.order.Remove(oldest)
		delete(cache.entries, oldest.Value.(*statsEntry).id)
	}
}

/*
================
statsEquipmentKey

The equipment band of these rows as a memo key: slot order, minus the
stack count.
================
*/
func statsEquipmentKey(rows []domain.InventoryRow) []statsEquipRow {
	key := make([]statsEquipRow, 0, inventory.EquipmentSlotEnd)
	for _, row := range rows {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		key = append(key, statsEquipRow{
			slot:         row.Slot,
			refObjID:     row.RefObjID,
			codename:     row.Codename,
			typeFlags:    row.TypeFlags,
			plus:         row.Plus,
			varianceBits: row.VarianceBits,
			durability:   row.Durability,
			magicOptions: slices.Clone(row.MagicOptions),
		})
	}
	return key
}

/*
================
statsEquipmentEqual

Whether a stored equipment key still describes these rows, field by
field, in order. Only the equipment band is keyed, so a bag change on
the same character keeps the entry.
================
*/
func statsEquipmentEqual(key []statsEquipRow, rows []domain.InventoryRow) bool {
	i := 0
	for _, row := range rows {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		if i >= len(key) {
			return false
		}
		k := key[i]
		if k.slot != row.Slot || k.refObjID != row.RefObjID || k.codename != row.Codename ||
			k.typeFlags != row.TypeFlags || k.plus != row.Plus || k.varianceBits != row.VarianceBits ||
			k.durability != row.Durability || !slices.Equal(k.magicOptions, row.MagicOptions) {
			return false
		}
		i++
	}
	return i == len(key)
}

/*
================
statsAvatarKey

The avatar rows as a memo key.
================
*/
func statsAvatarKey(avatar *domain.AvatarInventory) []statsAvatarRow {
	if avatar == nil {
		return nil
	}
	key := make([]statsAvatarRow, 0, len(avatar.Rows))
	for _, row := range avatar.Rows {
		key = append(key, statsAvatarRow{
			slot:         row.Slot,
			refObjID:     row.RefObjID,
			codename:     row.Codename,
			magicOptions: slices.Clone(row.MagicOptions),
		})
	}
	return key
}

/*
================
statsAvatarEqual

Whether a stored avatar key still describes this avatar inventory.
================
*/
func statsAvatarEqual(key []statsAvatarRow, avatar *domain.AvatarInventory) bool {
	if avatar == nil {
		return len(key) == 0
	}
	if len(key) != len(avatar.Rows) {
		return false
	}
	for i, row := range avatar.Rows {
		k := key[i]
		if k.slot != row.Slot || k.refObjID != row.RefObjID || k.codename != row.Codename ||
			!slices.Equal(k.magicOptions, row.MagicOptions) {
			return false
		}
	}
	return true
}

/*
================
ForgetPlayerStatsMemo

Drops a character's memoised stats when its session ends. Correctness
never depends on it (the memo is content-keyed); it returns the memory
early.
================
*/
func ForgetPlayerStatsMemo(id int64) {
	playerStatsMemo.mu.Lock()
	defer playerStatsMemo.mu.Unlock()
	if entry, ok := playerStatsMemo.entries[id]; ok {
		playerStatsMemo.order.Remove(entry.at)
		delete(playerStatsMemo.entries, id)
	}
}
