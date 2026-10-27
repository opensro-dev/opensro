/*
===========================================================================

berserk.go - berserk activation, expiry and kill point awards

===========================================================================
*/

package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

type berserkExpiry struct {
	division, name string
	until          int64
}

/*
================
berserkPointsFrame
================
*/
func berserkPointsFrame(c *enterworld.Character, source uint32) wire.Frame {
	return wire.Frame{Opcode: 0x30b3, Payload: wire.NewWriter(6).U8(4).U8(c.BerserkPoints).U32(source).Payload()}
}

/*
================
HandleBerserk

The retail request is 7341 [1]; the research server uses 70A7. Success is
authoritative points/body/speed publication; B341 carries refusals only.
================
*/
func (rt *Runtime) HandleBerserk(division string, c *enterworld.Character, payload []byte) OpResult {
	if c == nil || len(payload) != 1 || payload[0] != 1 {
		return OpResult{}
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	code := uint8(3)
	var frames, public []wire.Frame
	accepted := rt.deps.Update(c, "berserk-activate", func() bool {
		if c.DeletePending || !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 {
			return false
		}
		// Natively any transform (msch 1..4) refuses before the body checks.
		if c.NativeBodyStatus != 2 && c.TransformMode != 0 {
			code = 5
			return false
		}
		switch c.NativeBodyStatus {
		case 2:
			code = 4
			return false
		case 6, 7:
			code = 5
			return false
		case 1, 3, 4:
			return false
		}
		if c.ActiveCOS != nil && c.ActiveCOS.Mounted {
			code = 5
			return false
		}
		if c.BerserkPoints < 5 {
			code = 1
			return false
		}
		now := rt.Now().UnixMilli()
		ended := rt.effects.RetireEvent(division, c.Name, 4)
		if len(ended) > 0 {
			rt.retireSkillJobs(c, ended)
			tokens := make([]uint32, 0, len(ended))
			for _, e := range ended {
				tokens = append(tokens, e.InstanceToken)
			}
			payload, _ := (wire.EndedEffectInstances{InstanceTokens: tokens}).Encode()
			public = append(public, wire.Frame{Opcode: wire.OpEndedEffectInstances, Payload: payload})
		}

		c.ModifyBerserkPoints(-5)
		c.NativeBodyStatus, c.BodyStatusOwner, c.BerserkUntilMs = 1, 0, now+60000
		rt.berserkActors.Store(simulation.WorldKey(division, c.Name), berserkExpiry{division, c.Name, c.BerserkUntilMs})
		frames = append(frames, berserkPointsFrame(c, 0))
		public = append(public, bodyStatusFrame(enterworld.ObjectIDForCharacter(c), 1))
		public = append(public, rt.refreshMovementEffects(division, c, now)...)
		return true
	})
	if !accepted {
		return OpResult{Frames: []wire.Frame{{Opcode: 0xb341, Payload: []byte{2, code}}}}
	}
	return OpResult{Frames: append(frames, public...), Broadcast: public}
}

/*
================
advanceBerserk

Character store owns the two-state lifecycle. The map is only its tick index.
No goroutines/timers retain an actor after session teardown.
================
*/
func (rt *Runtime) advanceBerserk(now int64) []simulation.DivisionFrames {
	var keys []string
	rt.berserkActors.Range(func(k, v any) bool {
		if v.(berserkExpiry).until <= now {
			keys = append(keys, k.(string))
		}
		return true
	})
	sort.Strings(keys)
	var out []simulation.DivisionFrames
	for _, key := range keys {
		value, ok := rt.berserkActors.Load(key)
		if !ok {
			continue
		}
		job := value.(berserkExpiry)
		sourceGID, frames := rt.expireBerserkInDivision(key, job, now)
		if len(frames) > 0 {
			batch := simulation.DivisionFrames{DivisionID: job.division, SourceGID: sourceGID}
			for _, f := range frames {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
			}
			out = append(out, batch)
		}
	}
	return out
}

/*
================
expireBerserkInDivision
================
*/
func (rt *Runtime) expireBerserkInDivision(key string, job berserkExpiry, now int64) (uint32, []wire.Frame) {
	unlock := rt.lockDivision(job.division)
	defer unlock()
	c := rt.findCharacter(job.division, job.name)
	var sourceGID uint32
	var frames []wire.Frame
	if c != nil {
		sourceGID = enterworld.ObjectIDForCharacter(c)
		rt.deps.UpdateDeferred(c, "berserk-expire", func() bool {
			if c.BerserkUntilMs != job.until || now < c.BerserkUntilMs {
				return false
			}
			c.BerserkUntilMs = 0
			// AJStateChanger kind4 preserves current invisible body4.
			if c.NativeBodyStatus != 4 {
				if c.TransitionBodyStatus(domain.BodyStatusTransition{}) {
					frames = append(frames, bodyStatusFrame(sourceGID, 0))
				}
				frames = append(frames, rt.refreshMovementEffects(job.division, c, now)...)
			}
			return true
		})
	}
	rt.berserkActors.CompareAndDelete(key, job)
	if len(frames) > 0 && rt.PushCharacterFrames != nil && rt.PushDivisionPeerFrames != nil {
		rt.publishBodyStatus(job.division, job.name, frames)
		frames = nil
	}
	return sourceGID, frames
}

/*
================
berserkKillAward
The native berserk point gain, consumed as unsigned16 when results apply.
Preserve native CRT modulo sampling: threshold 800 is not an exact uniform
8-percent probability.
sampling: threshold 800 is not an exact uniform 8-percent probability.
================
*/
func berserkKillAward(playerLevel, monsterLevel int64, rarity uint8, roll uint32) int {
	return berserkKillAwardWithGainRate(playerLevel, monsterLevel, rarity, 100, roll)
}

/*
================
berserkKillAwardWithGainRate

The native berserk point gain formula: after computing
the base and monster-rarity threshold, native reads parameter 0xb8 ('hwir',
base 100, +20 from SKILL_TC_CROWN_BUFF_01 20163) and scales
threshold = param_0xb8 * threshold / 100 before the uint16 truncation.
================
*/
func berserkKillAwardWithGainRate(playerLevel, monsterLevel int64, rarity uint8, gainPercent uint32, roll uint32) int {
	chance := 8.0
	if monsterLevel-playerLevel < -3 {
		chance = math.Max(.25, math.Min(8, 8-float64(playerLevel-monsterLevel-5)*.25))
	}
	threshold, count := uint32(chance*100), 1
	switch rarity & 0x0f {
	case 3, 8:
		threshold, count = 10000000, 5
	case 4:
		threshold *= 5
	case 6:
		threshold *= 2
	}
	if gainPercent != 0 && gainPercent != 100 {
		threshold = gainPercent * threshold / 100
	}
	if roll%10000 < uint32(uint16(threshold)) {
		return count
	}
	return 0
}

/*
================
grantBerserkForKill

Called once by the committed fatal-hit owner, not by the EXP winner.
================
*/
func (rt *Runtime) grantBerserkForKill(actor *enterworld.Character, roster rewardRoster, impact simulation.MonsterDamageResult) []RecipientFrames {
	if actor == nil || actor.NativeBodyStatus == 1 {
		return nil
	}
	roll := rt.BerserkRoll
	if roll == nil {
		roll = combat.SecureRoll32767
	}
	r, err := roll()
	if err != nil {
		return nil
	}
	gainPercent := uint32(100)
	if rt.hasCrownHonorBuff(roster.division, actor) {
		gainPercent = 120
	}
	count := berserkKillAwardWithGainRate(rewardLevel(actor), int64(impact.Instance.Ref.Level), impact.Instance.Rarity(), gainPercent, r)
	if count == 0 {
		return nil
	}
	members := []*enterworld.Character{actor}
	a := roster.actors[enterworld.ObjectIDForCharacter(actor)]
	if impact.Instance.Rarity()>>4 == 1 && a.party != nil && a.party.Options&1 != 0 {
		members = nil
		for _, gid := range a.party.Members {
			if m, ok := roster.actors[gid]; ok && m.world == a.world && enterworld.CharacterAlive(m.character) {
				members = append(members, m.character)
			}
		}
	}
	var out []RecipientFrames
	for _, c := range members {
		if c.ModifyBerserkPoints(count) {
			out = append(out, RecipientFrames{CharacterID: c.ID, Frames: []wire.Frame{berserkPointsFrame(c, impact.Instance.Gid)}})
		}
	}
	return out
}
