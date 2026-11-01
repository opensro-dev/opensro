/*
===========================================================================

trainingcamp.go - training-camp records and the store door contract

The camp row, its member rows, and the policy inputs and results of the
store door's opening and admission commands.

===========================================================================
*/
package domain

// Training-camp (mentor/academy) records + the store door contract.
//
// A training camp is a cross-character entity like a guild, so it persists
// in the authority store's training_camps / training_camp_members tables
// (the guilds/guild_members pattern), reached through the TrainingCampStore
// door below. EVIDENCE(v1.188, logic): the retail GameServer persists camps
// in the _TRAININGCAMP / _TRAININGCAMPMEMBER DB tables (the server's DB
// record table binds) with honor-rank and buff-status satellites - the
// satellites are deliberately NOT modeled here (honor/buff mechanics are
// outside the invite lane).
//
// Deliberately NO Character FK (unlike GuildID): the camp seed resolves
// through CampOfCharacter at enter-world, so a second character-schema key
// buys nothing - a DECISION documented in internal/game/social/mentor.
type TrainingCampRecord struct {
	// ID is the camp id. DECISION: allocated as the master's character
	// id (one camp per master; no wire consumer ever sees a camp id -
	// the v1.150 client's 0x3AC5 frames carry member ids only - so a
	// separate id space would be invention).
	ID int64 `json:"id"`
	// MasterCharID is the founding master's character id (the kind-0
	// member row's CharID; kept on the row too so the camp resolves
	// masterless-ness without scanning members).
	MasterCharID         int64  `json:"masterCharId"`
	Subject              string `json:"subject,omitempty"`
	Contents             string `json:"contents,omitempty"`
	CreatedAtMs          int64  `json:"createdAtMs,omitempty"`
	GraduateCount        uint32 `json:"graduateCount,omitempty"`
	EvaluationPoints     int32  `json:"evaluationPoints,omitempty"`
	EvaluationRank       uint8  `json:"evaluationRank,omitempty"`
	LatestEvaluationAtMs int64  `json:"latestEvaluationAtMs,omitempty"`
}

// TrainingCampMemberRecord is one persisted camp member. The wire member
// id is NOT stored - it derives from ObjectIDForCharacter at encode time
// (the party MemberID posture), and level/name project from the live
// character record so they can never rot.
type TrainingCampMemberRecord struct {
	CharID int64 `json:"charId"`
	// Kind is the client record's +0x44 byte: 0 master, 1 sub-mentor
	// (assistant guardian), 2 student/apprentice - the input the client's
	// TrainingCampMember_ClassifyGrade classifier switches on.
	Kind        uint8  `json:"kind"`
	JoinedAtMs  int64  `json:"joinedAtMs,omitempty"`
	JoinLevel   uint8  `json:"joinLevel,omitempty"`
	HonorPoints uint32 `json:"honorPoints,omitempty"`
}

// TrainingCampAdmission is the complete policy input for admitting one
// student. ExpectedCampID names the camp the master opened at the NPC
// (OpenCamp); the master must still own that exact camp. An admission never
// creates a camp: v1.150 opens one only through the NPC's 0x72EC.
//
// The level and capacity values are supplied by the gameplay package because
// they are protocol/version policy. The authority store applies them while it
// owns the character and camp records, eliminating a check-then-commit race.
type TrainingCampAdmission struct {
	MasterCharID    int64
	StudentCharID   int64
	ExpectedCampID  int64
	MasterMinLevel  int64
	StudentMaxLevel int64
	StudentLimit    int
	NowMs           int64
}

// TrainingCampAdmissionResult is the committed aggregate snapshot returned
// by AdmitStudent. Members are in persisted wire order.
type TrainingCampAdmissionResult struct {
	Camp    TrainingCampRecord
	Members []TrainingCampMemberRecord
}

// TrainingCampOpening is the policy input for opening a camp at the NPC
// (the v1.188 camp opening check and its manager): the master's level floor
// and the gold fee the opening burns. The gameplay package supplies both;
// the store applies them with the debit and the new rows under one lock.
type TrainingCampOpening struct {
	MasterCharID int64
	MinLevel     int64
	Fee          int64
	NowMs        int64
}

// TrainingCampOpenRefusal is why OpenCamp refused. The gameplay package maps
// each to the v1.150 category-0x1D code the 0xB2EC answer carries.
type TrainingCampOpenRefusal uint8

const (
	TrainingCampOpenAccepted TrainingCampOpenRefusal = iota
	// TrainingCampOpenUnavailable: no such live character, or one pending
	// deletion. No answer names it.
	TrainingCampOpenUnavailable
	// TrainingCampOpenDead: the opening check's IsAlive test (0x6807).
	TrainingCampOpenDead
	// TrainingCampOpenLevel: below the level floor (0x6809).
	TrainingCampOpenLevel
	// TrainingCampOpenGold: less gold than the fee (0x680A).
	TrainingCampOpenGold
	// TrainingCampOpenInCamp: already a member of a camp (0x6812).
	TrainingCampOpenInCamp
	// TrainingCampOpenReopenWait: within the 7-day disband cooldown (0x681C).
	TrainingCampOpenReopenWait
)

// TrainingCampOpenResult is the committed opening: the new camp, its one
// master row, and the master's gold after the fee.
type TrainingCampOpenResult struct {
	Camp             TrainingCampRecord
	Members          []TrainingCampMemberRecord
	Gold             int64
	RemainingSeconds uint32
}

// TrainingCampRankChange reports a camp whose EvaluationRank (0 unranked,
// 1..4 ranked) changed when division honor rankings were recomputed.
type TrainingCampRankChange struct {
	CampID       int64
	MasterCharID int64
	Evaluation   uint8
	Members      []TrainingCampMemberRecord
}

// TrainingCampDisbandRefusal is why DisbandCamp refused
// (the v1.188 0x7471 disband handler validates the request, submits it to
// the camp manager, which marks the camp deleted, cancels the camp buffs
// and stamps each member's leave time).
type TrainingCampDisbandRefusal uint8

const (
	TrainingCampDisbandAccepted TrainingCampDisbandRefusal = iota
	TrainingCampDisbandUnavailable
	TrainingCampDisbandDead
	TrainingCampDisbandNotInCamp
	TrainingCampDisbandNotMaster
	TrainingCampDisbandWait
)

// TrainingCampDisbandResult is the outcome of DisbandCamp.
type TrainingCampDisbandResult struct {
	Refusal          TrainingCampDisbandRefusal
	RemainingSeconds uint32
	Camp             TrainingCampRecord
	MembersBefore    []TrainingCampMemberRecord
	RankChanges      []TrainingCampRankChange
}

// TrainingCampLeaveRefusal is why LeaveCamp refused
// (the v1.188 0x7474 leave handler, through the camp's shared leave-or-ban
// command).
type TrainingCampLeaveRefusal uint8

const (
	TrainingCampLeaveAccepted TrainingCampLeaveRefusal = iota
	TrainingCampLeaveUnavailable
	TrainingCampLeaveDead
	TrainingCampLeaveNotInCamp
	TrainingCampLeaveIsMaster
	TrainingCampLeaveWait24h
)

// TrainingCampLeaveResult is the outcome of LeaveCamp.
type TrainingCampLeaveResult struct {
	Refusal       TrainingCampLeaveRefusal
	Camp          TrainingCampRecord
	RemovedMember TrainingCampMemberRecord
	MembersBefore []TrainingCampMemberRecord
}

// TrainingCampKickRefusal is why KickMember refused
// (the v1.188 0x7473 kick handler, through the camp's shared leave-or-ban
// command).
type TrainingCampKickRefusal uint8

const (
	TrainingCampKickAccepted TrainingCampKickRefusal = iota
	TrainingCampKickUnavailable
	TrainingCampKickDead
	TrainingCampKickNotInCamp
	TrainingCampKickNotMaster
	TrainingCampKickTargetNotFound
	TrainingCampKickWait24h
)

// TrainingCampKickResult is the outcome of KickMember.
type TrainingCampKickResult struct {
	Refusal       TrainingCampKickRefusal
	Camp          TrainingCampRecord
	RemovedMember TrainingCampMemberRecord
	MembersBefore []TrainingCampMemberRecord
}

// TrainingCampGraduateRefusal is why Graduate or CompulsoryGraduate refused
// (the v1.188 0x7475 graduate and 0x7476 compulsory graduate handlers).
type TrainingCampGraduateRefusal uint8

const (
	TrainingCampGraduateAccepted TrainingCampGraduateRefusal = iota
	TrainingCampGraduateUnavailable
	TrainingCampGraduateDead
	TrainingCampGraduateNotInCamp
	TrainingCampGraduateNotMaster
	TrainingCampGraduateTargetNotFound
	TrainingCampGraduateNotApprentice
	TrainingCampGraduateLevelLow
	TrainingCampGraduateCompulsoryLevelLow
)

// TrainingCampHonorDelta reports a mentor's updated HonorPoints after a
// graduation or honor-shop purchase.
type TrainingCampHonorDelta struct {
	CharID      int64
	HonorPoints uint32
}

// TrainingCampGraduateResult is the outcome of Graduate or CompulsoryGraduate.
type TrainingCampGraduateResult struct {
	Refusal       TrainingCampGraduateRefusal
	Camp          TrainingCampRecord
	Graduate      TrainingCampMemberRecord
	MembersBefore []TrainingCampMemberRecord
	MembersAfter  []TrainingCampMemberRecord
	HonorDeltas   []TrainingCampHonorDelta
	RankChanges   []TrainingCampRankChange
	AwardedExp    bool
}

// TrainingCampHonorRanking is one row of the Top-50 division Honor Ranking
// table (0xB2D0, as the v1.188 camp manager's honor rank list writes each
// entry).
type TrainingCampHonorRanking struct {
	Ranking       uint32
	Rank          uint8
	MasterCharID  int64
	MasterName    string
	Level         uint8
	MaxLevel      uint8
	GraduateCount uint32
	GuildName     string
}

// TrainingCampStore is the authority store's training-camp door (the
// GuildStore twin over training_camps / training_camp_members). The dirty
// unit is the WHOLE camp - the row plus its member set - committed as one
// whole-set replace, and the member SLICE ORDER is the wire order the
// 0x3AC5 status-10 sub-1 roster loop emits.
type TrainingCampStore interface {
	// Camp returns copies of the camp row and its member list in wire
	// order; ok=false when no such camp is stored.
	Camp(divisionID string, campID int64) (TrainingCampRecord, []TrainingCampMemberRecord, bool)
	// CampOfCharacter answers the camp a character is a MEMBER of
	// (master included - the master is a kind-0 member row), resolved
	// from the stored member sets.
	CampOfCharacter(divisionID string, characterID int64) (int64, bool)
	// OpenCamp opens the master's camp at the NPC: it checks the live
	// character, level and gold and that no camp holds the master, then
	// burns the fee and installs the camp row and its kind-0 master row in
	// one transaction.
	OpenCamp(divisionID string, opening TrainingCampOpening) (TrainingCampOpenResult, TrainingCampOpenRefusal)
	// AdmitStudent appends one student to the master's existing camp. It
	// validates the expected camp, master ownership, both live character
	// records, lifecycle and level policy, student uniqueness and capacity
	// under one authority lock, then commits in one transaction.
	AdmitStudent(divisionID string, admission TrainingCampAdmission) (TrainingCampAdmissionResult, bool)
	// UpdateNotice rechecks the expected camp and kind-0 author under the
	// authority lock, persists the notice, and returns a detached snapshot.
	UpdateNotice(divisionID string, authorID, expectedCampID int64, subject, contents string) (TrainingCampRecord, []TrainingCampMemberRecord, bool)
	// DisbandCamp dissolves the master's camp after the 7-day creation cooldown,
	// stamps CampDisbandedAtMs on the master, and recomputes division honor ranks.
	DisbandCamp(divisionID string, masterCharID int64, nowMs int64) TrainingCampDisbandResult
	// LeaveCamp removes a non-master member from their camp after the 24h join cooldown.
	LeaveCamp(divisionID string, memberCharID int64, nowMs int64) TrainingCampLeaveResult
	// KickMember removes a named non-master member from the master's camp after the 24h join cooldown.
	KickMember(divisionID string, masterCharID int64, targetName string, nowMs int64) TrainingCampKickResult
	// Graduate completes a level-40+ student's voluntary graduation, awards
	// evaluation and honor points from satisfaction (0..4), and recomputes
	// division honor ranks.
	Graduate(divisionID string, memberCharID int64, satisfaction uint8, nowMs int64) TrainingCampGraduateResult
	// CompulsoryGraduate completes a master-initiated graduation of a level-41+
	// student, awarding Compulsory satisfaction (2).
	CompulsoryGraduate(divisionID string, masterCharID int64, targetName string, nowMs int64) TrainingCampGraduateResult
	// HonorRankings returns the division's Top-50 ranked camps in ranking order.
	HonorRankings(divisionID string) []TrainingCampHonorRanking
	// HonorPoints returns the character's current camp HonorPoints (0 if not in a camp).
	HonorPoints(divisionID string, charID int64) uint32
	// SpendHonorPoints deducts cost from the character's camp HonorPoints in one
	// transaction and returns the new balance and the camp's member list.
	SpendHonorPoints(divisionID string, charID int64, cost uint32) (uint32, []TrainingCampMemberRecord, bool)
}
