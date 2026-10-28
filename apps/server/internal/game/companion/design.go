/*
===========================================================================

design.go - the attack-pet design JSON, embedded

Port-only, not native. petdesign.json is the owner's single source (rules,
skills, gear, degrees, affixes; #1631): the asset build reads it in place
and scripts/tools/pet_design_export.py generates the client's copy. It is
compiled into the binary, so a malformed export is a build defect and
panics at package init (the go:embed rule in apps/server/AGENTS.md).

===========================================================================
*/
package companion

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed petdesign.json
var designJSON []byte

// Skill rows: two active rows, then three passive rows.
const (
	RowCount       = 5
	ActiveRowCount = 2
)

/*
================
Rules
================
*/
type Rules struct {
	SkillMaxLevel          uint8   `json:"skillMaxLevel"`
	Rank2Level             uint8   `json:"rank2Level"`
	Rank3Level             uint8   `json:"rank3Level"`
	PetLevelsPerSkillLevel uint8   `json:"petLevelsPerSkillLevel"`
	PointsPerPetLevels     uint8   `json:"pointsPerPetLevels"`
	OptionUnlockStep       uint8   `json:"optionUnlockStep"`
	RowUnlock              []uint8 `json:"rowUnlock"`
	ProcInternalCooldownMs int64   `json:"procInternalCooldownMs"`
	MaxBlues               int     `json:"maxBlues"`
	MagicTwoBluesChance    float64 `json:"magicTwoBluesChance"`
	RankMultipliers        struct {
		General, Champion, Giant, Unique float64
	} `json:"rankMultipliers"`
}

/*
================
PetDef
================
*/
type PetDef struct {
	HealthByLevel       []PetHealth `json:"healthByLevel"`
	Name                string      `json:"name"`
	Role                string      `json:"role"`
	CodenamePrefix      string      `json:"codenamePrefix"`
	HPPercent           float64     `json:"hpPercent"`
	PhysicalPercent     float64     `json:"physicalPercent"`
	MagicalPercent      float64     `json:"magicalPercent"`
	DefensePercent      float64     `json:"defensePercent"`
	MagicDefensePercent float64     `json:"magicDefensePercent"`
	MagicPrimary        bool        `json:"magicPrimary"`
}

/*
================
PetHealth

An authored survival benchmark; intermediate levels interpolate.
================
*/
type PetHealth struct {
	Level uint8  `json:"level"`
	HP    uint32 `json:"hp"`
}

/*
================
SkillDef

One skill row of the workbook. Value and cooldown grow linearly with the
skill level; Status other than "Implemented" cannot be learned.
================
*/
type SkillDef struct {
	Family           string   `json:"family"`
	TierEnd          uint8    `json:"tierEnd"`
	ID               uint8    `json:"id"`
	Pet              string   `json:"pet"`
	Row              int      `json:"row"`
	Active           bool     `json:"active"`
	Option           int      `json:"option"`
	Name             string   `json:"name"`
	Trigger          string   `json:"trigger"`
	Cooldown         *float64 `json:"cooldown"`
	CooldownPerLevel *float64 `json:"cooldownPerLevel"`
	Value            float64  `json:"value"`
	ValuePerLevel    float64  `json:"valuePerLevel"`
	MaxLevel         uint8    `json:"maxLevel"`
	UnlockLevel      uint8    `json:"unlockLevel"`
	Status           string   `json:"status"`
}

/*
================
GearItem / GearDegree
================
*/
type GearItem struct {
	Slot      string    `json:"slot"`
	Name      string    `json:"name"`
	Codename  string    `json:"codename"`
	ID        uint32    `json:"id"`
	BaseStats GearStats `json:"baseStats"`
}

type GearDegree struct {
	ProcDamage  uint32     `json:"procDamage"`
	Degree      int        `json:"degree"`
	MinPetLevel uint8      `json:"minPetLevel"`
	DropChance  float64    `json:"dropChance"`
	MagicChance float64    `json:"magicChance"`
	RareChance  float64    `json:"rareChance"`
	RareWeights []float64  `json:"rareWeights"`
	MaxBlues    int        `json:"maxBlues"`
	RollFloor   float64    `json:"rollFloor"`
	Items       []GearItem `json:"items"`
}

/*
================
AffixDef

One blue option tier. Code is its stable wire number (the option's param
id is AffixParamBase + Code).
================
*/
type AffixDef struct {
	SkillID   uint8    `json:"skillId,omitempty"`
	RollPower float64  `json:"rollPower,omitempty"`
	ID        string   `json:"id"`
	Code      uint16   `json:"code"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Group     string   `json:"group"`
	Slots     []string `json:"slots"`
	Tier      int      `json:"tier"`
	MinDegree int      `json:"minDegree"`
	Min       float64  `json:"min"`
	Max       float64  `json:"max"`
	Weight    float64  `json:"weight"`
	Pets      string   `json:"pets"`
	Status    string   `json:"status"`
}

/*
================
Design
================
*/
type Design struct {
	TreeSkills []SkillDef   `json:"treeSkills"`
	Rules      Rules        `json:"rules"`
	Pets       []PetDef     `json:"pets"`
	Skills     []SkillDef   `json:"skills"`
	Gear       []GearDegree `json:"gear"`
	Affixes    []AffixDef   `json:"affixes"`
}

// D is the embedded design.
var D = mustDesign(designJSON)

/*
================
mustDesign
================
*/
func mustDesign(raw []byte) Design {
	var d Design
	if err := json.Unmarshal(raw, &d); err != nil {
		panic(fmt.Sprintf("companion: petdesign.json: %v", err))
	}
	if len(d.Rules.RowUnlock) != RowCount || d.Rules.SkillMaxLevel == 0 || d.Rules.PointsPerPetLevels == 0 {
		panic("companion: petdesign.json rules are incomplete")
	}
	ids := map[uint8]bool{}
	for i, g := range d.Gear {
		if g.Degree != i+1 || g.MaxBlues < 1 || g.MaxBlues > d.Rules.MaxBlues || g.RollFloor < 0 || g.RollFloor > 1 || len(g.RareWeights) != 4 {
			panic(fmt.Sprintf("companion: petdesign.json degree %d is invalid", g.Degree))
		}
	}
	for _, s := range d.Skills {
		if s.ID == 0 || ids[s.ID] || s.Row < 0 || s.Row >= RowCount || s.MaxLevel == 0 || petByName(d, s.Pet) == nil {
			panic(fmt.Sprintf("companion: petdesign.json skill %d %q is invalid", s.ID, s.Name))
		}
		ids[s.ID] = true
	}
	codes := map[uint16]bool{}
	for _, a := range d.Affixes {
		if a.Code == 0 || codes[a.Code] || a.Max < a.Min {
			panic(fmt.Sprintf("companion: petdesign.json affix %q is invalid", a.ID))
		}
		if a.SkillID != 0 {
			valid := false
			for _, skill := range d.TreeSkills {
				if skill.ID == a.SkillID && skill.Pet == a.Pets && skill.Option == 1 && skill.Row >= 0 && skill.Row < RowCount {
					valid = a.Group == "OneSkill" && a.Min >= 1 && a.Max <= 5
				}
			}
			if !valid {
				panic(fmt.Sprintf("companion: affix %q has an invalid skill bonus", a.ID))
			}
		}
		codes[a.Code] = true
	}
	return d
}

/*
================
petByName
================
*/
func petByName(d Design, name string) *PetDef {
	for i := range d.Pets {
		if d.Pets[i].Name == name {
			return &d.Pets[i]
		}
	}
	return nil
}

/*
================
PetKind

The design pet a COS codename belongs to; "" for every other companion.
================
*/
func PetKind(codename string) string {
	for _, p := range D.Pets {
		if strings.HasPrefix(codename, p.CodenamePrefix) {
			return p.Name
		}
	}
	return ""
}

/*
================
Skill
================
*/
func Skill(id uint8) (SkillDef, bool) {
	for _, s := range D.Skills {
		if s.ID == id {
			return s, true
		}
	}
	return SkillDef{}, false
}

/*
================
PetSkills

The skills of one pet, in workbook order (rows, then options), from
the per-kind index built once at package init: the workbook is parsed
once from the embedded design and never changes at runtime, so the
lists are shared read-only instead of rebuilt on every call. Every
caller ranges the result and none writes, appends to, sorts or
retains it (SlottedPassive keeps one element copy, the rest only
read fields), and the stored slices are capacity-trimmed, so even a
future caller's append allocates its own backing.
================
*/
func PetSkills(pet string) []SkillDef {
	return petSkillsByKind[pet]
}

// The workbook's two skill tables, indexed by pet kind once.
var (
	petSkillsByKind  = skillsByKind(D.Skills)
	treeSkillsByKind = skillsByKind(D.TreeSkills)
)

/*
================
skillsByKind

One pet kind's rows in workbook order, capacity-trimmed so the
shared slice can never be grown in place.
================
*/
func skillsByKind(rows []SkillDef) map[string][]SkillDef {
	byKind := make(map[string][]SkillDef)
	for _, s := range rows {
		byKind[s.Pet] = append(byKind[s.Pet], s)
	}
	for kind, skills := range byKind {
		byKind[kind] = skills[:len(skills):len(skills)]
	}
	return byKind
}

/*
================
ValueAt / CooldownAt

A skill's effect value and cooldown at a level (level 0 is unlearned).
================
*/
func (s SkillDef) ValueAt(level uint8) float64 {
	if level == 0 {
		return 0
	}
	value := s.Value + s.ValuePerLevel*float64(level-1)
	if s.Family == "Hunting Howl" {
		return min(500, value) // +skills equipment must respect the howl's ceiling too.
	}
	return value
}

func (s SkillDef) CooldownAt(level uint8) float64 {
	if s.Cooldown == nil {
		return 0
	}
	per := 0.0
	if s.CooldownPerLevel != nil {
		per = *s.CooldownPerLevel
	}
	return *s.Cooldown + per*float64(max(level, 1)-1)
}

/*
================
RoarRadiusMeters

Port-only tier I/III range tuning; tier II retains its existing progression.
================
*/
func (s SkillDef) RoarRadiusMeters(level uint8) float64 {
	steps := float64(max(level, 1) - 1)
	switch s.Option {
	case 1:
		return 10 + steps
	case 3:
		return 30 + steps
	default:
		return 12 + steps*0.4
	}
}

/*
================
Live

Only implemented skills can be learned; the rest are shown as planned.
================
*/
func (s SkillDef) Live() bool {
	return s.Status == "Implemented"
}

/*
================
GearByID / GearByCodename

The degree and slot index of a pet gear item.
================
*/
func GearByID(id uint32) (GearDegree, int, bool) {
	tier := SealTier(id)
	id -= uint32(tier * 10)
	for _, d := range D.Gear {
		for slot, item := range d.Items {
			if item.ID == id {
				return sealedDegree(d, slot, tier), slot, true
			}
		}
	}
	return GearDegree{}, 0, false
}

func GearByCodename(codename string) (GearDegree, int, bool) {
	codename, tier := sealCodename(codename)
	for _, d := range D.Gear {
		for slot, item := range d.Items {
			if item.Codename == codename {
				return sealedDegree(d, slot, tier), slot, true
			}
		}
	}
	return GearDegree{}, 0, false
}

/*
================
IsGearRef

The pet gear reference range: these port items carry their blue options on
the wire like equipment (item/wire, client inventory-item.ts).
================
*/
func IsGearRef(id uint32) bool {
	_, _, ok := GearByID(id)
	return ok
}

/*
================
AffixByCode
================
*/
func AffixByCode(code uint16) (AffixDef, bool) {
	for _, a := range D.Affixes {
		if a.Code == code {
			return a, true
		}
	}
	return AffixDef{}, false
}
