/*
===========================================================================

killstats_test.go - the port-only kill and EXP rollup (SRO_KILL_STATS)

Off counts nothing. On, a settled kill counts once in the killer's
band, every recipient's EXP and skill EXP count in their own band,
online minutes accrue per entered character, and a flush drains
counts only - the distinct-character set behind a band never leaves
memory and a past day's cell is dropped at its last flush.

===========================================================================
*/

package action

import (
	"errors"
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// killStatNow is 2026-10-26 12:00 UTC; its day starts at 1792972800.
const killStatNow = int64(1793016000) * 1000

func killStatCharacter(id int64, level int64) *enterworld.Character {
	return &enterworld.Character{ID: id, Level: &level}
}

/*
================
TestKillStatsFromEnv
================
*/
func TestKillStatsFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvKillStats, value)
		if got := KillStatsFromEnv(); got != want {
			t.Fatalf("%s=%q: %v, want %v", EnvKillStats, value, got, want)
		}
	}
}

/*
================
TestKillStatBandAndDayEdges
================
*/
func TestKillStatBandAndDayEdges(t *testing.T) {
	for level, want := range map[int64]int{0: 0, 1: 0, 10: 0, 11: 1, 80: 7, 81: 8, 90: 8, 91: 9, 110: 10} {
		if got := killStatBandOf(level); got != want {
			t.Fatalf("level %d: band %d, want %d", level, got, want)
		}
	}
	day := killStatDayOf(killStatNow)
	if day != 1792972800 {
		t.Fatalf("day of %d = %d, want 1792972800", killStatNow, day)
	}
	if next := killStatDayOf(killStatNow + killStatMsPerDay); next != day+86400 {
		t.Fatalf("next day = %d, want %d", next, day+86400)
	}
}

/*
================
TestKillStatsCountsAndFlushDrainsCountsOnly

Kills and awards land in the right bands; the flush reports counts
and distinct characters, clears what the sink accepted, and a second
flush of an unmoved rollup sends nothing.
================
*/
func TestKillStatsCountsAndFlushDrainsCountsOnly(t *testing.T) {
	rt := &Runtime{KillStats: true}
	rt.Now = func() time.Time { return time.UnixMilli(killStatNow) }
	var flushed []KillStatRow
	rt.KillStatsFlush = func(rows []KillStatRow) error {
		flushed = append(flushed, rows...)
		return nil
	}
	killer := killStatCharacter(7, 85)
	peer := killStatCharacter(9, 35)
	rt.countKillStatKill(killer, killStatNow)
	rt.countKillStatAward(killer, 100, 40, killStatNow)
	rt.countKillStatAward(peer, 55, 0, killStatNow)
	rt.killStats.accrue(killStatDayOf(killStatNow), []*enterworld.Character{killer, peer}, 3600000)
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 2 {
		t.Fatalf("flushed %+v, want two band rows", flushed)
	}
	byBand := map[int]KillStatRow{}
	for _, row := range flushed {
		byBand[row.Band] = row
		if row.DayS != 1792972800 {
			t.Fatalf("row day = %d, want 1792972800", row.DayS)
		}
	}
	high := byBand[8]
	if high.Kills != 1 || high.Exp != 100 || high.SkillExp != 40 || high.OnlineMs != 3600000 || high.Characters != 1 {
		t.Fatalf("band 81-90 row = %+v", high)
	}
	low := byBand[3]
	if low.Kills != 0 || low.Exp != 55 || low.SkillExp != 0 || low.OnlineMs != 3600000 || low.Characters != 1 {
		t.Fatalf("band 31-40 row = %+v", low)
	}
	flushed = nil
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if len(flushed) != 0 {
		t.Fatalf("second flush sent %+v, want nothing (the deltas were accepted)", flushed)
	}
}

/*
================
TestKillStatsOffCountsNothing

The default: no cell is ever created, so nothing can flush even with
a sink wired.
================
*/
func TestKillStatsOffCountsNothing(t *testing.T) {
	rt := &Runtime{}
	c := killStatCharacter(7, 85)
	rt.countKillStatKill(c, killStatNow)
	rt.countKillStatAward(c, 100, 40, killStatNow)
	rt.advanceKillStats(killStatNow + killStatMsPerDay)
	if len(rt.killStats.cells) != 0 {
		t.Fatalf("flag off created cells: %+v", rt.killStats.cells)
	}
}

/*
================
blockingKillStatSink

A sink whose first write waits for release; every written row is
recorded, so a test can total what reached the store.
================
*/
type blockingKillStatSink struct {
	mu      sync.Mutex
	rows    []KillStatRow
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingKillStatSink() *blockingKillStatSink {
	return &blockingKillStatSink{entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingKillStatSink) write(rows []KillStatRow) error {
	s.mu.Lock()
	s.rows = append(s.rows, rows...)
	s.mu.Unlock()
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return nil
}

func (s *blockingKillStatSink) kills() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, row := range s.rows {
		total += row.Kills
	}
	return total
}

/*
================
TestKillStatsConcurrentFlushesWriteEachDeltaOnce

The cadence write is in flight (blocked in the sink) while a shutdown
flush runs: the shutdown waits it out, then writes only what accrued
meanwhile. Two counted kills reach the sink as two, never three.
================
*/
func TestKillStatsConcurrentFlushesWriteEachDeltaOnce(t *testing.T) {
	rt := &Runtime{KillStats: true}
	rt.Now = func() time.Time { return time.UnixMilli(killStatNow) }
	sink := newBlockingKillStatSink()
	rt.KillStatsFlush = sink.write
	killer := killStatCharacter(7, 85)
	rt.countKillStatKill(killer, killStatNow)
	rt.flushKillStatsAsync()
	<-sink.entered
	rt.countKillStatKill(killer, killStatNow)
	flushed := make(chan error, 1)
	go func() { flushed <- rt.FlushKillStats() }()
	close(sink.release)
	if err := <-flushed; err != nil {
		t.Fatal(err)
	}
	if got := sink.kills(); got != 2 {
		t.Fatalf("the sink received %d kills, want the counted 2 exactly once", got)
	}
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if got := sink.kills(); got != 2 {
		t.Fatalf("a later flush rewrote deltas: %d kills", got)
	}
}

/*
================
TestKillStatsCadenceFlushWhileOneFliesWaitsItsTurn

A second cadence flush while the first write is still in the sink
finds the write in flight and keeps its deltas for later: the
in-flight write settles once (a second settle would close its done
channel twice), and the shutdown flush then writes the rest. Two
counted kills reach the sink as two.
================
*/
func TestKillStatsCadenceFlushWhileOneFliesWaitsItsTurn(t *testing.T) {
	rt := &Runtime{KillStats: true}
	rt.Now = func() time.Time { return time.UnixMilli(killStatNow) }
	sink := newBlockingKillStatSink()
	rt.KillStatsFlush = sink.write
	killer := killStatCharacter(7, 85)
	rt.countKillStatKill(killer, killStatNow)
	rt.flushKillStatsAsync()
	<-sink.entered
	rt.countKillStatKill(killer, killStatNow)
	rt.flushKillStatsAsync()
	close(sink.release)
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if got := sink.kills(); got != 2 {
		t.Fatalf("the sink received %d kills, want the counted 2 exactly once", got)
	}
}

/*
================
TestKillStatsFailedWriteReturnsItsDeltas

A write the sink refuses hands its deltas back to the cells, so the
next flush writes them: one counted kill, one failed write, one good
write, and the store holds the one kill.
================
*/
func TestKillStatsFailedWriteReturnsItsDeltas(t *testing.T) {
	rt := &Runtime{KillStats: true}
	rt.Now = func() time.Time { return time.UnixMilli(killStatNow) }
	var stored int64
	refuse := true
	rt.KillStatsFlush = func(rows []KillStatRow) error {
		if refuse {
			refuse = false
			return errors.New("database is locked")
		}
		for _, row := range rows {
			stored += row.Kills
		}
		return nil
	}
	rt.countKillStatKill(killStatCharacter(7, 85), killStatNow)
	if err := rt.FlushKillStats(); err == nil {
		t.Fatal("the refused write reported no error")
	}
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("after a refused then a good write the store holds %d kills, want 1", stored)
	}
}

/*
================
TestKillStatsTickFlushNeverCallsTheSinkOnTheTick

The cadence path hands the write to a goroutine: advanceKillStats
returns while the sink is still blocked, and the write lands once
afterwards.
================
*/
func TestKillStatsTickFlushNeverCallsTheSinkOnTheTick(t *testing.T) {
	rt := &Runtime{KillStats: true}
	rt.Now = func() time.Time { return time.UnixMilli(killStatNow) }
	sink := newBlockingKillStatSink()
	rt.KillStatsFlush = sink.write
	rt.advanceKillStats(killStatNow)
	rt.countKillStatKill(killStatCharacter(7, 85), killStatNow)
	returned := make(chan struct{})
	go func() {
		rt.advanceKillStats(killStatNow + killStatFlushEveryMs + 1000)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("the tick blocked on the sink")
	}
	<-sink.entered
	close(sink.release)
	if err := rt.FlushKillStats(); err != nil {
		t.Fatal(err)
	}
	if got := sink.kills(); got != 1 {
		t.Fatalf("the sink received %d kills, want the counted 1", got)
	}
}

/*
================
TestKillStatsSettlementCountsTheKillAndTheAward

Through a real settlement: one fatal kill counts once in the killer's
band, and the EXP and skill EXP the settlement pays count there too.
Flag off, the same kill counts nothing.
================
*/
func TestKillStatsSettlementCountsTheKillAndTheAward(t *testing.T) {
	for _, on := range []bool{true, false} {
		rt, _, actor, target := newCombatTestRuntime(t, 100)
		rt.KillStats = on
		var flushed []KillStatRow
		rt.KillStatsFlush = func(rows []KillStatRow) error {
			flushed = append(flushed, rows...)
			return nil
		}
		var paidExp, paidSkill int64
		rt.UpdateExperience = func(c *enterworld.Character, exp, sexp int64, defeated uint32) ([]wire.Frame, bool) {
			if c == actor {
				paidExp += exp
				paidSkill += sexp
			}
			return []wire.Frame{{Opcode: wire.OpExpUpdate}}, true
		}
		gid := enterworld.ObjectIDForCharacter(actor)
		hits := rt.Monsters.ApplyDamageSequence(testDivision, target.Gid, 100, []simulation.MonsterDamagePlan{{GID: target.Gid, Damage: 99, CreditGID: gid}})
		if len(hits) != 1 || hits[0].Fatal {
			t.Fatal("preparation failed")
		}
		now := rt.Now().UnixMilli()
		result := rt.HandleTargetInteract(testDivision, actor, wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: target.Gid}.Encode())
		if result.DiagnosticRefusal != "" {
			t.Fatalf("the kill was refused: %q", result.DiagnosticRefusal)
		}
		if err := rt.FlushKillStats(); err != nil {
			t.Fatal(err)
		}
		if !on {
			if len(flushed) != 0 || len(rt.killStats.cells) != 0 {
				t.Fatalf("flag off counted %+v", flushed)
			}
			continue
		}
		if paidExp <= 0 {
			t.Fatal("the fixture kill paid no EXP; the test would prove nothing")
		}
		if len(flushed) != 1 {
			t.Fatalf("flushed %+v, want the one band row", flushed)
		}
		row := flushed[0]
		if row.Band != 0 || row.Kills != 1 || row.Exp != paidExp || row.SkillExp != paidSkill || row.Characters != 1 {
			t.Fatalf("row = %+v, want one kill and the paid %d/%d in band 1-10", row, paidExp, paidSkill)
		}
		if row.DayS != killStatDayOf(now) {
			t.Fatalf("row day = %d, want %d", row.DayS, killStatDayOf(now))
		}
	}
}
