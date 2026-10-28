/*
===========================================================================

projectioncache.go - each character's learned passive projections, computed once per skill and equipment content

PlayerStatsWithModifiers runs the three learned projections (passives,
status resistance, damage return) on every snapshot: per hit, per effect
refresh, per recovery tick. The trio is a pure function of the character's
skill list, the skill source, the equipment the reqi walks read and the
item source those rows resolve through, so one character's answer is
computed once and shared until any of that content moves.

The cache is keyed on CONTENT, like rankcache.go: any writer of the skill
list or the equipment, in any package, misses on its next read. No writer
has to remember to invalidate anything.

Results handed out are shared and read-only: the keeper batch only reads
the writes it is given, and the other results are values. The cache is
safe for concurrent use; a miss computes outside the lock.

===========================================================================
*/
package combat

import (
	"container/list"
	"fmt"
	"slices"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/paramkeeper"
)

// projectionCacheMax bounds the cache; the least recently used entry
// leaves first. Production peaks at a few dozen characters.
const projectionCacheMax = 2048

/*
================
learnedProjection

One stats call's three learned projections together: the keeper writes
and the learned skill-parameter dictionary (passives.go), the
status-resistance buckets (statusresistance.go) and the damage return
(damagereturn.go).
================
*/
type learnedProjection struct {
	writes            []paramkeeper.Write
	power             enterworld.SkillParameterValues
	resistance        [17]abnormal.Resistance
	damageReturn      enterworld.SkillDamageReturn
	damageReturnSkill uint32
	err               error
}

/*
================
learnedProjections

The projection of c's learned passives, status resistances and damage
return. A character with no learned skills projects nothing; a missing
skill source refuses, as learnedPassives always has.
================
*/
func learnedProjections(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) learnedProjection {
	if len(c.Skills) == 0 {
		return learnedProjection{}
	}
	if skills == nil {
		return learnedProjection{err: fmt.Errorf("combat: learned skills require skill references")}
	}
	return learnedProjectionsCached(c, skills, items, weaponKind)
}

/*
================
computeLearnedProjection

The uncached trio over the shared rank rows: exactly the calls
PlayerStatsWithModifiers made per snapshot before the cache.
================
*/
func computeLearnedProjection(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) learnedProjection {
	rows := learnedRankRowsCached(c, skills)
	writes, power, err := learnedPassivesFrom(c, rows, items, weaponKind)
	result := learnedProjection{writes: writes, power: power, err: err}
	if err == nil {
		result.resistance = learnedStatusResistanceFrom(c, rows, items)
		result.damageReturn, result.damageReturnSkill = learnedDamageReturnFrom(c, rows, items)
	}
	return result
}

/*
================
learnedProjectionsCached

The trio of c, shared and read-only. A character without an ID or a
source without an identity is computed fresh.
================
*/
func learnedProjectionsCached(c *domain.Character, skills enterworld.SkillDataSource, items enterworld.ItemRefSource, weaponKind uint8) learnedProjection {
	skillsSource, itemsSource := sourceIdentity(skills), sourceIdentity(items)
	if c.ID == 0 || skillsSource == 0 || itemsSource == 0 {
		return computeLearnedProjection(c, skills, items, weaponKind)
	}
	if result, ok := projections.lookup(c, skillsSource, itemsSource, weaponKind); ok {
		return result
	}
	// Computed outside the lock: two concurrent misses compute the same
	// content-keyed result, and the later insert wins harmlessly.
	result := computeLearnedProjection(c, skills, items, weaponKind)
	projections.insert(c, skillsSource, itemsSource, weaponKind, result)
	return result
}

/*
================
projectionEquipRow

The fields of one MissionInventory row the reqi walks can read: where it
sits, what it is, and whether its durability is spent. Nothing else
about a row reaches a projection.
================
*/
type projectionEquipRow struct {
	slot       int64
	codename   string
	durability int64
}

/*
================
projectionEntry

One character's projections: the content they were computed from, and
the result.
================
*/
type projectionEntry struct {
	id           int64
	skills       []uint32
	skillsSource uintptr
	itemsSource  uintptr
	equipment    []projectionEquipRow
	weaponKind   uint8
	result       learnedProjection
	at           *list.Element
}

/*
================
projectionCache
================
*/
type projectionCache struct {
	mu      sync.Mutex
	entries map[int64]*projectionEntry
	order   *list.List
}

var projections = projectionCache{entries: make(map[int64]*projectionEntry), order: list.New()}

/*
================
projectionCache.lookup
================
*/
func (cache *projectionCache) lookup(c *domain.Character, skillsSource, itemsSource uintptr, weaponKind uint8) (learnedProjection, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[c.ID]
	if !ok || entry.skillsSource != skillsSource || entry.itemsSource != itemsSource ||
		entry.weaponKind != weaponKind || !slices.Equal(entry.skills, c.Skills) ||
		!projectionEquipmentEqual(entry.equipment, c.MissionInventory) {
		return learnedProjection{}, false
	}
	cache.order.MoveToFront(entry.at)
	return entry.result, true
}

/*
================
projectionCache.insert
================
*/
func (cache *projectionCache) insert(c *domain.Character, skillsSource, itemsSource uintptr, weaponKind uint8, result learnedProjection) {
	// Only the equipment band is keyed: bag rows never reach a
	// projection, so a narrow equipment-only copy of the same
	// character keys exactly like the full copy.
	equipment := make([]projectionEquipRow, 0, inventory.EquipmentSlotEnd)
	for _, row := range c.MissionInventory {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		equipment = append(equipment, projectionEquipRow{slot: row.Slot, codename: row.Codename, durability: row.Durability})
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry, ok := cache.entries[c.ID]; ok {
		entry.skills = slices.Clone(c.Skills)
		entry.skillsSource, entry.itemsSource = skillsSource, itemsSource
		entry.equipment, entry.weaponKind, entry.result = equipment, weaponKind, result
		cache.order.MoveToFront(entry.at)
		return
	}
	entry := &projectionEntry{id: c.ID, skills: slices.Clone(c.Skills), skillsSource: skillsSource,
		itemsSource: itemsSource, equipment: equipment, weaponKind: weaponKind, result: result}
	entry.at = cache.order.PushFront(entry)
	cache.entries[c.ID] = entry
	for cache.order.Len() > projectionCacheMax {
		oldest := cache.order.Back()
		cache.order.Remove(oldest)
		delete(cache.entries, oldest.Value.(*projectionEntry).id)
	}
}

/*
================
projectionEquipmentEqual

Whether a stored equipment key still describes these rows, field by
field, in order. Only the equipment band is keyed, so a bag change on
a full copy and an equipment-only narrow copy of the same character
both keep the entry.
================
*/
func projectionEquipmentEqual(key []projectionEquipRow, rows []domain.InventoryRow) bool {
	i := 0
	for _, row := range rows {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		if i >= len(key) || key[i].slot != row.Slot || key[i].codename != row.Codename || key[i].durability != row.Durability {
			return false
		}
		i++
	}
	return i == len(key)
}

/*
================
ForgetLearnedProjections

Drops a character's cached projections when its session ends.
Correctness never depends on it (the cache is content-keyed); it returns
the memory early.
================
*/
func ForgetLearnedProjections(id int64) {
	projections.mu.Lock()
	defer projections.mu.Unlock()
	if entry, ok := projections.entries[id]; ok {
		projections.order.Remove(entry.at)
		delete(projections.entries, id)
	}
}
