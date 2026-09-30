/*
===========================================================================

skilljobs_test.go - timed skill jobs across restore, checkpoint and session changes

Restored jobs keep their lifetimes and native casting states, unknown
producers survive, and a stale session close cannot erase a newer effect.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestTimedJobRestoreDoesNotSkipMissingJobsOrRewriteResidentLifetime
================
*/
func TestTimedJobRestoreDoesNotSkipMissingJobsOrRewriteResidentLifetime(t *testing.T) {
	row := enterworld.SkillRow{ID: 100, Group: 9, EffectDurationMs: 10000,
		MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: true, Percent: 50, Persistent: true}}
	other := row
	other.ID, other.Group = 101, 10
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: row, 101: other, 102: {ID: 102, Group: 11}})
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 102, 90, statuseffect.StateActive, false) {
		t.Fatal("ordinary effect")
	}
	c.TimedSkillJobs = []domain.TimedSkillJob{{SkillID: 100, Token: 800, RemainingMs: 7000}}
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	first := rt.effects.Snapshot(testDivision, c.Name)
	if len(first) != 2 || len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0].Token == 800 {
		t.Fatal("ordinary effect blocked restoration", first, c.TimedSkillJobs)
	}
	c.TimedSkillJobs = append(c.TimedSkillJobs, domain.TimedSkillJob{SkillID: 101, Token: 801, RemainingMs: 6000})
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 3 || len(c.TimedSkillJobs) != 2 {
		t.Fatal("resident job blocked second restoration", c.TimedSkillJobs)
	}
	for _, before := range first {
		found := false
		for _, after := range rt.effects.Snapshot(testDivision, c.Name) {
			if before.SkillID == after.SkillID {
				found = true
				if before.InstanceToken != after.InstanceToken || before.StartedAtMs != after.StartedAtMs || before.ExpiresAtMs != after.ExpiresAtMs {
					t.Fatal("resident lifetime reset", before, after)
				}
			}
		}
		if !found {
			t.Fatal("resident removed")
		}
	}
}

/*
================
TestTimedJobUnknownProducerSurvivesRestoreAndCheckpoint
================
*/
func TestTimedJobUnknownProducerSurvivesRestoreAndCheckpoint(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{})
	job := domain.TimedSkillJob{SkillID: 999999, Token: 51, RemainingMs: 123000}
	c.TimedSkillJobs = []domain.TimedSkillJob{job}
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	rt.checkpointSkillJobs(c, rt.effects.Snapshot(testDivision, c.Name), rt.Now().UnixMilli())
	if len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0] != job {
		t.Fatal("unimplemented producer destroyed durable record", c.TimedSkillJobs)
	}
	if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("unknown job executed")
	}
}

/*
================
TestTimedJobFailedInstallationRetainsRecordForRetry
================
*/
func TestTimedJobFailedInstallationRetainsRecordForRetry(t *testing.T) {
	row := enterworld.SkillRow{ID: 100, Group: 9, EffectDurationMs: 10000,
		MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: true, Persistent: true, Percent: 50}}
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: row})
	for i := uint32(0); i < statuseffect.MaxAttachedEffectsPerCharacter; i++ {
		if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: 1000 + i, SkillGroup: 1000 + i, InstanceToken: 1000 + i, ClientCancelable: true}) {
			t.Fatal("fill registry", i)
		}
	}
	job := domain.TimedSkillJob{SkillID: 100, Token: 900, RemainingMs: 7000}
	c.TimedSkillJobs = []domain.TimedSkillJob{job}
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	rt.checkpointSkillJobs(c, rt.effects.Snapshot(testDivision, c.Name), rt.Now().UnixMilli())
	if len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0] != job {
		t.Fatal("refused installation erased job", c.TimedSkillJobs)
	}
	if _, ok := rt.effects.RequestVoluntaryStop(testDivision, c.Name, 1000, 1000); !ok {
		t.Fatal("release capacity")
	}
	rt.effects.DrainStopRequested()
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0].Token == job.Token || c.TimedSkillJobs[0].RemainingMs != job.RemainingMs {
		t.Fatal("retry failed", c.TimedSkillJobs)
	}
}

/*
================
TestShippedSkillConsumableLifecycle
================
*/
func TestShippedSkillConsumableLifecycle(t *testing.T) {
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	skills := enterworld.NewTextdataSkills(dir)
	for _, code := range []string{"ITEM_ETC_SPEED_UP_BASIC", "ITEM_MALL_MOVE_SPEED_UP_50", "ITEM_MALL_MOVE_SPEED_UP_100"} {
		t.Run(code, func(t *testing.T) {
			ref, ok := items.ItemRefByCodename(code)
			if !ok {
				t.Fatal("missing shipped item", code)
			}
			skill, ok := skills.SkillByCodename(ref.AssociatedSkillCodename)
			if !ok || !skill.MovementModifier.Supported || !skill.MovementModifier.Persistent {
				t.Fatalf("unadmitted descriptor: %+v", skill.MovementModifier)
			}
			c := testCharacter()
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: code, TypeFlags: ref.TypeFlags(), StackCount: 2}}
			rt, clock := newTestRuntime(c, items)
			rt.deps.(*enterworld.Deps).Skills = skills
			installMidMove(rt, c, clock)
			key := simulation.WorldKey(testDivision, c.Name)
			seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
			before := rt.Worlds.Snapshot(key, seed).LiveSpawnAt(clock.NowMs())
			flags := ref.TypeFlags()
			request := []byte{21, byte(flags), byte(flags >> 8)}
			result := rt.HandleItemUse(testDivision, c, request)
			if len(result.Frames) < 3 || result.Frames[0].Payload[0] != 1 || c.MissionInventory[0].StackCount != 1 {
				t.Fatalf("activation failed: %+v", result)
			}
			wantRun := float32(50) * (1 + float32(skill.MovementModifier.Percent)/100)
			world := rt.Worlds.Snapshot(key, seed)
			walk, run := world.MovementSpeeds()
			if run != wantRun || walk != wantRun*0.4 || world.LiveSpawnAt(clock.NowMs()) != before {
				t.Fatalf("speed/position mismatch: %v %v %+v %+v", walk, run, before, world)
			}
			token := rt.effects.Snapshot(testDivision, c.Name)[0].InstanceToken
			if len(c.TimedSkillJobs) != 1 || c.TimedSkillJobs[0].RemainingMs != 3600000 {
				t.Fatalf("job: %+v", c.TimedSkillJobs)
			}
			// Duplicate activation neither consumes nor resets the current job.
			result = rt.HandleItemUse(testDivision, c, request)
			if result.Frames[0].Payload[0] == 1 || c.MissionInventory[0].StackCount != 1 {
				t.Fatal("duplicate refreshed job")
			}
			clock.Advance(10 * time.Minute)
			rt.ForgetCharacter(testDivision, c.Name)
			if c.TimedSkillJobs[0].RemainingMs != 3000000 {
				t.Fatalf("logout checkpoint: %+v", c.TimedSkillJobs)
			}
			clock.Advance(24 * time.Hour)
			rt.RestoreTimedSkillJobs(testDivision, c.Name)
			rows := rt.EntrySkills(testDivision, c.Name)
			if len(rows) != 1 || rows[0].Remaining == nil || *rows[0].Remaining != 3000000 || *rows[0].Token == token {
				t.Fatalf("offline time/reset: %+v", rows)
			}
			rt.RestoreTimedSkillJobs(testDivision, c.Name)
			again := rt.EntrySkills(testDivision, c.Name)
			if *again[0].Token != *rows[0].Token {
				t.Fatal("duplicate entry recreated effect")
			}
			_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
			if run != wantRun {
				t.Fatal("entry lost speed")
			}
			rt.retireBodyEffectsOnDeath(testDivision, c)
			if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
				t.Fatal("cbuf incorrectly cleared on death")
			}
			clock.Advance(50 * time.Minute)
			rt.effects.Expire(clock.NowMs())
			ended := rt.drainStoppedCharacterEffects()
			_, run = rt.EntryMovementSpeeds(testDivision, c.Name)
			if run != 50 || len(c.TimedSkillJobs) != 0 || len(ended) != 1 {
				t.Fatalf("expiry failed: %v %v %+v", run, c.TimedSkillJobs, ended)
			}
			if len(rt.EntrySkills(testDivision, c.Name)) != 0 {
				t.Fatal("expired job resurrected")
			}
		})
	}
}

/*
================
TestTimedJobRestoresNativeCastingStates
================
*/
func TestTimedJobRestoresNativeCastingStates(t *testing.T) {
	row := enterworld.SkillRow{ID: 100, Group: 9, EffectDurationMs: 3600000,
		MovementModifier:  enterworld.SkillMovementModifier{Present: true, Supported: true, Percent: 100, Persistent: true},
		ReplacementPinned: true, Replacement: statuseffect.ReplacementDescriptor{PackedStates: 5, Ovl2Present: true, Ovl2: 6}}
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: row})
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 100, 7, statuseffect.StateActive, false) {
		t.Fatal("apply")
	}
	want := rt.effects.CastingStates(testDivision, c.Name)
	if !want.Conflicts(5) || !want.Conflicts(6) {
		t.Fatal("timed-job states not installed", want)
	}
	rt.ForgetCharacter(testDivision, c.Name)
	if rt.effects.CastingStates(testDivision, c.Name) != (statuseffect.CastingConflictSnapshot{}) {
		t.Fatal("old actor state retained")
	}
	rt.RestoreTimedSkillJobs(testDivision, c.Name)
	if got := rt.effects.CastingStates(testDivision, c.Name); got != want {
		t.Fatal("reconnect state mismatch", got, want)
	}
	effects := rt.effects.Snapshot(testDivision, c.Name)
	if len(effects) != 1 {
		t.Fatal("restored effect missing")
	}
	rt.effects.Expire(effects[0].ExpiresAtMs)
	rt.drainStoppedCharacterEffects()
	if rt.effects.CastingStates(testDivision, c.Name) != (statuseffect.CastingConflictSnapshot{}) {
		t.Fatal("job expiry retained conflicts")
	}
}

/*
================
TestOrdinaryRecipientEffectStateOperations
================
*/
func TestOrdinaryRecipientEffectStateOperations(t *testing.T) {
	for _, ovl := range []bool{false, true} {
		row := enterworld.SkillRow{ID: 100, Group: 9, EffectDurationMs: 1000,
			MovementModifier:  enterworld.SkillMovementModifier{Present: true, Supported: true, Percent: 20},
			ReplacementPinned: true, Replacement: statuseffect.ReplacementDescriptor{PackedStates: 5, DttpPresent: true, Ovl2Present: ovl, Ovl2: 6}}
		rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: row})
		if !rt.ApplyCharacterEffect(testDivision, c.Name, 100, 7, statuseffect.StateActive, false) {
			t.Fatal("application")
		}
		s := rt.effects.CastingStates(testDivision, c.Name)
		if s.Conflicts(5) || s.Conflicts(6) != ovl {
			t.Fatal("recipient branch installed wrong word", ovl, s)
		}
		e := rt.effects.Snapshot(testDivision, c.Name)[0]
		if !ovl && e.RetirementStates[0] != 5 {
			t.Fatal("cleanup asymmetry lost")
		}
		rt.effects.RequestVoluntaryStop(testDivision, c.Name, 100, 7)
		rt.drainStoppedCharacterEffects()
		if rt.effects.CastingStates(testDivision, c.Name) != (statuseffect.CastingConflictSnapshot{}) {
			t.Fatal("retirement")
		}
	}
}

/*
================
TestMovementEffectCancellationRetimesRemainingTravel
================
*/
func TestMovementEffectCancellationRetimesRemainingTravel(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: {ID: 100, Group: 9, EffectDurationMs: 3600000, MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: true, Percent: 100, Persistent: true}}})
	clock := &fakeClock{now: time.UnixMilli(1000000)}
	rt.Now = clock.Now
	installMidMove(rt, c, clock)
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 100, 7, statuseffect.StateActive, false) {
		t.Fatal("apply")
	}
	clock.Advance(100 * time.Millisecond)
	key := simulation.WorldKey(testDivision, c.Name)
	seed := func() simulation.WorldState { return simulation.SeedWorldState(c) }
	before := rt.Worlds.Snapshot(key, seed).LiveSpawnAt(clock.NowMs())
	rt.HandleTargetInteract(testDivision, c, (wire.CancelActiveEffectRequest{EffectID: 100}).Encode())
	rt.drainStoppedCharacterEffects()
	world := rt.Worlds.Snapshot(key, seed)
	_, run := world.MovementSpeeds()
	if world.LiveSpawnAt(clock.NowMs()) != before || run != 50 || len(c.TimedSkillJobs) != 0 {
		t.Fatal("cancel teleported/kept job", world, c.TimedSkillJobs)
	}
}

/*
================
TestDelayedPreviousSessionCloseCannotEraseAdmittedSpeedScroll
================
*/
func TestDelayedPreviousSessionCloseCannotEraseAdmittedSpeedScroll(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{100: {ID: 100, Group: 9, SpawnStatus: true, EffectDurationMs: 3600000, MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: true, Percent: 100, Persistent: true}}})
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	rt.BindPetSession(testDivision, c, 1)
	if !rt.ApplyCharacterEffect(testDivision, c.Name, 100, 7, statuseffect.StateActive, false) {
		t.Fatal("apply")
	}
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 2); err != nil {
		t.Fatal(err)
	}
	before := rt.EntrySkills(testDivision, c.Name)
	_, speed := rt.EntryMovementSpeeds(testDivision, c.Name)
	if speed != 100 || len(before) != 1 {
		t.Fatal("entry must contain fast speed and scroll")
	}
	// The old socket closes after entry serialization but before game-ready.
	rt.ForgetCharacterSession(testDivision, c.Name, 1)
	rt.BindPetSession(testDivision, c, 2)
	_, speed = rt.EntryMovementSpeeds(testDivision, c.Name)
	if speed != 100 {
		t.Fatalf("admitted scroll lost its speed before game-ready: %v", speed)
	}
	if after := rt.effects.Snapshot(testDivision, c.Name); len(after) != 1 || after[0].InstanceToken != 7 {
		t.Fatal("admitted effect identity changed")
	}
	// Closing the current owner still retires its runtime and checkpoints the job.
	rt.ForgetCharacterSession(testDivision, c.Name, 2)
	if len(rt.EntrySkills(testDivision, c.Name)) != 0 || len(c.TimedSkillJobs) != 1 {
		t.Fatal("current session did not retire/checkpoint")
	}
	if _, exists := rt.characterAdmissions.Load(simulation.WorldKey(testDivision, c.Name)); exists {
		t.Fatal("admission owner leaked")
	}
	// A replacement that closes before game-ready must clean up even though
	// the companion lane still names the previous session.
	rt.BindPetSession(testDivision, c, 2)
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 3); err != nil {
		t.Fatal(err)
	}
	rt.ForgetCharacterSession(testDivision, c.Name, 3)
	if len(rt.EntrySkills(testDivision, c.Name)) != 0 {
		t.Fatal("abandoned admission retained effects")
	}
	if _, exists := rt.characterAdmissions.Load(simulation.WorldKey(testDivision, c.Name)); exists {
		t.Fatal("abandoned admission owner leaked")
	}
}

type namedMovementSkills struct {
	staticSkillSource
	code string
}

/*
================
SkillByCodename
================
*/
func (s namedMovementSkills) SkillByCodename(code string) (enterworld.SkillRow, bool) {
	if code != s.code {
		return enterworld.SkillRow{}, false
	}
	return s.SkillByID(100)
}

/*
================
TestSkillConsumableAdmissionIsDescriptorDrivenAndAtomic
================
*/
func TestSkillConsumableAdmissionIsDescriptorDrivenAndAtomic(t *testing.T) {
	for _, supported := range []bool{true, false} {
		c := testCharacter()
		ref := &enterworld.ItemRef{RefObjID: 90001, Codename: "SYN_ARBITRARY_CONSUMABLE", TypeIDs: [4]int64{3, 3, 13, 2}, AssociatedSkillCodename: "SYN_EFFECT", Country: 3, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 2}}
		rt, clock := newTestRuntime(c, staticItemSource{ref.Codename: ref})
		rt.deps.(*enterworld.Deps).Skills = namedMovementSkills{staticSkillSource{100: {ID: 100, Group: 7, EffectDurationMs: 10000, MovementModifier: enterworld.SkillMovementModifier{Present: true, Supported: supported, Persistent: true, Percent: 50}}}, "SYN_EFFECT"}
		flags := ref.TypeFlags()
		result := rt.HandleItemUse(testDivision, c, []byte{21, byte(flags), byte(flags >> 8)})
		if !supported {
			if c.MissionInventory[0].StackCount != 2 || len(c.TimedSkillJobs) != 0 || result.Frames[0].Payload[0] == 1 {
				t.Fatal("unsupported compound mutated inventory/job")
			}
			continue
		}
		if c.MissionInventory[0].StackCount != 1 || result.Frames[0].Payload[0] != 1 {
			t.Fatal("arbitrary supported family refused")
		}
		clock.Advance(1123 * time.Millisecond)
		rt.ForgetCharacter(testDivision, c.Name)
		if c.TimedSkillJobs[0].RemainingMs != 9000 {
			t.Fatal("native whole-second job checkpoint", c.TimedSkillJobs)
		}
		dead := int64(0)
		c.CurrentHP = &dead
		rt.RestoreTimedSkillJobs(testDivision, c.Name)
		if len(rt.effects.Snapshot(testDivision, c.Name)) != 1 {
			t.Fatal("persistent job lost during dead actor restoration")
		}
	}
}
