/*
===========================================================================

job.go - the job guilds: joining, withdrawing and the job alias

A trader, thief or hunter guild NPC (simulation.NpcJobGuild) offers the
v1.150 job menu (CIFNPCTalk_AppendJobMenuRows). Its confirmation boxes and
alias window send:

	0x7439 [u32 npc][u8 job]               join      -> 0xB439 [1][job][grade][u32 exp]
	0x7661 [u32 npc]                       withdraw  -> 0xB661 [1]
	0x7620 [u32 npc][u8 mode][ascii alias] alias     -> 0xB620 [1][mode][ascii alias]

and every refusal is [2][code] in notice category 0x18 (the alias reply
also echoes mode and alias). The rules are the reference server's job join,
job leave and job alias handlers:

  - join: the NPC keeps that job's guild, level 20, no job yet, gold for
    (level - 20) * 5000, and a thief or hunter who withdrew waits seven days;
  - withdraw: the NPC keeps the player's guild and no job suit is worn
    (job state 4); a thief or hunter starts the seven-day wait;
  - alias: the NPC keeps the player's guild, no job suit is worn, and the
    name passes the naming rule; mode 0 only asks whether it is free and
    mode 1 takes it. Changing an alias the player already has costs
        level x 50,000 gold (the reference server tests it before its database job: 0x4807
    short), the cost the client's modify window shows (645E90).

INFERENCE: v1.188 tests the join and alias-change fees but the deduction
happens inside its database jobs; the port charges each with its commit. The alias follows the
character name shape (the shared name filter) and is unique
among the division's aliases, and - a native bug we do not copy
(owner, 2026-10-28) - never equal to a character name either.

===========================================================================
*/

package action

import (
	"regexp"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opJobJoinRequest      uint16 = 0x7439
	opJobJoinResponse     uint16 = 0xB439
	opJobWithdrawRequest  uint16 = 0x7661
	opJobWithdrawResponse uint16 = 0xB661
	opJobAliasRequest     uint16 = 0x7620
	opJobAliasResponse    uint16 = 0xB620

	// Category 0x18 notice codes (v1.188 0x48xx low bytes).
	jobErrTooFar       uint8 = 0x03
	jobErrNoGold       uint8 = 0x07
	jobErrNotGuild     uint8 = 0x0f
	jobErrLevel        uint8 = 0x19
	jobErrHasJob       uint8 = 0x20
	jobErrWearing      uint8 = 0x22
	jobErrAliasRule    uint8 = 0x23
	jobErrRejoinWait   uint8 = 0x24
	jobErrAliasTaken   uint8 = 0x25
	jobErrInvalidJob   uint8 = 0x28
	jobAliasModeCheck  uint8 = 0
	jobAliasModeCreate uint8 = 1

	// jobMinLevel and jobFeePerLevel are the reference server's level 20 and 5000 gold
	// for each level above it.
	jobMinLevel    = 20
	jobFeePerLevel = 5000
	// jobAliasChangePerLevel is the reference server's 0xC350: changing an existing alias
	// costs the level times 50,000 gold (the client's 645E90 shows it).
	jobAliasChangePerLevel = 50000
	// jobRejoinWaitMs is the reference server's 0x93A80-second timed job: seven days.
	jobRejoinWaitMs = 604800 * 1000
	// jobStartGrade is the grade a new member starts at (CICUser_ResetSpawn).
	jobStartGrade = 1
)

// jobAliasShape is the character name shape the alias shares.
var jobAliasShape = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

/*
================
jobRefusal
================
*/
func jobRefusal(opcode uint16, code uint8) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opcode, Payload: []byte{2, code}}}}
}

/*
================
jobGuildNpc

The selected NPC in range whose guild is job, or the refusal code.
================
*/
func (rt *Runtime) jobGuildNpc(division string, c *enterworld.Character, gid uint32, job uint8) uint8 {
	if selected, ok := rt.Selected.Get(division, c.Name); !ok || selected != gid {
		return jobErrTooFar
	}
	npc, ok := rt.npcForCurrentViewer(division, c, gid)
	if !ok || !rt.npcWithinHitRange(division, c, npc) {
		return jobErrTooFar
	}
	if simulation.NpcJobGuild(npc) != job {
		return jobErrNotGuild
	}
	return 0
}

/*
================
HandleJobJoin
================
*/
func (rt *Runtime) HandleJobJoin(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	job, e2 := r.U8()
	if c == nil || e != nil || e2 != nil || r.Done() != nil {
		return jobRefusal(opJobJoinResponse, jobErrInvalidJob)
	}
	if job < domain.JobTrader || job > domain.JobHunter {
		return jobRefusal(opJobJoinResponse, jobErrInvalidJob)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if code := rt.jobGuildNpc(division, c, gid, job); code != 0 {
		return jobRefusal(opJobJoinResponse, code)
	}
	result := jobRefusal(opJobJoinResponse, jobErrInvalidJob)
	now := rt.Now().UnixMilli()
	rt.deps.Update(c, "job-join", func() bool {
		level := int64(0)
		if c.Level != nil {
			level = *c.Level
		}
		fee := max(level-jobMinLevel, 0) * jobFeePerLevel
		switch {
		case c.DeletePending:
			return false
		case level < jobMinLevel:
			result = jobRefusal(opJobJoinResponse, jobErrLevel)
			return false
		case c.Job.Type != domain.JobNone:
			result = jobRefusal(opJobJoinResponse, jobErrHasJob)
			return false
		case now < c.Job.RejoinAtMs:
			result = jobRefusal(opJobJoinResponse, jobErrRejoinWait)
			return false
		case uint64(fee) > goldOf(c):
			result = jobRefusal(opJobJoinResponse, jobErrNoGold)
			return false
		}
		gold := int64(goldOf(c)) - fee
		c.Gold = &gold
		c.Job = domain.CharacterJob{Type: job, Grade: jobStartGrade}
		payload := wire.NewWriter(8).U8(1).U8(job).U8(jobStartGrade).U32(0).Payload()
		result = OpResult{Frames: []wire.Frame{{Opcode: opJobJoinResponse, Payload: payload}}}
		if fee != 0 {
			result.Frames = append(result.Frames, goldFrame(c))
		}
		return true
	})
	return result
}

/*
================
HandleJobWithdraw
================
*/
func (rt *Runtime) HandleJobWithdraw(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	if c == nil || e != nil || r.Done() != nil {
		return jobRefusal(opJobWithdrawResponse, jobErrInvalidJob)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if c.Job.Type < domain.JobTrader || c.Job.Type > domain.JobHunter {
		return jobRefusal(opJobWithdrawResponse, jobErrInvalidJob)
	}
	if code := rt.jobGuildNpc(division, c, gid, c.Job.Type); code != 0 {
		return jobRefusal(opJobWithdrawResponse, code)
	}
	result := jobRefusal(opJobWithdrawResponse, jobErrWearing)
	now := rt.Now().UnixMilli()
	rt.deps.Update(c, "job-withdraw", func() bool {
		if c.DeletePending || rt.jobDressed(c) || rt.jobDressPending(division, c.Name) {
			return false
		}
		rejoin := int64(0)
		if c.Job.Type != domain.JobTrader {
			rejoin = now + jobRejoinWaitMs
		}
		c.Job = domain.CharacterJob{RejoinAtMs: rejoin}
		result = OpResult{Frames: []wire.Frame{{Opcode: opJobWithdrawResponse, Payload: []byte{1}}}}
		return true
	})
	return result
}

/*
================
jobAliasRefusal
================
*/
func jobAliasRefusal(code, mode uint8, alias string) OpResult {
	payload := wire.NewWriter(4 + len(alias)).U8(2).U8(code).U8(mode).Str(alias).Payload()
	return OpResult{Frames: []wire.Frame{{Opcode: opJobAliasResponse, Payload: payload}}}
}

/*
================
jobAliasTaken
================
*/
func (rt *Runtime) jobAliasTaken(division string, c *enterworld.Character, alias string) bool {
	for _, other := range rt.deps.CharactersForDivision(division) {
		if other != nil && other != c && strings.EqualFold(other.Job.Alias, alias) {
			return true
		}
	}
	// An alias may not equal a character name either. The reference
	// server never refuses this (its alias validity is the name filter
	// alone), which lets a suited player speak under another player's
	// real name; that impersonation is a native bug we do not copy
	// (owner, 2026-10-28), so the refusal is unconditional - and it
	// keeps the alias and name namespaces disjoint for typed-target
	// routing. Deleted-pending names count: the name is still spoken
	// for until the deletion matures.
	for _, other := range rt.deps.CharactersForDivision(division) {
		if other != nil && other != c && strings.EqualFold(other.Name, alias) {
			return true
		}
	}
	return false
}

/*
================
HandleJobAlias
================
*/
func (rt *Runtime) HandleJobAlias(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	mode, e2 := r.U8()
	alias, e3 := r.Str()
	if c == nil || e != nil || e2 != nil || e3 != nil || r.Done() != nil ||
		mode != jobAliasModeCheck && mode != jobAliasModeCreate {
		return jobAliasRefusal(jobErrInvalidJob, mode, "")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	if c.Job.Type == domain.JobNone {
		return jobAliasRefusal(jobErrInvalidJob, mode, alias)
	}
	if code := rt.jobGuildNpc(division, c, gid, c.Job.Type); code != 0 {
		return jobAliasRefusal(code, mode, alias)
	}
	if rt.jobDressed(c) || rt.jobDressPending(division, c.Name) {
		return jobAliasRefusal(jobErrWearing, mode, alias)
	}
	n := len(alias)
	if !jobAliasShape.MatchString(alias) || n < domain.CharacterNameMinBytes || n > domain.CharacterNameMaxBytes ||
		!rt.nameAllowed(alias) {
		return jobAliasRefusal(jobErrAliasRule, mode, alias)
	}
	// The reference server tests a change's fee before its database job checks the name.
	if mode == jobAliasModeCreate && uint64(jobAliasChangeFee(c)) > goldOf(c) {
		return jobAliasRefusal(jobErrNoGold, mode, alias)
	}
	if rt.jobAliasTaken(division, c, alias) {
		return jobAliasRefusal(jobErrAliasTaken, mode, alias)
	}
	ok := wire.NewWriter(4 + n).U8(1).U8(mode).Str(alias).Payload()
	result := OpResult{Frames: []wire.Frame{{Opcode: opJobAliasResponse, Payload: ok}}}
	if mode == jobAliasModeCheck {
		return result
	}
	refusal := jobErrInvalidJob
	if !rt.deps.Update(c, "job-alias", func() bool {
		if c.DeletePending {
			return false
		}
		// Re-read under the door: the balance may have moved since the test.
		fee := jobAliasChangeFee(c)
		if uint64(fee) > goldOf(c) {
			refusal = jobErrNoGold
			return false
		}
		if fee > 0 {
			gold := int64(goldOf(c)) - fee
			c.Gold = &gold
			result.Frames = append(result.Frames, goldFrame(c))
		}
		c.Job.Alias = alias
		return true
	}) {
		return jobAliasRefusal(refusal, mode, alias)
	}
	return result
}

/*
================
jobAliasChangeFee

The gold changing an alias costs: nothing for a first alias, else the
level times 50,000 (reference server: a non-empty current alias is a change).
================
*/
func jobAliasChangeFee(c *enterworld.Character) int64 {
	if c.Job.Alias == "" {
		return 0
	}
	level := int64(1)
	if c.Level != nil {
		level = *c.Level
	}
	return level * jobAliasChangePerLevel
}
