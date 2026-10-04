/*
===========================================================================

skillwall.go - the Chinese Force walls (Crystal Wall, Fire Wall)

The cast installs the caster's persistent instance and fills the actor's
single wall slot (+0xC0C) with it, the pool loaded from pw word 1
(593662..5936A5). While the wall stands:

	hits      58E5F0 splits every monster hit: the wall's lanes land on a
	          second target entry (type-7 records), the defender's own
	          record loses them (combat.ResolveAgainstWall)
	drain     593C5E takes the impacts' absorbed total from the pool
	update    5851F7 retires the wall once the pool is 0; 585262 pays onff
	          word 1 MP every onff period or retires it
	footing   the wall's motion (0x11) refuses further ao / pw casts
	          (58E0EB), as sitting or riding does

The slot empties when the instance retires (5829D0 at 582DC0).

===========================================================================
*/

package action

import (
	"strings"
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// standingWall is one actor's wall slot and its context's remaining counter.
type standingWall struct {
	division, name string
	skillID, token uint32
	wall           enterworld.SkillWall
	pool           uint32
	nextPulse      int64
}

func wallKey(division, name string) string {
	return strings.ToLower(division) + "\x00" + strings.ToLower(name)
}

/*
==================
acceptWall

583657 for a pw row: charge the cast, install the caster's persistent
instance and load the slot.
==================
*/
func (rt *Runtime) acceptWall(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow) OpResult {
	now := rt.Now().UnixMilli()
	if !skill.Wall.Pinned || cast.HasTarget || cast.HasGroundTarget {
		return OpResult{DiagnosticRefusal: "wall-admission-refused"}
	}
	if !enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "wall-admission-refused"}
	}
	if rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "wall-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, nil, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	var frames []wire.Frame
	var refusal uint16
	committed := rt.deps.Update(c, "wall", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		installed, ok := rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, true, EffectPresentation{Phase: 1}, now)
		if !ok {
			return false
		}
		success := wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: enterworld.ObjectIDForCharacter(c), InstanceToken: token}
		frames = append(frames, wire.SkillCastSelfFrame(success))
		frames = append(frames, installed...)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		return true
	})
	if !committed {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "wall-commit-refused"}
	}

	rt.wallMu.Lock()
	if rt.walls == nil {
		rt.walls = map[string]*standingWall{}
	}
	rt.walls[wallKey(division, c.Name)] = &standingWall{
		division: division, name: c.Name, skillID: skill.ID, token: token,
		wall: skill.Wall, pool: skill.Wall.Pool, nextPulse: now + int64(skill.Aura.PulseMs),
	}
	rt.wallMu.Unlock()
	return OpResult{Frames: frames, Broadcast: frames}
}

// wallInstanceLive reports the wall's instance still installed and not
// asked to stop.
func (rt *Runtime) wallInstanceLive(w *standingWall) bool {
	for _, effect := range rt.effects.Snapshot(w.division, w.name) {
		if effect.SkillID == w.skillID && effect.InstanceToken == w.token {
			return !effect.StopRequested
		}
	}
	return false
}

// standingWallOf is the defender's slot: the wall rule and pool, when a
// live wall stands.
func (rt *Runtime) standingWallOf(division, name string) (standingWall, bool) {
	rt.wallMu.Lock()
	w := rt.walls[wallKey(division, name)]
	rt.wallMu.Unlock()
	if w == nil || !rt.wallInstanceLive(w) {
		return standingWall{}, false
	}
	rt.wallMu.Lock()
	defer rt.wallMu.Unlock()
	return *w, true
}

// drainWall is 593C5E: the pool loses the absorbed total, floored at 0.
func (rt *Runtime) drainWall(division, name string, token, absorbed uint32) {
	rt.wallMu.Lock()
	defer rt.wallMu.Unlock()
	w := rt.walls[wallKey(division, name)]
	if w == nil || w.token != token {
		return
	}
	w.pool -= min(w.pool, absorbed)
}

/*
==================
wallRecords

58EE49..58F0B2 for one hit's committed impacts: each covered impact adds
its absorbed damage to the group total and reports the pool that total
leaves; an empty pool or an uncovered attack yields a bare type 8. Returns
the records and the total the pool must lose.
==================
*/
func wallRecords(w standingWall, splits []wallSplit, count int) ([]wire.SkillCastTargetImpact, uint32) {
	records := make([]wire.SkillCastTargetImpact, 0, count)
	var total uint32
	for _, split := range splits[:count] {
		if w.pool == 0 {
			records = append(records, wire.SkillCastTargetImpact{Skipped: true})
			continue
		}
		total += split.absorbed
		record := wire.SkillCastTargetImpact{
			ResultFlags: split.flags,
			Damage:      split.absorbed,
			Absorb: &wire.SkillCastAbsorb{
				Remaining: uint16(w.pool - min(w.pool, total)),
				Max:       uint16(w.wall.Pool),
				Broken:    w.pool <= total,
			},
		}
		if !split.covered {
			record = wire.SkillCastTargetImpact{Skipped: true}
		}
		records = append(records, record)
	}
	return records, total
}

// wallSplit is one impact's wall share.
type wallSplit struct {
	absorbed uint32
	flags    uint8
	covered  bool
}

/*
==================
advanceWalls

The 5830B0 update of every standing wall: the caster and instance must
live, an empty pool retires it (5851F7), and the onff pulse is paid or
retires it (585262).
==================
*/
func (rt *Runtime) advanceWalls(now int64) []simulation.DivisionFrames {
	rt.wallMu.Lock()
	walls := make([]*standingWall, 0, len(rt.walls))
	for _, w := range rt.walls {
		walls = append(walls, w)
	}
	rt.wallMu.Unlock()

	var out []simulation.DivisionFrames
	for _, w := range walls {
		caster := rt.findCharacter(w.division, w.name)
		skill, known := rt.deps.SkillData().SkillByID(w.skillID)
		rt.wallMu.Lock()
		pool := w.pool
		rt.wallMu.Unlock()
		if caster == nil || !known || !enterworld.CharacterAlive(caster) || !rt.wallInstanceLive(w) || pool == 0 {
			rt.retireWall(w)
			continue
		}
		if now < w.nextPulse {
			continue
		}
		paid, ok := rt.pulseAura(w.division, caster, skill)
		if !ok {
			rt.retireWall(w)
			continue
		}
		out = append(out, paid...)
		w.nextPulse = now + int64(skill.Aura.PulseMs)
	}
	return out
}

// retireWall empties the slot and asks the instance to stop.
func (rt *Runtime) retireWall(w *standingWall) {
	rt.wallMu.Lock()
	if rt.walls[wallKey(w.division, w.name)] == w {
		delete(rt.walls, wallKey(w.division, w.name))
	}
	rt.wallMu.Unlock()
	request := func() bool {
		rt.effects.RequestVoluntaryStop(w.division, w.name, w.skillID, w.token)
		return true
	}
	if caster := rt.findCharacter(w.division, w.name); caster != nil {
		rt.deps.Update(caster, "wall-retire", request)
		return
	}
	request()
}

// wallStanding feeds the 58E0EB footing gate.
func (rt *Runtime) wallStanding(division, name string) bool {
	_, ok := rt.standingWallOf(division, name)
	return ok
}
