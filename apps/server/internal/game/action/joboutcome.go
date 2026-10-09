/*
===========================================================================

joboutcome.go - the thief and hunter weekly outcome, and previous job info

	0x77BE [u32 npc][u8 mode] -> 0xB7BE [1][mode][u32 reward] | [2][code][mode]
	0x75EE [u32 npc]          -> 0xB5EE [2][code]

v1.188 CGObjPC_HandleJobOutcome70E5 (512E70): the NPC in range (code 3); a
thief or hunter (0x28) at their own guild's NPC (function option 0x15 or
0x16, 0x0F). Mode 0 tells the reward the weekly close credited; mode 1
refuses an empty one (0x2A), otherwise pays it as gold, clears it and
echoes it. Any other mode is 0x0F.

CGObjPC_HandleJobPrevInfo70E6 (513190) reads _OldTrijob through
_GetOldTrijobData: the job levels and experience a character had under the
job system before the tri-job update, kept once for migrated characters.
No port character predates it, so every answer is the procedure's Ret -1:
category 0x18 code 0x29, UIIT_MSG_JOBINFO_OLD_NOTEXIST.

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opJobOutcomeRequest   uint16 = 0x77BE
	opJobOutcomeResponse  uint16 = 0xB7BE
	opJobPrevInfoRequest  uint16 = 0x75EE
	opJobPrevInfoResponse uint16 = 0xB5EE

	jobOutcomeQuery   uint8 = 0
	jobOutcomeCollect uint8 = 1
	// jobErrNoOutcome is 512E70's 0x482A low byte.
	jobErrNoOutcome uint8 = 0x2a
	// jobErrNoPrevInfo is _GetOldTrijobData's Ret -1 (0x4829).
	jobErrNoPrevInfo uint8 = 0x29
)

/*
================
jobOutcomeRefusal
================
*/
func jobOutcomeRefusal(code, mode uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opJobOutcomeResponse, Payload: []byte{2, code, mode}}}}
}

/*
================
HandleJobOutcome
================
*/
func (rt *Runtime) HandleJobOutcome(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	mode, e2 := r.U8()
	if c == nil || e != nil || e2 != nil || r.Done() != nil {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return jobOutcomeRefusal(jobErrTooFar, mode)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || !rt.npcWithinHitRange(division, c, npc) {
		return jobOutcomeRefusal(jobErrTooFar, mode)
	}
	if c.Job.Type != domain.JobThief && c.Job.Type != domain.JobHunter {
		return jobOutcomeRefusal(jobErrInvalidJob, mode)
	}
	if simulation.NpcJobGuild(npc) != c.Job.Type || mode > jobOutcomeCollect {
		return jobOutcomeRefusal(jobErrNotGuild, mode)
	}
	if mode == jobOutcomeQuery {
		reward := uint32(max(c.Job.Reward, 0))
		return OpResult{Frames: []wire.Frame{{Opcode: opJobOutcomeResponse, Payload: wire.NewWriter(6).U8(1).U8(mode).U32(reward).Payload()}}}
	}
	result := jobOutcomeRefusal(jobErrNoOutcome, mode)
	rt.deps.Update(c, "job-outcome", func() bool {
		if c.DeletePending || c.Job.Reward <= 0 {
			return false
		}
		reward := uint64(c.Job.Reward)
		if goldOf(c) > math.MaxInt64-reward {
			return false
		}
		gold := int64(goldOf(c) + reward)
		c.Gold = &gold
		c.Job.Reward = 0
		answer := wire.NewWriter(6).U8(1).U8(mode).U32(uint32(reward)).Payload()
		result = OpResult{Frames: []wire.Frame{{Opcode: opJobOutcomeResponse, Payload: answer}, goldFrame(c)}}
		return true
	})
	return result
}

/*
================
HandleJobPrevInfo
================
*/
func (rt *Runtime) HandleJobPrevInfo(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	if c == nil || e != nil || r.Done() != nil {
		return OpResult{}
	}
	code := jobErrNoPrevInfo
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		code = jobErrTooFar
	} else if npc, ok := rt.npcForCurrentViewer(division, c, gid); !ok || !rt.npcWithinHitRange(division, c, npc) {
		code = jobErrTooFar
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opJobPrevInfoResponse, Payload: []byte{2, code}}}}
}
