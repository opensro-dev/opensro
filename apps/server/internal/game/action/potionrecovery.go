/*
===========================================================================

potionrecovery.go - queued potion pulses on the resident character clock

4A7042 registers a one-second timer; 52AA90 dispatches 49A510 once per
callback. Vitals commit through the ordinary authority door and reductions
are evaluated afresh, so curing Panic affects the remaining potion pulses.

===========================================================================
*/

package action

import (
	"math"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/recovery"
	"opensro.online/server/internal/game/world/simulation"
)

const potionRecoveryIntervalMs int64 = 1000

/*
================
admitPotionRecovery

The division and character doors are held. A direct server operation can
precede resident binding; its provisional session owns only potion pulses.
Authenticated binding replaces that lifetime with the ordinary resident.
================
*/
func (rt *Runtime) admitPotionRecovery(division string, c *enterworld.Character, in recovery.Admission, now int64) recovery.Amount {
	rt.recoveryMu.Lock()
	defer rt.recoveryMu.Unlock()
	key := recoveryKey{division, strings.ToLower(c.Name)}
	state := rt.recoverySessions[key]
	if state == nil || state.character != c {
		state = &recoverySession{character: c, nextMs: math.MaxInt64, questNextMs: math.MaxInt64,
			questItemNextMs: math.MaxInt64, potionNextMs: now + potionRecoveryIntervalMs}
		if rt.recoverySessions == nil {
			rt.recoverySessions = make(map[recoveryKey]*recoverySession)
		}
		rt.recoverySessions[key] = state
	}
	return state.potions.Admit(in)
}

/*
================
clearPotionRecovery
================
*/
func (rt *Runtime) clearPotionRecovery(division, name string) {
	rt.recoveryMu.Lock()
	defer rt.recoveryMu.Unlock()
	if state := rt.recoverySessions[recoveryKey{division, strings.ToLower(name)}]; state != nil {
		state.potions = recovery.Queue{}
	}
}

/*
================
recoverPotionResident

Keep overdue timer phase and process one pulse per distinct host update.
Queued healing bypasses Zombie's item-admission reversal, as 49A510 does.
================
*/
func (rt *Runtime) recoverPotionResident(key recoveryKey, now int64) []simulation.DivisionFrames {
	rt.recoveryMu.Lock()
	state := rt.recoverySessions[key]
	if state == nil || now < state.potionNextMs || now <= state.potionLastMs {
		rt.recoveryMu.Unlock()
		return nil
	}
	state.potionNextMs += potionRecoveryIntervalMs
	state.potionLastMs = now
	credit := state.potions.Tick()
	if state.session == 0 && state.potions.Empty() {
		delete(rt.recoverySessions, key)
	}
	rt.recoveryMu.Unlock()
	if credit.HP == 0 && credit.MP == 0 {
		return nil
	}
	c := state.character
	var frames []simulation.DivisionFrames
	committed := rt.deps.Update(c, "potion-recovery", func() bool {
		if c.DeletePending || !enterworld.CharacterAlive(c) {
			rt.clearPotionRecovery(key.division, c.Name)
			return false
		}
		stats, _, err := rt.playerCombatStats(key.division, c)
		if err != nil {
			log.WithError(err).Error("potion recovery keeper projection failed")
			return false
		}
		hpMaximum, _ := stats.Param(itemParamMaxHP)
		mpMaximum, _ := stats.Param(itemParamMaxMP)
		hpReduction, _ := stats.Param(combat.HPRecoveryReductionParameter)
		mpReduction, _ := stats.Param(combat.MPRecoveryReductionParameter)
		hp, mp := clampKeeperVital(c.CurrentHP, int64(hpMaximum)), clampKeeperVital(c.CurrentMP, int64(mpMaximum))
		nextHP := combat.RecoverVital(hp, int64(hpMaximum), credit.HP, hpReduction)
		nextMP := combat.RecoverVital(mp, int64(mpMaximum), credit.MP, mpReduction)
		if nextHP == hp && nextMP == mp {
			return false
		}
		c.CurrentHP, c.CurrentMP = &nextHP, &nextMP
		gid := enterworld.ObjectIDForCharacter(c)
		frames = append(frames, simulation.DivisionFrames{DivisionID: key.division, OnlyCharacterID: c.ID, Frames: []simulation.Frame{{Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshPayload(gid, simulation.Vitals{CurrentHP: uint32(nextHP), CurrentMP: uint32(nextMP)})}}})
		if nextHP != hp {
			frames = append(frames, simulation.DivisionFrames{DivisionID: key.division, SourceGID: gid, Frames: []simulation.Frame{{Opcode: simulation.OpVitalsUpdate,
				Payload: simulation.HPRefreshPayload(gid, 0, uint32(nextHP))}}})
		}
		return true
	})
	if !committed {
		return nil
	}
	return frames
}
