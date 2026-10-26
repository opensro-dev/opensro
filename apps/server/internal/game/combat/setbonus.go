/*
===========================================================================

setbonus.go - degree-10/11 equipment set bonuses

Port-only, not native (issue #1747): the v1.150 client ships no set
rows, so this owner exists only for the port-only cap-110 overlay and
runs only while SRO_CAP110_SET_BONUSES is on. PlayerStatsWithModifiers
receives a nil catalogue when the flag is off and every path here is
skipped, leaving the off snapshot byte-identical.

The rules, traced from the native server and stated in words: a worn
count is kept per set group; the highest tier whose piece requirement
the count meets replaces the lower tiers, so one magic group applies
per group and tiers never stack; broken pieces still count toward the
tier; option 389 adds its tier value to each worn group piece's total
opt level inside that piece's own stat derivation; option 390 adds its
value flat to the six abnormal-resistance keeper params 0x1b..0x20 and
as a percent to the Sleep, Fear, Disease and Combustion buckets.

The native server writes these under the set id itself. This port's
small keeper sources are already owned (0 base, 1024+slot equipment,
0x10000+ effects), so set writes take source 2048+setID - a port
allocation, not a native fact.

===========================================================================
*/

package combat

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/paramkeeper"
)

// setWriteSourceBase starts the keeper source range set writes own.
const setWriteSourceBase = 2048

// The abnormal-resistance keeper params option 390 feeds: 0x1b..0x20,
// the same six ElementResistanceWrites writes (freeze, frostbite, shock,
// burn, poison, zombie - the abnormal sources whose Resist index is -1).
const (
	setResistParamFirst = 0x1b
	setResistParamCount = 6
)

/*
================
setGroupEffect

setGroupEffect is one worn set group's resolved tier: the option 389
value reinforcing each worn piece (0 = the tier carries no reinforce
option) and the option 390 value (0 = none).
================
*/
type setGroupEffect struct {
	setID     int64
	reinforce int
	resist    int
}

/*
================
activeSetEffects

activeSetEffects counts the worn pieces of every set group and resolves
each group's active tier. Counting is deliberately independent of the
stat loop: a piece counts even when it is too broken to contribute
stats, and a piece whose reference the stat loop would reject is
skipped here so the loop's error stays the only one. The result is
sorted by set id so write order never depends on map iteration.
================
*/
func activeSetEffects(
	character *domain.Character,
	items enterworld.ItemRefSource,
	sets enterworld.SetCatalogueSource,
) (map[int64]setGroupEffect, []setGroupEffect) {
	counts := map[int64]int{}
	for _, row := range character.MissionInventory {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok || ref == nil || ref.SetID == 0 {
			continue
		}
		counts[ref.SetID]++
	}
	if len(counts) == 0 {
		return nil, nil
	}
	byID := make(map[int64]setGroupEffect, len(counts))
	var effects []setGroupEffect
	for setID, count := range counts {
		tiers := sets.SetGroupTiers(setID)
		active := -1
		for i := range tiers {
			if tiers[i].RequiredCount <= count &&
				(active < 0 || tiers[i].RequiredCount > tiers[active].RequiredCount) {
				active = i
			}
		}
		if active < 0 {
			continue
		}
		effect := setGroupEffect{setID: setID}
		for _, option := range sets.MagicGroupOptions(tiers[active].MagicGroupID) {
			switch option.OptionID {
			case enterworld.SetOptionReinforceItem:
				effect.reinforce = option.Value
			case enterworld.SetOptionResistAll:
				effect.resist = option.Value
			}
		}
		byID[setID] = effect
		effects = append(effects, effect)
	}
	sort.Slice(effects, func(i, j int) bool {
		return effects[i].setID < effects[j].setID
	})
	return byID, effects
}

/*
================
setResistWrites

setResistWrites builds option 390's flat writes: its value on each of
the six abnormal-resistance keeper params, under the group's own
source so two worn groups add instead of overwriting.
================
*/
func setResistWrites(effect setGroupEffect) []paramkeeper.Write {
	if effect.resist == 0 {
		return nil
	}
	writes := make([]paramkeeper.Write, 0, setResistParamCount)
	for i := uint16(0); i < setResistParamCount; i++ {
		writes = append(writes, paramkeeper.Write{
			Parameter: setResistParamFirst + i,
			Channel:   paramkeeper.Flat,
			Source:    uint32(setWriteSourceBase + effect.setID),
			Value:     float32(effect.resist),
		})
	}
	return writes
}

/*
================
applySetStatusResistance

applySetStatusResistance adds each active group's option 390 value as
a percent to the four buckets the option raises. The buckets are found
through the abnormal source table itself - Sleep (0), Fear (3),
Disease (8), Combustion (15) - the same numbering the learned real
projection files by; a fifth status is never touched.
================
*/
func applySetStatusResistance(out *[17]abnormal.Resistance, effects []setGroupEffect) {
	for _, effect := range effects {
		if effect.resist == 0 {
			continue
		}
		for _, source := range abnormal.Sources {
			switch source.Status {
			case abnormal.Sleep, abnormal.Fear, abnormal.Disease, abnormal.Combustion:
				out[source.Resist].Percent += int32(effect.resist)
			}
		}
	}
}
