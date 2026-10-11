/*
===========================================================================

player_parameter.go - one player parameter without the whole projection

PlayerStatsWithModifiers builds every equipment, passive and magic-option
write to answer all of a player's parameters. Some callers need one
parameter every tick: the entry snapshot of every session reads action
speed (0x8C), and that walk was most of the tick (2026-10-11 chase bench:
25.5% of CPU). A parameter no other parameter feeds is fixed by the writes
that name it alone, so it is evaluated from those, through the same graph,
validation and abnormal application as the full projection.

===========================================================================
*/
package combat

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
==================
playerSnapshotCore

The level, high-water level and base stats every player projection reads,
validated once for the full projection and for a single parameter.
==================
*/
func playerSnapshotCore(character *domain.Character) (level, maxLevel uint8, strength, intellect float64, err error) {
	level, err = requiredLevel("level", character.Level)
	if err != nil {
		return 0, 0, 0, 0, err
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
	maxLevel, err = requiredLevel("maxLevel", maxLevelSource)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if maxLevel < level {
		return 0, 0, 0, 0, fmt.Errorf("combat: maxLevel %d is below current level %d", maxLevel, level)
	}
	if strength, err = requiredStat("strength", character.Strength); err != nil {
		return 0, 0, 0, 0, err
	}
	if intellect, err = requiredStat("intellect", character.Intellect); err != nil {
		return 0, 0, 0, 0, err
	}
	return level, maxLevel, strength, intellect, nil
}

/*
==================
applyAbnormalModifiers

The abnormal block's writes (source 5, or 0 for frostbite's 8C base
replacement), applied after every other owner as the callbacks write into
the live keeper.
==================
*/
func applyAbnormalModifiers(graph *paramkeeper.Graph, block *abnormal.Block) error {
	if block == nil {
		return nil
	}
	for _, m := range block.Modifiers {
		if !m.Used {
			continue
		}
		if _, err := graph.Apply(m.Param, paramkeeper.Channel(m.Channel), m.Source, m.Value); err != nil {
			return fmt.Errorf("combat: abnormal write to parameter %d: %w", m.Param, err)
		}
	}
	return nil
}

/*
==================
PlayerUnlinkedParameter

One parameter that no other parameter feeds (no Link targets it), as
PlayerStatsWithModifiers would leave it: the graph's own base write for it,
the effect modifiers that name it and the abnormal block. Equipment,
passives and magic options write no parameter this serves;
TestUnlinkedParameterMatchesFullProjection holds that on shipped data. A
linked parameter is refused, never approximated.
==================
*/
func PlayerUnlinkedParameter(
	character *domain.Character,
	modifiers []paramkeeper.Write,
	block *abnormal.Block,
	id uint16,
) (float32, error) {
	if character == nil {
		return 0, fmt.Errorf("combat: missing character snapshot")
	}
	level, _, _, _, err := playerSnapshotCore(character)
	if err != nil {
		return 0, err
	}
	graph, base, err := playerParameterGraph(level)
	if err != nil {
		return 0, err
	}
	if graph.Linked(id) {
		return 0, fmt.Errorf("combat: parameter %d is fed by another parameter", id)
	}
	var writes []paramkeeper.Write
	for _, w := range base {
		if w.Parameter == id {
			writes = append(writes, w)
		}
	}
	for _, w := range modifiers {
		// The full projection refuses a source-0 effect write; so does this.
		if w.Source == 0 {
			return 0, fmt.Errorf("combat: effect source %d aliases a base/equipment/passive owner", w.Source)
		}
		if w.Parameter == id {
			writes = append(writes, w)
		}
	}
	if err = graph.ApplyBatch(writes); err != nil {
		return 0, err
	}
	if err = applyAbnormalModifiers(graph, block); err != nil {
		return 0, err
	}
	return graph.Value(id)
}
