package enterworld

import "opensro.online/server/internal/domain"

// Bootstrap exposes the persisted cross-character records and authority
// ports because its wire builders consume them. Ownership remains in domain;
// aliases keep one contract and one pointer/type identity across layers.
type LetterRecord = domain.LetterRecord
type LetterStore = domain.LetterStore
type GuildRecord = domain.GuildRecord
type GuildVote = domain.GuildVote
type GuildMemberRecord = domain.GuildMemberRecord
type GuildAuthorization = domain.GuildAuthorization
type GuildSnapshot = domain.GuildSnapshot
type GuildRemovalResult = domain.GuildRemovalResult
type GuildDonationResult = domain.GuildDonationResult
type GuildRefusal = domain.GuildRefusal
type GuildStore = domain.GuildStore
type FortressStore = domain.FortressStore
type AllianceStore = domain.AllianceStore

const (
	GuildRefusalNone                   = domain.GuildRefusalNone
	GuildRefusalActorNotFound          = domain.GuildRefusalActorNotFound
	GuildRefusalDeletePending          = domain.GuildRefusalDeletePending
	GuildRefusalNotMember              = domain.GuildRefusalNotMember
	GuildRefusalInconsistentMembership = domain.GuildRefusalInconsistentMembership
	GuildRefusalPermissionDenied       = domain.GuildRefusalPermissionDenied
	GuildRefusalLeaderRequired         = domain.GuildRefusalLeaderRequired
	GuildRefusalUpdateRejected         = domain.GuildRefusalUpdateRejected
	GuildRefusalGuildChanged           = domain.GuildRefusalGuildChanged
	GuildRefusalInvalidMember          = domain.GuildRefusalInvalidMember
	GuildRefusalRosterFull             = domain.GuildRefusalRosterFull
	GuildRefusalAlreadyMember          = domain.GuildRefusalAlreadyMember
	GuildRefusalTargetNotFound         = domain.GuildRefusalTargetNotFound
	GuildRefusalSelfTarget             = domain.GuildRefusalSelfTarget
	GuildRefusalTargetLeader           = domain.GuildRefusalTargetLeader
	GuildRefusalLeaderCannotLeave      = domain.GuildRefusalLeaderCannotLeave
	GuildRefusalInvalidAmount          = domain.GuildRefusalInvalidAmount
	GuildRefusalInsufficientPoints     = domain.GuildRefusalInsufficientPoints
	GuildRefusalNumericOverflow        = domain.GuildRefusalNumericOverflow
	GuildRefusalMaxLevel               = domain.GuildRefusalMaxLevel
	GuildRefusalGPDeficit              = domain.GuildRefusalGPDeficit
	GuildRefusalGoldDeficit            = domain.GuildRefusalGoldDeficit
	GuildRefusalNoCompensation         = domain.GuildRefusalNoCompensation
	GuildRefusalVoteOpen               = domain.GuildRefusalVoteOpen
	GuildRefusalVoteNotTime            = domain.GuildRefusalVoteNotTime
	GuildRefusalNoVote                 = domain.GuildRefusalNoVote
	GuildRefusalNotCandidate           = domain.GuildRefusalNotCandidate
	GuildRefusalVoteInProgress         = domain.GuildRefusalVoteInProgress
)

type TrainingCampRecord = domain.TrainingCampRecord
type TrainingCampMemberRecord = domain.TrainingCampMemberRecord
type TrainingCampAdmission = domain.TrainingCampAdmission
type TrainingCampAdmissionResult = domain.TrainingCampAdmissionResult
type TrainingCampStore = domain.TrainingCampStore
