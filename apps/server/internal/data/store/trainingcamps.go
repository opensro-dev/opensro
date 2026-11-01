/*
===========================================================================

trainingcamps.go - the training-camp door: opening, admission, notice and honor

The authority store's camp aggregate. OpenCamp creates a camp (0x72EC);
AdmitStudent appends to one; UpdateNotice edits its text; DisbandCamp,
LeaveCamp, KickMember, Graduate, CompulsoryGraduate, HonorRankings and
SpendHonorPoints own the Academy lifecycle and honor-point ledger under
the store lock.

===========================================================================
*/
package store

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/domain/charactervitals"
)

const (
	// campDisbandCooldownSec is the 7-day creation wait before a master can
	// dissolve a newly opened camp (the v1.188 server's disband check reads
	// the camp's wait as 0x93A80 seconds; client UIIT_MSG_TC_ERROR_NOT_DISORGANIZATION_7D).
	campDisbandCooldownSec = 604_800
	// campReopenCooldownSec is the 7-day wait after dissolving a camp before
	// the master can open another (the v1.188 server's open check: TimedJob
	// type 2 subType 7, 604,800 s).
	campReopenCooldownSec = 604_800
	// campCompulsoryWaitMs is the 24-hour join wait before a member can leave
	// or be kicked (the v1.188 server's join-cooldown test: 0x15180 seconds).
	campCompulsoryWaitMs = 86_400_000
	// campGraduationLevel is the voluntary apprentice graduation level floor
	// (40 = 0x28: the v1.188 graduation handler refuses below 0x28 with
	// error 0x681A).
	campGraduationLevel = 40
	// campCompulsoryGraduationLevel is the master-initiated compulsory
	// graduation level floor (41 = > 0x28: the v1.188 compulsory graduation
	// handler refuses at or below 0x28 with error 0x6822; client
	// UIIT_MSG_TC_ERROR_POSIBLE_41L).
	campCompulsoryGraduationLevel = 41
	// campHonorAwardMaxJoinLevel is the maximum apprentice join level that
	// awards Honor and Evaluation points on graduation (30 = 0x1E: the v1.188
	// server's graduation point update and its honor award both compare the
	// join level against 0x1E;
	// retail SP _TRAINING_CAMP_MEMBER_GRADUATE @ 0x53E9104: IF @JoinLvl <= 30).
	campHonorAwardMaxJoinLevel = 30
	// campHonorRankingLimit caps the division Honor Ranking table at 50 entries
	// (retail SP _TRAINING_CAMP_UPDATEHONORRANK @ 0x53FAE8C: SELECT TOP 50).
	campHonorRankingLimit = 50
)

/*
================
TrainingCamps

TrainingCamps returns the training-camp door (domain.TrainingCampStore
over the training_camps / training_camp_members tables). Same lifetime
contract as Characters().
================
*/
func (s *Store) TrainingCamps() domain.TrainingCampStore {
	return storeCampDoor{s: s}
}

type storeCampDoor struct{ s *Store }

/*
================
Camp

Camp returns copies of the camp row and its member list in wire order;
ok=false when no such camp is stored.
================
*/
func (door storeCampDoor) Camp(divisionID string, campID int64) (domain.TrainingCampRecord, []domain.TrainingCampMemberRecord, bool) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	camp, ok := door.s.camps[divisionID][campID]
	if !ok {
		return domain.TrainingCampRecord{}, nil, false
	}
	live := door.s.campMembers[divisionID][campID]
	members := make([]domain.TrainingCampMemberRecord, len(live))
	copy(members, live)
	return camp, members, true
}

/*
================
CampOfCharacter

CampOfCharacter answers the camp a character is a MEMBER of (the master
is a kind-0 member row, so masters resolve here too), resolved from the
stored member sets (the GuildOfCharacter shape - no Character FK).
================
*/
func (door storeCampDoor) CampOfCharacter(divisionID string, characterID int64) (int64, bool) {
	door.s.mu.RLock()
	defer door.s.mu.RUnlock()
	for campID, members := range door.s.campMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return campID, true
			}
		}
	}
	return 0, false
}

/*
================
AdmitStudent

AdmitStudent appends one student to the master's existing camp. The gameplay
layer supplies version-specific level/capacity policy, and this door applies
it together with relationship and lifecycle invariants under one lock.
================
*/
func (door storeCampDoor) AdmitStudent(
	divisionID string,
	admission domain.TrainingCampAdmission,
) (domain.TrainingCampAdmissionResult, bool) {
	var refused domain.TrainingCampAdmissionResult
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	if admission.MasterCharID < 1 ||
		admission.StudentCharID < 1 ||
		admission.MasterCharID == admission.StudentCharID ||
		admission.MasterMinLevel < 1 ||
		admission.StudentMaxLevel < 1 ||
		admission.StudentLimit < 1 ||
		admission.StudentLimit > 7 {
		return refused, false
	}
	master := s.characterByIDLocked(divisionID, admission.MasterCharID)
	student := s.characterByIDLocked(divisionID, admission.StudentCharID)
	if master == nil || student == nil || master.DeletePending || student.DeletePending {
		return refused, false
	}
	studentLevel := resolvedCharacterLevel(student)
	if resolvedCharacterLevel(master) < admission.MasterMinLevel ||
		studentLevel > admission.StudentMaxLevel {
		return refused, false
	}
	if _, joined := door.campOfCharacterLocked(divisionID, student.ID); joined {
		return refused, false
	}

	campID, masterJoined := door.campOfCharacterLocked(divisionID, master.ID)
	if !masterJoined || admission.ExpectedCampID < 1 || campID != admission.ExpectedCampID {
		return refused, false
	}
	camp, exists := s.camps[divisionID][campID]
	if !exists || camp.MasterCharID != master.ID {
		return refused, false
	}
	members := s.campMembers[divisionID][campID]

	studentCount := 0
	for _, member := range members {
		if member.Kind == 2 {
			studentCount++
		}
	}
	if studentCount >= admission.StudentLimit || len(members) >= 8 {
		return refused, false
	}

	nowMs := admission.NowMs
	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	next := make([]domain.TrainingCampMemberRecord, len(members), len(members)+1)
	copy(next, members)
	next = append(next, domain.TrainingCampMemberRecord{
		CharID:     student.ID,
		Kind:       2,
		JoinedAtMs: nowMs,
		JoinLevel:  uint8(studentLevel),
	})
	s.campMembers[divisionID][campID] = next
	s.changes.camps[campKey{division: divisionID, campID: campID}] = true
	s.commitLocked(fmt.Sprintf("training-camp-admit-student %s/%d", divisionID, campID))

	resultMembers := make([]domain.TrainingCampMemberRecord, len(next))
	copy(resultMembers, next)
	return domain.TrainingCampAdmissionResult{
		Camp:    camp,
		Members: resultMembers,
	}, true
}

/*
================
OpenCamp

The NPC's camp opening (v1.150 0x72EC; the reference server's opening check
runs first, then its camp creation burns the fee and answers). The check's
order: alive (0x6807),
level (0x6809), gold (0x680A), already in a camp (0x6812), and TimedJob (2, 7)'s
7-day reopen wait after a disband (0x681C).
================
*/
func (door storeCampDoor) OpenCamp(divisionID string, opening domain.TrainingCampOpening) (domain.TrainingCampOpenResult, domain.TrainingCampOpenRefusal) {
	var refused domain.TrainingCampOpenResult
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	if opening.MasterCharID < 1 || opening.MinLevel < 1 || opening.Fee < 0 {
		return refused, domain.TrainingCampOpenUnavailable
	}
	master := s.characterByIDLocked(divisionID, opening.MasterCharID)
	if master == nil || master.DeletePending {
		return refused, domain.TrainingCampOpenUnavailable
	}
	if charactervitals.CurrentHP(master) == 0 {
		return refused, domain.TrainingCampOpenDead
	}
	masterLevel := resolvedCharacterLevel(master)
	if masterLevel < opening.MinLevel {
		return refused, domain.TrainingCampOpenLevel
	}
	if ledgerGold(master) < opening.Fee {
		return refused, domain.TrainingCampOpenGold
	}
	if _, joined := door.campOfCharacterLocked(divisionID, master.ID); joined {
		return refused, domain.TrainingCampOpenInCamp
	}
	if _, exists := s.camps[divisionID][master.ID]; exists {
		return refused, domain.TrainingCampOpenInCamp
	}

	nowMs := opening.NowMs
	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	if master.CampDisbandedAtMs > 0 {
		elapsedSec := (nowMs - master.CampDisbandedAtMs) / 1000
		if elapsedSec < 0 {
			elapsedSec = 0
		}
		if elapsedSec < campReopenCooldownSec {
			return domain.TrainingCampOpenResult{
				RemainingSeconds: uint32(campReopenCooldownSec - elapsedSec),
			}, domain.TrainingCampOpenReopenWait
		}
	}

	gold := ledgerGold(master) - opening.Fee
	master.Gold = &gold
	master.CampDisbandedAtMs = 0
	camp := domain.TrainingCampRecord{
		ID:           master.ID,
		MasterCharID: master.ID,
		CreatedAtMs:  nowMs,
	}
	members := []domain.TrainingCampMemberRecord{{
		CharID:     master.ID,
		Kind:       0,
		JoinedAtMs: nowMs,
		JoinLevel:  uint8(masterLevel),
	}}
	if s.camps[divisionID] == nil {
		s.camps[divisionID] = map[int64]domain.TrainingCampRecord{}
	}
	if s.campMembers[divisionID] == nil {
		s.campMembers[divisionID] = map[int64][]domain.TrainingCampMemberRecord{}
	}
	s.camps[divisionID][camp.ID] = camp
	s.campMembers[divisionID][camp.ID] = members
	key := campKey{division: divisionID, campID: camp.ID}
	delete(s.changes.dissolvedCamp, key)
	s.changes.camps[key] = true
	s.changes.characters[master] = true
	s.commitLocked(fmt.Sprintf("training-camp-open %s/%d", divisionID, camp.ID))
	return domain.TrainingCampOpenResult{
		Camp:    camp,
		Members: append([]domain.TrainingCampMemberRecord(nil), members...),
		Gold:    gold,
	}, domain.TrainingCampOpenAccepted
}

/*
================
resolvedCharacterLevel
================
*/
func resolvedCharacterLevel(character *domain.Character) int64 {
	if character == nil || character.Level == nil || *character.Level < 1 {
		return 1
	}
	return *character.Level
}

/*
================
UpdateNotice

The reference server's notice-edit handler, through its camp manager's
validation (case 6), authorizes the kind-0 camp master. Keep the permission and expected aggregate checks in the same
critical section as the update so a stale gameplay precheck cannot edit
another camp.
================
*/
func (door storeCampDoor) UpdateNotice(divisionID string, authorID, expectedCampID int64, subject, contents string) (domain.TrainingCampRecord, []domain.TrainingCampMemberRecord, bool) {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()
	camp, exists := s.camps[divisionID][expectedCampID]
	author := s.characterByIDLocked(divisionID, authorID)
	if !exists || author == nil || author.DeletePending || camp.MasterCharID != authorID || charactervitals.CurrentHP(author) == 0 {
		return domain.TrainingCampRecord{}, nil, false
	}
	members := s.campMembers[divisionID][expectedCampID]
	authorized := false
	for _, member := range members {
		if member.CharID == authorID && member.Kind == 0 {
			authorized = true
			break
		}
	}
	if !authorized {
		return domain.TrainingCampRecord{}, nil, false
	}
	camp.Subject, camp.Contents = subject, contents
	s.camps[divisionID][expectedCampID] = camp
	s.changes.camps[campKey{division: divisionID, campID: expectedCampID}] = true
	s.commitLocked(fmt.Sprintf("training-camp-update-notice %s/%d", divisionID, expectedCampID))
	return camp, append([]domain.TrainingCampMemberRecord(nil), members...), true
}

/*
================
DisbandCamp

The v1.188 server's disband request runs its disband check (with the
camp's remaining wait), submits the disband, marks the camp deleted and
cancels the camp buffs:
 1. Master must belong to a camp (0x680B -> 0x0B) and be alive (0x6807 -> 0x07).
 2. Requester must own the camp as founding master (0x6816 -> 0x16).
 3. At least 7 days (604,800 s = 0x93A80) must have elapsed
    since CreatedAtMs (0x681D -> 0x1D with remainingSeconds u32).
 4. Deletes the camp and its members, stamps CampDisbandedAtMs on the master
    for the 7-day reopen cooldown (the server stamps the player's camp
    leave time), and recomputes division honor rankings.

================
*/
func (door storeCampDoor) DisbandCamp(divisionID string, masterCharID int64, nowMs int64) domain.TrainingCampDisbandResult {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	master := s.characterByIDLocked(divisionID, masterCharID)
	if master == nil || master.DeletePending {
		return domain.TrainingCampDisbandResult{Refusal: domain.TrainingCampDisbandUnavailable}
	}
	if charactervitals.CurrentHP(master) == 0 {
		return domain.TrainingCampDisbandResult{Refusal: domain.TrainingCampDisbandDead}
	}
	campID, joined := door.campOfCharacterLocked(divisionID, masterCharID)
	camp, exists := s.camps[divisionID][campID]
	if !joined || !exists {
		return domain.TrainingCampDisbandResult{Refusal: domain.TrainingCampDisbandNotInCamp}
	}
	if camp.MasterCharID != masterCharID {
		return domain.TrainingCampDisbandResult{Refusal: domain.TrainingCampDisbandNotMaster}
	}

	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	if camp.CreatedAtMs > 0 {
		elapsedSec := (nowMs - camp.CreatedAtMs) / 1000
		if elapsedSec < 0 {
			elapsedSec = 0
		}
		if elapsedSec < campDisbandCooldownSec {
			return domain.TrainingCampDisbandResult{
				Refusal:          domain.TrainingCampDisbandWait,
				RemainingSeconds: uint32(campDisbandCooldownSec - elapsedSec),
			}
		}
	}

	membersBefore := append([]domain.TrainingCampMemberRecord(nil), s.campMembers[divisionID][campID]...)
	delete(s.camps[divisionID], campID)
	delete(s.campMembers[divisionID], campID)
	key := campKey{division: divisionID, campID: campID}
	delete(s.changes.camps, key)
	s.changes.dissolvedCamp[key] = true

	master.CampDisbandedAtMs = nowMs
	s.changes.characters[master] = true

	rankChanges := door.recomputeEvaluationRanksLocked(divisionID)
	if camp.EvaluationRank > 0 {
		rankChanges = append([]domain.TrainingCampRankChange{{
			CampID:       camp.ID,
			MasterCharID: camp.MasterCharID,
			Evaluation:   0,
			Members:      membersBefore,
		}}, rankChanges...)
	}

	s.commitLocked(fmt.Sprintf("training-camp-disband %s/%d", divisionID, campID))
	return domain.TrainingCampDisbandResult{
		Refusal:       domain.TrainingCampDisbandAccepted,
		Camp:          camp,
		MembersBefore: membersBefore,
		RankChanges:   rankChanges,
	}
}

/*
================
LeaveCamp

The v1.188 server's leave request, submitted through its shared
leave-or-ban command:
 1. Member must belong to a camp (0x680B -> 0x0B) and be alive
    (0x6807 -> 0x07).
 2. Founding master cannot leave via 0x7785 (0x6816 -> 0x16).
 3. At least 24 hours (86,400 s = 0x15180 in the server's join-cooldown
    test) must have elapsed since JoinedAtMs (0x6821 -> 0x21).

================
*/
func (door storeCampDoor) LeaveCamp(divisionID string, memberCharID int64, nowMs int64) domain.TrainingCampLeaveResult {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	memberChar := s.characterByIDLocked(divisionID, memberCharID)
	if memberChar == nil || memberChar.DeletePending {
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveUnavailable}
	}
	if charactervitals.CurrentHP(memberChar) == 0 {
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveDead}
	}
	campID, joined := door.campOfCharacterLocked(divisionID, memberCharID)
	camp, exists := s.camps[divisionID][campID]
	if !joined || !exists {
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveNotInCamp}
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
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveNotInCamp}
	}
	removed := members[memberIdx]
	if removed.Kind == 0 || camp.MasterCharID == memberCharID {
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveIsMaster}
	}

	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	if removed.JoinedAtMs > 0 && (nowMs < removed.JoinedAtMs || nowMs-removed.JoinedAtMs < campCompulsoryWaitMs) {
		return domain.TrainingCampLeaveResult{Refusal: domain.TrainingCampLeaveWait24h}
	}

	membersBefore := append([]domain.TrainingCampMemberRecord(nil), members...)
	next := make([]domain.TrainingCampMemberRecord, 0, len(members)-1)
	next = append(next, members[:memberIdx]...)
	next = append(next, members[memberIdx+1:]...)
	s.campMembers[divisionID][campID] = next
	s.changes.camps[campKey{division: divisionID, campID: campID}] = true
	s.commitLocked(fmt.Sprintf("training-camp-leave %s/%d char=%d", divisionID, campID, memberCharID))

	return domain.TrainingCampLeaveResult{
		Refusal:       domain.TrainingCampLeaveAccepted,
		Camp:          camp,
		RemovedMember: removed,
		MembersBefore: membersBefore,
	}
}

/*
================
KickMember

The v1.188 server's kick request, submitted through its shared
leave-or-ban command:
 1. Master must belong to a camp (0x680B -> 0x0B) and be alive
    (0x6807 -> 0x07).
 2. Requester must be the camp's master (0x6816 -> 0x16).
 3. Target must exist in the same camp by case-insensitive name
    (the server's member-by-name lookup: 0x6815 -> 0x15) and not be the
    master (0x6816 -> 0x16).
 4. At least 24 hours (86,400 s = 0x15180 in the server's join-cooldown
    test) must have elapsed since JoinedAtMs (0x6820 -> 0x20).

================
*/
func (door storeCampDoor) KickMember(divisionID string, masterCharID int64, targetName string, nowMs int64) domain.TrainingCampKickResult {
	s := door.s
	s.mu.Lock()
	defer s.mu.Unlock()

	master := s.characterByIDLocked(divisionID, masterCharID)
	if master == nil || master.DeletePending {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickUnavailable}
	}
	if charactervitals.CurrentHP(master) == 0 {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickDead}
	}
	campID, joined := door.campOfCharacterLocked(divisionID, masterCharID)
	camp, exists := s.camps[divisionID][campID]
	if !joined || !exists {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickNotInCamp}
	}
	if camp.MasterCharID != masterCharID {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickNotMaster}
	}

	members := s.campMembers[divisionID][campID]
	targetIdx := -1
	for i, member := range members {
		candidate := s.characterByIDLocked(divisionID, member.CharID)
		if candidate != nil && !candidate.DeletePending && strings.EqualFold(candidate.Name, targetName) {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickTargetNotFound}
	}
	removed := members[targetIdx]
	if removed.CharID == masterCharID || removed.Kind == 0 {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickNotMaster}
	}

	if nowMs <= 0 {
		nowMs = s.now().UnixMilli()
	}
	if removed.JoinedAtMs > 0 && (nowMs < removed.JoinedAtMs || nowMs-removed.JoinedAtMs < campCompulsoryWaitMs) {
		return domain.TrainingCampKickResult{Refusal: domain.TrainingCampKickWait24h}
	}

	membersBefore := append([]domain.TrainingCampMemberRecord(nil), members...)
	next := make([]domain.TrainingCampMemberRecord, 0, len(members)-1)
	next = append(next, members[:targetIdx]...)
	next = append(next, members[targetIdx+1:]...)
	s.campMembers[divisionID][campID] = next
	s.changes.camps[campKey{division: divisionID, campID: campID}] = true
	s.commitLocked(fmt.Sprintf("training-camp-kick %s/%d char=%d", divisionID, campID, removed.CharID))

	return domain.TrainingCampKickResult{
		Refusal:       domain.TrainingCampKickAccepted,
		Camp:          camp,
		RemovedMember: removed,
		MembersBefore: membersBefore,
	}
}

/*
================
campOfCharacterLocked

campOfCharacterLocked is CampOfCharacter's body for callers already
holding s.mu.
================
*/
func (door storeCampDoor) campOfCharacterLocked(divisionID string, characterID int64) (int64, bool) {
	for campID, members := range door.s.campMembers[divisionID] {
		for _, member := range members {
			if member.CharID == characterID {
				return campID, true
			}
		}
	}
	return 0, false
}
