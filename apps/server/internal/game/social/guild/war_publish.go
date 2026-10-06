/*
===========================================================================

war_publish.go - guild-war rows, combat receipts and terminal publication

GameServer 5CFD30 writes remaining seconds, victory-point index, combined
stake, opponents and scores. Client 75AD90 consumes that exact layout.

===========================================================================
*/
package guild

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
writeWarRow
================
*/
func writeWarRow(w *wire.Writer, war domain.GuildWarRecord, enemy string, now int64) {
	w.U32(war.ID).U32(guildwar.RemainingSeconds(war.EndMs, now)).U8(war.ScoreIndex).U32(war.Stake).
		U32(uint32(war.Guilds[0])).U32(uint32(war.Guilds[1])).U32(war.Scores[0]).U32(war.Scores[1]).Str(enemy)
}

/*
================
publishWarBegin
================
*/
func (r *WarRuntime) publishWarBegin(division string, war domain.GuildWarRecord, now int64) {
	for side, guildID := range war.Guilds {
		enemy, _, _ := r.deps.GuildAuthority().Guild(division, war.Guilds[1-side])
		w := wire.NewWriter(48).U8(0x19)
		writeWarRow(w, war, enemy.Name, now)
		r.unions.sendToGuild(division, guildID, OpGuildUpdatePush, w.Payload())
	}
}

/*
================
SeedFrame
================
*/
func (r *WarRuntime) SeedFrame(division string, c *enterworld.Character) (enterworld.Packet, bool) {
	if c == nil || c.GuildID == nil {
		return enterworld.Packet{}, false
	}
	rows := r.Authority.Wars(division, *c.GuildID)
	now := r.Now().UnixMilli()
	w := wire.NewWriter(1 + len(rows)*48).U8(uint8(len(rows)))
	for _, war := range rows {
		enemyID := war.Guilds[0]
		if enemyID == *c.GuildID {
			enemyID = war.Guilds[1]
		}
		enemy, _, _ := r.deps.GuildAuthority().Guild(division, enemyID)
		if war.EndMs != 0 && war.EndMs < now {
			war.EndMs = now
		}
		writeWarRow(w, war, enemy.Name, now)
	}
	return enterworld.NewPacket(OpGuildWarSeed, w.Payload()), true
}

/*
================
publishWarEnd
================
*/
func (r *WarRuntime) publishWarEnd(division string, war domain.GuildWarRecord, winner int64) {
	payload := wire.NewWriter(9).U8(0x1c).U32(war.ID).U32(uint32(winner)).Payload()
	for _, guildID := range war.Guilds {
		r.unions.sendToGuild(division, guildID, OpGuildUpdatePush, payload)
	}
}

/*
================
handleWarSurrender

5C7390 requires a guild master; the opposing guild receives the stake.
================
*/
func (r *WarRuntime) handleWarSurrender(s *transport.Session, _ uint16, payload []byte) {
	c, division, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	id, err := decodeUnionU32(payload)
	if err != nil {
		return
	}
	c = characterSnapshot(r.deps, division, c)
	r.mu.Lock()
	defer r.mu.Unlock()
	code := uint8(0)
	if c == nil || c.GuildID == nil {
		code = 0x0d
	}
	var war domain.GuildWarRecord
	if code == 0 {
		// Approved parity exception: native 5C7390 / ShardManager 43CB50
		// omit participant admission. A third guild cannot surrender this war.
		for _, row := range r.Authority.Wars(division, *c.GuildID) {
			if row.ID == id {
				war = row
				break
			}
		}
		if war.ID == 0 {
			code = 2
		}
	}
	if code == 0 {
		_, _, code = r.unions.unionActor(division, c)
	}
	if code != 0 {
		_ = s.Send(OpGuildWarSurrenderResult, []byte{2, code})
		return
	}
	winner := war.Guilds[0]
	if winner == *c.GuildID {
		winner = war.Guilds[1]
	}
	// Shard 4289F0 admits the job per requester/opcode, not per war.
	// Both masters may surrender; the first queued deadline settles the war.
	for _, pending := range r.ending {
		if pending.requester == c.Name {
			_ = s.Send(OpGuildWarSurrenderResult, []byte{2, 2})
			return
		}
	}
	r.ending = append(r.ending, pendingWarEnd{war: war, winner: winner, requester: c.Name, deadline: r.Now().UnixMilli() + guildWarSurrenderDelayMs})

	// 5CAC40 sends the surrendering side 1A, the other side 1B, before 1C.
	r.unions.sendToGuild(division, *c.GuildID, OpGuildUpdatePush, wire.NewWriter(5).U8(0x1a).U32(id).Payload())
	r.unions.sendToGuild(division, winner, OpGuildUpdatePush, wire.NewWriter(5).U8(0x1b).U32(id).Payload())

}

/*
================
RecordKill

Called after the fatal character transaction. A duplicate corpse attack
never reaches this boundary. Both guilds receive their relative member row.
================
*/
func (r *WarRuntime) RecordKill(division string, combat domain.GuildWarCombat, nowMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	war, winner, code, err := r.Authority.Combat(combat, nowMs)
	if err != nil {
		log.WithError(err).Error("guild war combat failed")
	}
	if code != 0 {
		return
	}
	var killer, victim domain.GuildMemberRecord
	var killerGuild, victimGuild int64
	for _, guildID := range war.Guilds {
		_, members, _ := r.deps.GuildAuthority().Guild(division, guildID)
		for _, member := range members {
			if member.CharID == combat.KillerID {
				killer = member
				killerGuild = guildID
			}
			if member.CharID == combat.VictimID {
				victim = member
				victimGuild = guildID
			}
		}
	}
	for side, row := range []domain.GuildMemberRecord{killer, victim} {
		guildID, enemyName := killerGuild, victim.Name
		if side == 1 {
			guildID = victimGuild
			enemyName = killer.Name
		}
		payload := wire.NewWriter(32).U8(0x1d).U8(uint8(side + 1)).U32(war.ID).U32(uint32(combat.Score)).U32(row.JID).Str("").Str(enemyName).Payload()
		r.unions.sendToGuild(division, guildID, OpGuildUpdatePush, payload)
	}
	if winner != 0 {
		r.publishWarEnd(division, war, winner)
	}
}

/*
================
Tick
================
*/
func (r *WarRuntime) Tick(nowMs int64) []simulation.DivisionFrames {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, row := range r.pending {
		if nowMs > row.expires {
			delete(r.pending, key)
			r.expireProposal(r.Authority.Division(), row)
		}
	}
	pendingEnds := r.ending
	r.ending = nil
	for _, ending := range pendingEnds {
		id := ending.war.ID
		if nowMs < ending.deadline {
			r.ending = append(r.ending, ending)
			continue
		}
		if row, active := r.Authority.Find(r.Authority.Division(), ending.war.Guilds[0], ending.war.Guilds[1]); !active || row.ID != id {
			continue
		}
		code, err := r.Authority.End(id, ending.winner)
		if err != nil {
			log.WithError(err).Error("guild war surrender checkpoint failed")
			r.ending = append(r.ending, ending)
			continue
		}
		if code == 0 {
			r.publishWarEnd(r.Authority.Division(), ending.war, ending.winner)
		}
		if session, online := r.presence.SessionByName(r.Authority.Division(), ending.requester); online {
			if code == 0 {
				_ = session.Send(OpGuildWarSurrenderResult, []byte{1})
			} else {
				_ = session.Send(OpGuildWarSurrenderResult, []byte{2, code})
			}
		}
	}
	for _, war := range r.Authority.Expired(nowMs) {
		winner := war.Guilds[1]
		if war.Scores[0] > war.Scores[1] {
			winner = war.Guilds[0]
		}
		code, err := r.Authority.End(war.ID, winner)
		if err != nil {
			log.WithError(err).Error("guild war expiry failed")
			continue
		}
		if code == 0 {
			r.publishWarEnd(r.Authority.Division(), war, winner)
		}
	}
	return nil
}

// 43CB50 initializes the surrender job with float duration 60 seconds.
const guildWarSurrenderDelayMs int64 = 60 * 1000

/*
================
pendingWarEnd

Native queued jobs are transient; restarting retains the active war but
retires an unfinished surrender, just as it retires unanswered proposals.
================
*/
type pendingWarEnd struct {
	war       domain.GuildWarRecord
	winner    int64
	requester string
	deadline  int64
}
