package statuseffect

import (
	"bufio"
	"fmt"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestRelativeJobClockAgainstNativeSequences(t *testing.T) {
	f, err := licensed.ServerFixtureReader(t, "internal/game/item/statuseffect/testdata/native-timed-job-sequences.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var clock relativeJobClock
	var queries, forced uint32
	count := 0
	for s.Scan() {
		var group, step, initial, dt, remaining, checkpoint, accrued, active, guard, retirement, wantQueries, wantForced, notices uint32
		var reason int32
		n, err := fmt.Sscan(s.Text(), &group, &step, &initial, &dt, &reason, &remaining, &checkpoint, &accrued, &active, &guard, &retirement, &wantQueries, &wantForced, &notices)
		if err != nil || n != 14 {
			t.Fatal("bad native fixture", n, err)
		}
		if step == 0 {
			clock = relativeJobClock{remaining: initial, active: true}
			queries, forced = 0, 0
		}
		if reason != -1 {
			clock.retire(reason)
		}
		q, stop := clock.advance(math.Float32frombits(dt))
		if q {
			queries++
		}
		if stop {
			forced++
		}
		if clock.remaining != remaining || math.Float32bits(clock.checkpoint) != checkpoint || math.Float32bits(clock.accrued) != accrued || clock.active != (active != 0) || clock.retiredByEffect != (guard != 0) || queries != wantQueries || forced != wantForced {
			t.Fatalf("native mismatch %d/%d: %+v queries=%d forced=%d", group, step, clock, queries, forced)
		}
		count++
	}
	if s.Err() != nil || count != 28 {
		t.Fatal("incomplete fixture", count, s.Err())
	}
}

func TestPersistentRegistryUsesNativeWholeSecondClock(t *testing.T) {
	r := NewRegistry()
	e := Effect{DivisionID: "d", CharacterName: "c", SkillID: 1, SkillGroup: 1, InstanceToken: 1, State: StateActive, Persistent: true, DurationPresent: true, StartedAtMs: 100, ExpiresAtMs: 1100}
	if !r.Apply(e) {
		t.Fatal("apply")
	}
	r.Expire(1100)
	got := r.Snapshot("d", "c")[0]
	if got.StopRequested || got.Expired(1100) || got.PersistentRemainingMs(1100) != 1000 {
		t.Fatal("retired at accumulator == 1", got)
	}
	if len(r.DrainStopRequested()) != 0 {
		t.Fatal("early retirement")
	}
	r.Expire(1101)
	if len(r.DrainStopRequested()) != 1 || len(r.Snapshot("d", "c")) != 0 {
		t.Fatal("expiry not dispatched")
	}
	if len(r.DrainStopRequested()) != 0 {
		t.Fatal("duplicate retirement")
	}
}

func TestJobCheckpointsFollowEachInstallationClock(t *testing.T) {
	r := NewRegistry()
	for i, start := range []int64{0, 1000} {
		if !r.Apply(Effect{DivisionID: "d", CharacterName: "c", SkillID: uint32(i + 1), SkillGroup: uint32(i + 1), InstanceToken: uint32(i + 1), Persistent: true, StartedAtMs: start, ExpiresAtMs: start + 1000000}) {
			t.Fatal("apply")
		}
	}
	batches := r.TakeJobCheckpoints(300000)
	if len(batches) != 1 || len(batches[0].Effects) != 1 || batches[0].Effects[0].SkillID != 1 {
		t.Fatal("installation clocks merged", batches)
	}
	if batches[0].Effects[0].PersistentRemainingMs(300000) != 700000 {
		t.Fatal("bad checkpoint duration")
	}
	if len(r.TakeJobCheckpoints(300000)) != 0 {
		t.Fatal("checkpoint duplicated")
	}
	// The second job reaches 300 seconds, but its second accumulator equals
	// exactly one. Native defers this checkpoint until a qualifying update.
	if len(r.TakeJobCheckpoints(301000)) != 0 {
		t.Fatal("checkpoint bypassed native tick gate")
	}
	batches = r.TakeJobCheckpoints(301001)
	if len(batches) != 1 || len(batches[0].Effects) != 1 || batches[0].Effects[0].SkillID != 2 {
		t.Fatal("second job checkpoint lost", batches)
	}
}
