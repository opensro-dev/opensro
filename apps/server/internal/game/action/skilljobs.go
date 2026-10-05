/*
===========================================================================

skilljobs.go - online timed-effect checkpoints and actor admission

Persistent duration advances on the simulation clock and survives reconnect.
Admission restores companion binding and effects before bootstrap projection.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/world/simulation"
	"slices"
	"sync/atomic"
)

/*
================
checkpointSkillJobs
================
*/
func (rt *Runtime) checkpointSkillJobs(c *enterworld.Character, effects []statuseffect.Effect, now int64) bool {
	var jobs []domain.TimedSkillJob
	// Job ownership is independent of effect installation. An absent effect
	// (missing producer or refused restoration) does not prove job retirement.
	// Explicit teardown removes jobs in retireSkillJobs; resident expired or
	// stopped effects are excluded below without reviving their old checkpoint.
	for _, job := range c.TimedSkillJobs {
		resident := false
		for _, effect := range effects {
			if effect.Persistent && effect.SkillID == job.SkillID && effect.InstanceToken == job.Token {
				resident = true
				break
			}
		}
		if !resident && job.RemainingMs != 0 {
			jobs = append(jobs, job)
		}
	}
	for _, e := range effects {
		if e.Persistent && !e.StopRequested && !e.Expired(now) {
			jobs = append(jobs, domain.TimedSkillJob{SkillID: e.SkillID, Token: e.InstanceToken, RemainingMs: e.PersistentRemainingMs(now)})
		}
	}
	changed := len(jobs) != len(c.TimedSkillJobs)
	if !changed {
		for i := range jobs {
			if jobs[i] != c.TimedSkillJobs[i] {
				changed = true
				break
			}
		}
	}
	if changed {
		c.TimedSkillJobs = jobs
	}
	return changed
}

/*
================
retireSkillJobs
================
*/
func (rt *Runtime) retireSkillJobs(c *enterworld.Character, ended []statuseffect.Effect) bool {
	jobs := make([]domain.TimedSkillJob, 0, len(c.TimedSkillJobs))
	for _, job := range c.TimedSkillJobs {
		remove := false
		for _, e := range ended {
			if e.Persistent && e.InstanceToken == job.Token {
				remove = true
				break
			}
		}
		if !remove {
			jobs = append(jobs, job)
		}
	}
	if len(jobs) == len(c.TimedSkillJobs) {
		return false
	}
	c.TimedSkillJobs = jobs
	return true
}

// Called under the division lock before serializing entry. Duplicate bootstrap
// reads retain existing deadlines and tokens; reconnect cannot refresh duration.
/*
================
restoreSkillJobs
================
*/
func (rt *Runtime) restoreSkillJobs(division, name string) {
	c := rt.findCharacter(division, name)
	if c == nil || rt.deps.SkillData() == nil {
		return
	}
	rt.deps.Update(c, "restore-skill-jobs", func() bool {
		if len(c.TimedSkillJobs) == 0 {
			return false
		}
		jobs := append([]domain.TimedSkillJob(nil), c.TimedSkillJobs...)
		var restored []domain.TimedSkillJob
		c.TimedSkillJobs = nil
		for _, job := range jobs {
			if job.RemainingMs == 0 {
				continue
			}
			resident := false
			for _, effect := range rt.effects.Snapshot(division, name) {
				if effect.Persistent && effect.SkillID == job.SkillID {
					// Includes pending retirement: admission must not resurrect it.
					job.Token = effect.InstanceToken
					resident = true
					break
				}
			}
			if resident {
				restored = append(restored, job)
				continue
			}
			row, ok := rt.restorableSkillJob(job.SkillID)
			if !ok {
				log.WithFields(log.Fields{"division": division, "character": name, "skill": job.SkillID}).Error("timed skill job retained: execution producer unavailable")
				restored = append(restored, job)
				continue
			}
			if job.RemainingMs < row.EffectDurationMs {
				row.EffectDurationMs = job.RemainingMs
			}
			token := atomic.AddUint32(&rt.castTokenCounter, 1)
			if token == 0 {
				token = atomic.AddUint32(&rt.castTokenCounter, 1)
			}
			if _, applied := rt.commitCharacterEffectWithCheckpoint(division, c, row, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, rt.Now().UnixMilli(), false); applied {
				job.Token = token
				job.RemainingMs = row.EffectDurationMs / 1000 * 1000
			} else {
				log.WithFields(log.Fields{"division": division, "character": name, "skill": job.SkillID}).Error("timed skill job retained: effect installation refused")
			}
			restored = append(restored, job)
		}
		c.TimedSkillJobs = restored
		return !slices.Equal(jobs, restored)
	})
}

/*
================
restorableSkillJob
================
*/
func (rt *Runtime) restorableSkillJob(skillID uint32) (enterworld.SkillRow, bool) {
	if rt.deps.SkillData() == nil {
		return enterworld.SkillRow{}, false
	}
	row, ok := rt.deps.SkillData().SkillByID(skillID)
	return row, ok && row.TimedJobExecutable()
}

// Native keeper commits each 300 online seconds and on actor teardown. An
// abnormal process loss can recover the last checkpoint, as in that protocol.
/*
================
checkpointOnlineSkillJobs
================
*/
func (rt *Runtime) checkpointOnlineSkillJobs(now int64) {
	rt.maintenance.Lock()
	defer rt.maintenance.Unlock()
	for _, batch := range rt.effects.TakeJobCheckpoints(now) {
		if c := rt.findCharacter(batch.DivisionID, batch.CharacterName); c != nil {
			rt.deps.Update(c, "checkpoint-skill-jobs", func() bool {
				return rt.checkpointSkillJobs(c, batch.Effects, now)
			})
		}
	}
}

/*
================
EntryMovementSpeeds
================
*/
func (rt *Runtime) EntryMovementSpeeds(division, name string) (float32, float32) {
	c := rt.characterSnapshot(division, rt.findCharacter(division, name))
	if c == nil {
		return simulation.WalkSpeed, simulation.RunSpeed
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(division, name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	return world.MovementSpeeds()
}

// New authenticated admission restores jobs; in-world resets only project them.
/*
================
RestoreTimedSkillJobs
================
*/
func (rt *Runtime) RestoreTimedSkillJobs(division, name string) {
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.restoreCharacterCOS(division, name)
	rt.restoreSkillJobs(division, name)
}

// Claim runtime lifetime before projecting entry effects/speeds. A displaced
// socket can close while the replacement is still loading, before pet binding.
/*
================
AdmitCharacterSession
================
*/
func (rt *Runtime) AdmitCharacterSession(division, name string, session uint64) error {
	unlock := rt.lockDivision(division)
	defer unlock()
	if err := rt.admitPopulationSession(division, name, session); err != nil {
		return err
	}
	rt.restoreCharacterCOS(division, name)
	rt.restoreSkillJobs(division, name)
	rt.restorePKRecord(division, name)
	if character := rt.findCharacter(division, name); character != nil {
		rt.bindPetSession(division, character, session, false)
	}
	return nil
}
