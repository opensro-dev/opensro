/*
===========================================================================

jobrank_test.go - the job guilds' rank and contribution lists

===========================================================================
*/

package action

import (
	"bytes"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
jobRankSnapshot

The fixture's dependencies with a closed week's snapshot.
================
*/
type jobRankSnapshot struct {
	Dependencies
	rankings domain.JobRankings
}

/*
================
WeekRankings
================
*/
func (s jobRankSnapshot) WeekRankings(string) domain.JobRankings { return s.rankings }

/*
================
TestJobRankServesLastWeek

512C20 answers the snapshot's list as [1][job][kind][n] rows.
================
*/
func TestJobRankServesLastWeek(t *testing.T) {
	rt, _, c := jobFixture(t)
	snapshot := domain.JobRankings{Week: 3}
	snapshot.Lists[domain.JobTrader-1][domain.JobRankActivity] = []domain.JobRankRow{{Alias: "High", Grade: 2, Value: 10}, {Alias: "Mid", Grade: 1, Value: 950}}
	rt.deps = jobRankSnapshot{Dependencies: rt.deps, rankings: snapshot}
	out := rt.HandleJobRank(testDivision, c, wire.NewWriter(6).U32(jobTestNpc).U8(domain.JobTrader).U8(0).Payload())
	want := wire.NewWriter(64).U8(1).U8(domain.JobTrader).U8(0).U8(2).
		U8(1).Str("High").U8(2).U32(10).
		U8(2).Str("Mid").U8(1).U32(950).Payload()
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opJobRankResponse || !bytes.Equal(out.Frames[0].Payload, want) {
		t.Fatalf("activity = %+v, want %x", out.Frames, want)
	}
	empty := rt.HandleJobRank(testDivision, c, wire.NewWriter(6).U32(jobTestNpc).U8(domain.JobHunter).U8(1).Payload())
	if !bytes.Equal(empty.Frames[0].Payload, []byte{1, domain.JobHunter, 1, 0}) {
		t.Fatalf("an empty list = %x", empty.Frames[0].Payload)
	}
}

/*
================
TestJobRankRefusals

512C20's order: the NPC in range (3), the job (0x28), then the kind (0x0F).
================
*/
func TestJobRankRefusals(t *testing.T) {
	rt, _, c := jobFixture(t)
	for _, tc := range []struct {
		npc       uint32
		job, kind uint8
		code      uint8
	}{
		{jobTestNpc + 1, domain.JobTrader, 0, jobErrTooFar},
		{jobTestNpc, 4, 0, jobErrInvalidJob},
		{jobTestNpc, domain.JobHunter, 2, jobRankErrKind},
	} {
		out := rt.HandleJobRank(testDivision, c, wire.NewWriter(6).U32(tc.npc).U8(tc.job).U8(tc.kind).Payload())
		if want := []byte{2, tc.code, tc.job, tc.kind}; !bytes.Equal(out.Frames[0].Payload, want) {
			t.Fatalf("%+v = %x, want %x", tc, out.Frames[0].Payload, want)
		}
	}
	if out := rt.HandleJobRank(testDivision, c, []byte{1, 2, 3}); len(out.Frames) != 0 {
		t.Fatalf("a short request was answered: %+v", out.Frames)
	}
}

/*
================
TestJobWeekIndexTurnsOnMonday

Sunday 23:59 and Monday 00:00 fall in consecutive weeks; the epoch's
Thursday belongs to week 0.
================
*/
func TestJobWeekIndexTurnsOnMonday(t *testing.T) {
	sunday := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	monday := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	if a, b := JobWeekIndex(sunday, time.UTC), JobWeekIndex(monday, time.UTC); b != a+1 {
		t.Fatalf("sunday %d, monday %d", a, b)
	}
	if JobWeekIndex(time.Unix(0, 0), time.UTC) != 0 || JobWeekIndex(time.Date(1970, 1, 5, 0, 0, 0, 0, time.UTC), time.UTC) != 1 {
		t.Fatal("the epoch week is misplaced")
	}
}

/*
================
jobWeekRecorder

The fixture's dependencies, recording the weeks each division closes.
================
*/
type jobWeekRecorder struct {
	Dependencies
	closed map[string][]int64
}

/*
================
CloseWeek
================
*/
func (r jobWeekRecorder) CloseWeek(division string, week int64, label string) bool {
	r.closed[division] = append(r.closed[division], week)
	return true
}

/*
================
TestJobWeekTickClosesEveryDivision

Each division hands its own week to the store once per week; one division
latching a week must not hide it from another.
================
*/
func TestJobWeekTickClosesEveryDivision(t *testing.T) {
	rt, _, _ := jobFixture(t)
	recorder := jobWeekRecorder{Dependencies: rt.deps, closed: map[string][]int64{}}
	rt.deps = recorder
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.Local).UnixMilli()
	for _, at := range []int64{now, now + 1000} {
		rt.JobWeekTick("a", at)
		rt.JobWeekTick("b", at)
	}
	week := JobWeekIndex(time.UnixMilli(now), time.Local)
	if len(recorder.closed["a"]) != 1 || len(recorder.closed["b"]) != 1 || recorder.closed["b"][0] != week {
		t.Fatalf("closed %+v", recorder.closed)
	}
	rt.JobWeekTick("a", now+7*24*3600*1000)
	if got := recorder.closed["a"]; len(got) != 2 || got[1] != week+1 {
		t.Fatalf("the next week did not reach the store: %+v", got)
	}
}
