/*
===========================================================================

abnormal_store_test.go - status ticks through the production authority lock.

Detached character fixtures cannot expose a source lookup that reenters the
store during a write. Run the real store composition in a bounded child so a
regression reports a deadlock without wedging the rest of the test suite.

===========================================================================
*/

package action

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
)

const (
	abnormalStoreChild         = "SRO_TEST_ABNORMAL_STORE_CHILD"
	abnormalStoreDeadline      = 15 * time.Second
	abnormalStoreHP            = 100
	abnormalStoreDamage        = 10
	abnormalStoreMissingSource = 400_003
	abnormalStoreDuration      = 30_000
	abnormalStorePetGID        = 9_001
)

/*
================
TestAbnormalTicksUseAuthorityStore

The child holds the same non-reentrant write lock as GameWorld. A missing
monster source used to fall through to a character lookup under that lock.
================
*/
func TestAbnormalTicksUseAuthorityStore(t *testing.T) {
	if os.Getenv(abnormalStoreChild) == "1" {
		testAbnormalStoreTicks(t)
		t.Run("pet tick", testCosAbnormalStoreTick)
		t.Run("monster admission", testMonsterAbnormalStoreAdmission)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), abnormalStoreDeadline)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestAbnormalTicksUseAuthorityStore$", "-test.v")
	child.Env = append(os.Environ(), abnormalStoreChild+"=1")
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("authority-store status tick failed: %v (deadline: %v)\n%s", err, ctx.Err(), output)
	}
}

/*
================
testCosAbnormalStoreTick

The sibling COS callback must also finish when its source has despawned.
The pet's HP debit must survive a restart, not merely advance a slot timestamp.
================
*/
func testCosAbnormalStoreTick(t *testing.T) {
	seed := testCharacter()
	seed.ActiveCOS = &enterworld.CharacterCOS{GID: abnormalStorePetGID, CurrentHP: abnormalStoreHP, Summoned: true}
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	now := d.clock.NowMs()
	block := &abnormal.Block{Mask: abnormal.Poison.Bit()}
	block.Slots[abnormal.Poison] = abnormal.Slot{
		Active: true, StartedAt: now,
		Record: abnormal.Record{Status: abnormal.Poison, Level: 1, DurationMs: abnormalStoreDuration,
			SourceGID: abnormalStoreMissingSource, Param38: abnormalStoreDamage},
	}
	d.rt.storeCosAbnormal(testDivision, d.character.Name, abnormalStorePetGID, block)
	d.rt.advanceCosAbnormals(now)
	got := d.rt.cosAbnormal(testDivision, d.character.Name, abnormalStorePetGID)
	if got == nil || got.Slots[abnormal.Poison].LastTickAt != now {
		t.Fatalf("pet tick did not advance its retained slot: %+v", got)
	}
	if health := d.authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("store health after pet tick: %+v", health)
	}
	wantHP := uint32(abnormalStoreHP - abnormalStoreDamage)
	if d.character.ActiveCOS.CurrentHP != wantHP {
		t.Fatalf("pet poison HP %d, want %d", d.character.ActiveCOS.CurrentHP, wantHP)
	}
	reopened := d.reboot(t)
	if reopened.character.ActiveCOS.CurrentHP != wantHP {
		t.Fatalf("persisted pet poison HP %d, want %d", reopened.character.ActiveCOS.CurrentHP, wantHP)
	}
}

/*
================
testMonsterAbnormalStoreAdmission

Exercise source admission and the HP/status commit through MonsterBasicAttack,
using its real store composition instead of the detached fixture's callback.
================
*/
func testMonsterAbnormalStoreAdmission(t *testing.T) {
	rt, clock, seed, instance := newCombatTestRuntime(t, abnormalStoreHP)
	authority, err := store.Open(filepath.Join(t.TempDir(), "authority"), store.Options{DefaultSkills: doorSkillSeeder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	if err := authority.CreateCharacter(testDivision, "test-account", seed); err != nil {
		t.Fatal(err)
	}
	deps := rt.deps.(*enterworld.Deps)
	deps.Characters = authority.Characters()
	deps.MutateCharacter = authority.MutateCharacter
	deps.UpdateCharacter = authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { authority.ReadState(read) }
	character := deps.CharactersForDivision(testDivision)[0]
	skills := deps.SkillData().(staticSkillSource)
	for _, id := range character.Skills {
		if _, present := skills[id]; !present {
			skills[id] = enterworld.SkillRow{ID: id}
		}
	}
	instance.Ref.DefaultSkillIDs[0] = 2
	stunSkill(rt)
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	result := rt.MonsterBasicAttack(testDivision, instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
	block := rt.playerAbnormal(testDivision, character.Name)
	if !result.Accepted || block == nil || !block.Has(abnormal.Stun) {
		t.Fatalf("store-backed monster admission: accepted %v, block %+v", result.Accepted, block)
	}
	if health := authority.Health(); health.FailedWrites != 0 {
		t.Fatalf("store health after admission: %+v", health)
	}
}

/*
================
testAbnormalStoreTicks

Check missing, living and dead sources through the persisted character
composition. Poison must debit HP regardless of whether damage is credited.
================
*/
func testAbnormalStoreTicks(t *testing.T) {
	for _, sourceState := range []string{"missing", "living", "dead", "self"} {
		t.Run(sourceState, func(t *testing.T) {
			seed := testCharacter()
			seed.Level = testInt64(1)
			seed.MaxLevel = testInt64(1)
			seed.CurrentHP = testInt64(abnormalStoreHP)
			d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), seed)
			deps := d.rt.deps.(*enterworld.Deps)
			deps.UpdateCharacter = d.authority.UpdateCharacter
			deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
			sourceGID := uint32(abnormalStoreMissingSource)
			if sourceState == "self" {
				sourceGID = enterworld.ObjectIDForCharacter(d.character)
			} else if sourceState != "missing" {
				peer := testCharacter()
				peer.ID = 0
				peer.Name = "StatusSource"
				peer.CurrentHP = testInt64(abnormalStoreHP)
				if sourceState == "dead" {
					peer.CurrentHP = testInt64(0)
				}
				if err := d.authority.CreateCharacter(testDivision, "source-account", peer); err != nil {
					t.Fatal(err)
				}
				sourceGID = enterworld.ObjectIDForCharacter(peer)
			}
			now := d.clock.NowMs()
			record := abnormal.Record{
				Status: abnormal.Poison, Level: 1, DurationMs: abnormalStoreDuration,
				SourceGID: sourceGID, Param38: abnormalStoreDamage,
			}
			// The source may have despawned after admission. Seed the retained
			// slot so the tick must resolve even a vanished monster correctly.
			block := &abnormal.Block{Mask: abnormal.Poison.Bit()}
			block.Slots[abnormal.Poison] = abnormal.Slot{Active: true, StartedAt: now, Record: record}
			d.rt.storePlayerAbnormal(testDivision, d.character.Name, block)
			frames := d.rt.advancePlayerAbnormals(now)
			wantHP := int64(abnormalStoreHP - abnormalStoreDamage)
			if sourceState == "self" {
				// 52A288 rejects a live source that is the victim itself.
				wantHP = abnormalStoreHP
			}
			if got := enterworld.CurrentHP(d.character); got != wantHP || (len(frames) == 0) != (sourceState == "self") {
				t.Fatalf("status tick: HP %d, want %d; publications %d", got, wantHP, len(frames))
			}
			block = d.rt.playerAbnormal(testDivision, d.character.Name)
			if block == nil || block.Slots[abnormal.Poison].SourceDied != (sourceState == "dead") {
				t.Fatalf("source death latch: %+v", block)
			}
			if health := d.authority.Health(); health.FailedWrites != 0 {
				t.Fatalf("store health after status tick: %+v", health)
			}
			reopened := d.reboot(t)
			if got := enterworld.CurrentHP(reopened.character); got != wantHP {
				t.Fatalf("persisted status HP %d, want %d", got, wantHP)
			}
		})
	}
}
