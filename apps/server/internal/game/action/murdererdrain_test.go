/*
===========================================================================

murdererdrain_test.go - the murderer status HP drain on the PK sweep

The drain is a clock, so the tests drive the injected clock through the PK
sweep the real tick runs (advancePKRecords): a murderer loses 5% of its
maximum HP on the tenth second and not before, a non-murderer or an offline
record loses nothing, a lethal pulse settles as a murderer death with the
existing penalty relief and drop roll and publishes one 0-HP baseline, a
revival restarts the full period, and the drain stops once the penalty is
gone.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	murdererDrainTestPenalty = 5000
	murdererDrainTestPeriod  = 10 * time.Second
)

/*
================
admitDrainTestPlayer

The drain runs only for a world resident: bind the recovery session the
real world bind stores (naturalrecovery.go). The drain's tests call the PK
sweep directly, so the session's own recovery pulse never runs here.
================
*/
func admitDrainTestPlayer(rt *Runtime, c *enterworld.Character) {
	rt.BindRecoverySession(testDivision, c, 1)
}

/*
================
vitalsHPRefresh

The HP carried by a 0x33A6 HP-only refresh, and whether a frame is one.
================
*/
func vitalsHPRefresh(frame simulation.Frame) (uint32, bool) {
	if frame.Opcode != simulation.OpVitalsUpdate || len(frame.Payload) != 11 || frame.Payload[6] != 0x01 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(frame.Payload[7:11]), true
}

/*
================
TestMurdererDrainsFivePercentOnTheTenthSecond

5% of the maximum, truncated, every 10 s: the frame before the period is
silent and the frame on it publishes the loss to the victim and observers.
================
*/
func TestMurdererDrainsFivePercentOnTheTenthSecond(t *testing.T) {
	c := testCharacter()
	maximum := enterworld.DerivedMaxHP(c)
	hp := maximum
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)
	admitDrainTestPlayer(rt, c)

	// The first sweep only arms the clock.
	rt.advancePKRecords()
	if got := enterworld.CurrentHP(c); got != maximum {
		t.Fatalf("HP %d on the arming sweep, want %d", got, maximum)
	}

	clock.Advance(murdererDrainTestPeriod - time.Millisecond)
	if frames := rt.advancePKRecords(); len(frames) != 0 {
		t.Fatalf("drained before the period: %+v", frames)
	}
	if got := enterworld.CurrentHP(c); got != maximum {
		t.Fatalf("HP %d one millisecond early, want %d", got, maximum)
	}

	clock.Advance(time.Millisecond)
	frames := rt.advancePKRecords()
	want := maximum - maximum/20
	if got := enterworld.CurrentHP(c); got != want {
		t.Fatalf("HP %d on the period, want %d (5%% of %d)", got, want, maximum)
	}
	if len(frames) != 2 {
		t.Fatalf("drain published %+v, want the victim refresh and the observer refresh", frames)
	}
	gid := enterworld.ObjectIDForCharacter(c)
	var private, public []simulation.Frame
	for _, routed := range frames {
		if routed.OnlyCharacterID == c.ID {
			private = append(private, routed.Frames...)
			continue
		}
		if routed.SourceGID != gid {
			t.Fatalf("public drain batch names source %d, want %d", routed.SourceGID, gid)
		}
		public = append(public, routed.Frames...)
	}
	if !hasSimOpcode(private, simulation.OpVitalsUpdate) {
		t.Fatalf("victim frames %+v lack the vitals refresh", private)
	}
	observed, ok := uint32(0), false
	for _, frame := range public {
		if value, isVitals := vitalsHPRefresh(frame); isVitals {
			observed, ok = value, true
		}
	}
	if !ok || observed != uint32(want) {
		t.Fatalf("observers saw HP %d (refresh %v), want %d", observed, ok, want)
	}
}

/*
================
TestNonMurdererLosesNothing

An armed penalty keeper runs a record clock, but only a positive penalty
is murderer status: nothing is drained.
================
*/
func TestNonMurdererLosesNothing(t *testing.T) {
	c := testCharacter()
	hp := enterworld.DerivedMaxHP(c)
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{TotalCount: 3, TotalDecayAt: time.UnixMilli(1_000_000).Add(time.Hour).Unix()}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)
	admitDrainTestPlayer(rt, c)

	rt.advancePKRecords()
	clock.Advance(time.Minute)
	if frames := rt.advancePKRecords(); len(frames) != 0 {
		t.Fatalf("a non-murderer was drained: %+v", frames)
	}
	if got := enterworld.CurrentHP(c); got != hp {
		t.Fatalf("non-murderer HP %d, want %d", got, hp)
	}
}

/*
================
TestOfflineMurdererLosesNothing

The PK keeper decays an offline record's counters, but the drain is HP
pressure on a body in the world: an unadmitted murderer is not drained.
================
*/
func TestOfflineMurdererLosesNothing(t *testing.T) {
	c := testCharacter()
	hp := enterworld.DerivedMaxHP(c)
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)

	rt.advancePKRecords()
	clock.Advance(time.Minute)
	if frames := rt.advancePKRecords(); len(frames) != 0 {
		t.Fatalf("an offline murderer was drained: %+v", frames)
	}
	if got := enterworld.CurrentHP(c); got != hp {
		t.Fatalf("offline murderer HP %d, want %d", got, hp)
	}
}

/*
================
TestMurdererDrainStopsWhenPenaltyReachesZero

The drain stops with murderer status, and the clock is forgotten so a
returning penalty starts a fresh period.
================
*/
func TestMurdererDrainStopsWhenPenaltyReachesZero(t *testing.T) {
	c := testCharacter()
	maximum := enterworld.DerivedMaxHP(c)
	hp := maximum
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{Penalty: 200}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)
	admitDrainTestPlayer(rt, c)

	rt.advancePKRecords()
	clock.Advance(murdererDrainTestPeriod)
	rt.advancePKRecords()
	if got := enterworld.CurrentHP(c); got != maximum-maximum/20 {
		t.Fatalf("HP %d after one drain, want %d", got, maximum-maximum/20)
	}

	pk.AddPenalty(c, -200, rt.Now())
	if c.PK.Penalty != 0 || murderer(c) {
		t.Fatalf("record %+v, want the penalty gone", c.PK)
	}
	clock.Advance(murdererDrainTestPeriod)
	if frames := rt.advancePKRecords(); len(frames) != 0 {
		t.Fatalf("drained after the penalty reached zero: %+v", frames)
	}
	if got := enterworld.CurrentHP(c); got != maximum-maximum/20 {
		t.Fatalf("HP %d after the drain stopped, want %d", got, maximum-maximum/20)
	}
	if _, tracked := rt.murdererDrains[petOwnerKey{division: testDivision, name: c.Name}]; tracked {
		t.Fatal("the drain clock survived murderer status")
	}
}

/*
================
TestMurdererDrainDeathIsAMurdererDeath

A lethal pulse reuses the shared fatal transition: the death is classified
as pk.DeathPlayer, the penalty drops by 200, the daily count by one, and
the murderer drop roll runs (penalty 30000 always drops). The fixture's
rand() is 100, so the equipped roll lands on an empty slot and the bag
potion goes.
================
*/
func TestMurdererDrainDeathIsAMurdererDeath(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	admitDrainTestPlayer(rt, c)
	c.Level = testInt64(11)
	c.MaxLevel = testInt64(11)
	hp := int64(1)
	c.CurrentHP = &hp
	day := clock.Now().Local()
	c.PK = &domain.PKRecord{Penalty: 30000, DailyCount: 1, DailyDay: int32(day.Year()*10000 + int(day.Month())*100 + day.Day())}
	potion := &enterworld.ItemRef{RefObjID: 9, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1}, CanDropOnDeath: true}
	rt.deps.ItemReferences().(staticItemSource)[potion.Codename] = potion
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 13, RefObjID: potion.RefObjID,
		Codename: potion.Codename, TypeFlags: potion.TypeFlags(), VarianceBits: "0", StackCount: 5})
	rt.pkOwners.track(testDivision, c.Name)
	var loss pk.LossRule
	rt.ApplyDeathPenalty = func(_ *enterworld.Character, penalty pk.DeathPenalty) ([]wire.Frame, bool) {
		loss = penalty.Rule
		return nil, true
	}

	rt.advancePKRecords()
	clock.Advance(murdererDrainTestPeriod)
	frames := rt.advancePKRecords()

	if got := enterworld.CurrentHP(c); got != 0 {
		t.Fatalf("HP %d after the lethal pulse, want the corpse", got)
	}
	if c.DeathKind != uint8(pk.DeathPlayer) {
		t.Fatalf("death kind %d, want a player death", c.DeathKind)
	}
	// No killer object: DeathLoss reads the killer-less murderer row, the
	// same row a monster's blow takes.
	if loss != (pk.LossRule{Percent: 0.06, CapFactor: 300, SP: 60}) {
		t.Fatalf("death loss %+v, want the killer-less murderer row", loss)
	}
	if c.PK.Penalty != 30000-200 || c.PK.DailyCount != 0 {
		t.Fatalf("record %+v, want penalty 29800 and daily 0", c.PK)
	}
	for _, row := range c.MissionInventory {
		if row.Slot == 13 {
			t.Fatal("the murderer drop roll did not run")
		}
	}
	var private, public []simulation.Frame
	for _, routed := range frames {
		if routed.OnlyCharacterID == c.ID {
			private = append(private, routed.Frames...)
		} else {
			public = append(public, routed.Frames...)
		}
	}
	if !hasSimOpcode(private, wire.OpDeathNotice) || !hasSimOpcode(private, wire.OpItemMoveResponse) ||
		!hasSimOpcode(private, wire.OpPKPenalty) || !hasSimOpcode(private, wire.OpPKDaily) {
		t.Fatalf("victim frames %+v lack the death notice, drop result or PK updates", private)
	}
	if !hasSimOpcode(public, wire.OpSingleObjectSpawn) {
		t.Fatalf("observer frames %+v lack the drop spawn", public)
	}
	zeroHP := 0
	for _, frame := range public {
		if value, isVitals := vitalsHPRefresh(frame); isVitals && value == 0 {
			zeroHP++
		}
	}
	if zeroHP != 1 {
		t.Fatalf("observers saw %d zero-HP vitals frames, want the death baseline's one", zeroHP)
	}

	// A corpse is not drained, even though it is still a murderer.
	clock.Advance(murdererDrainTestPeriod)
	if frames := rt.advancePKRecords(); len(frames) != 0 || enterworld.CurrentHP(c) != 0 {
		t.Fatalf("a corpse was drained: HP %d, frames %+v", enterworld.CurrentHP(c), frames)
	}
}

/*
================
TestMurdererDrainRestartsAfterRevival

A corpse keeps no drain clock: a murderer revived 9.9 s into a period is
not bled 100 ms later. The clock arms at the revival sweep and the first
pulse lands a full period after it.
================
*/
func TestMurdererDrainRestartsAfterRevival(t *testing.T) {
	c := rebirthTestCharacter(1, 0)
	c.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)
	admitDrainTestPlayer(rt, c)

	// Two dead sweeps: the first sight used to arm the clock on the corpse.
	rt.advancePKRecords()
	clock.Advance(murdererDrainTestPeriod - 100*time.Millisecond)
	rt.advancePKRecords()
	if _, tracked := rt.murdererDrains[petOwnerKey{division: testDivision, name: c.Name}]; tracked {
		t.Fatal("a corpse kept a drain clock")
	}

	if out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtPresentPoint}); len(out.Frames) == 0 || !enterworld.CharacterAlive(c) {
		t.Fatalf("rebirth refused: %+v", out)
	}
	hp := enterworld.CurrentHP(c)
	rt.advancePKRecords() // arm at the revival sweep

	clock.Advance(100 * time.Millisecond)
	if frames := rt.advancePKRecords(); len(frames) != 0 || enterworld.CurrentHP(c) != hp {
		t.Fatalf("drained 100 ms after revival: HP %d, frames %+v", enterworld.CurrentHP(c), frames)
	}
	clock.Advance(murdererDrainTestPeriod - 101*time.Millisecond)
	if frames := rt.advancePKRecords(); len(frames) != 0 {
		t.Fatalf("drained before the revived period: %+v", frames)
	}
	clock.Advance(time.Millisecond)
	rt.advancePKRecords()
	if got, want := enterworld.CurrentHP(c), hp-pk.DrainAmount(enterworld.DerivedMaxHP(c)); got != want {
		t.Fatalf("HP %d after the revived period, want %d", got, want)
	}
}

/*
================
TestMurdererDrainRunsOnTheActionTick

The drain belongs to the action runtime's tick hook, which the
sro-gameworld wiring installs, so the rule runs in the real GameWorld and
not only in tests.
================
*/
func TestMurdererDrainRunsOnTheActionTick(t *testing.T) {
	c := testCharacter()
	maximum := enterworld.DerivedMaxHP(c)
	hp := maximum
	c.CurrentHP = &hp
	c.PK = &domain.PKRecord{Penalty: murdererDrainTestPenalty}
	rt, clock := newTestRuntime(c, testItems())
	rt.pkOwners.track(testDivision, c.Name)
	admitDrainTestPlayer(rt, c)

	rt.TickHook()(clock.NowMs())
	clock.Advance(murdererDrainTestPeriod)
	if enterworld.CurrentHP(c) != maximum {
		t.Fatal("the arming tick drained")
	}
	rt.TickHook()(clock.NowMs())
	if got, want := enterworld.CurrentHP(c), maximum-maximum/20; got != want {
		t.Fatalf("HP %d after a tick period, want %d", got, want)
	}
}
