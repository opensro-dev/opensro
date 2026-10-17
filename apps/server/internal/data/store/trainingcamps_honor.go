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

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
)

/*
================
Graduate

CTrainingCampManager_OnGraduateRequest_5e00b0 (0x005e00b0):
Voluntary apprentice graduation at level >= 40 with satisfaction 0..4.
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

CTrainingCampManager_OnCompulsoryGraduateRequest_5e07c0 (0x005e07c0):
Master-initiated compulsory graduation of a level-40+ apprentice after 24h
in the camp, using satisfaction 2 (Compulsory: +3 evaluation points).
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
	if !joined || !exists || camp.MasterCharID != masterCharID {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateNotMaster}
	}

	members := s.campMembers[divisionID][campID]
	targetIdx := -1
	var targetChar *domain.Character
	for i, member := range members {
		candidate := s.characterByIDLocked(divisionID, member.CharID)
		if candidate != nil && !candidate.DeletePending && candidate.Name == targetName {
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
	if resolvedCharacterLevel(targetChar) < campGraduationLevel {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateLevelLow}
	}

	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	if targetMember.JoinedAtMs > 0 && (nowMs-targetMember.JoinedAtMs) < campCompulsoryWaitMs {
		return domain.TrainingCampGraduateResult{Refusal: domain.TrainingCampGraduateWait24h}
	}

	return door.completeGraduationLocked(divisionID, campID, targetIdx, 2, false, nowMs)
}

/*
================
completeGraduationLocked

CTrainingCamp_GetGuardianHonorReward_5dc8b0 (0x005dc8b0) and
CTrainingCamp_OnApprenticeGraduate_5df9f0 (0x005df9f0):
Awards mentor honor points and camp evaluation points, increments
GraduateCount, removes the graduating apprentice, and recomputes division
honor rankings under s.mu.
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

	subMentors := 0
	for _, member := range members {
		if member.Kind == 1 {
			subMentors++
		}
	}

	next := make([]domain.TrainingCampMemberRecord, 0, len(members)-1)
	var honorDeltas []domain.TrainingCampHonorDelta
	for i, member := range members {
		if i == memberIdx {
			continue
		}
		if (member.Kind == 0 || member.Kind == 1) && satisfaction >= 1 && satisfaction <= 4 && joinLevel < campGraduationLevel {
			gainedLevels := float64(campGraduationLevel - joinLevel)
			var share float64
			if member.Kind == 0 {
				if subMentors == 0 {
					share = 1.0
				} else {
					share = 0.6
				}
			} else if subMentors == 1 {
				share = 0.4
			} else if subMentors == 2 {
				share = 0.2
			}
			reward := uint32(math.Ceil(gainedLevels * share))
			if member.HonorPoints < campMaxHonorPoints && reward > 0 {
				if campMaxHonorPoints-member.HonorPoints < reward {
					reward = campMaxHonorPoints - member.HonorPoints
				}
				member.HonorPoints += reward
				honorDeltas = append(honorDeltas, domain.TrainingCampHonorDelta{
					CharID:      member.CharID,
					HonorPoints: member.HonorPoints,
				})
			}
		}
		next = append(next, member)
	}

	if joinLevel < campGraduationLevel {
		switch satisfaction {
		case 0:
			camp.EvaluationPoints -= 15
		case 1:
			camp.EvaluationPoints -= 10
		case 2:
			camp.EvaluationPoints += 3
		case 3:
			camp.EvaluationPoints += 10
		case 4:
			camp.EvaluationPoints += 12
		}
	}
	camp.GraduateCount++
	camp.LatestEvaluationAtMs = nowMs

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
		AwardedExp:    voluntary && joinLevel < campGraduationLevel,
	}
}

/*
================
qualifiedHonorCampsLocked

CTrainingCamp_HasHonorRankingQualification_5dc7f0 (0x005dc7f0) and
std_less_TrainingCampHonorRank_5df6c0 (0x005df6c0):
Collects camps with GraduateCount >= 1 and EvaluationPoints > 0, sorted by
EvaluationPoints descending, then LatestEvaluationAtMs ascending, capped
at Top 50.
================
*/
func (door storeCampDoor) qualifiedHonorCampsLocked(divisionID string) []domain.TrainingCampRecord {
	s := door.s
	var qualified []domain.TrainingCampRecord
	for _, camp := range s.camps[divisionID] {
		if camp.GraduateCount < 1 || camp.EvaluationPoints <= 0 {
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

CTrainingCampManager_AssignHonorBuffTiers_5ddb30 (0x005ddb30):
Top 10% = 1 (Gold), next 20% = 2 (Silver), next 30% = 3 (Copper),
remainder = 4 (Iron/Bronze).
================
*/
func honorTierRankForIndex(index, total int) uint8 {
	goldCount := int(float64(total) * 0.1)
	silverCount := int(float64(total) * 0.2)
	copperCount := int(float64(total) * 0.3)
	if index < goldCount {
		return 1
	}
	if index < goldCount+silverCount {
		return 2
	}
	if index < goldCount+silverCount+copperCount {
		return 3
	}
	return 4
}

/*
================
recomputeEvaluationRanksLocked

Recomputes EvaluationRank (0 unranked, 1..4 ranked) across all camps in the
division and returns every camp whose EvaluationRank changed.
================
*/
func (door storeCampDoor) recomputeEvaluationRanksLocked(divisionID string) []domain.TrainingCampRankChange {
	s := door.s
	qualified := door.qualifiedHonorCampsLocked(divisionID)
	desiredRank := make(map[int64]uint8, len(qualified))
	for i, camp := range qualified {
		desiredRank[camp.ID] = honorTierRankForIndex(i, len(qualified))
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

CTrainingCampManager_OnHonorRankRequest_5dd1c0 (0x005dd1c0) and
CTrainingCamp_WriteHonorRankEntry_5deeb0 (0x005deeb0):
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
			rank = honorTierRankForIndex(i, len(qualified))
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
