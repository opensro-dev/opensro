/*
===========================================================================

monsters.go - C4 monsters.json and droptables.json: who lives where, and
what a kill drops

The monsters are the server's population template (monster.LoadTemplate):
every monster with a nest, and every unique. A monster can spawn as more
than its authored grade (the server's spawn grade roll: a nest with champion tactics promotes to
champion or giant, an armed nest makes a party monster), so each row lists
the variants it can spawn as, each with the kill budget the planner uses
(loot.MonsterDropBudget) and its gold heap range.

The odds are exact counts over the planner's own tables (loot odds.go),
never sampled. A monster's assigned drops are on its row; the level tables
every monster of a level shares (equipment, consumables, a unique's
special picks) are in droptables.json, keyed by level and country, so the
same table is not repeated on hundreds of rows. Every chance is before
the kill's item capacity and before the level-gap admission (the server's drop admission),
which depends on the killer.

===========================================================================
*/
package catalogue

import (
	"fmt"
	"sort"
	"strings"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/quest"
	"opensro.online/server/internal/game/world/monster"
)

// Native grades (RefObjChar MonsterType, the spawn byte's low nibble) as
// the client's target window names them (target-status.ts).
const (
	gradeNormal   = 0
	gradeChampion = 1
	gradeUnique   = 3
	gradeGiant    = 4
	gradeParty    = 0x10
)

// questMonsterTypeID4 is TID4 4, the MOB_QT_* quest monsters, which never
// become party monsters, as the server's nest spawn makes them.
const questMonsterTypeID4 = 4

var gradeEnum = []string{"normal", "champion", "unique", "giant", "titan", "elite"}

var gradeNames = map[uint8]string{0: "normal", 1: "champion", 3: "unique", 4: "giant", 5: "titan", 6: "elite"}

/*
================
MonsterVariant

One way the monster can spawn: its grade, whether it is a party monster,
and what one kill of it rolls.
================
*/
type MonsterVariant struct {
	Grade string `json:"grade" enum:"grade"`
	Party bool   `json:"party,omitempty"`
	// Capacity is the most items one kill can leave; Passes the ordinary
	// drop passes; Attempts the tries each equipment or consumable draw
	// gets before it gives up.
	Capacity int `json:"capacity"`
	Passes   int `json:"passes"`
	Attempts int `json:"attempts"`
	// GoldMin and GoldMax bound one gold heap; a kill makes one per pass.
	GoldMin uint64 `json:"goldMin"`
	GoldMax uint64 `json:"goldMax"`
}

/*
================
MonsterDrop

One assigned drop: the item, its chance per try, the tries a kill makes and
the chance a kill drops it at least once.
================
*/
type MonsterDrop struct {
	ItemID   uint32  `json:"itemId"`
	Plus     uint8   `json:"plus,omitempty"`
	Chance   float64 `json:"chance"`
	TriesMin int     `json:"triesMin"`
	TriesMax int     `json:"triesMax"`
	PerKill  float64 `json:"perKill"`
	// Group marks a member of a random group: one draw of the group picks
	// one member, so the members of one group exclude each other.
	Group bool `json:"group,omitempty"`
}

/*
================
UniqueBonus

What a unique drops before anything else (prepareUniqueDrops): special
equipment picks from droptables special[Table], Elixir recipes and return
scrolls, each a count range.
================
*/
type UniqueBonus struct {
	Table        string `json:"table"`
	EquipmentMin int    `json:"equipmentMin"`
	EquipmentMax int    `json:"equipmentMax"`
	RecipesMin   int    `json:"recipesMin"`
	RecipesMax   int    `json:"recipesMax"`
	ScrollsMin   int    `json:"scrollsMin"`
	ScrollsMax   int    `json:"scrollsMax"`
}

/*
================
Monster
================
*/
type Monster struct {
	ID       uint32 `json:"id"`
	Codename string `json:"codename"`
	Name     string `json:"name"`
	Level    int    `json:"level"`
	HP       uint32 `json:"hp"`
	Grade    string `json:"grade" enum:"grade"`
	// Country is the drop country (RefObjCommon.Country): 0 China, 1
	// Europe, 3 either at random.
	Country uint8  `json:"country"`
	Model   string `json:"model,omitempty"`
	// Portrait is filled by the Node stage's portrait pass.
	Portrait string `json:"portrait,omitempty" doc:"The monster's portrait, portraits/<id>.webp beside this file: a transparent still of its model from the game's own renderer (the viewer). Absent when the build drew none."`
	// Regions are the world regions with a nest of this monster.
	Regions  []uint16         `json:"regions"`
	Variants []MonsterVariant `json:"variants"`
	// Table keys droptables equipment[] (level/country); the consumable
	// tables are keyed family/level.
	Table  string        `json:"table"`
	Drops  []MonsterDrop `json:"drops"`
	Unique *UniqueBonus  `json:"unique,omitempty"`
	Standing
}

/*
================
MonstersFile
================
*/
type MonstersFile struct {
	Header
	Monsters []Monster `json:"monsters"`
}

/*
================
MonsterSource
================
*/
type MonsterSource struct {
	Template monster.Template
	ItemID   func(codename string) uint32
	// Skills resolves the default skills whose summons a placed unique
	// brings into the world (enterworld.MonsterSummonClosure).
	Skills enterworld.SkillDataSource
}

/*
================
BuildMonsters

Every monster the shard can put into the world (producibleMonsters), in id
order. A retail row nothing spawns (a unique's other incarnations, the
_L2/_L3 rows the client mirror carries only so it could draw them) is not
listed: the site describes this shard, not the reference tables.
================
*/
func BuildMonsters(source MonsterSource) ([]Monster, error) {
	produced, err := producibleMonsters(source)
	if err != nil {
		return nil, err
	}
	regions := map[uint32]map[uint16]bool{}
	promotes := map[uint32]bool{}
	for _, nest := range source.Template.Nests {
		// A nest with no instance cap never spawns (Template.SpawnableRefs).
		if nest.InstanceLimit() == 0 {
			continue
		}
		if regions[nest.RefObjID] == nil {
			regions[nest.RefObjID] = map[uint16]bool{}
		}
		regions[nest.RefObjID][nest.RegionID] = true
		if nest.HasChampionTactics && nest.ChampionGenPercentage > 0 {
			promotes[nest.RefObjID] = true
		}
	}
	var monsters []Monster
	for id, ref := range source.Template.Refs {
		if ref.Structure || produced[id] == "" {
			continue
		}
		m := Monster{ID: id, Codename: ref.Codename, Name: ref.Name, Level: int(ref.Level), HP: ref.MaxHP,
			Grade: gradeName(ref.MonsterType), Country: ref.Country, Model: strings.ToLower(strings.ReplaceAll(ref.ModelPath, "\\", "/")), Regions: sortedRegions(regions[id]),
			Variants: monsterVariants(ref, promotes[id]), Table: equipmentTableKey(ref.Level, ref.Country), Drops: []MonsterDrop{}}
		if m.Name == "" {
			m.Name = ref.Codename
		}
		drops, err := assignedDrops(ref.Codename, source.ItemID)
		if err != nil {
			return nil, err
		}
		m.Drops = drops
		m.Unique = uniqueBonus(ref)
		m.Standing = applyGaps("monster", id, Standing{Status: StatusImplemented})
		monsters = append(monsters, m)
	}
	sort.Slice(monsters, func(i, j int) bool { return monsters[i].ID < monsters[j].ID })
	return monsters, nil
}

/*
================
producibleMonsters

The world monsters the server itself creates, from its own spawn sources:
  - a nest with an instance cap (the population's nests);
  - a quest script's monster (quest.ScriptMonsterCodenames);
  - a summon child of a placed unique's default skills, transitively, as the
    unique encounter roster resolves it (enterworld.MonsterSummonClosure),
    seeded only with uniques one of the sources above produces.

Two creators are left out on purpose. A GM's LOADMONSTER can create any row
and is not a player-facing source. Caravan bandits (the caravan ambush) are the four
job families MOB_THIEF_NPC, MOB_HUNTER_NPC and their EU kin, 1250 level
rows each: real, but a list of them is a site design question, not a
monster page per row, and the catalogue never listed them.
Each reference maps to the first source that admits it, in that order.
================
*/
func producibleMonsters(source MonsterSource) (map[uint32]string, error) {
	template := source.Template
	produced := map[uint32]string{}
	admit := func(id uint32, from string) {
		if produced[id] == "" {
			produced[id] = from
		}
	}
	for _, nest := range template.Nests {
		if nest.InstanceLimit() > 0 {
			admit(nest.RefObjID, SourceNest)
		}
	}
	byCodename := map[string]uint32{}
	for id, ref := range template.Refs {
		byCodename[ref.Codename] = id
	}
	for _, codename := range quest.ScriptMonsterCodenames() {
		id, ok := byCodename[codename]
		if !ok {
			return nil, fmt.Errorf("quest script monster %s has no reference", codename)
		}
		admit(id, SourceQuest)
	}
	var summoners []uint32
	for id := range produced {
		if monster.UniqueSummonPolicy(template.Refs[id].Codename) != monster.NoSummonPolicy {
			summoners = append(summoners, id)
		}
	}
	summoned, err := enterworld.MonsterSummonClosure(template, source.Skills, summoners)
	if err != nil {
		return nil, err
	}
	for _, id := range summoned {
		admit(id, SourceSummon)
	}
	return produced, nil
}

// The spawn sources producibleMonsters reads, as its result names them.
const (
	SourceNest   = "nest"
	SourceQuest  = "quest"
	SourceSummon = "summon"
)

/*
================
gradeName
================
*/
func gradeName(grade uint8) string {
	if name, ok := gradeNames[grade&0x0f]; ok {
		return name
	}
	return "normal"
}

/*
================
monsterVariants

The spawn grade roll: an authored grade spawns as itself; an ordinary monster spawns
normal, as a champion or giant from a nest with champion tactics, and as a
party monster from an armed nest unless it is a quest monster. A nest
that forces its own grade (RarityOverride) is per position, not per
reference, so it is not listed here.
================
*/
func monsterVariants(ref monster.MonsterRef, promotes bool) []MonsterVariant {
	grades := []uint8{ref.MonsterType}
	if ref.MonsterType == gradeNormal {
		if promotes {
			grades = append(grades, gradeChampion, gradeGiant)
		}
		if ref.TypeID4 != questMonsterTypeID4 {
			grades = append(grades, gradeParty)
		}
	}
	out := make([]MonsterVariant, 0, len(grades))
	for _, grade := range grades {
		capacity, passes, attempts := loot.MonsterDropBudget(grade, ref.Codename)
		goldMin, goldMax, _ := action.MonsterGoldHeapRange(ref.Level, grade)
		out = append(out, MonsterVariant{Grade: gradeName(grade), Party: grade>>4 == 1, Capacity: capacity, Passes: passes,
			Attempts: attempts, GoldMin: goldMin, GoldMax: goldMax})
	}
	return out
}

/*
================
assignedDrops
================
*/
func assignedDrops(codename string, itemID func(string) uint32) ([]MonsterDrop, error) {
	rows, err := loot.AssignedDropChances(codename)
	if err != nil {
		return nil, err
	}
	out := []MonsterDrop{}
	for _, row := range rows {
		id := itemID(row.Codename)
		if id == 0 {
			continue
		}
		out = append(out, MonsterDrop{ItemID: id, Plus: row.Plus, Chance: row.Chance, TriesMin: int(row.Min), TriesMax: int(row.Max),
			PerKill: row.PerKill, Group: row.Group})
	}
	return out, nil
}

/*
================
uniqueBonus

The counts prepareUniqueDrops rolls for a monster whose grade runs the
prepass (action.RollsUniqueDrops, UniqueDropCounts); its two equipment
batches are added together.
================
*/
func uniqueBonus(ref monster.MonsterRef) *UniqueBonus {
	if !action.RollsUniqueDrops(ref.MonsterType) {
		return nil
	}
	c := action.UniqueDropCounts(ref.MonsterType, ref.Codename)
	return &UniqueBonus{Table: equipmentTableKey(ref.Level, ref.Country),
		EquipmentMin: c[0].Min + c[1].Min, EquipmentMax: c[0].Max + c[1].Max,
		RecipesMin: c[2].Min, RecipesMax: c[2].Max, ScrollsMin: c[3].Min, ScrollsMax: c[3].Max}
}

/*
================
equipmentTableKey
================
*/
func equipmentTableKey(level, country uint8) string {
	return fmt.Sprintf("%d/%d", level, country)
}

/*
================
sortedRegions
================
*/
func sortedRegions(set map[uint16]bool) []uint16 {
	out := make([]uint16, 0, len(set))
	for region := range set {
		out = append(out, region)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

/*
================
MonsterStandings
================
*/
func MonsterStandings(monsters []Monster) []Standing {
	out := make([]Standing, len(monsters))
	for i, m := range monsters {
		out[i] = m.Standing
	}
	return out
}
