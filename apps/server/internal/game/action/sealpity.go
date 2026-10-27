/*
===========================================================================

sealpity.go - seal pity: luck-debt counters, the forced drop, the rollup

Port-only, not native (owner decision 2026-10-27, #1690; the settings
live in progression/sealpity.go). The loot planner (monsterloot.go)
ends each kill's plan here: for every tracked (degree, tier) the loot
owner's counter resets on its own admitted drop of the tier, or accrues
the kill's exact seal chance (loot.SealKillChance) times the kill's
level-gap admission chance. At the threshold the next eligible kill
forces a real drop of the tier. The plan returns the counter delta;
applyMonsterKillInsideDoor commits it in the same store door as the
loot, so a crash cannot persist one half of a kill.

Shadow mode counts and resets exactly as on but adds no item and draws
no roll, so its loot is byte-identical to off. GM-spawned monsters and
the GM kill path never move a counter (the seal board's exclusions).

===========================================================================
*/
package action

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// sealPityDegrees/sealPityTiers span the counters: degrees 1..9 and
// Star/Moon/Sun (enterworld.SealStar..SealSun).
const (
	sealPityDegrees = 9
	sealPityTiers   = 3
)

// sealPityTierNames names a tier for the log.
var sealPityTierNames = [sealPityTiers]string{"Star", "Moon", "Sun"}

/*
================
sealPityItemClass

The itemdata class of a (degree, tier): three classes make a degree and
(ItemClass-1)%3 is the seal (enterworld/itemgrade.go).
================
*/
func sealPityItemClass(degree, tier int) int {
	return (degree-1)*sealPityTiers + tier + 1
}

/*
================
sealPityDegreeTier

The (degree, tier) of an itemdata class; class 0 names neither.
================
*/
func sealPityDegreeTier(itemClass int) (degree, tier int) {
	if itemClass < 1 {
		return 0, 0
	}
	return (itemClass-1)/sealPityTiers + 1, (itemClass - 1) % sealPityTiers
}

/*
================
sealPityCounterDelta

One counter's settled state after a kill: the absolute debt and the
forced-day counters the commit writes. Forced marks a forced drop (in
shadow, the drop that would have been forced).
================
*/
type sealPityCounterDelta struct {
	itemClass   int
	debt        float64
	forcedDay   int64
	forcedToday int
	forced      bool
}

/*
================
sealPityRollupNote

One tracked (degree, tier)'s share of a kill for the daily rollup:
debt accrued, a natural drop, a forced drop, a daily-cap refusal.
================
*/
type sealPityRollupNote struct {
	degree  int
	tier    int
	accrued float64
	natural bool
	forced  bool
	capHit  bool
}

/*
================
sealPityDelta

What one kill's plan owes the commit: the touched counters and the
rollup notes. The zero value (off, GM-spawned, nothing tracked) commits
nothing.
================
*/
type sealPityDelta struct {
	counters []sealPityCounterDelta
	notes    []sealPityRollupNote
}

/*
================
sealChanceSettings

The runtime's loot settings as one SealKillChance composition reads
them (loot/sealchance.go), including the per-class keep-seal chances.
================
*/
func (rt *Runtime) sealChanceSettings(mob monster.Instance) loot.SealChanceSettings {
	rate := max(1, rt.DropPassRate)
	familyRate := func(family string) int { return rt.DropMix.Rate(family, rate) }
	settings := loot.SealChanceSettings{
		RareChance:    RareEquipmentChance(rt.rareWindow()),
		PassRate:      rate,
		EquipmentRate: familyRate("equipment") * rt.pharaohGearRate(mob),
		AssignedRate:  familyRate("assigned"),
		DropCap:       rt.DropCap,
		Band:          rt.SealRates.Enabled(),
		SunMultiplier: rt.SunRate.MassMultiplier(),
	}
	for category, name := range consumableMixNames {
		settings.CategoryRates[category] = familyRate(name)
	}
	widening := rt.SealRates.Widening()
	for degree := 1; degree <= sealPityDegrees; degree++ {
		for tier := 0; tier < sealPityTiers; tier++ {
			keep := 1.0
			if widening > 1 {
				keep = min(1, float64(rt.SealRates.Multiplier(sealPityItemClass(degree, tier)))/float64(widening))
			}
			settings.SealKeep[degree-1][tier] = keep
		}
	}
	return settings
}

/*
================
planSealPity

The planner's pity step, after admission (monsterloot.go): settles
each tracked counter against the admitted list and returns the
admitted list (a forced drop appended in mode on) with the counter
delta for the commit. Draws rolls only when a forced drop is prepared,
so off and shadow keep the native random sequence.
================
*/
func (rt *Runtime) planSealPity(snapshot *enterworld.Character, mob monster.Instance, at simulation.Spawn, admitted []grounditem.Item, now time.Time) ([]grounditem.Item, sealPityDelta) {
	var delta sealPityDelta
	if !rt.SealPity.Enabled() || mob.GMSpawned || snapshot == nil || snapshot.Level == nil {
		return admitted, delta
	}
	increments := loot.SealKillChance(mob.Ref.Level, mob.Rarity(), mob.Ref.Codename, mob.Ref.Country, rt.sealChanceSettings(mob))
	admission := rt.admissionChance(uint8(*snapshot.Level), mob)
	today := killStatDayOf(now.UnixMilli())

	// The character's own admitted seals of this kill, by item class:
	// each resets its counter instead of accruing.
	natural := map[int]bool{}
	if items := rt.deps.ItemReferences(); items != nil {
		for _, drop := range admitted {
			ref, found := items.ItemRefByCodename(drop.Codename)
			if !found || ref == nil {
				continue
			}
			if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
				natural[sealPityItemClass(enterworld.EquipmentDegree(ref), tier)] = true
			}
		}
	}

	for degree := 1; degree <= sealPityDegrees; degree++ {
		for tier := 0; tier < sealPityTiers; tier++ {
			threshold := rt.SealPity.Threshold(degree, tier)
			calibration := degree == sealPityDegrees && tier == enterworld.SealStar
			if threshold <= 0 && !calibration {
				continue
			}
			itemClass := sealPityItemClass(degree, tier)
			note := sealPityRollupNote{degree: degree, tier: tier}
			row, hasRow := sealPityRow(snapshot, itemClass)
			if natural[itemClass] {
				note.natural = true
				if threshold > 0 && hasRow {
					delta.counters = append(delta.counters, sealPityCounterDelta{
						itemClass: itemClass, debt: 0,
						forcedDay: row.ForcedDay, forcedToday: sealPityForcedToday(row, today),
					})
				}
				delta.notes = append(delta.notes, note)
				continue
			}
			increment := increments[degree-1][tier] * admission
			if increment <= 0 {
				continue
			}
			note.accrued = increment
			if threshold <= 0 {
				// The calibration tier counts debt it never forces.
				delta.notes = append(delta.notes, note)
				continue
			}
			counter := sealPityCounterDelta{itemClass: itemClass, debt: row.Debt + increment,
				forcedDay: row.ForcedDay, forcedToday: sealPityForcedToday(row, today)}
			if counter.debt >= threshold {
				if counter.forcedToday >= rt.SealPity.DailyCap() {
					note.capHit = true
				} else if item, ok := rt.forceSealDrop(mob, at, snapshot, degree, tier, now, counter.debt); ok {
					if item.Codename != "" {
						admitted = append(admitted, item)
					}
					counter.debt, counter.forced, counter.forcedDay = 0, true, today
					counter.forcedToday++
					note.forced = true
				}
				// A forced drop that cannot be prepared keeps the debt
				// at or past the threshold: the rollup's over-threshold
				// count is the alarm for that bug state.
			}
			delta.counters = append(delta.counters, counter)
			delta.notes = append(delta.notes, note)
		}
	}
	return admitted, delta
}

/*
================
forceSealDrop

Fires one reached threshold. In mode on it prepares the forced item
(owner set, scattered, marked) and returns it with true; in shadow it
logs the drop it would have forced and returns true with no item. False
only when mode on cannot prepare the item.
================
*/
func (rt *Runtime) forceSealDrop(mob monster.Instance, at simulation.Spawn, snapshot *enterworld.Character, degree, tier int, now time.Time, debt float64) (grounditem.Item, bool) {
	if !rt.SealPity.ForcesDrops() {
		log.WithFields(log.Fields{"character": snapshot.Name, "degree": degree,
			"tier": sealPityTierNames[tier], "debt": debt}).
			Info("seal pity: shadow would force a seal drop")
		return grounditem.Item{}, true
	}
	item, ok := rt.prepareForcedSealDrop(mob, at, snapshot, degree, tier, now)
	if !ok {
		log.WithFields(log.Fields{"character": snapshot.Name, "degree": degree,
			"tier": sealPityTierNames[tier], "debt": debt}).
			Warn("seal pity: forced drop could not be prepared; the debt stands")
		return grounditem.Item{}, false
	}
	item.OwnerJID = enterworld.ObjectIDForCharacter(snapshot)
	item.SealPityForced = true
	log.WithFields(log.Fields{"character": snapshot.Name, "degree": degree,
		"tier": sealPityTierNames[tier], "item": item.Codename}).
		Info("seal pity: forced a seal drop")
	return rt.scatterMonsterDrop(item, at, mob.Rarity()), true
}

/*
================
prepareForcedSealDrop

A real drop of the tier: the country resolves natively, an item of the
exact (degree, tier) class is drawn from the monster's own rare row by
native weights (loot.PickEquipmentOfClass), then the ordinary drop
tail (plus, variance, magic options) runs. It skips keep-seal
thinning, the drop cap and level-gap admission, so the guarantee is
never lost silently.
================
*/
func (rt *Runtime) prepareForcedSealDrop(mob monster.Instance, at simulation.Spawn, snapshot *enterworld.Character, degree, tier int, now time.Time) (grounditem.Item, bool) {
	country, ok := rt.resolveMonsterDropCountry(mob.Ref.Country)
	if !ok {
		return grounditem.Item{}, false
	}
	codename, ok := loot.PickEquipmentOfClass(country, (degree-1)*sealPityTiers+tier, rt.DropRoll)
	if !ok {
		return grounditem.Item{}, false
	}
	plus, ok := rt.rollDroppedEquipmentPlus()
	if !ok {
		return grounditem.Item{}, false
	}
	return rt.prepareSelectedDrop(loot.DropItem{
		Codename: codename, Count: 1, Plus: plus,
		FromUnique: RollsUniqueDrops(mob.Rarity()),
	}, at, snapshot.Name, now)
}

/*
================
dropSealPityForced

The planned list without its forced drops, for a settlement that must
not land one (the GM kill path). Unforced plans return untouched.
================
*/
func dropSealPityForced(planned []grounditem.Item) []grounditem.Item {
	forced := false
	for _, item := range planned {
		if item.SealPityForced {
			forced = true
			break
		}
	}
	if !forced {
		return planned
	}
	kept := make([]grounditem.Item, 0, len(planned))
	for _, item := range planned {
		if !item.SealPityForced {
			kept = append(kept, item)
		}
	}
	return kept
}

/*
================
sealPityRow

The character's counter row for one item class, or the zero row.
================
*/
func sealPityRow(c *enterworld.Character, itemClass int) (domain.SealPityDebt, bool) {
	for _, row := range c.SealPity {
		if row.ItemClass == itemClass {
			return row, true
		}
	}
	return domain.SealPityDebt{ItemClass: itemClass}, false
}

/*
================
sealPityForcedToday

A row's forced count for the UTC day: a stale day counts nothing.
================
*/
func sealPityForcedToday(row domain.SealPityDebt, today int64) int {
	if row.ForcedDay != today {
		return 0
	}
	return row.ForcedToday
}

/*
================
commitSealPity

Applies one kill's counter delta to the loot owner and feeds the
rollup. Runs inside the fatal hit's door (applyMonsterKillInsideDoor),
so the debt lands in the same transaction as the loot it describes.
================
*/
func (rt *Runtime) commitSealPity(character *enterworld.Character, delta sealPityDelta, nowMs int64) {
	if character == nil || (len(delta.counters) == 0 && len(delta.notes) == 0) {
		return
	}
	for _, counter := range delta.counters {
		at := -1
		for i := range character.SealPity {
			if character.SealPity[i].ItemClass == counter.itemClass {
				at = i
				break
			}
		}
		row := domain.SealPityDebt{ItemClass: counter.itemClass, Debt: counter.debt,
			ForcedDay: counter.forcedDay, ForcedToday: counter.forcedToday}
		if at < 0 {
			character.SealPity = append(character.SealPity, row)
		} else {
			character.SealPity[at] = row
		}
	}
	// Counters resting at or past their threshold after the settle:
	// always zero unless a forced drop could not be prepared or the
	// table changed under a standing debt. The daily check reads this.
	over := map[int]bool{}
	for _, row := range character.SealPity {
		degree, tier := sealPityDegreeTier(row.ItemClass)
		if threshold := rt.SealPity.Threshold(degree, tier); threshold > 0 && row.Debt >= threshold {
			over[row.ItemClass] = true
		}
	}
	rt.noteSealPityStats(delta.notes, over, nowMs)
}

//============================================================================

/*
================
SealPityStatRow

One (UTC day, degree, tier) flush row of the pity rollup: the deltas
since the last flush. Name-free, like the kill stats it drains beside.
================
*/
type SealPityStatRow struct {
	DayS          int64
	Degree        int
	Tier          int
	DebtAccrued   float64
	NaturalDrops  int64
	ForcedDrops   int64
	DailyCapHits  int64
	OverThreshold int64
}

type sealPityStatKey struct {
	dayS   int64
	degree int
	tier   int
}

type sealPityStatCell struct {
	accrued                  float64
	natural, forced, capHits int64
	over                     int64
}

/*
================
sealPityStatTracker

The rollup. Counting runs inside character doors, so every entry takes
mu; the tick only drains the flush, whose write runs on its own
goroutine (the kill stats' shape, killstats.go).
================
*/
type sealPityStatTracker struct {
	mu          sync.Mutex
	cells       map[sealPityStatKey]*sealPityStatCell
	lastFlushMs int64
	flushing    bool
	flushDone   chan struct{}
}

/*
================
cell

The (day, degree, tier) cell, created on first use. Caller holds mu.
================
*/
func (t *sealPityStatTracker) cell(dayS int64, degree, tier int) *sealPityStatCell {
	if t.cells == nil {
		t.cells = map[sealPityStatKey]*sealPityStatCell{}
	}
	key := sealPityStatKey{dayS: dayS, degree: degree, tier: tier}
	found, ok := t.cells[key]
	if !ok {
		found = &sealPityStatCell{}
		t.cells[key] = found
	}
	return found
}

/*
================
noteSealPityStats

One kill's notes into the rollup, plus the settled counters found at
or past their threshold. Nothing counts while the rule is off.
================
*/
func (rt *Runtime) noteSealPityStats(notes []sealPityRollupNote, over map[int]bool, nowMs int64) {
	if !rt.SealPity.Enabled() || (len(notes) == 0 && len(over) == 0) {
		return
	}
	dayS := killStatDayOf(nowMs)
	rt.sealPityStats.mu.Lock()
	defer rt.sealPityStats.mu.Unlock()
	for _, note := range notes {
		cell := rt.sealPityStats.cell(dayS, note.degree, note.tier)
		cell.accrued += note.accrued
		if note.natural {
			cell.natural++
		}
		if note.forced {
			cell.forced++
		}
		if note.capHit {
			cell.capHits++
		}
	}
	for itemClass := range over {
		degree, tier := sealPityDegreeTier(itemClass)
		rt.sealPityStats.cell(dayS, degree, tier).over++
	}
}

/*
================
advanceSealPityStats

The tick step: once a flush interval passes, the rollup drains to the
wiring's sink off the tick.
================
*/
func (rt *Runtime) advanceSealPityStats(nowMs int64) {
	if !rt.SealPity.Enabled() {
		return
	}
	if rt.sealPityStats.lastFlushMs == 0 {
		rt.sealPityStats.lastFlushMs = nowMs
	}
	if nowMs-rt.sealPityStats.lastFlushMs >= killStatFlushEveryMs {
		rt.sealPityStats.lastFlushMs = nowMs
		rt.flushSealPityStatsAsync()
	}
}

/*
================
drainSealPityStats

Swaps the pending deltas out of the cells and marks the one write in
flight, or nil when a write already flies or nothing is pending.
================
*/
func (rt *Runtime) drainSealPityStats() []SealPityStatRow {
	rt.sealPityStats.mu.Lock()
	defer rt.sealPityStats.mu.Unlock()
	if rt.sealPityStats.flushing {
		return nil
	}
	rows := make([]SealPityStatRow, 0, len(rt.sealPityStats.cells))
	for key, cell := range rt.sealPityStats.cells {
		if cell.accrued == 0 && cell.natural == 0 && cell.forced == 0 && cell.capHits == 0 && cell.over == 0 {
			continue
		}
		rows = append(rows, SealPityStatRow{DayS: key.dayS, Degree: key.degree, Tier: key.tier,
			DebtAccrued: cell.accrued, NaturalDrops: cell.natural, ForcedDrops: cell.forced,
			DailyCapHits: cell.capHits, OverThreshold: cell.over})
		*cell = sealPityStatCell{}
	}
	if len(rows) == 0 {
		return nil
	}
	rt.sealPityStats.flushing = true
	rt.sealPityStats.flushDone = make(chan struct{})
	return rows
}

/*
================
settleSealPityStats

The in-flight write settled: on failure the deltas go back into the
cells, merging with whatever accrued meanwhile.
================
*/
func (rt *Runtime) settleSealPityStats(rows []SealPityStatRow, err error) {
	rt.sealPityStats.mu.Lock()
	if err != nil {
		for _, row := range rows {
			cell := rt.sealPityStats.cell(row.DayS, row.Degree, row.Tier)
			cell.accrued += row.DebtAccrued
			cell.natural += row.NaturalDrops
			cell.forced += row.ForcedDrops
			cell.capHits += row.DailyCapHits
			cell.over += row.OverThreshold
		}
	}
	rt.sealPityStats.flushing = false
	close(rt.sealPityStats.flushDone)
	rt.sealPityStats.mu.Unlock()
}

/*
================
flushSealPityStatsAsync

The tick's flush: drain and hand the write to a goroutine, so the tick
never waits on the sink. A write already in flight keeps the new deltas
for the next cadence.
================
*/
func (rt *Runtime) flushSealPityStatsAsync() {
	if rt.SealPityStatsFlush == nil {
		return
	}
	rows := rt.drainSealPityStats()
	if rows == nil {
		return
	}
	go func() {
		err := rt.SealPityStatsFlush(rows)
		rt.settleSealPityStats(rows, err)
		if err != nil {
			log.WithError(err).Warn("seal pity stats: flush failed; the deltas return for the next cadence")
		}
	}()
}

/*
================
FlushSealPityStats

The shutdown flush: waits out any in-flight write, then drains and
writes synchronously. A no-op when unwired.
================
*/
func (rt *Runtime) FlushSealPityStats() error {
	if rt.SealPityStatsFlush == nil {
		return nil
	}
	for {
		rt.sealPityStats.mu.Lock()
		flying, done := rt.sealPityStats.flushing, rt.sealPityStats.flushDone
		rt.sealPityStats.mu.Unlock()
		if !flying {
			break
		}
		<-done
	}
	rows := rt.drainSealPityStats()
	if rows == nil {
		return nil
	}
	err := rt.SealPityStatsFlush(rows)
	rt.settleSealPityStats(rows, err)
	return err
}
