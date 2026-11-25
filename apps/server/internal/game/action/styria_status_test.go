/*
===========================================================================

styria_status_test.go - Styria Clash: the status snapshot lane

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/styria"
)

/*
================
styriaStatusLog

Records the status snapshots pushed per character name.
================
*/
type styriaStatusLog struct {
	byName map[string][]wire.StyriaStatus
}

func (log *styriaStatusLog) arm(rt *Runtime) {
	rt.PushCharacterFrames = func(_ string, name string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode != wire.OpStyriaStatus {
				continue
			}
			log.byName[name] = append(log.byName[name], styriaDecodeStatus(frame.Payload))
		}
	}
}

func (log *styriaStatusLog) last(name string) (wire.StyriaStatus, bool) {
	rows := log.byName[name]
	if len(rows) == 0 {
		return wire.StyriaStatus{}, false
	}
	return rows[len(rows)-1], true
}

/*
================
styriaDecodeStatus

The wire body read back (wire.EncodeStyriaStatus's layout).
================
*/
func styriaDecodeStatus(payload []byte) wire.StyriaStatus {
	u64 := func(at int) int64 { return int64(binary.LittleEndian.Uint64(payload[at:])) }
	u16 := func(at int) int { return int(binary.LittleEndian.Uint16(payload[at:])) }
	side := int(payload[29])
	if side == wire.StyriaNoSide {
		side = -1
	}
	return wire.StyriaStatus{
		Phase:         int(payload[0]),
		MatchSeq:      binary.LittleEndian.Uint32(payload[1:]),
		NextStartMs:   u64(5),
		WindowOpensMs: u64(13),
		PhaseEndsMs:   u64(21),
		OwnSide:       side,
		OwnState:      int(payload[30]),
		Hunters:       u16(31),
		Thieves:       u16(33),
		ScoreHunters:  u16(35),
		ScoreThieves:  u16(37),
		Outcome:       int(payload[39]),
		JobExp:        int64(binary.LittleEndian.Uint32(payload[40:])),
		SkillExp:      int64(binary.LittleEndian.Uint32(payload[44:])),
	}
}

/*
================
TestStyriaStatusRegistrationStanding

The registration standing: a watcher before the window's start,
then applied, with the division's counts; pushes fire on change
and stay silent while nothing moves.
================
*/
func TestStyriaStatusRegistrationStanding(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	log := &styriaStatusLog{byName: map[string][]wire.StyriaStatus{}}
	log.arm(rt)

	frame, ok := rt.styriaStatusFrame(testDivision, hunter)
	if !ok {
		t.Fatal("no status frame while the engine runs")
	}
	if frame.Opcode != wire.OpStyriaStatus {
		t.Fatalf("frame opcode = 0x%04X", frame.Opcode)
	}
	status := styriaDecodeStatus(frame.Payload)
	start := time.Date(2026, 1, 5, 20, 0, 0, 0, time.UTC).UnixMilli()
	if status.Phase != wire.StyriaPhaseRegistration || status.OwnState != wire.StyriaStateWatching {
		t.Fatalf("watcher standing = phase %d state %d", status.Phase, status.OwnState)
	}
	if status.NextStartMs != start || status.WindowOpensMs != start-10*60*1000 {
		t.Fatalf("window = opens %d start %d", status.WindowOpensMs, status.NextStartMs)
	}

	rt.styriaApply(testDivision, hunter)
	rt.styriaPushStatuses(clock.NowMs())
	status, ok = log.last("Hunter")
	if !ok {
		t.Fatal("the applicant was not pushed a snapshot")
	}
	if status.OwnState != wire.StyriaStateApplied || status.OwnSide != styriaSideHunters {
		t.Fatalf("applied standing = state %d side %d", status.OwnState, status.OwnSide)
	}
	if status.Hunters != 1 || status.Thieves != 0 {
		t.Fatalf("counts = %d/%d, want 1/0", status.Hunters, status.Thieves)
	}
	pushes := len(log.byName["Hunter"])
	rt.styriaPushStatuses(clock.NowMs())
	if len(log.byName["Hunter"]) != pushes {
		t.Fatal("an unchanged standing pushed again")
	}

	rt.styriaApply(testDivision, thief)
	rt.styriaPushStatuses(clock.NowMs())
	status, _ = log.last("Hunter")
	if status.Hunters != 1 || status.Thieves != 1 {
		t.Fatalf("counts after the second application = %d/%d, want 1/1", status.Hunters, status.Thieves)
	}
}

/*
================
TestStyriaStatusMatchAndResult

The entered pair's standing: the match phase with its score, then
the result with the outcome and the viewer's own reward figures
(the figures the reward drain pays: win 20, lose 3, the level band).
================
*/
func TestStyriaStatusMatchAndResult(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	thief := styriaTestCharacter(2, "Thief", domain.JobThief)
	rt, clock := newStyriaRuntime(t, hunter, thief)
	log := &styriaStatusLog{byName: map[string][]wire.StyriaStatus{}}
	log.arm(rt)

	match := styriaStartApplied(t, rt, clock, hunter, thief)
	status, ok := log.last("Hunter")
	if !ok {
		t.Fatal("no snapshot pushed at the match start")
	}
	if status.Phase != wire.StyriaPhaseMatch || status.MatchSeq != 1 ||
		status.OwnState != wire.StyriaStateEntered || status.OwnSide != styriaSideHunters {
		t.Fatalf("match standing = %+v", status)
	}
	if status.PhaseEndsMs != match.endsMs {
		t.Fatalf("phase end = %d, want the clock %d", status.PhaseEndsMs, match.endsMs)
	}

	match.mu.Lock()
	match.score[styriaSideHunters] = 2
	match.score[styriaSideThieves] = 1
	match.mu.Unlock()
	rt.styriaPushStatuses(clock.NowMs())
	status, _ = log.last("Thief")
	if status.ScoreHunters != 2 || status.ScoreThieves != 1 || status.OwnSide != styriaSideThieves {
		t.Fatalf("score standing = %+v", status)
	}

	clock.now = time.Date(2026, 1, 5, 20, 11, 0, 0, time.UTC)
	rt.advanceStyriaClash(clock.NowMs())
	status, _ = log.last("Hunter")
	if status.Phase != wire.StyriaPhaseResult || status.Outcome != styriaOutcomeHunters {
		t.Fatalf("result standing = %+v", status)
	}
	if status.JobExp != styria.RewardJobExpWin || status.SkillExp != styria.RewardSkillExp(20) {
		t.Fatalf("winner figures = job %d skill %d", status.JobExp, status.SkillExp)
	}
	status, _ = log.last("Thief")
	if status.JobExp != styria.RewardJobExpLose || status.SkillExp != styria.RewardSkillExp(20) {
		t.Fatalf("loser figures = job %d skill %d", status.JobExp, status.SkillExp)
	}
}

/*
================
TestStyriaStatusOffStaysSilent

No engine, no lane: no frame for the conversation, no pushes, and
the built standing is the empty registration shape.
================
*/
func TestStyriaStatusOffStaysSilent(t *testing.T) {
	hunter := styriaTestCharacter(1, "Hunter", domain.JobHunter)
	rt, _ := newStyriaRuntime(t, hunter)
	rt.styria = nil

	if _, ok := rt.styriaStatusFrame(testDivision, hunter); ok {
		t.Fatal("a status frame was built with the engine off")
	}
	rt.styriaPushStatuses(0)
	status := rt.styriaStatusFor(testDivision, hunter, 0)
	if status.Phase != wire.StyriaPhaseRegistration || status.OwnSide != -1 {
		t.Fatalf("off standing = %+v", status)
	}
}
