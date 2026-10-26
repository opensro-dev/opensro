/*
===========================================================================

gear.go - pet equipment: blue options, drop rolls and the modifier sum

Port-only, not native (petdesign.json: Gear, Degrees,
Affixes). One gear family is worn by every pet in three slots (collar,
harness, charm), degrees 1-9, dropped by monsters only. Rolls follow
Diablo 2: normal (no blue), magic (1-2) or rare (3-6), capped by degree.
Each degree owns its odds; one affix per group; an affix tier may roll when its
minimum degree is at most the item's, picked by weight. A blue option is
stored in the native magic-option encoding (high u32 value, low u16 param)
with param AffixParamBase + the affix's Code, so it rides the item through
bags, storage and trades unchanged.

===========================================================================
*/
package companion

import (
	"math"

	"opensro.online/server/internal/domain"
)

// AffixParamBase marks a pet blue option's param id (native ids end at 256).
const AffixParamBase = 0x8000

/*
================
EncodeAffix / DecodeAffix
================
*/
func EncodeAffix(code uint16, value uint32) uint64 {
	return uint64(value)<<32 | uint64(AffixParamBase+code)
}

func DecodeAffix(option uint64) (AffixDef, uint32, bool) {
	param := uint16(option)
	if param <= AffixParamBase {
		return AffixDef{}, 0, false
	}
	a, ok := AffixByCode(param - AffixParamBase)
	return a, uint32(option >> 32), ok
}

/*
================
Roller

The random source a roll draws from: Float in [0, 1).
================
*/
type Roller interface {
	Float() float64
}

/*
================
RollGear

One pet gear drop of a degree for a slot: its blue options (empty for a
normal item). Only implemented affixes roll, so no option is ever inert.
findPct raises the magic and rare odds (Lucky Feather, "of Fortune").
================
*/
func RollGear(d GearDegree, slot int, findPct float64, r Roller) []uint64 {
	return RollGearWithAffixes(d, slot, findPct, r, D.Affixes)
}

/*
================
RollGearWithAffixes

Validated DB rules affect future drops, never saved rolls.
================
*/
func RollGearWithAffixes(d GearDegree, slot int, findPct float64, r Roller, affixes []AffixDef) []uint64 {
	scale := 1 + findPct/100
	roll := r.Float()
	count := 0
	switch {
	case roll < d.RareChance*scale:
		total := 0.0
		for _, w := range d.RareWeights {
			total += w
		}
		pick := r.Float() * total
		count = 3
		for i, w := range d.RareWeights {
			if pick < w {
				count = 3 + i
				break
			}
			pick -= w
		}
		count = min(count, D.Rules.MaxBlues)
	case roll < (d.RareChance+d.MagicChance)*scale:
		count = 1
		if r.Float() < D.Rules.MagicTwoBluesChance {
			count = 2
		}
	}
	count = min(count, d.MaxBlues)
	slotName := d.Items[slot].Slot
	used := map[string]bool{}
	var options []uint64
	for range count {
		var pool []AffixDef
		total := 0.0
		for _, a := range affixes {
			if a.Status != "Implemented" || a.Weight <= 0 || used[a.Group] || a.MinDegree > d.Degree || !hasSlot(a, slotName) {
				continue
			}
			pool = append(pool, a)
			total += a.Weight
		}
		if len(pool) == 0 {
			break
		}
		pick := r.Float() * total
		chosen := pool[len(pool)-1]
		for _, a := range pool {
			if pick < a.Weight {
				chosen = a
				break
			}
			pick -= a.Weight
		}
		// A group rolls once; a higher tier rolls in place of a lower one.
		used[chosen.Group] = true
		// Higher degrees raise the floor without exceeding the affix tier's cap.
		low := chosen.Min + math.Floor((chosen.Max-chosen.Min)*d.RollFloor)
		value := low + math.Floor(math.Pow(r.Float(), max(1, chosen.RollPower))*(chosen.Max-low+1))
		options = append(options, EncodeAffix(chosen.Code, uint32(min(value, chosen.Max))))
	}
	return options
}

/*
================
hasSlot
================
*/
func hasSlot(a AffixDef, slot string) bool {
	for _, s := range a.Slots {
		if s == slot {
			return true
		}
	}
	return false
}

/*
================
DropChance

A kill's chance of one pet gear drop: the degree's base chance times the
monster rank's multiplier (native rarity: low nibble 1 champion, 4-7 giant
kinds, 3 and 8 unique).
================
*/
func DropChance(d GearDegree, rarity uint8) float64 {
	m := D.Rules.RankMultipliers
	switch rarity & 0x0f {
	case 1:
		return d.DropChance * m.Champion
	case 3, 8:
		return d.DropChance * m.Unique
	case 4, 5, 6, 7:
		return d.DropChance * m.Giant
	}
	return d.DropChance * m.General
}

/*
================
DegreeForLevel

The degree a monster of this level drops (its band's minimum pet level is
the first monster level of the band); uniques drop one degree higher.
================
*/
func DegreeForLevel(level uint8, unique bool) (GearDegree, bool) {
	var found GearDegree
	ok := false
	for i, d := range D.Gear {
		if level >= d.MinPetLevel {
			found, ok = d, true
			if unique && i+1 < len(D.Gear) {
				found = D.Gear[i+1]
			}
		}
	}
	return found, ok
}

//============================================================================

/*
================
Modifiers

The sum of a pet's slotted passives, worn gear (base stats and blues) at
one moment. Percentages are added (+10 is +10%).
================
*/
type Modifiers struct {
	GearStats      GearStats
	AttackPct      float64 // both attack types (collar implicit)
	PhysicalPct    float64 // physical affixes only
	MagicPct       float64 // magical attack only
	DefensePct     float64
	CritPct        float64
	AttackSpeedPct float64
	LeechPct       float64
	CooldownPct    float64
	CrushPct       float64
	DeadlyPct      float64
	PiercePct      float64
	// Separate yellow elemental hits; PoisonAdd is the total over four seconds.
	FireAdd, ColdAdd, ShockAdd, PoisonAdd float64
	SkillBonus                            int
	RowSkillBonus                         [RowCount]int
	FindPct                               float64
	// Chance-to-cast blues, percent per hit.
	Procs [ProcCount][2]GearProc
}

/*
================
ModifiersFor

The always-on modifiers: worn gear and the stat passives. Conditional
passives (Execute, Shatter, Bloodlust, ...) are read at the strike.
================
*/
func ModifiersFor(p *domain.CharacterCOS) Modifiers {
	var m Modifiers
	pet := PetKind(p.Codename)
	for slot, g := range p.Training.Gear {
		if g.RefObjID == 0 {
			continue
		}
		d, itemSlot, ok := GearByID(g.RefObjID)
		if !ok || itemSlot != slot || p.Level < d.MinPetLevel {
			continue
		}
		m.GearStats.add(d.Items[slot].BaseStats)
		for _, o := range g.Options {
			if a, v, ok := DecodeAffix(o); ok && a.Status == "Implemented" {
				if !m.addProc(a.Group, float64(v), d.ProcDamage) {
					m.addAffix(a, float64(v), pet)
				}
			}
		}
	}
	for row := ActiveRowCount; row < RowCount; row++ {
		s, ok := SlottedPassive(p, row)
		if !ok {
			continue
		}
		v := s.ValueAt(EffectiveLevel(p, s, m))
		switch s.EffectName() {
		case "Predator":
			m.AttackPct += v
		case "Cold Mastery":
			m.MagicPct += v
		case "Swift Fangs":
			m.AttackSpeedPct += v
		case "Thick Fur", "Frost Armor":
			m.DefensePct += v
		case "Lifebite":
			m.LeechPct += v
		case "Arcane Haste":
			m.CooldownPct += v
		case "Lucky Feather":
			m.FindPct += v
		}
	}
	if PetKind(p.Codename) == "Penguin" {
		m.MagicPct += p.ArcaneMagicPercent
	}
	if p.BroodStage != 0 {
		return Modifiers{GearStats: m.GearStats, AttackPct: m.AttackPct, PhysicalPct: m.PhysicalPct, MagicPct: m.MagicPct,
			DefensePct: m.DefensePct, CritPct: m.CritPct, AttackSpeedPct: m.AttackSpeedPct}
	}
	m.CooldownPct = min(m.CooldownPct, 80)
	return m
}

/*
================
addAffix
================
*/
func (m *Modifiers) addAffix(a AffixDef, v float64, pet string) {
	switch a.Group {
	case "DmgPhys":
		m.PhysicalPct += v
	case "DmgMag":
		m.MagicPct += v
	case "Crit":
		m.CritPct += v
	case "Fire":
		m.FireAdd += v
	case "Cold":
		m.ColdAdd += v
	case "Lightning":
		m.ShockAdd += v
	case "Poison":
		m.PoisonAdd += v
	case "Leech":
		m.LeechPct += v
	case "Crush":
		m.CrushPct += v
	case "Deadly":
		m.DeadlyPct += v
	case "Pierce":
		m.PiercePct += v
	case "Def":
		m.DefensePct += v
	case "Speed":
		m.AttackSpeedPct += v
	case "Cooldown":
		m.CooldownPct += v
	case "Find":
		m.FindPct += v
	case "OneSkill":
		for _, skill := range D.TreeSkills {
			if skill.ID == a.SkillID && skill.Pet == pet {
				m.RowSkillBonus[skill.Row] += int(v)
				break
			}
		}
	case "TreeBear", "TreeWolf", "TreePenguin", "TreeCrow":
		if a.Pets == pet {
			m.SkillBonus += int(v)
		}
	}
}
