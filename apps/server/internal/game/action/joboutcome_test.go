/*
===========================================================================

joboutcome_test.go - the thief and hunter weekly outcome, previous job info

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
hunterGuildFixture

jobFixture's character as a hunter, its NPC a hunter guild (NPC_KT_MINISTER).
================
*/
func hunterGuildFixture(t *testing.T, reward int32) (*Runtime, *domain.Character) {
	t.Helper()
	rt, _, c := jobFixture(t)
	rt.NpcRoster[0].Codename = "NPC_KT_MINISTER"
	rt.NpcRoster[0].Services = simulation.NpcServicesForCodename("NPC_KT_MINISTER")
	c.Job = domain.CharacterJob{Type: domain.JobHunter, Grade: 2, Alias: "Watch", Reward: reward}
	gold := int64(1000)
	c.Gold = &gold
	return rt, c
}

/*
================
TestJobOutcomeQueryThenCollect

512E70: mode 0 tells the reward; mode 1 pays it as gold and clears it;
collecting nothing is 0x2A.
================
*/
func TestJobOutcomeQueryThenCollect(t *testing.T) {
	rt, c := hunterGuildFixture(t, 750)
	query := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(0).Payload())
	if !bytes.Equal(query.Frames[0].Payload, wire.NewWriter(6).U8(1).U8(0).U32(750).Payload()) || c.Job.Reward != 750 {
		t.Fatalf("query = %x, reward %d", query.Frames[0].Payload, c.Job.Reward)
	}
	collect := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(1).Payload())
	if len(collect.Frames) != 2 || !bytes.Equal(collect.Frames[0].Payload, wire.NewWriter(6).U8(1).U8(1).U32(750).Payload()) {
		t.Fatalf("collect = %+v", collect.Frames)
	}
	if *c.Gold != 1750 || c.Job.Reward != 0 {
		t.Fatalf("gold %d, reward %d", *c.Gold, c.Job.Reward)
	}
	again := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(1).Payload())
	if !bytes.Equal(again.Frames[0].Payload, []byte{2, jobErrNoOutcome, 1}) {
		t.Fatalf("an empty collect = %x", again.Frames[0].Payload)
	}
}

/*
================
TestJobOutcomeRefusals

A trader is not a thief or hunter (0x28); a hunter at another guild or
with an unknown mode is 0x0F; out of range is 3.
================
*/
func TestJobOutcomeRefusals(t *testing.T) {
	rt, c := hunterGuildFixture(t, 1)
	if out := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(2).Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrNotGuild, 2}) {
		t.Fatalf("mode 2 = %x", out.Frames[0].Payload)
	}
	if out := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc+1).U8(0).Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrTooFar, 0}) {
		t.Fatalf("far = %x", out.Frames[0].Payload)
	}
	c.Job.Type = domain.JobThief
	if out := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(0).Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrNotGuild, 0}) {
		t.Fatalf("a thief at the hunters = %x", out.Frames[0].Payload)
	}
	c.Job.Type = domain.JobTrader
	if out := rt.HandleJobOutcome(testDivision, c, wire.NewWriter(5).U32(jobTestNpc).U8(0).Payload()); !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrInvalidJob, 0}) {
		t.Fatalf("a trader = %x", out.Frames[0].Payload)
	}
}

/*
================
TestJobPrevInfoHasNoOldJob

No port character carries pre-tri-job data: _GetOldTrijobData's Ret -1.
================
*/
func TestJobPrevInfoHasNoOldJob(t *testing.T) {
	rt, _, c := jobFixture(t)
	out := rt.HandleJobPrevInfo(testDivision, c, wire.NewWriter(4).U32(jobTestNpc).Payload())
	if len(out.Frames) != 1 || out.Frames[0].Opcode != opJobPrevInfoResponse || !bytes.Equal(out.Frames[0].Payload, []byte{2, jobErrNoPrevInfo}) {
		t.Fatalf("prev info = %+v", out.Frames)
	}
}
