/*
===========================================================================

sealchance.go - one kill's exact seal chances, for the pity luck-debt

Port-only, not native (#1690): the seal pity counter accrues, per
credited kill, that kill's chance of dropping each (degree, tier) seal.
SealKillChance composes the attempt chances odds.go already serves the
catalogue with the planner's rules (action/monsterloot.go), the same
composition catalogue/droptables.go publishes:

	passes and attempts (MonsterDropBudget), scaled by the beta rates;
	the rare window (one rarity roll per pass, its chance precomputed by
	the caller, which owns the window);
	keep-seal thinning (each class keeps with SealKeep probability);
	the drop cap and the kill capacity, both as expected-value factors:
	an item survives a uniform cap subset with chance cap / E[items],
	and a full kill truncates its later passes the same way.

The level-gap admission is the caller's (it depends on the killer, not
the monster), as is everything after the ground: this is the chance the
planner admits the seal, before admission. Expected counts ignore the
unique prepass (cap-exempt, and its items do not thin the ordinary
subset) and prepareSelectedDrop's beta exclusions (elixir and material
removals shave the consumable expectation, never an equipment chance);
at the shipped settings both move the result by less than the
calibration test's tolerance (action/sealpity_test.go).

Results are cached per (monster, rarity, settings): a kill on the hot
path reads one map entry.

===========================================================================
*/
package loot

import (
	"math"
	"sort"
	"sync"
)

// sealDegrees and sealTiers span the result: degrees 1..9, Star/Moon/Sun.
const (
	sealDegrees = 9
	sealTiers   = 3
)

/*
================
SealChanceSettings

The loot settings one SealKillChance composition runs under, snapshotted
by the caller (action) from its runtime. Comparable, so it keys the
cache: two settings that differ in any input never share a result.
================
*/
type SealChanceSettings struct {
	// RareChance is the chance one pass's rarity roll is rare
	// (action.RareEquipmentChance of the caller's rare window).
	RareChance float64
	// PassRate scales the kill's capacity (max(1, DropPassRate)).
	PassRate int
	// EquipmentRate is the equipment passes per native pass (the drop
	// mix's equipment rate times any Pharaoh gear rate).
	EquipmentRate int
	// CategoryRates is the consumable passes per native pass of each
	// of the planner's seven categories, in ConsumableFamilies order.
	CategoryRates [7]int
	// AssignedRate is the assigned-drop rounds per kill.
	AssignedRate int
	// DropCap bounds the ordinary items one kill keeps (0: no cap).
	DropCap int
	// Band runs the beta seal band's rare row (sealband.go).
	Band bool
	// SunMultiplier widens the degree-9 Sun class (sunscale.go); 1 off.
	SunMultiplier int
	// SealKeep is the keep-seal probability of each (degree, tier):
	// multiplier / widening, 1 where the seal rates are off or widest.
	SealKeep [sealDegrees][sealTiers]float64
}

type sealChanceKey struct {
	level    uint8
	rarity   uint8
	codename string
	country  uint8
	settings SealChanceSettings
}

var sealChanceCache = struct {
	sync.Mutex
	entries map[sealChanceKey][sealDegrees][sealTiers]float64
}{entries: map[sealChanceKey][sealDegrees][sealTiers]float64{}}

/*
================
SealKillChance

One kill of this monster's chance to admit a seal of each (degree,
tier), before level-gap admission: entry [degree-1][tier] with tier
Star, Moon, Sun in enterworld's order. Zero everywhere the monster
cannot drop seals at these settings.
================
*/
func SealKillChance(level uint8, rarity uint8, codename string, country uint8, settings SealChanceSettings) [sealDegrees][sealTiers]float64 {
	key := sealChanceKey{level: level, rarity: rarity, codename: codename, country: country, settings: settings}
	sealChanceCache.Lock()
	cached, ok := sealChanceCache.entries[key]
	sealChanceCache.Unlock()
	if ok {
		return cached
	}
	chances := sealKillChance(level, rarity, codename, country, settings)
	sealChanceCache.Lock()
	sealChanceCache.entries[key] = chances
	sealChanceCache.Unlock()
	return chances
}

/*
================
sealKillChance

The composition itself; SealKillChance caches it.
================
*/
func sealKillChance(level uint8, rarity uint8, codename string, country uint8, settings SealChanceSettings) [sealDegrees][sealTiers]float64 {
	var out [sealDegrees][sealTiers]float64
	capacity, nativePasses, attempts := MonsterDropBudget(rarity, codename)
	classes, rareSuccess := rareClassAttemptChances(level, country, settings.Band, settings.SunMultiplier)
	if rareSuccess <= 0 || settings.RareChance <= 0 {
		return out
	}
	equipmentPasses := nativePasses * max(settings.EquipmentRate, 0)
	if equipmentPasses <= 0 {
		return out
	}

	// The cap and capacity as expected-value survival (file banner).
	normalSuccess := attemptSuccess(EquipmentAttemptChances(level, country, false))
	equipmentYield := settings.RareChance*atLeastOnce(rareSuccess, attempts) +
		(1-settings.RareChance)*atLeastOnce(normalSuccess, attempts)
	expected := float64(nativePasses) // one gold heap per native pass
	expected += float64(equipmentPasses) * equipmentYield
	for category, shares := range ConsumableFamilies() {
		yield := 0.0
		for _, share := range shares {
			yield += share.Share * atLeastOnce(attemptSuccess(ConsumableAttemptChances(share.Family, level)), attempts)
		}
		expected += float64(nativePasses*max(settings.CategoryRates[category], 0)) * yield
	}
	expected += float64(max(settings.AssignedRate, 0)) * assignedExpectation(codename)
	survival := 1.0
	if bound := float64(capacity * max(settings.PassRate, 1)); expected > bound && expected > 0 {
		survival = bound / expected
	}
	if ordinary := expected - float64(nativePasses); settings.DropCap > 0 && ordinary > float64(settings.DropCap) {
		survival *= float64(settings.DropCap) / ordinary
	}

	// One pass yields class k with RareChance * (its share of the rare
	// attempts that succeed within the attempt budget) * keep; a kill
	// is equipmentPasses independent passes.
	share := atLeastOnce(rareSuccess, attempts) / rareSuccess
	for degree := 0; degree < sealDegrees; degree++ {
		for tier := 0; tier < sealTiers; tier++ {
			pass := settings.RareChance * classes[degree][tier] * share * settings.SealKeep[degree][tier]
			if pass <= 0 {
				continue
			}
			out[degree][tier] = (1 - math.Pow(1-pass, float64(equipmentPasses))) * survival
		}
	}
	return out
}

/*
================
rareClassAttemptChances

One rare attempt's chances by (degree, tier) over the composed rare row
(the seal band and Sun widening the planner rolls), and the attempt's
total success. Mirrors ComposedRareChances walk for walk, attributing
each settled item to its own class group instead of its codename.
================
*/
func rareClassAttemptChances(level, country uint8, band bool, sunMultiplier int) ([sealDegrees][sealTiers]float64, float64) {
	var classes [sealDegrees][sealTiers]float64
	row, ok := composedRareRow(level, band, sunMultiplier)
	if !ok {
		return classes, 0
	}
	total := 0.0
	add := func(ref equipmentRef, mass float64) {
		total += mass
		if ref.Group < 0 || ref.Group >= sealDegrees*sealTiers {
			return
		}
		classes[ref.Group/sealTiers][ref.Group%sealTiers] += mass
	}
	for group, classMass := range classChances(row) {
		for dropCountry, countryMass := range equipmentCountries(country) {
			equipment.selectionChances(dropCountry, group, true, level, classMass*countryMass, add)
		}
	}
	return classes, total
}

/*
================
attemptSuccess

The summed success of one attempt's chances.
================
*/
func attemptSuccess(chances []Chance) float64 {
	total := 0.0
	for _, chance := range chances {
		total += chance.Chance
	}
	return total
}

/*
================
assignedExpectation

The expected assigned items of one AssignedDrops round for this
monster: each row's per-try chance times its mean count. A row shape
with no closed form (AssignedDropChances' error) counts nothing.
================
*/
func assignedExpectation(codename string) float64 {
	rows, err := AssignedDropChances(codename)
	if err != nil {
		return 0
	}
	total := 0.0
	for _, row := range rows {
		total += row.Chance * float64(int(row.Min)+int(row.Max)) / 2
	}
	return total
}

/*
================
PickEquipmentOfClass

A weighted pick inside one exact rare class bucket (degree and tier as
a class group), by the bucket's native weights and the caller's roll:
no step down to a lower class and no absolute admission, so the pick is
always of the class named. The seal pity guarantee draws through this
(action/sealpity.go) after resolving the country natively; false when
the class holds nothing for the country.
================
*/
func PickEquipmentOfClass(country uint8, group int, roll func() (uint32, error)) (string, bool) {
	if country > 1 || group < 0 || group >= sealDegrees*sealTiers || roll == nil {
		return "", false
	}
	bucket := equipment.buckets[equipmentKey{country, group, true}]
	if bucket == nil || bucket.expansionOnly || len(bucket.refs) == 0 {
		return "", false
	}
	r, err := roll()
	if err != nil {
		return "", false
	}
	wanted := r % bucket.weights[len(bucket.weights)-1]
	at := sort.Search(len(bucket.weights), func(i int) bool { return bucket.weights[i] >= wanted })
	return bucket.refs[at].Codename, true
}
