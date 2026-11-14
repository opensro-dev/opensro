/*
===========================================================================

monstervitals.go - a monster's vitals to the players that hold it

The reference server never broadcasts a monster's HP. Once per tick, for a
monster whose HP or status moved, its vitals publisher broadcasts the
status alone (flags 4) and hands the change to the relations that hold the
monster with the vitals bit: each of their players receives a private
0x33A6 with the cause word, the HP and the status. The monster's cause
words and that payload are MonsterState's (simulation/monster_vitals.go);
this lane finds the holders.

A player holds a monster through the first of the seven relation slots a
player owns, bound with the vitals bit:
  - the object select (v1.150 0x745A) empties the slot and binds the
    selected character to it; the selection plane (select.go) is that
    slot;
  - a cast that begins in hit range does the same for its target, so a
    player who attacks without selecting still holds what it attacks
    (bindCastTarget).
Selecting anything else or releasing the target leaves the monster. The
slot also carries a range (320, or 910 for a monster the select bound),
but only a different refresh judges it, never the vitals publication, so
this lane keeps no range.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
vitalsTarget

A changed monster, as a holder's selection names it: its division and gid.
================
*/
type vitalsTarget struct {
	division string
	gid      uint32
}

/*
================
publishMonsterVitals

The action tick's share of the reference server's per-object tick: every
monster whose vitals changed since the last tick sends its private 0x33A6
to each player that holds it, then forgets the change. A changed monster nobody holds sends
nothing. Runs after every step that can change a monster's HP or status,
so a change made in this tick leaves in it.
================
*/
func (rt *Runtime) publishMonsterVitals(nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil || rt.Selected == nil {
		return nil
	}
	rt.monsterVitals = rt.Monsters.DrainChangedVitals(rt.monsterVitals[:0])
	if len(rt.monsterVitals) == 0 {
		return nil
	}
	if rt.vitalsTargets == nil {
		rt.vitalsTargets = make(map[vitalsTarget]int)
	}
	clear(rt.vitalsTargets)
	for index, changed := range rt.monsterVitals {
		rt.vitalsTargets[vitalsTarget{division: changed.DivisionID, gid: changed.Gid}] = index
	}
	rt.vitalsHolders = rt.Selected.holders(rt.vitalsHolders[:0])
	var out []simulation.DivisionFrames
	for _, holder := range rt.vitalsHolders {
		index, held := rt.vitalsTargets[vitalsTarget{division: holder.division, gid: holder.gid}]
		if !held {
			continue
		}
		characterID := rt.vitalsHolderID(holder)
		if characterID == 0 {
			continue
		}
		out = append(out, simulation.DivisionFrames{
			DivisionID:      holder.division,
			OnlyCharacterID: characterID,
			Frames:          []simulation.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: rt.monsterVitals[index].Payload}},
		})
	}
	// The selection plane is a map: order the private routes by their
	// recipient, so a tick's output does not depend on its walk.
	sort.Slice(out, func(i, j int) bool { return out[i].OnlyCharacterID < out[j].OnlyCharacterID })
	return out
}

/*
================
vitalsHolderID

The character id a holder's private frame routes to, or 0 once the
character left the division.
================
*/
func (rt *Runtime) vitalsHolderID(holder selectionHolder) int64 {
	unlock := rt.lockDivision(holder.division)
	defer unlock()
	c := rt.findCharacter(holder.division, holder.name)
	if c == nil {
		return 0
	}
	return c.ID
}

/*
================
bindCastTarget

A player's cast at a monster began in hit range: the monster takes the
player's first relation slot, as the reference server binds a cast's
target there once the cast passes its hit-range check. The bind is the
selection plane's, so the monster's vitals reach the caster from the next
tick on, whether or not the client selected it first.
================
*/
func (rt *Runtime) bindCastTarget(divisionID, characterName string, gid uint32) {
	if rt.Selected == nil {
		return
	}
	rt.Selected.Set(divisionID, characterName, gid)
}
