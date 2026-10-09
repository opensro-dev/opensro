/*
===========================================================================

pk_enemy_test.go - legal-enemy kills must not criminalize their helper

Exercise the fatal commit with a resolved hit so combat stat fixtures remain
at their authored level. The relation level and pre-death penalty are inputs
to the native bookkeeping decision, independent of damage calculation.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestPlayerKillLegalEnemyBookkeeping

4E2004 passes killer in EAX and victim in ESI to 4EB590. The ordinary
controller reaches 52B6D0 before death relief can clear the victim's red state.
================
*/
func TestPlayerKillLegalEnemyBookkeeping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		level      int64
		penalty    uint32
		aggression uint32
		murder     bool
	}{
		{"neutral", 20, 0, 0, true},
		{"red", 20, 500, 0, false},
		{"last-red-kill", 20, 200, 0, false},
		{"grey", 20, 0, 20, false},
		{"retained-last-tick", 20, 0, 1, false},
		{"below-native-floor", 19, 500, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, killer, victim := newPvpPair(t)
			killer.Level, victim.Level = testInt64(tc.level), testInt64(tc.level)
			killer.MaxLevel, victim.MaxLevel = killer.Level, victim.Level
			killer.Aggressions = nil
			victim.CurrentHP = testInt64(1)
			victim.PK = &domain.PKRecord{Penalty: tc.penalty, TotalCount: 1}
			if tc.aggression != 0 {
				victim.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(killer): tc.aggression}
			}
			hit := playerHit{target: combatTarget{player: victim}, kill: rt.classifyPlayerKill(testDivision, killer, victim)}
			hit.strike = playerStrike{division: testDivision, victim: victim, killer: deathKiller{player: killer},
				formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
			rt.commitPlayerHitInDoor(testDivision, killer, &hit, clock.NowMs())
			if !hit.struck.fatal || enterworld.CharacterAlive(victim) {
				t.Fatal("resolved fatal hit did not kill victim")
			}
			booked := killer.PK != nil && killer.PK.Penalty != 0
			if booked != tc.murder {
				t.Fatalf("killer PK %+v, want murder=%v", killer.PK, tc.murder)
			}
			if !tc.murder && tc.aggression == 0 && killer.PVPState() != 0 {
				t.Fatalf("legal kill left helper state %d", killer.PVPState())
			}
			wantPenalty := uint32(0)
			if tc.penalty > 200 {
				wantPenalty = tc.penalty - 200
			}
			if victim.PK.Penalty != wantPenalty {
				t.Fatalf("victim relief %d, want %d", victim.PK.Penalty, wantPenalty)
			}
		})
	}
}

/*
================
TestPeriodicPlayerKillBooksOnlyNeutralVictims

52A3F9 uses the same killer callback for status damage as direct damage.
================
*/
func TestPeriodicPlayerKillBooksOnlyNeutralVictims(t *testing.T) {
	for _, tc := range []struct {
		penalty  uint32
		credited bool
	}{{0, true}, {200, true}, {0, false}} {
		penalty := tc.penalty
		rt, clock, killer, victim := newPvpPair(t)
		killer.Level, victim.Level = testInt64(20), testInt64(20)
		killer.MaxLevel, victim.MaxLevel = killer.Level, victim.Level
		victim.CurrentHP = testInt64(1)
		victim.PK = &domain.PKRecord{Penalty: penalty, TotalCount: 1}
		gid := enterworld.ObjectIDForCharacter(killer)
		owner := rt.newPlayerAbnormalOwner(testDivision, victim, clock.NowMs())
		owner.sources = rt.capturePlayerAbnormalSources(testDivision, victim, owner.block, []abnormal.Record{{SourceGID: gid}})
		owner.Hit(gid, tc.credited, 1, abnormalDamageOverTimeReason, abnormal.Burn)
		rt.playerAbnormalPublication(testDivision, victim, owner)
		booked := killer.PK != nil && killer.PK.Penalty != 0
		if booked != (penalty == 0 && tc.credited) {
			t.Fatalf("status victim penalty %d left killer PK %+v", penalty, killer.PK)
		}
	}
}
