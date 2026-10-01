/*
===========================================================================

movementeffect.go - effective player movement and observer speed publication

Active effects, body state and abnormal modifiers meet at the live mover.
The same committed speeds are sent to the client for interpolation.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"math"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	movementWalkParameter = 0x17
	movementRunParameter  = 0x18
	movementSpeedOpcode   = 0x376f
	movementSpeedBytes    = 12
)

/*
================
refreshMovementEffects

CGObjChar_RefreshMovementSpeeds (4AA410) reads the effective keeper pair.
Pending-stop rows remain installed until their replacement handoff or drain.
Abnormal writes apply to the resulting base, including transformed actors.
================
*/
func (rt *Runtime) refreshMovementEffects(division string, c *enterworld.Character, now int64) []wire.Frame {
	if rt.Worlds == nil {
		return nil
	}
	if c.ActiveCOS != nil && c.ActiveCOS.Mounted {
		// Mounted motion belongs to the COS keeper. A rider's haste or an
		// expiring body state cannot replace the vehicle's effective speeds.
		return rt.refreshCosAbnormalSpeed(rt.newCosAbnormalOwner(division, c, now))
	}
	factor := float32(1)
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.MovementPercent != 0 {
			factor = float32(float64(factor) * (1 + float64(float32(effect.MovementPercent))/100))
		}
	}
	for _, effect := range rt.effects.Snapshot(division, c.Name) {
		if effect.HidePenaltyPercent == 0 && effect.HideBonusPercent == 0 {
			continue
		}
		// PercentProduct reduces in source-key order: STSP's key 6 first.
		factor = float32(float64(factor) * (1 + float64(effect.HideBonusPercent)/100))
		factor = float32(float64(factor) * (1 - float64(effect.HidePenaltyPercent)/100))
	}
	// SPDU is the built-in mode-3 percentage channel, separate from haste.
	if c.NativeBodyStatus == 1 {
		factor *= 2
	}
	walk, run := float32(simulation.WalkSpeed)*factor, float32(simulation.RunSpeed)*factor
	if tw, tr, ok := rt.transformSpeeds(c); ok {
		walk, run = tw, tr
	}
	if block := rt.playerAbnormal(division, c.Name); block != nil {
		walkDefinition, _ := paramkeeper.NativeDefinition(movementWalkParameter)
		runDefinition, _ := paramkeeper.NativeDefinition(movementRunParameter)
		var err error
		walk, err = block.Evaluate(movementWalkParameter, walkDefinition, walk)
		if err != nil {
			log.WithError(err).Error("abnormal walk-speed projection failed")
			return nil
		}
		run, err = block.Evaluate(movementRunParameter, runDefinition, run)
		if err != nil {
			log.WithError(err).Error("abnormal run-speed projection failed")
			return nil
		}
	}
	changed := false
	rt.Worlds.Update(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(world *simulation.WorldState) { changed = world.UpdateMovementSpeeds(walk, run, now) })
	if !changed {
		return nil
	}
	payload := make([]byte, movementSpeedBytes)
	binary.LittleEndian.PutUint32(payload, enterworld.ObjectIDForCharacter(c))
	binary.LittleEndian.PutUint32(payload[4:], math.Float32bits(walk))
	binary.LittleEndian.PutUint32(payload[8:], math.Float32bits(run))
	return []wire.Frame{{Opcode: movementSpeedOpcode, Payload: payload}}
}
