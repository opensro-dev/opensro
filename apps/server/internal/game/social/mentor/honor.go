/*
===========================================================================

honor.go - academy dissolution, leave, kick, graduation, and honor rankings

Implements the v1.150 Academy and Honor Camp lifecycle handlers:
  - 0x77C5 -> 0xB7C5: camp dissolution (CGObjPC_HandleAcademyDisband7471 0x00519120)
  - 0x7785 -> 0xB785: voluntary leave (CGObjPC_HandleAcademyLeave7474 0x00519510, flag 1)
  - 0x74D4 -> 0xB4D4: member expulsion (CGObjPC_HandleAcademyKick7473 0x005193A0, flag 2)
  - 0x710A -> 0xB10A: voluntary graduation + evaluation (CGObjPC_HandleAcademyGraduate7475 0x00519600, flag 0)
  - 0x736D -> 0xB36D: compulsory graduation by master (CGObjPC_HandleAcademyCompulsoryGraduate7476 0x00519770, flag 1)
  - 0x72D0 -> 0xB2D0: honor rankings board (client 771750, server 0x00519AD0 / 0x005DF010 / 0x005DD1C0)

===========================================================================
*/
package mentor

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

// Client->server and server->client opcodes for Academy lifecycle and Honor Rank.
const (
	OpTCDisbandRequest            uint16 = 0x77C5
	OpTCDisbandAnswer             uint16 = 0xB7C5
	OpTCLeaveRequest              uint16 = 0x7785
	OpTCLeaveAnswer               uint16 = 0xB785
	OpTCKickRequest               uint16 = 0x74D4
	OpTCKickAnswer                uint16 = 0xB4D4
	OpTCGraduateRequest           uint16 = 0x710A
	OpTCGraduateAnswer            uint16 = 0xB10A
	OpTCCompulsoryGraduateRequest uint16 = 0x736D
	OpTCCompulsoryGraduateAnswer  uint16 = 0xB36D
	OpTCHonorRankRequest          uint16 = 0x72D0
	OpTCHonorRankAnswer           uint16 = 0xB2D0
)

// Category-0x1D error codes (client sub_689420 jump table at 0x0068afd6).
const (
	ErrTCDead           uint8 = 0x07 // UIIT_MSG_TC_ERROR_DEAD
	ErrTCNotInCamp      uint8 = 0x14 // UIIT_MSG_TC_ERROR_NOT_BELONG_TC
	ErrTCOnlyBossCanUse uint8 = 0x15 // UIIT_MSG_TC_ERROR_ONLY_BOSS_CAN_USE
	ErrTCNotCancel      uint8 = 0x16 // UIIT_MSG_TC_ERROR_NOT_CANCEL
	ErrTCTargetNotInTC  uint8 = 0x17 // UIIT_MSG_TC_ERROR_TARGET_NOT_IN_TC
	ErrTCDisbandWait24h uint8 = 0x1D // UIIT_MSG_TC_ERROR_NOT_CANCEL_24H
	ErrTCGraduateLevel  uint8 = 0x1F // UIIT_MSG_TC_ERROR_GRADUATE_LEVEL
	ErrTCCompulsory24h  uint8 = 0x20 // UIIT_MSG_TC_ERROR_COMPULSORY_GRADUATION_24H
	ErrTCCantBanMember  uint8 = 0x24 // UIIT_MSG_TC_ERROR_CANT_BAN_MEMBER
)

// GraduateBonusExp is the flat 2,000,000 EXP reward granted to an apprentice
// upon voluntary graduation (v1.188 CTrainingCampMgr_OnGraduateDBSuccess
// 0x005DFC30 @ 0x005DFC69: push 0x1E8480 -> vtable+0x170 CGObjPC_ApplyExpDelta
// 0x004E5250 when +0x38 == 1 && +0x40 == 1).
const GraduateBonusExp int64 = 2_000_000

/*
================
BonusGraduateExp

BonusGraduateExp returns the native voluntary apprentice graduation experience
reward (2,000,000 EXP = 0x1E8480, v1.188 CTrainingCampMgr_OnGraduateDBSuccess
0x005DFC30 @ 0x005DFC69).
================
*/
func BonusGraduateExp(joinLevel uint8, _ enterworld.LevelDataSource) int64 {
	if joinLevel < 1 || joinLevel >= StudentMaxLevel {
		return 0
	}
	return GraduateBonusExp
}

/*
================
encodeResultAck
================
*/
func encodeResultAck() []byte {
	return []byte{1}
}

/*
================
encodeResultError
================
*/
func encodeResultError(code uint8) []byte {
	return []byte{2, code}
}

/*
================
encodeDisbandWaitError

76FE00 reads [u8 2][u8 0x1D][u32 remainingSeconds] when DisbandCamp refuses
within the 24-hour creation window.
================
*/
func encodeDisbandWaitError(remainingSeconds uint32) []byte {
	return wire.NewWriter(6).U8(2).U8(ErrTCDisbandWait24h).U32(remainingSeconds).Payload()
}

/*
================
broadcastCampStatus

broadcastCampStatus sends one 0x3AC5 status frame to every online character
in members.
================
*/
func (r *InviteRuntime) broadcastCampStatus(divisionID string, members []domain.TrainingCampMemberRecord, payload []byte) {
	for _, member := range members {
		character := r.findDivisionCharacterByID(divisionID, member.CharID)
		if character == nil {
			continue
		}
		peer, live := r.presence.SessionByName(divisionID, character.Name)
		if !live {
			continue
		}
		_ = peer.Send(OpTCStatus, payload)
	}
}

/*
================
publishRankChanges

Broadcasts 0x3AC5 status 12 to online members of every camp whose
EvaluationRank changed and syncs the camp master's active Honor Rank buffs.
================
*/
func (r *InviteRuntime) publishRankChanges(divisionID string, changes []domain.TrainingCampRankChange) {
	for _, change := range changes {
		if len(change.Members) > 0 {
			r.broadcastCampStatus(divisionID, change.Members, EncodeCampEvaluationUpdate3AC5(change.Evaluation))
		}
		if r.SyncMasterHonorBuffs != nil {
			r.SyncMasterHonorBuffs(divisionID, change.MasterCharID, change.Evaluation)
		}
	}
}

/*
================
handleDisband

Handles 0x77C5 (client 702E70, 0 bytes). The master dissolves the camp at
least 24 hours after opening, receives 0xB7C5 [1], all online members
receive 0x3AC5 [1], and any changed Honor Rank buffs are synchronized.
================
*/
func (r *InviteRuntime) handleDisband(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || len(payload) != 0 {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	res := r.deps.TrainingCampAuthority().DisbandCamp(divisionID, actor.ID, r.Now().UnixMilli())
	switch res.Refusal {
	case domain.TrainingCampDisbandAccepted:
		_ = s.Send(OpTCDisbandAnswer, encodeResultAck())
		r.broadcastCampStatus(divisionID, res.MembersBefore, EncodeCampDisband3AC5())
		if r.SyncMasterHonorBuffs != nil {
			r.SyncMasterHonorBuffs(divisionID, actor.ID, 0)
		}
		for _, change := range res.RankChanges {
			if change.CampID == res.Camp.ID {
				continue
			}
			r.publishRankChanges(divisionID, []domain.TrainingCampRankChange{change})
		}
		log.Debugf("mentor: %s disbanded academy (camp %d)", actor.Name, res.Camp.ID)
	case domain.TrainingCampDisbandWait:
		_ = s.Send(OpTCDisbandAnswer, encodeDisbandWaitError(res.RemainingSeconds))
	case domain.TrainingCampDisbandDead:
		_ = s.Send(OpTCDisbandAnswer, encodeResultError(ErrTCDead))
	case domain.TrainingCampDisbandNotMaster:
		_ = s.Send(OpTCDisbandAnswer, encodeResultError(ErrTCOnlyBossCanUse))
	default:
		_ = s.Send(OpTCDisbandAnswer, encodeResultError(ErrTCNotInCamp))
	}
}

/*
================
handleLeave

Handles 0x7785 (client 702FD0, 0 bytes). A non-master member leaves their
camp voluntarily, receives 0xB785 [1], and all online camp members (including
the leaver) receive 0x3AC5 [3][u32 gid][1].
================
*/
func (r *InviteRuntime) handleLeave(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || len(payload) != 0 {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	res := r.deps.TrainingCampAuthority().LeaveCamp(divisionID, actor.ID, r.Now().UnixMilli())
	switch res.Refusal {
	case domain.TrainingCampLeaveAccepted:
		_ = s.Send(OpTCLeaveAnswer, encodeResultAck())
		leaverGID := enterworld.ObjectIDForCharacter(actor)
		leavePush := EncodeCampMemberLeave3AC5(leaverGID, 1)
		r.broadcastCampStatus(divisionID, res.MembersBefore, leavePush)
		log.Debugf("mentor: %s left academy (camp %d)", actor.Name, res.Camp.ID)
	case domain.TrainingCampLeaveDead:
		_ = s.Send(OpTCLeaveAnswer, encodeResultError(ErrTCDead))
	case domain.TrainingCampLeaveIsMaster:
		_ = s.Send(OpTCLeaveAnswer, encodeResultError(ErrTCNotCancel))
	default:
		_ = s.Send(OpTCLeaveAnswer, encodeResultError(ErrTCNotInCamp))
	}
}

/*
================
decodeCampTargetName

Reads a sized ANSI string {u16 len, bytes} from a 0x74D4 or 0x736D payload.
================
*/
func decodeCampTargetName(payload []byte) (string, bool) {
	reader := wire.NewReader(payload)
	n, err := reader.U16()
	if err != nil || n == 0 || n > 64 {
		return "", false
	}
	raw, err := reader.Bytes(int(n))
	if err != nil || reader.Done() != nil {
		return "", false
	}
	return string(raw), true
}

/*
================
handleKick

Handles 0x74D4 (client 704BB0, {str name}). The master expels a sub-mentor
or an under-40 student, receives 0xB4D4 [1], and all online camp members
(including the kicked target) receive 0x3AC5 [3][u32 targetGid][2].
================
*/
func (r *InviteRuntime) handleKick(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	targetName, ok := decodeCampTargetName(payload)
	if !ok {
		return
	}
	res := r.deps.TrainingCampAuthority().KickMember(divisionID, actor.ID, targetName, r.Now().UnixMilli())
	switch res.Refusal {
	case domain.TrainingCampKickAccepted:
		_ = s.Send(OpTCKickAnswer, encodeResultAck())
		target := r.findDivisionCharacterByID(divisionID, res.RemovedMember.CharID)
		var targetGID uint32
		if target != nil {
			targetGID = enterworld.ObjectIDForCharacter(target)
		}
		kickPush := EncodeCampMemberLeave3AC5(targetGID, 2)
		r.broadcastCampStatus(divisionID, res.MembersBefore, kickPush)
		log.Debugf("mentor: %s kicked %s from academy (camp %d)", actor.Name, targetName, res.Camp.ID)
	case domain.TrainingCampKickDead:
		_ = s.Send(OpTCKickAnswer, encodeResultError(ErrTCDead))
	case domain.TrainingCampKickTargetGraduatedLevel:
		_ = s.Send(OpTCKickAnswer, encodeResultError(ErrTCCantBanMember))
	case domain.TrainingCampKickTargetNotFound:
		_ = s.Send(OpTCKickAnswer, encodeResultError(ErrTCTargetNotInTC))
	case domain.TrainingCampKickNotMaster:
		_ = s.Send(OpTCKickAnswer, encodeResultError(ErrTCOnlyBossCanUse))
	default:
		_ = s.Send(OpTCKickAnswer, encodeResultError(ErrTCNotInCamp))
	}
}

/*
================
handleGraduate

Handles 0x710A (client 702F10, {u8 satisfaction}). A level-40+ student
graduates, awards the camp's evaluation points and mentor honor points,
receives bonus graduate EXP, and broadcasts 0x3AC5 [4][gid][0], [5][mentorGid][3][honor],
and (when any camp's EvaluationRank changes) [12][evaluationRank].
================
*/
func (r *InviteRuntime) handleGraduate(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || len(payload) != 1 {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	satisfaction := payload[0]
	res := r.deps.TrainingCampAuthority().Graduate(divisionID, actor.ID, satisfaction, r.Now().UnixMilli())
	switch res.Refusal {
	case domain.TrainingCampGraduateAccepted:
		_ = s.Send(OpTCGraduateAnswer, encodeResultAck())
		gradGID := enterworld.ObjectIDForCharacter(actor)
		gradPush := EncodeCampMemberGraduate3AC5(gradGID, 0)
		r.broadcastCampStatus(divisionID, res.MembersBefore, gradPush)
		r.publishGraduationUpdates(divisionID, res)
		if res.AwardedExp {
			if bonus := BonusGraduateExp(res.Graduate.JoinLevel, r.LevelData); bonus > 0 && r.GrantGraduateExperience != nil {
				r.GrantGraduateExperience(divisionID, actor.ID, bonus)
			}
		}
		log.Debugf("mentor: %s graduated from academy (camp %d, joinLevel %d)", actor.Name, res.Camp.ID, res.Graduate.JoinLevel)
	case domain.TrainingCampGraduateDead:
		_ = s.Send(OpTCGraduateAnswer, encodeResultError(ErrTCDead))
	case domain.TrainingCampGraduateLevelLow:
		_ = s.Send(OpTCGraduateAnswer, encodeResultError(ErrTCGraduateLevel))
	default:
		_ = s.Send(OpTCGraduateAnswer, encodeResultError(ErrTCNotInCamp))
	}
}

/*
================
handleCompulsoryGraduate

Handles 0x736D (client 704D30, {str name}; v1.188
CGObjPC_HandleAcademyCompulsoryGraduate7476 0x00519770). The master
compulsory-graduates a level-40+ student after 24 hours in the camp,
awarding satisfaction=2 (Compulsory: +1 eval point per 0x005198A4: push 0x1)
and mentor honor points (no bonus EXP per 0x005DFC30).
================
*/
func (r *InviteRuntime) handleCompulsoryGraduate(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending || r.deps.TrainingCampAuthority() == nil {
		return
	}
	targetName, ok := decodeCampTargetName(payload)
	if !ok {
		return
	}
	res := r.deps.TrainingCampAuthority().CompulsoryGraduate(divisionID, actor.ID, targetName, r.Now().UnixMilli())
	switch res.Refusal {
	case domain.TrainingCampGraduateAccepted:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultAck())
		target := r.findDivisionCharacterByID(divisionID, res.Graduate.CharID)
		var targetGID uint32
		if target != nil {
			targetGID = enterworld.ObjectIDForCharacter(target)
		}
		gradPush := EncodeCampMemberGraduate3AC5(targetGID, 1)
		r.broadcastCampStatus(divisionID, res.MembersBefore, gradPush)
		r.publishGraduationUpdates(divisionID, res)
		if res.AwardedExp && target != nil {
			if bonus := BonusGraduateExp(res.Graduate.JoinLevel, r.LevelData); bonus > 0 && r.GrantGraduateExperience != nil {
				r.GrantGraduateExperience(divisionID, target.ID, bonus)
			}
		}
		log.Debugf("mentor: %s compulsory-graduated %s from academy (camp %d)", actor.Name, targetName, res.Camp.ID)
	case domain.TrainingCampGraduateDead:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCDead))
	case domain.TrainingCampGraduateWait24h:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCCompulsory24h))
	case domain.TrainingCampGraduateLevelLow:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCGraduateLevel))
	case domain.TrainingCampGraduateTargetNotFound, domain.TrainingCampGraduateNotApprentice:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCTargetNotInTC))
	case domain.TrainingCampGraduateNotMaster:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCOnlyBossCanUse))
	default:
		_ = s.Send(OpTCCompulsoryGraduateAnswer, encodeResultError(ErrTCNotInCamp))
	}
}

/*
================
publishGraduationUpdates

Broadcasts updated mentor honor point balances (0x3AC5 status 5 flag 3) and
any changed camp EvaluationRank (0x3AC5 status 12), and syncs active Honor
Rank buffs across impacted camps.
================
*/
func (r *InviteRuntime) publishGraduationUpdates(divisionID string, res domain.TrainingCampGraduateResult) {
	for _, delta := range res.HonorDeltas {
		mentor := r.findDivisionCharacterByID(divisionID, delta.CharID)
		if mentor == nil {
			continue
		}
		mentorGID := enterworld.ObjectIDForCharacter(mentor)
		r.broadcastCampStatus(divisionID, res.MembersAfter, EncodeCampMemberHonorUpdate3AC5(mentorGID, delta.HonorPoints))
	}
	r.publishRankChanges(divisionID, res.RankChanges)
}

/*
================
EncodeHonorRankB2D0

Encodes the 0xB2D0 Honor Rank response (client 771750, academy.ts:383):
{u8 1}{u8 count} followed by count <= 50 rows of
{u32 ranking}{u8 rank}{str masterName}{u8 level}{u8 maxLevel}{u32 graduates}{str guildName}.
================
*/
func EncodeHonorRankB2D0(rows []domain.TrainingCampHonorRanking) []byte {
	count := len(rows)
	if count > 50 {
		count = 50
	}
	writer := wire.NewWriter(2 + count*32)
	writer.U8(1)
	writer.U8(uint8(count))
	for i := 0; i < count; i++ {
		entry := rows[i]
		writer.U32(entry.Ranking)
		writer.U8(entry.Rank)
		writeCampString(writer, entry.MasterName)
		writer.U8(entry.Level)
		writer.U8(entry.MaxLevel)
		writer.U32(entry.GraduateCount)
		writeCampString(writer, entry.GuildName)
	}
	return writer.Payload()
}

/*
================
handleHonorRank

Handles 0x72D0 (client 703070, {u32 npcGid}). Replies with 0xB2D0 carrying
the division's top 50 Honor Rank camps.
================
*/
func (r *InviteRuntime) handleHonorRank(s *transport.Session, opcode uint16, payload []byte) {
	_, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound || (len(payload) != 4 && len(payload) != 1) || r.deps.TrainingCampAuthority() == nil {
		return
	}
	rows := r.deps.TrainingCampAuthority().HonorRankings(divisionID)
	_ = s.Send(OpTCHonorRankAnswer, EncodeHonorRankB2D0(rows))
}

/*
================
HonorPoints

Returns a character's usable Academy honor point balance for Honor Shop
transactions.
================
*/
func (r *InviteRuntime) HonorPoints(divisionID string, character *enterworld.Character) uint32 {
	if r == nil || character == nil || r.deps == nil || r.deps.TrainingCampAuthority() == nil {
		return 0
	}
	return r.deps.TrainingCampAuthority().HonorPoints(divisionID, character.ID)
}

/*
================
SpendHonorPoints

Deducts cost honor points from a character's camp membership row and
broadcasts 0x3AC5 status 5 flag 3 to all online camp members.
================
*/
func (r *InviteRuntime) SpendHonorPoints(divisionID string, character *enterworld.Character, cost uint32) bool {
	if r == nil || character == nil || r.deps == nil || r.deps.TrainingCampAuthority() == nil {
		return false
	}
	if cost == 0 {
		return true
	}
	remaining, members, ok := r.deps.TrainingCampAuthority().SpendHonorPoints(divisionID, character.ID, cost)
	if !ok {
		return false
	}
	memberGID := enterworld.ObjectIDForCharacter(character)
	r.broadcastCampStatus(divisionID, members, EncodeCampMemberHonorUpdate3AC5(memberGID, remaining))
	return true
}

/*
================
NotifyMemberLevel

NotifyMemberLevel broadcasts a 0x3AC5 status 5 flag 2 level update to all
online members of character's camp when character levels up.
================
*/
func (r *InviteRuntime) NotifyMemberLevel(divisionID string, character *enterworld.Character) {
	if r == nil || character == nil || r.deps == nil || r.deps.TrainingCampAuthority() == nil {
		return
	}
	campID, joined := r.deps.TrainingCampAuthority().CampOfCharacter(divisionID, character.ID)
	if !joined {
		return
	}
	_, members, ok := r.deps.TrainingCampAuthority().Camp(divisionID, campID)
	if !ok {
		return
	}
	gid := enterworld.ObjectIDForCharacter(character)
	r.broadcastCampStatus(divisionID, members, EncodeCampMemberLevelUpdate3AC5(gid, characterLevel(character)))
}
