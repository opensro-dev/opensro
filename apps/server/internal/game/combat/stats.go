/*
===========================================================================

stats.go - combat stat snapshots for players and monsters

Package combat owns immutable combat-stat snapshots and the v1.188
CFormulae translation. It never owns live HP; simulation.MonsterState remains
the sole mutation authority.

===========================================================================
*/
package combat

import (
	"fmt"
	"math"
	"strconv"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/durability"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
Stats

Stats is the ParamKeeper 1..16 subset read by the v1.188 damage helpers.
Values are a detached snapshot: formula evaluation cannot retain a live
character or monster pointer.
==================
*/
type Stats struct {
	PhysicalBasicRate, PhysicalSkillRate, MagicalBasicRate, MagicalSkillRate float64
	Berserk                                                                  bool // body-mode1 contributes native result flag4, not attack stats
	StealthStrike                                                            bool // command issued in body mode 6: hit flag 8 (58EDD7)
	// Native B2..B5 final lane factors. Zero is the native no-modifier sentinel.
	PhysicalOutgoing, MagicalOutgoing, PhysicalIncoming, MagicalIncoming float32
	// Native AE..B1 received-damage factors by lane and attack kind (odar,
	// dara), the defender's counterpart of 80..83. Zero means no modifier.
	PhysicalBasicTaken, PhysicalSkillTaken, MagicalBasicTaken, MagicalSkillTaken float32
	masteries                                                                    []domain.CharacterMastery       // private copied snapshot; never aliases live authority
	MotionState                                                                  uint8                           // live authority snapshot; native state 8 is knocked down
	AbnormalMask                                                                 uint32                          // character +0xD34, the published abnormal mask; atca 58F52F tests it
	SkillParameters                                                              enterworld.SkillParameterValues // learned native dictionary; not displayed stats
	// StatusResistance is the learned status-resistance buckets real fills,
	// indexed like abnormal.Source.Resist (59DE50).
	StatusResistance [17]abnormal.Resistance
	// graph is the player's evaluated keeper; nil for monster snapshots. It is
	// built once per snapshot and never mutated afterwards.
	graph    *paramkeeper.Graph
	Level    uint8
	MaxLevel uint8

	Strength  float64
	Intellect float64

	PhysicalDefense float64 // Param 5
	MagicalDefense  float64 // Param 6
	ParryRate       float64 // Param 7
	MagicalParry    float64 // Param 8
	EvasionRate     float64 // Param 9
	BlockRate       float64 // Param 10
	HitRate         float64 // Param 11
	CriticalRate    float64 // Param 12

	PhysicalAttackMin float64 // Param 13
	PhysicalAttackMax float64 // Param 14
	MagicalAttackMin  float64 // Param 15
	MagicalAttackMax  float64 // Param 16
}

/*
================
Loadout

Equipment identity used to admit an action beside its detached stat snapshot.
================
*/
type Loadout struct {
	WeaponKind  uint8
	ActionRange float64
	HasWeapon   bool
}

/*
================
Catalogs

Immutable reference sources read while reconstructing a player's keeper.
================
*/
type Catalogs struct {
	Items        enterworld.ItemRefSource
	Skills       enterworld.SkillDataSource
	MagicOptions enterworld.MagicOptionSource
}

/*
================
Param

Read the evaluated player keeper. Monster snapshots and parameters outside
the player closure read as absent.
================
*/
func (s Stats) Param(id uint16) (float32, bool) {
	if s.graph == nil {
		return 0, false
	}
	v, err := s.graph.Value(id)
	return v, err == nil
}

/*
==================
PlayerStats

PlayerStats snapshots a character's base ParamKeeper graph and every
equipped item's derived variance stats. The supplied character must already
be a Character.Snapshot taken under the authority read door.
==================
*/
func PlayerStats(character *domain.Character, catalogs Catalogs) (Stats, Loadout, error) {
	return PlayerStatsWithModifiers(character, catalogs, nil, nil)
}

/*
==================
PlayerStatsWithModifiers

PlayerStatsWithModifiers applies installation-time effect writes through the
same dependency graph as equipment and passives. The effect owner supplies
an immutable snapshot with independent source identities; this function does
not infer lifetimes, rebuild caps from current stats, or allocate identities.

block carries the abnormal-state writes (source 5, or 0 for frostbite's 8C
base replacement), applied after every other owner as the callbacks write
into the live keeper.
==================
*/
func PlayerStatsWithModifiers(
	character *domain.Character,
	catalogs Catalogs,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
) (Stats, Loadout, error) {
	items, skills := catalogs.Items, catalogs.Skills
	var out Stats
	var loadout Loadout
	if character == nil {
		return out, loadout, fmt.Errorf("combat: missing character snapshot")
	}
	level, err := requiredLevel("level", character.Level)
	if err != nil {
		return out, loadout, err
	}
	// MaxLevel is an optional historical-high-water mark. Current records do
	// not persist it unless it differs from the normal level projection; the
	// entered-avatar wire uses Level as its exact fallback (bootstrap/wire.go).
	// Combat must consume the same canonical value instead of making an
	// omitted, optional field silently disable every attack.
	maxLevelSource := character.MaxLevel
	if maxLevelSource == nil {
		maxLevelSource = character.Level
	}
	maxLevel, err := requiredLevel("maxLevel", maxLevelSource)
	if err != nil {
		return out, loadout, err
	}
	if maxLevel < level {
		return out, loadout, fmt.Errorf(
			"combat: maxLevel %d is below current level %d",
			maxLevel,
			level,
		)
	}
	strength, err := requiredStat("strength", character.Strength)
	if err != nil {
		return out, loadout, err
	}
	intellect, err := requiredStat("intellect", character.Intellect)
	if err != nil {
		return out, loadout, err
	}

	out = Stats{
		Level:    level,
		MaxLevel: maxLevel,
		Berserk:  character.NativeBodyStatus == 1,
		// Native reads body mode 6 once per command (4ACE9E); this reads it
		// per strike, which differs only for a command that outlives stealth.
		StealthStrike: character.NativeBodyStatus == 6,
	}
	graph, writes, err := playerParameterGraph(level)
	if err != nil {
		return Stats{}, Loadout{}, err
	}
	writes = append(writes, paramkeeper.Write{Parameter: 0, Value: float32(level)},
		paramkeeper.Write{Parameter: 1, Value: float32(strength)}, paramkeeper.Write{Parameter: 2, Value: float32(intellect)})

	out.masteries = append([]domain.CharacterMastery(nil), character.Masteries...)
	if items == nil {
		return Stats{}, Loadout{}, fmt.Errorf("combat: item reference source is unavailable")
	}
	var passiveWeaponKind uint8
	seenSlots := make(map[int64]bool)
	for _, row := range character.MissionInventory {
		if row.Slot < 0 || row.Slot >= int64(inventory.EquipmentSlotEnd) {
			continue
		}
		if seenSlots[row.Slot] {
			return Stats{}, Loadout{}, fmt.Errorf("combat: duplicate equipped slot %d", row.Slot)
		}
		seenSlots[row.Slot] = true
		ref, ok := items.ItemRefByCodename(row.Codename)
		if !ok || ref == nil {
			return Stats{}, Loadout{}, fmt.Errorf(
				"combat: equipped item %q has no v1.150 reference row",
				row.Codename,
			)
		}
		if ref.RefObjID != row.RefObjID {
			return Stats{}, Loadout{}, fmt.Errorf(
				"combat: equipped item %q ref id %d does not match itemdata id %d",
				row.Codename,
				row.RefObjID,
				ref.RefObjID,
			)
		}
		if row.TypeFlags != 0 && row.TypeFlags != ref.TypeFlags() {
			return Stats{}, Loadout{}, fmt.Errorf(
				"combat: equipped item %q type flags %#x do not match itemdata %#x",
				row.Codename,
				row.TypeFlags,
				ref.TypeFlags(),
			)
		}
		// The native socket map deliberately shares equipment socket 7
		// between shields (TID 3.1.4) and arrows/bolts (TID 3.3.4).
		// Ammunition is live equipment state but it contributes no ParamKeeper
		// stats; its presence/count is admitted by action at shot commit.
		if ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 3 && ref.TypeIDs[2] == 4 {
			continue
		}
		if durability.Broken(ref.TypeFlags(), row.Durability == 0) {
			continue
		}
		// reqi reads slot 6 through 4EAD40 and CGObjPC +5D0 (4EC5A0).
		// Both reject item+190, whose 495980/496D90 producer marks depleted
		// durability. An unavailable/broken weapon must not activate a passive.
		if row.Slot == 6 && ref.TypeIDs[0] == 3 && ref.TypeIDs[1] == 1 && ref.TypeIDs[2] == 6 {
			passiveWeaponKind = uint8(ref.TypeIDs[3])
		}
		if ref.Combat == nil {
			return Stats{}, Loadout{}, fmt.Errorf(
				"combat: equipped item %q has an incomplete combat reference",
				row.Codename,
			)
		}
		varianceBits, parseErr := strconv.ParseUint(row.VarianceBits, 10, 64)
		if parseErr != nil {
			return Stats{}, Loadout{}, fmt.Errorf(
				"combat: equipped item %q has invalid variance bits: %w",
				row.Codename,
				parseErr,
			)
		}
		contribution, weapon, deriveErr := deriveItemStats(
			ref,
			varianceBits,
			uint8(clampInt64(row.Plus, 0, math.MaxUint8)),
		)
		if deriveErr != nil {
			return Stats{}, Loadout{}, deriveErr
		}
		options, err := resolveMagicOptions(row.Codename, row.MagicOptions, catalogs.MagicOptions)
		if err != nil {
			return Stats{}, Loadout{}, err
		}
		// 495CA0: CalculateBaseStats, then ApplyMagicOptions scales the
		// item's own HR (+1D8) and ER (+19C) before any keeper insertion.
		applyItemMagicOptions(&contribution, options)
		source := uint32(1024 + row.Slot)
		writes = append(writes, statWrites(contribution, source)...)
		writes = append(writes, equipmentReinforcementWrites(ref, varianceBits)...)
		writes = append(writes, equipmentResourceWrites(ref.TypeFlags(), source)...)
		optionWrites, err := magicOptionWrites(row.Codename, options, source)
		if err != nil {
			return Stats{}, Loadout{}, err
		}
		writes = append(writes, optionWrites...)
		if weapon {
			if loadout.HasWeapon {
				return Stats{}, Loadout{}, fmt.Errorf(
					"combat: more than one weapon-family item is equipped",
				)
			}
			loadout.HasWeapon = true
			loadout.WeaponKind = uint8(ref.TypeIDs[3])
			loadout.ActionRange = ref.Combat.ActionRange
		}
	}
	passives, power, err := learnedPassives(character, skills, catalogs.Items, passiveWeaponKind)
	if err != nil {
		return Stats{}, Loadout{}, err
	}
	out.SkillParameters = power
	out.StatusResistance = learnedStatusResistance(character, skills, catalogs.Items)
	writes = append(writes, passives...)
	// Source identity belongs to an application, not a parameter number. A
	// modifier must not alias even a different parameter of a static owner.
	staticSources := make(map[uint32]struct{}, len(writes))
	for _, w := range writes {
		staticSources[w.Source] = struct{}{}
	}
	for _, w := range modifiers {
		if _, collision := staticSources[w.Source]; collision || w.Source == 0 {
			return Stats{}, Loadout{}, fmt.Errorf("combat: effect source %d aliases a base/equipment/passive owner", w.Source)
		}
	}
	writes = append(writes, modifiers...)
	if err = graph.ApplyBatch(writes); err != nil {
		return Stats{}, Loadout{}, err
	}
	if block != nil {
		for _, m := range block.Modifiers {
			if !m.Used {
				continue
			}
			if _, err = graph.Apply(m.Param, paramkeeper.Channel(m.Channel), m.Source, m.Value); err != nil {
				return Stats{}, Loadout{}, fmt.Errorf("combat: abnormal write to parameter %d: %w", m.Param, err)
			}
		}
	}
	out.graph = graph
	if err = readParameterStats(graph, &out); err != nil {
		return Stats{}, Loadout{}, err
	}
	return out, loadout, nil
}

/*
==================
PlayerBaseStats

PlayerBaseStats projects the canonical ParamKeeper snapshot onto the eight
combat fields carried by 0x343C. CIFPlayerInfo shows Param 9 as the retail
"Parry Ratio" row; Param 7 is the accessory absorption input used by the
damage formula and is not that UI value.

The packet fields are integers. Native ParamKeeper-to-wire conversion
truncates positive fractional values, so this boundary saturates before
converting instead of allowing an overflowing Go cast to wrap.
==================
*/
func PlayerBaseStats(character *domain.Character, catalogs Catalogs) (wire.BaseStats, error) {
	return PlayerBaseStatsWithModifiers(character, catalogs, nil, nil)
}

/*
================
PlayerBaseStatsWithModifiers

Publish the effect-aware projection used by damage calculations. Packet
truncation stays at this boundary, after all equipment and effect owners.
================
*/
func PlayerBaseStatsWithModifiers(
	character *domain.Character,
	catalogs Catalogs,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
) (wire.BaseStats, error) {
	stats, _, err := PlayerStatsWithModifiers(character, catalogs, modifiers, block)
	if err != nil {
		return wire.BaseStats{}, err
	}
	hp, _ := stats.Param(3)
	mp, _ := stats.Param(4)
	return wire.BaseStats{
		PhysicalAttackMin: clampDisplayU32(stats.PhysicalAttackMin),
		PhysicalAttackMax: clampDisplayU32(stats.PhysicalAttackMax),
		MagicalAttackMin:  clampDisplayU32(stats.MagicalAttackMin),
		MagicalAttackMax:  clampDisplayU32(stats.MagicalAttackMax),
		PhysicalDefense:   clampDisplayU16(stats.PhysicalDefense),
		MagicalDefense:    clampDisplayU16(stats.MagicalDefense),
		HitRate:           clampDisplayU16(stats.HitRate),
		ParryRate:         clampDisplayU16(stats.EvasionRate),
		// Params 3 and 4 are the keeper maxima (4E3294): item HP/MP options
		// and abnormal factors are already in the graph.
		MaxHP: clampDisplayU32(float64(hp)),
		MaxMP: clampDisplayU32(float64(mp)),
		// 75BE90 applies the final words on every refresh. Populate them
		// here so effect retirement cannot overwrite the login attributes
		// with zero when callers encode the projection directly.
		StrWord: clampDisplayU16(float64(*character.Strength)),
		IntWord: clampDisplayU16(float64(*character.Intellect)),
	}, nil
}

/*
================
clampDisplayU32

Saturate before converting the keeper's resource and attack values to wire.
================
*/
func clampDisplayU32(value float64) uint32 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(math.Trunc(value))
}

/*
================
clampDisplayU16

Keep defense and ratio fields inside their narrower packet representation.
================
*/
func clampDisplayU16(value float64) uint16 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(math.Trunc(value))
}

/*
================
MonsterStats

Translate a pinned RefObjChar row into the monster's combat parameters.
An incomplete catalog row must not silently create a defenseless monster.
================
*/
func MonsterStats(ref monster.MonsterRef) (Stats, error) {
	if !ref.CombatPinned {
		return Stats{}, fmt.Errorf(
			"combat: monster %q has no pinned RefObjChar combat tail",
			ref.Codename,
		)
	}
	return Stats{
		Level:           ref.Level,
		MaxLevel:        ref.Level,
		PhysicalDefense: ref.PhysicalDefense,
		MagicalDefense:  ref.MagicalDefense,
		ParryRate:       ref.ParryRate,
		MagicalParry:    ref.MagicalParry,
		EvasionRate:     ref.EvasionRate,
		BlockRate:       ref.BlockRate,
		HitRate:         ref.HitRate,
		CriticalRate:    ref.CriticalRate,
	}, nil
}

/*
================
requiredLevel

Validate persisted levels before narrowing them to the native byte field.
================
*/
func requiredLevel(name string, value *int64) (uint8, error) {
	if value == nil || *value < 1 || *value > math.MaxUint8 {
		return 0, fmt.Errorf("combat: %s is absent or outside 1..255", name)
	}
	return uint8(*value), nil
}

/*
================
requiredStat

Missing persisted attributes are load failures, not zero-valued characters.
================
*/
func requiredStat(name string, value *int64) (float64, error) {
	if value == nil || *value < 0 || *value > math.MaxUint16 {
		return 0, fmt.Errorf("combat: %s is absent or outside 0..65535", name)
	}
	return float64(*value), nil
}

/*
================
clampInt64

Clamp wide persisted values before the item derivation narrows their type.
================
*/
func clampInt64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
