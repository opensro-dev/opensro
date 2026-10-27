/*
===========================================================================

player_parameter.go - one player parameter without the whole projection

PlayerStatsWithModifiers builds every equipment, passive and magic-option
write to answer all of a player's parameters. Some callers need one
parameter every tick: the entry snapshot of every session reads action
speed (0x8C), and that walk was most of the tick (2026-10-11 chase bench:
25.5% of CPU). A parameter no other parameter feeds is fixed by the writes
that name it alone, so it is evaluated on one keeper element from those:
its base write, the effect writes and the abnormal writes that name it, in
the full projection's order. The abnormal block writes stored constants
only (applyAbnormalModifiers), so no other parameter's value reaches it.
Every write the full projection would refuse for its shape (an undefined
parameter, a reserved or source-0 source, a bad channel, a non-finite
value) is refused here too. One case differs: an evaluation error on
another parameter, downstream of an abnormal write to it, fails the full
projection but not this one, which does not depend on it.

===========================================================================
*/
package combat

import (
	"fmt"
	"math"
	"sync"

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
	shape, err := playerParameterShape()
	if err != nil {
		return 0, err
	}
	if _, defined := shape.definitions[id]; !defined {
		return 0, fmt.Errorf("paramkeeper: undefined parameter %d", id)
	}
	if shape.linked[id] {
		return 0, fmt.Errorf("combat: parameter %d is fed by another parameter", id)
	}
	var writes []paramkeeper.Write
	for _, w := range playerBaseWrites(level) {
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
	for _, w := range writes {
		if shape.reserved[w.Source] {
			return 0, fmt.Errorf("paramkeeper: external source %d collides with parameter identity", w.Source)
		}
	}
	element, err := paramkeeper.New(shape.definitions[id])
	if err != nil {
		return 0, err
	}
	for _, w := range writes {
		if err = applyElementWrite(element, w.Channel, w.Source, w.Value); err != nil {
			return 0, err
		}
	}
	if block != nil {
		// The full projection applies every abnormal write; one it would
		// refuse for its shape fails this call too, whichever parameter it
		// names.
		for _, m := range block.Modifiers {
			if m.Used {
				if err = shape.checkAbnormalWrite(m); err != nil {
					return 0, fmt.Errorf("combat: abnormal write to parameter %d: %w", m.Param, err)
				}
			}
		}
		for _, m := range block.Modifiers {
			if !m.Used || m.Param != id {
				continue
			}
			if err = applyElementWrite(element, paramkeeper.Channel(m.Channel), m.Source, m.Value); err != nil {
				return 0, fmt.Errorf("combat: abnormal write to parameter %d: %w", m.Param, err)
			}
		}
	}
	return element.Value()
}

/*
==================
applyElementWrite

One write as the graph applies it: a write that changes the element is
evaluated at once (the graph's propagate step), so a value that
overflows on the way fails the read even when a later write would bring it
back.
==================
*/
func applyElementWrite(element *paramkeeper.Element, channel paramkeeper.Channel, source uint32, value float32) error {
	changed, err := element.Apply(channel, source, value)
	if err != nil || !changed {
		return err
	}
	_, err = element.Value()
	return err
}

/*
==================
playerShape

The closure's static shape, from the same definitions and link table the
graph is built with: each parameter's definition, the source identities
reserved for parameter nodes, and the parameters a link feeds.
==================
*/
type playerShape struct {
	definitions map[uint16]paramkeeper.Definition
	reserved    map[uint32]bool
	linked      map[uint16]bool
}

var (
	playerShapeOnce  sync.Once
	playerShapeValue playerShape
	playerShapeErr   error
)

/*
==================
playerParameterShape

Built once: the definitions (paramkeeper.NativeDefinition, a compiled
table) and the link table are boot-static, so the shape never changes
while the process runs.
==================
*/
func playerParameterShape() (playerShape, error) {
	playerShapeOnce.Do(func() {
		definitions, err := playerParameterDefinitions()
		if err != nil {
			playerShapeErr = err
			return
		}
		shape := playerShape{definitions: map[uint16]paramkeeper.Definition{}, reserved: map[uint32]bool{},
			linked: map[uint16]bool{}}
		for _, d := range definitions {
			shape.definitions[d.ID] = d.Definition
			shape.reserved[d.SourceKey] = true
		}
		for _, link := range playerParameterLinks() {
			shape.linked[link[1]] = true
		}
		playerShapeValue = shape
	})
	return playerShapeValue, playerShapeErr
}

/*
==================
playerShape.checkAbnormalWrite

What the graph would refuse in one abnormal write before evaluating
anything: an undefined parameter, a reserved source, a bad channel or a
non-finite value.
==================
*/
func (shape playerShape) checkAbnormalWrite(m abnormal.Modifier) error {
	if _, defined := shape.definitions[m.Param]; !defined {
		return fmt.Errorf("paramkeeper: undefined parameter %d", m.Param)
	}
	if shape.reserved[m.Source] {
		return fmt.Errorf("paramkeeper: external source %d collides with parameter identity", m.Source)
	}
	if paramkeeper.Channel(m.Channel) > paramkeeper.FactorProduct || math.IsNaN(float64(m.Value)) || math.IsInf(float64(m.Value), 0) {
		return fmt.Errorf("paramkeeper: invalid channel %d or non-finite modifier", m.Channel)
	}
	return nil
}
