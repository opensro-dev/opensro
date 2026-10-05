package guild

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
)

// Validation bounds and permission bits the mutators enforce.
const (
	defaultGuildMemberLevel int64 = 1
	maxGuildMemberLevel     int64 = 140

	// GuildNameMaxBytes is the guild-name byte cap. SOFT PIN: the
	// v1.188 client rejects a create name longer than 12 bytes before
	// composing; no v1.150-side pin exists, so 12 is adopted as the
	// documented DECISION (it also matches the character-name cap the
	// same UI family enforces).
	GuildNameMaxBytes = domain.GuildNameMaxBytes
	// NoticeSubjectMaxBytes / NoticeContentsMaxBytes mirror the client's
	// notice-compose edit-control caps (0x7F / 0x3FF): a longer field
	// bypassed the client gate and is refused.
	NoticeSubjectMaxBytes  = domain.GuildNoticeSubjectBytes
	NoticeContentsMaxBytes = domain.GuildNoticeBodyBytes
	// PermMaskNoticeEdit / PermMaskKick / PermMaskInvite are the
	// permMask bits the client arms the matching buttons on (invite:
	// mask & 0x1, sub_5e1e80 @0x005e2047). The mask is the ONLY
	// evidenced check: the client never grade-gates these buttons, so
	// grade 0 (leader) gets NO implicit bypass here - a leader holds
	// the bits because the create door granted the full mask (a
	// documented DECISION, LeaderPermMask below).
	PermMaskNoticeEdit uint32 = 0x10
	PermMaskKick       uint32 = 0x2
	PermMaskInvite     uint32 = 0x1
	// PermMaskStorage is the warehouse bit 5C7440 asks of a non-master
	// (CGuild_MemberHasPermission(member, 8)).
	PermMaskStorage uint32 = 0x8
	// LeaderGrade is the member grade the client's sub_826610 fold
	// publishes as the leader name (grade 0 = leader).
	LeaderGrade uint8 = 0
	// LeaderPermMask is the creating leader's initial permission mask.
	// DECISION - initial values are not evidenced (see HandleCreate):
	// all bits set, so every mask-gated button arms for the founder.
	LeaderPermMask uint32 = 0xFFFFFFFF
	// JoinerGrade is the member grade an accepted invitation installs.
	// DECISION - no retail join answer is captured. 0x0a is the only
	// non-leader grade the client renders an authored rank label for
	// (the member-slot populate maps 0 -> UIIT_STT_LEADER, 5 ->
	// UIIT_STT_GUILD_STAFF, 0x0a -> UIIT_STT_GUILDSMAN and everything
	// else to an EMPTY cell), so the plain-guildsman value is the one
	// that paints honestly.
	JoinerGrade uint8 = 0x0a
	// JoinerPermMask is the joiner's initial permission mask. DECISION:
	// no bits - the conservative floor. (The original rationale - "a
	// grantable-permissions door stays refused, so bits granted here
	// could never be revoked" - aged out when 0x765F landed, but 0x765F
	// grants the FORTRESS ROLE, not permMask bits: no permMask mutator
	// exists, so the floor still stands and no subOp-6 &0x10 delta is
	// ever emitted.)
	JoinerPermMask uint32 = 0
	// GuildWireMemberCap bounds the roster at the u8 the 0x32C4 /
	// 0xB663 block's memberCount field carries. DECISION - a
	// wire-integrity bound only: no native guild-capacity table is
	// pinned, but a 256th member would wrap the count byte and corrupt
	// every block frame.
	GuildWireMemberCap = domain.GuildMemberMaxCount
)

// GuildJID projects a store character id onto the u32 wire jid space.
// The authority store proves ids are inside domain.MaxCharacterID before
// adoption or allocation, so this conversion is one-to-one.
func GuildJID(id int64) uint32 {
	return uint32(id)
}

// memberLevel projects a character's persisted level onto the member
// row's u8 with the same [1, 140] clamp the 0x32B3 char-data writer uses
// (absent reads as 1).
func memberLevel(c *enterworld.Character) uint8 {
	level := defaultGuildMemberLevel
	if c.Level != nil && *c.Level >= defaultGuildMemberLevel {
		level = *c.Level
		if level > maxGuildMemberLevel {
			level = maxGuildMemberLevel
		}
	}
	return uint8(level)
}

// characterSnapshot copies mutable character state while the authority read
// door is held. Guild commands pass immutable IDs into aggregate command
// doors; they never make policy decisions from a retained live record.
func characterSnapshot(
	deps Dependencies,
	divisionID string,
	character *enterworld.Character,
) *enterworld.Character {
	if deps == nil || character == nil {
		return nil
	}
	var snapshot *enterworld.Character
	deps.Read(divisionID, func() {
		snapshot = character.Snapshot()
	})
	return snapshot
}

func guildRefusalReason(refusal enterworld.GuildRefusal) string {
	switch refusal {
	case enterworld.GuildRefusalActorNotFound:
		return "characterNotFound"
	case enterworld.GuildRefusalDeletePending:
		return "deletePending"
	case enterworld.GuildRefusalNotMember:
		return "not in a guild"
	case enterworld.GuildRefusalInconsistentMembership:
		return "inconsistent guild membership"
	case enterworld.GuildRefusalPermissionDenied:
		return "permission denied"
	case enterworld.GuildRefusalLeaderRequired:
		return "leader authority required"
	case enterworld.GuildRefusalUpdateRejected:
		return "guild update rejected"
	case enterworld.GuildRefusalGuildChanged:
		return "guild changed"
	case enterworld.GuildRefusalInvalidMember:
		return "invalid guild member"
	case enterworld.GuildRefusalRosterFull:
		return "guild roster full"
	case enterworld.GuildRefusalAlreadyMember:
		return "already in a guild"
	case enterworld.GuildRefusalTargetNotFound:
		return "target member not found"
	case enterworld.GuildRefusalSelfTarget:
		return "cannot target yourself"
	case enterworld.GuildRefusalTargetLeader:
		return "cannot target the leader"
	case enterworld.GuildRefusalLeaderCannotLeave:
		return "the leader cannot leave"
	case enterworld.GuildRefusalInvalidAmount:
		return "invalid amount"
	case enterworld.GuildRefusalInsufficientPoints:
		return "insufficient skill points"
	case enterworld.GuildRefusalNumericOverflow:
		return "numeric overflow"
	case enterworld.GuildRefusalMaxLevel:
		return "the guild is at its last level"
	case enterworld.GuildRefusalGPDeficit:
		return "not enough guild points"
	case enterworld.GuildRefusalGoldDeficit:
		return "not enough gold"
	case enterworld.GuildRefusalNoCompensation:
		return "no war compensation is owed"
	case enterworld.GuildRefusalVoteInProgress:
		return "voters and candidates stay while the vote runs"
	default:
		return "guild command refused"
	}
}
