/*
===========================================================================

pk_lock_test.go - fatal character transactions never reacquire relation locks

Real fortress and union mutators pause in their persistence callbacks while
holding their authority mutexes. Combat prepares relation facts before that
pause, then must finish inside a real character-store write transaction before
the external save is released. This catches the reverse lock order without
source inspection or access to either authority's private mutex.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/testsupport/wait"
)

const pkLockTimeout = 2 * time.Second
const pkLockFortressID = 1

/*
================
pkLockedRelationStore

Only the exercised load/save methods are supplied. Unexpected persistence
surfaces fail through the nil embedded interfaces instead of silently passing.
================
*/
type pkLockedRelationStore struct {
	domain.FortressStore
	domain.AllianceStore
	entered chan struct{}
	release chan struct{}
}

/*
================
FortressState
================
*/
func (*pkLockedRelationStore) FortressState(string) ([]domain.FortressRecord, []domain.FortressRequestRecord, error) {
	return nil, nil, nil
}

/*
================
Alliances
================
*/
func (*pkLockedRelationStore) Alliances(string) ([]domain.AllianceRecord, error) {
	return nil, nil
}

/*
================
SaveFortress
================
*/
func (s *pkLockedRelationStore) SaveFortress(string, domain.FortressRecord) error {
	close(s.entered)
	<-s.release
	return nil
}

/*
================
SaveAlliance
================
*/
func (s *pkLockedRelationStore) SaveAlliance(string, domain.AllianceRecord, bool) error {
	close(s.entered)
	<-s.release
	return nil
}

/*
================
TestPKFatalTransactionsDoNotAcquireRelationLocks

The neutral siege-world fixture has different guilds and an inactive war, so
the old classifier had to query both union identity and fortress war state.
No generated fortress catalog or special death-reward path is required.
================
*/
func TestPKFatalTransactionsDoNotAcquireRelationLocks(t *testing.T) {
	for _, authorityName := range []string{"fortress", "union"} {
		for _, hitKind := range []string{"direct", "dot"} {
			t.Run(authorityName+"/"+hitKind, func(t *testing.T) {
				rt, clock, killer, victim := newPvpPair(t)
				packed := uint32(instance.Pack(2, 1))
				world, found := instance.Lookup(instance.ID(packed).Definition())
				if !found || !world.Siege() {
					t.Fatal("fixture requires a siege world")
				}
				killer.World.PackedInstance, victim.World.PackedInstance = &packed, &packed
				killer.GuildID, victim.GuildID = testInt64(7), testInt64(8)
				killer.Aggressions, victim.Aggressions = nil, nil
				victim.CurrentHP = testInt64(1)
				backend := &pkLockedRelationStore{entered: make(chan struct{}), release: make(chan struct{})}
				rt.Fortresses = fortress.New([]fortress.Catalog{{ID: pkLockFortressID, CodeName: world.Strings[0]}})
				if err := rt.Fortresses.Restore(testDivision, backend); err != nil {
					t.Fatal(err)
				}
				rt.Unions = union.New()
				if err := rt.Unions.Restore(testDivision, backend); err != nil {
					t.Fatal(err)
				}
				// This store supplies the real write lock around the fixture's
				// character mutation; fixture character lookup remains unchanged.
				characterStore, err := store.Open(t.TempDir(), store.Options{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(characterStore.Close)
				gid := enterworld.ObjectIDForCharacter(killer)
				hit := playerHit{target: combatTarget{player: victim}, kill: rt.classifyPlayerKill(testDivision, killer, victim)}
				hit.strike = playerStrike{division: testDivision, victim: victim,
					killer:   rt.prepareDeathKiller(testDivision, victim, deathKiller{player: killer}),
					formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
				owner := rt.newPlayerAbnormalOwner(testDivision, victim, clock.NowMs())
				owner.sources = rt.capturePlayerAbnormalSources(testDivision, victim, owner.block,
					[]abnormal.Record{{SourceGID: gid}})
				if hit.kill.legalEnemy || owner.sources[gid].killer.player == nil {
					t.Fatal("fixture did not prepare a neutral player source")
				}
				saved, committed := make(chan struct{}), make(chan struct{})
				var saveErr error
				started := false
				t.Cleanup(func() {
					// Unblock the old implementation after its expected timeout,
					// then join workers before any fixture resources are destroyed.
					close(backend.release)
					wait.Eventually(t, pkLockTimeout, "authority save cleanup", func() bool {
						select {
						case <-saved:
							return true
						default:
							return false
						}
					})
					if started {
						wait.Eventually(t, pkLockTimeout, "character transaction cleanup", func() bool {
							select {
							case <-committed:
								return true
							default:
								return false
							}
						})
					}
					if saveErr != nil {
						t.Error(saveErr)
					}
				})
				go func() {
					defer close(saved)
					if authorityName == "fortress" {
						if !rt.Fortresses.SetTaxRate(testDivision, pkLockFortressID, 1) {
							saveErr = fmt.Errorf("fortress save was refused")
						}
					} else {
						_, saveErr = rt.Unions.Join(testDivision, 7, 9)
					}
				}()
				wait.Eventually(t, pkLockTimeout, "persistence callback holding "+authorityName+" mutex", func() bool {
					select {
					case <-backend.entered:
						return true
					default:
						return false
					}
				})
				started = true
				go func() {
					defer close(committed)
					characterStore.Mutate("pk-lock-regression", func() {
						if hitKind == "direct" {
							rt.commitPlayerHitInDoor(testDivision, killer, &hit, clock.NowMs())
						} else {
							owner.Hit(gid, true, 1, abnormalDamageOverTimeReason, abnormal.Burn)
						}
					})
				}()
				wait.Eventually(t, pkLockTimeout, "fatal "+hitKind+" transaction while "+authorityName+" mutex is held", func() bool {
					select {
					case <-committed:
						return true
					default:
						return false
					}
				})
				if enterworld.CharacterAlive(victim) || (hitKind == "direct" && !hit.struck.fatal) || (hitKind == "dot" && !owner.fatal) {
					t.Fatal("transaction completed without the fatal gameplay path")
				}
			})
		}
	}
}
