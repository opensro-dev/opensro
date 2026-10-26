/*
===========================================================================

monsterlifetime.go - monsters that leave the world on a timer

The native spawn base arms three timers that the mob tick counts
down on every living monster, each ending in life state 3 (gone):

  - +0x1CE0: a thief or hunter monster leaves 20 s after its spawn or
    its last attack (rearmed on every hit it deals);
  - +0x1CE8: the quest clones MOB_QT_01_WINGTRIBE and
    MOB_QT_02_PUNISHER_CLON live 180 s and 300 s;
  - +0x1CF0: a grade-7 monster lives 300 s.

INFERENCE: the spawn base arms the grade-7 timer only where the
world's slot 0x1E
answers 0; the port opens ordinary field worlds only, where it is armed.
The unique class's 60 s variant of +0x1CE0 names no v1.150
monster the port spawns.

===========================================================================
*/

package simulation

import (
	"strings"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const (
	jobMonsterIdleMs      = 20000  // 0x4E20
	wingTribeLifetimeMs   = 180000 // 0x2BF20
	punisherCloneLifetime = 300000 // 0x493E0
	gradeSevenLifetimeMs  = 300000 // 0x493E0
	gradeSeven            = 7
)

/*
================
monsterLifetime

One monster's deadline; refreshed marks the job monster's idle timer.
================
*/
type monsterLifetime struct {
	untilMs   int64
	refreshed bool
}

/*
================
spawnLifetime

The spawn base's timers for a monster entering the world at now. ok is false for a
monster with none.
================
*/
func spawnLifetime(instance monster.Instance, now int64) (monsterLifetime, bool) {
	switch {
	case strings.EqualFold(instance.Ref.Codename, "MOB_QT_01_WINGTRIBE"):
		return monsterLifetime{untilMs: now + wingTribeLifetimeMs}, true
	case strings.EqualFold(instance.Ref.Codename, "MOB_QT_02_PUNISHER_CLON"):
		return monsterLifetime{untilMs: now + punisherCloneLifetime}, true
	case instance.Rarity()&15 == gradeSeven:
		return monsterLifetime{untilMs: now + gradeSevenLifetimeMs}, true
	case instance.ThiefMonster() || instance.HunterMonster():
		return monsterLifetime{untilMs: now + jobMonsterIdleMs, refreshed: true}, true
	}
	return monsterLifetime{}, false
}

/*
================
armLifetimeLocked

The caller holds s.mu and has just stored instance in state.
================
*/
func armLifetimeLocked(state *divisionMonsterState, instance monster.Instance, now int64) {
	lifetime, ok := spawnLifetime(instance, now)
	if !ok {
		return
	}
	if state.lifetimes == nil {
		state.lifetimes = make(map[uint32]monsterLifetime)
	}
	state.lifetimes[instance.Gid] = lifetime
}

/*
================
RefreshJobMonster

The native attack rearm (from the normal-hit processing): a job
monster's own attack rearms its idle timer.
================
*/
func (s *MonsterState) RefreshJobMonster(division string, gid uint32, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.populationForObject(division, gid)
	if lifetime, ok := state.lifetimes[gid]; ok && lifetime.refreshed {
		state.lifetimes[gid] = monsterLifetime{untilMs: now + jobMonsterIdleMs, refreshed: true}
	}
}

/*
================
ExpireMonsterLifetimes

The mob tick's countdowns: every living monster past its deadline
leaves through
the nest death path (Defeat), as life state 3 does. Returns how many left.
================
*/
func (s *MonsterState) ExpireMonsterLifetimes(now int64) int {
	type expired struct {
		division string
		gid      uint32
	}
	var due []expired
	s.mu.Lock()
	collect := func(division string, state *divisionMonsterState) {
		for gid, lifetime := range state.lifetimes {
			if now < lifetime.untilMs {
				continue
			}
			if instance, ok := state.instances.lookup(gid); ok && instance.CurrentHP > 0 {
				due = append(due, expired{division, gid})
			} else {
				delete(state.lifetimes, gid)
			}
		}
	}
	for division, state := range s.divs {
		collect(division, state)
	}
	for key, state := range s.worldPopulations {
		collect(key.division, state)
	}
	s.mu.Unlock()
	for _, e := range due {
		s.Defeat(e.division, e.gid, time.UnixMilli(now))
	}
	return len(due)
}
