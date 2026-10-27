/*
===========================================================================

trainingcamps_honor.go - training-camp graduation, honor rankings and points

Owns the graduation and Honor Rank half of storeCampDoor:
Graduate, CompulsoryGraduate, completeGraduationLocked,
recomputeEvaluationRanksLocked, HonorRankings, HonorPoints and
SpendHonorPoints.

===========================================================================
*/
package store

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
)

/*
================
Graduate

The v1.188 server's graduation request applies a member command that the
camp manager submits as persistent operation case 4:
Voluntary apprentice graduation at level >= 40 with satisfaction 0..4
(a satisfaction of 5 or more is refused).
================
*/
func (door storeCampDoor) Graduate(divisionID string, memberCharID int64, satisfaction uint8, nowMs int64) domain.TrainingCampGraduateResult {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	if satisfaction > 4 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateUnavailable}
	}
	student := s.characterByIDLocked(divisionID, memberCharID)
	if student == nil || student.DeletePending {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateUnavailable}
	}
	if charactervitals.CurrentHP(student) == 0 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateDead}
	}
	campID, joined := door.campOfCharacterLocked(divisionID, memberCharID)
	_, exists := s.camps[divisionID][campID]
	if !joined || !exists {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotInCamp}
	}

	members := s.campMembers[divisionID][campID]
	memberIdx := -1
	for i, member := range members {
		if member.CharID == memberCharID {
			memberIdx = i
			break
		}
	}
	if memberIdx < 0 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotInCamp}
	}
	if members[memberIdx].Kind != 2 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotApprentice}
	}
	if resolvedCharacterLevel(student) < campGraduationLevel {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateLevelLow}
	}

	return door.completeGraduationLocked(divisionID, campID, memberIdx, satisfaction, true, nowMs)
}

/*
================
CompulsoryGraduate

The v1.188 server's compulsory graduation request applies the same member
command:
Master-initiated compulsory graduation of a level-41+ apprentice
(the member must be in state 2; a level at or below 0x28 is refused with
0x6822), passing arg3 = +1 (@Evaluation delta +1, matching the
evaluation table's entry for satisfaction 2) and arg2 = 0 (compulsory).
================
*/
func (door storeCampDoor) CompulsoryGraduate(divisionID string, masterCharID int64, targetName string, nowMs int64) domain.TrainingCampGraduateResult {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	master := s.characterByIDLocked(divisionID, masterCharID)
	if master == nil || master.DeletePending {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateUnavailable}
	}
	if charactervitals.CurrentHP(master) == 0 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateDead}
	}
	campID, joined := door.campOfCharacterLocked(divisionID, masterCharID)
	camp, exists := s.camps[divisionID][campID]
	if !joined || !exists {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotInCamp}
	}
	if camp.MasterCharID != masterCharID {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotMaster}
	}

	members := s.campMembers[divisionID][campID]
	targetIdx := -1
	var targetChar *domain.Character
	for i, member := range members {
		candidate := s.characterByIDLocked(divisionID, member.CharID)
		if candidate != nil && !candidate.DeletePending && strings.EqualFold(candidate.Name, targetName) {
			targetIdx = i
			targetChar = candidate
			break
		}
	}
	if targetIdx < 0 || targetChar == nil {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateTargetNotFound}
	}
	targetMember := members[targetIdx]
	if targetMember.Kind != 2 {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotApprentice}
	}
	if resolvedCharacterLevel(targetChar) < campCompulsoryGraduationLevel {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateCompulsoryLevelLow}
	}

	return door.completeGraduationLocked(divisionID, campID, targetIdx, 2, false, nowMs)
}

/*
================
completeGraduationLocked

The v1.188 server's graduation point update, its honor and evaluation
award, its clamped honor add, and retail stored
procedure _TRAINING_CAMP_MEMBER_GRADUATE (SRO_VT_SHARD.bak @ 0x53E8B30):
  - Always increments camp.GraduateCount by 1 (SQL 0x53E9376/0x53E94B0).
  - When joinLevel <= 30 (0x1E in both server tests; SQL IF @JoinLvl <= 30 at 0x53E9104):
  - mentorPoint = 40 - joinLevel (0x28 minus the join level)
  - subMentorPoint = (40 - joinLevel) / 10 (x87 trunc(* 0.1); SQL @MentorPoint / 10)
  - Master (Kind 0) gains mentorPoint; Sub-Mentors (Kind 1) gain subMentorPoint
    (clamped against signed int32 overflow 0x7FFFFFFF by the clamped add, and
    emitting HonorDeltas only when reward > 0, as the point update skips a zero).
  - Evaluation delta from the server's evaluation table: [-3, -1, +1, +3, +5]
    for satisfaction 0..4 (compulsory graduation passes satisfaction 2 = +1).
  - When joinLevel > 30, neither Honor points nor Evaluation points change.
  - Voluntary apprentice graduation sets AwardedExp = true (the camp
    manager's graduation database-success step).

================
*/
func (door storeCampDoor) completeGraduationLocked(
	divisionID string,
	campID int64,
	memberIdx int,
	satisfaction uint8,
	voluntary bool,
	nowMs int64,
) domain.TrainingCampGraduateResult {
	s := door.s
	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}

	camp := s.camps[divisionID][campID]
	members := s.campMembers[divisionID][campID]
	membersBefore := append([]domain.TrainingCampMemberRecord(nil), members...)
	graduate := members[memberIdx]

	joinLevel := int(graduate.JoinLevel)
	if joinLevel < 1 {
		joinLevel = 1
	}

	var mentorReward, subMentorReward uint32
	if joinLevel <= campHonorAwardMaxJoinLevel {
		mentorReward = uint32(campGraduationLevel - joinLevel)
		subMentorReward = mentorReward / 10
	}

	next := make([]domain.TrainingCampMemberRecord, 0, len(members)-1)
	var honorDeltas []domain.TrainingCampHonorDelta
	for i, member := range members {
		if i == memberIdx {
			continue
		}
		var reward uint32
		switch member.Kind {
		case 0:
			reward = mentorReward
		case 1:
			reward = subMentorReward
		}
		if reward > 0 {
			if uint64(member.HonorPoints)+uint64(reward) > math.MaxInt32 {
				member.HonorPoints = math.MaxInt32
			} else {
				member.HonorPoints += reward
			}
			honorDeltas = append(honorDeltas, domain.TrainingCampHonorDelta{
				CharID:      member.CharID,
				HonorPoints: member.HonorPoints,
			})
		}
		next = append(next, member)
	}

	if joinLevel <= campHonorAwardMaxJoinLevel {
		evalDeltas := [5]int32{-3, -1, 1, 3, 5}
		camp.EvaluationPoints += evalDeltas[satisfaction]
		camp.LatestEvaluationAtMs = nowMs
	}
	camp.GraduateCount++

	s.camps[divisionID][campID] = camp
	s.campMembers[divisionID][campID] = next
	s.changes.camps[campKey{division: divisionID, campID: campID}] = true

	rankChanges := door.recomputeEvaluationRanksLocked(divisionID)
	camp = s.camps[divisionID][campID]
	s.commitLocked(fmt.Sprintf("training-camp-graduate %s/%d char=%d", divisionID, campID, graduate.CharID))

	return domain.TrainingCampGraduateResult{
		Refusal:       domain.TrainingCampGraduateAccepted,
		Camp:          camp,
		Graduate:      graduate,
		MembersBefore: membersBefore,
		MembersAfter:  append([]domain.TrainingCampMemberRecord(nil), next...),
		HonorDeltas:   honorDeltas,
		RankChanges:   rankChanges,
		AwardedExp:    voluntary && graduate.Kind == 2,
	}
}

/*
================
qualifiedHonorCampsLocked

Retail stored procedure _TRAINING_CAMP_UPDATEHONORRANK (SRO_VT_SHARD.bak
@ 0x53FAE8C: SELECT TOP 50 [ID] FROM _TrainingCamp WITH (NOLOCK) WHERE
EvaluationPoint >= 1 ORDER BY EvaluationPoint DESC, LatestEvaluationDate ASC):
Collects camps with GraduateCount >= 1 and EvaluationPoints >= 1, sorted by
EvaluationPoints descending, then LatestEvaluationAtMs ascending, capped
at Top 50.
================
*/
func (door storeCampDoor) qualifiedHonorCampsLocked(divisionID string) []domain.TrainingCampRecord {
	s := door.s
	var qualified []domain.TrainingCampRecord
	for _, camp := range s.camps[divisionID] {
		if camp.GraduateCount < 1 || camp.EvaluationPoints < 1 {
			continue
		}
		master := s.characterByIDLocked(divisionID, camp.MasterCharID)
		if master == nil || master.DeletePending {
			continue
		}
		qualified = append(qualified, camp)
	}
	slices.SortFunc(qualified, func(a, b domain.TrainingCampRecord) int {
		if a.EvaluationPoints != b.EvaluationPoints {
			return cmp.Compare(b.EvaluationPoints, a.EvaluationPoints)
		}
		if a.LatestEvaluationAtMs != b.LatestEvaluationAtMs {
			return cmp.Compare(a.LatestEvaluationAtMs, b.LatestEvaluationAtMs)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(qualified) > campHonorRankingLimit {
		qualified = qualified[:campHonorRankingLimit]
	}
	return qualified
}

/*
================
honorTierRankForIndex

Retail stored procedure _TRAINING_CAMP_UPDATEHONORRANK (at byte offset
0x53FB099 of the SRO_VT_SHARD.bak dump, a database offset, not a binary
address) and the reference server's camp honor-buff engagement:
  - Rankings 1..5   (index 0..4)   = Rank 1 (King: Crown + Gold + Silver + Copper buffs)
  - Rankings 6..15  (index 5..14)  = Rank 2 (Gold: Gold + Silver + Copper buffs)
  - Rankings 16..30 (index 15..29) = Rank 3 (Silver: Silver + Copper buffs)
  - Rankings 31..50 (index 30..49) = Rank 4 (Bronze: Copper buff)

================
*/
func honorTierRankForIndex(index int) uint8 {
	ranking := index + 1
	switch {
	case ranking <= 5:
		return 1
	case ranking <= 15:
		return 2
	case ranking <= 30:
		return 3
	default:
		return 4
	}
}

/*
================
recomputeEvaluationRanksLocked

Recomputes EvaluationRank (0 unranked, 1..4 ranked; native DB sentinel 5 in
the reference server's honor-rank reload, which sets each camp's rank and
broadcasts it) across all camps in the division and returns every camp
whose EvaluationRank changed.
================
*/
func (door storeCampDoor) recomputeEvaluationRanksLocked(divisionID string) []domain.TrainingCampRankChange {
	s := door.s
	qualified := door.qualifiedHonorCampsLocked(divisionID)
	desiredRank := make(map[int64]uint8, len(qualified))
	for i, camp := range qualified {
		desiredRank[camp.ID] = honorTierRankForIndex(i)
	}

	campIDs := make([]int64, 0, len(s.camps[divisionID]))
	for campID := range s.camps[divisionID] {
		campIDs = append(campIDs, campID)
	}
	slices.Sort(campIDs)

	var changes []domain.TrainingCampRankChange
	for _, campID := range campIDs {
		camp := s.camps[divisionID][campID]
		nextRank := desiredRank[campID]
		if camp.EvaluationRank == nextRank {
			continue
		}
		camp.EvaluationRank = nextRank
		s.camps[divisionID][campID] = camp
		s.changes.camps[campKey{division: divisionID, campID: campID}] = true
		changes = append(changes, domain.TrainingCampRankChange{
			CampID:       campID,
			MasterCharID: camp.MasterCharID,
			Evaluation:   nextRank,
			Members:      append([]domain.TrainingCampMemberRecord(nil), s.campMembers[divisionID][campID]...),
		})
	}
	return changes
}

/*
================
HonorRankings

The v1.188 server's honor-rank request: the camp manager sends its 0xB478
honor rank list from a rebuilt packet buffer, one written entry per camp:
Returns the division's Top-50 honor-ranked camps in ranking order.
================
*/
func (door storeCampDoor) HonorRankings(divisionID string) []domain.TrainingCampHonorRanking {
	s := door.s
	s.mu.RLock()
	defer s.mu.RUnlock()

	qualified := door.qualifiedHonorCampsLocked(divisionID)
	if len(qualified) == 0 {
		return nil
	}
	rows := make([]domain.TrainingCampHonorRanking, 0, len(qualified))
	for i, camp := range qualified {
		master := s.characterByIDLocked(divisionID, camp.MasterCharID)
		if master == nil || master.DeletePending {
			continue
		}
		level := uint8(resolvedCharacterLevel(master))
		maxLevel := level
		for _, member := range s.campMembers[divisionID][camp.ID] {
			if member.CharID == camp.MasterCharID && member.JoinLevel > maxLevel {
				maxLevel = member.JoinLevel
			}
		}
		rank := camp.EvaluationRank
		if rank < 1 || rank > 4 {
			rank = honorTierRankForIndex(i)
		}
		rows = append(rows, domain.TrainingCampHonorRanking{
			Ranking:       uint32(i + 1),
			Rank:          rank,
			MasterCharID:  master.ID,
			MasterName:    master.Name,
			Level:         level,
			MaxLevel:      maxLevel,
			GraduateCount: camp.GraduateCount,
			GuildName:     door.guildNameOfCharacterLocked(divisionID, master.ID),
		})
	}
	return rows
}

/*
================
guildNameOfCharacterLocked
================
*/
func (door storeCampDoor) guildNameOfCharacterLocked(divisionID string, characterID int64) string {
	for guildID, members := range door.s.guildMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				if guild, ok := door.s.guilds[divisionID][guildID]; ok {
					return guild.Name
				}
				return ""
			}
		}
	}
	return ""
}

/*
================
HonorPoints
================
*/
func (door storeCampDoor) HonorPoints(divisionID string, charID int64) uint32 {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	campID, joined := door.campOfCharacterLocked(divisionID, charID)
	if !joined {
		return 0
	}
	for _, member := range door.s.campMembers[divisionID][campID] {
		if member.CharID == charID {
			return member.HonorPoints
		}
	}
	return 0
}

/*
================
SpendHonorPoints
================
*/
func (door storeCampDoor) SpendHonorPoints(divisionID string, charID int64, cost uint32) (uint32, []domain.TrainingCampMemberRecord, bool) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	campID, joined := door.campOfCharacterLocked(divisionID, charID)
	if !joined {
		return 0, nil, false
	}
	members := s.campMembers[divisionID][campID]
	memberIdx := -1
	for i, member := range members {
		if member.CharID == charID {
			memberIdx = i
			break
		}
	}
	if memberIdx < 0 || members[memberIdx].HonorPoints < cost {
		return 0, nil, false
	}
	if cost == 0 {
		return members[memberIdx].HonorPoints, append([]domain.TrainingCampMemberRecord(nil), members...), true
	}

	next := make([]domain.TrainingCampMemberRecord, len(members))
	copy(next, members)
	next[memberIdx].HonorPoints -= cost
	s.campMembers[divisionID][campID] = next
	s.changes.camps[campKey{division: divisionID, campID: campID}] = true
	s.commitLocked(fmt.Sprintf("training-camp-spend-honor %s/%d char=%d cost=%d", divisionID, campID, charID, cost))

	return next[memberIdx].HonorPoints, append([]domain.TrainingCampMemberRecord(nil), next...), true
}
