/*
===========================================================================

fortress_battle.go - fatal-hit fortress scores and rank skill transitions

52D460 -> 635290 credits a player-caused death during fortress war.
61F2D0 records the victim, the killer, then living nearby party members.
Calls run after the fatal character transaction, under division authority;
persistence and each recipient's skill mutation enter their existing doors.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"sync/atomic"
)

/*
================
activeBattleFortress
================
*/
func (rt *Runtime) activeBattleFortress(division string, c *enterworld.Character) (uint32, bool) {
	if c == nil || rt.Fortresses == nil || !rt.Fortresses.WarActive(division) {
		return 0, false
	}
	world, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
	if !ok {
		return 0, false
	}
	return rt.Fortresses.ForWorld(world)
}

/*
================
fortressBattleScore

620110: subtype, fortress ID, kills, deaths. No record means no packet.
================
*/
func fortressBattleScore(id uint32, row domain.FortressBattleRecord) wire.Frame {
	return wire.Frame{Opcode: opFortressWarState, Payload: wire.NewWriter(13).U8(0x11).U32(id).U32(row.Kills).U32(row.Deaths).Payload()}
}

/*
================
FortressBattleFrames

4DFFB9 sends the retained score on world entry while the war is active.
The existing timed-job owner restores buffs; entry never earns a new rank.
================
*/
func (rt *Runtime) FortressBattleFrames(division string, c *enterworld.Character) []wire.Frame {
	unlock := rt.lockDivision(division)
	defer unlock()
	snapshot := rt.characterSnapshot(division, c)
	id, ok := rt.activeBattleFortress(division, snapshot)
	if !ok {
		return nil
	}
	row, ok := rt.Fortresses.BattleRecord(division, id, c.ID)
	if !ok {
		return nil
	}
	return []wire.Frame{fortressBattleScore(id, row)}
}

/*
================
recordFortressDeath

The killer gets credit even if it is dead. Only the other party members
are filtered by living state, matching world and planar distance <=1000.
No party sharing option or guild filter appears in 61F3C0.
================
*/
func (rt *Runtime) recordFortressDeath(division string, victim, killer *enterworld.Character, now int64) {
	if killer == nil || victim == nil || killer.ID == victim.ID {
		return
	}
	// Abnormal source facts are snapshots; rank jobs belong to the live owner.
	killer = rt.findCharacterByGid(division, enterworld.ObjectIDForCharacter(killer))
	if killer == nil {
		return
	}
	id, ok := rt.activeBattleFortress(division, victim)
	if !ok {
		return
	}
	rt.creditFortressBattle(division, id, victim, false, now)
	rt.creditFortressBattle(division, id, killer, true, now)
	party := rt.rewardPartyOf(division, enterworld.ObjectIDForCharacter(killer))
	if party == nil {
		return
	}
	origin := rt.liveSpawn(simulation.WorldKey(division, killer.Name), killer, now)
	for _, gid := range party.Members {
		if gid == enterworld.ObjectIDForCharacter(killer) {
			continue
		}
		member := rt.findCharacterByGid(division, gid)
		snapshot := rt.characterSnapshot(division, member)
		if snapshot == nil || !enterworld.CharacterAlive(snapshot) || domain.CharacterWorldInstance(snapshot) != domain.CharacterWorldInstance(killer) {
			continue
		}
		if !withinPartyRewardRange(origin, rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)) {
			continue
		}
		rt.creditFortressBattle(division, id, member, true, now)
	}
}

/*
================
creditFortressBattle
================
*/
func (rt *Runtime) creditFortressBattle(division string, id uint32, c *enterworld.Character, kill bool, now int64) {
	change := fortress.BattleChange{FortressID: id, CharacterID: c.ID, Kill: kill, NowMs: now}
	row, promote, err := rt.Fortresses.RecordBattle(division, change)
	if err != nil {
		log.WithError(err).Error("fortress battle record checkpoint failed")
	}
	if row.CharacterID == 0 {
		return
	}
	if promote != 0 {
		if !rt.promoteFortressBattle(division, c, row.Rank, promote, now) {
			return
		}
		if err := rt.Fortresses.CommitBattleRank(division, change, promote); err != nil {
			log.WithError(err).Error("fortress battle rank checkpoint failed")
		}
		rt.publishFortressRankNotice(division, c, promote)
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, []wire.Frame{fortressBattleScore(id, row)})
	}
}

/*
================
promoteFortressBattle

61F580 retires the previous authored rank skill before installing the next.
All modifiers, persistence and wire tokens belong to the shared effect owner.
================
*/
func (rt *Runtime) promoteFortressBattle(division string, c *enterworld.Character, previous, next uint8, now int64) bool {
	_, skillID, valid := fortress.BattleRank(next)
	if !valid || rt.deps.SkillData() == nil {
		return false
	}
	row, ok := rt.deps.SkillData().SkillByID(skillID)
	if !ok {
		return false
	}
	_, oldID, _ := fortress.BattleRank(previous)
	var public, private []wire.Frame
	applied := false
	rt.deps.Update(c, "fortress-battle-rank", func() bool {
		public, private = rt.retireFortressRankInDoor(division, c, oldID, now)
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		if token == 0 {
			token = atomic.AddUint32(&rt.castTokenCounter, 1)
		}
		frames, ok := rt.commitCharacterEffect(division, c, row, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2}, now)
		public = append(public, frames...)
		if ok {
			// Attached effects carry tokens, not the owner's new HP/stat projection.
			private = append(private, rt.gaugeDropFrames(division, c, true, true, true)...)
		}
		applied = ok
		return ok || len(public) != 0 || len(private) != 0
	})
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, append(append([]wire.Frame{}, public...), private...))
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, public)
	}
	return applied
}

/*
================
retireFortressRankInDoor
================
*/
func (rt *Runtime) retireFortressRankInDoor(division string, c *enterworld.Character, skillID uint32, now int64) ([]wire.Frame, []wire.Frame) {
	if skillID == 0 {
		return nil, nil
	}
	var tokens []uint32
	for _, e := range rt.effects.Snapshot(division, c.Name) {
		if e.SkillID == skillID {
			tokens = append(tokens, e.InstanceToken)
		}
	}
	return rt.finishEndedEffects(division, c, rt.effects.RetireInstances(division, c.Name, tokens), now)
}

/*
================
publishFortressRankNotice

61F7D5 tells the promoted player. Commander (rank six) names the player to
its guild/union in the fortress world (61D140 -> 5F8700).
================
*/
func (rt *Runtime) publishFortressRankNotice(division string, c *enterworld.Character, rank uint8) {
	if rt.PushCharacterFrames == nil {
		return
	}
	w := wire.NewWriter(32).U8(0x0e)
	if rank != fortress.MaxBattleRank {
		rt.PushCharacterFrames(division, c.Name, []wire.Frame{{Opcode: opFortressWarState, Payload: w.U8(1).U8(rank).Payload()}})
		return
	}
	w.U8(0).U16(uint16(len(c.Name))).Bytes([]byte(c.Name)).U8(rank)
	frame := wire.Frame{Opcode: opFortressWarState, Payload: w.Payload()}
	if c.GuildID == nil {
		return
	}
	for _, member := range rt.deps.CharactersForDivision(division) {
		snapshot := rt.characterSnapshot(division, member)
		if snapshot == nil || snapshot.GuildID == nil || domain.CharacterWorldInstance(snapshot) != domain.CharacterWorldInstance(c) {
			continue
		}
		if *snapshot.GuildID != *c.GuildID && (rt.Unions == nil || !rt.Unions.Allied(division, *c.GuildID, *snapshot.GuildID)) {
			continue
		}
		rt.PushCharacterFrames(division, snapshot.Name, []wire.Frame{frame})
	}
}

/*
================
retireFortressBattleRank

601170 mode two cancels the record's rank skill for every resident before
checking whether its guild may remain in the world.
================
*/
func (rt *Runtime) retireFortressBattleRank(division string, c *enterworld.Character, id uint32, now int64) {
	record, ok := rt.Fortresses.BattleRecord(division, id, c.ID)
	if !ok {
		return
	}
	_, skillID, ok := fortress.BattleRank(record.Rank)
	if !ok {
		return
	}
	var public, private []wire.Frame
	rt.deps.Update(c, "fortress-battle-end", func() bool {
		public, private = rt.retireFortressRankInDoor(division, c, skillID, now)
		return len(public) != 0 || len(private) != 0
	})
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, c.Name, append(append([]wire.Frame{}, public...), private...))
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, public)
	}
}

/*
================
prepareDeathKiller

4E65B8..4E65F3 classifies siege death before cape/job/guild relations.
Read fortress state before the character transaction: the fortress store
also writes through the authority and must never invert those two locks.
================
*/
func (rt *Runtime) prepareDeathKiller(division string, victim *enterworld.Character, killer deathKiller) deathKiller {
	if killer.player != nil || killer.monster != nil {
		_, killer.siege = rt.activeBattleFortress(division, victim)
	}
	return killer
}

/*
================
capturePlayerAbnormalSources

Capture source and fortress admission together before the damage door.
================
*/
func (rt *Runtime) capturePlayerAbnormalSources(division string, victim *enterworld.Character, block *abnormal.Block, records []abnormal.Record) map[uint32]abnormalSourceState {
	sources := rt.captureAbnormalSources(division, block, records)
	for gid, source := range sources {
		source.killer = rt.prepareDeathKiller(division, victim, source.killer)
		if source.killer.player != nil {
			source.kill = rt.classifyPlayerKill(division, source.killer.player, victim)
		}
		sources[gid] = source
	}
	return sources
}
