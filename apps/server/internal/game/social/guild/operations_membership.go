package guild

import "opensro.online/server/internal/game/enterworld"

// KickOutcome is one handled 0x74B1 request. EVERY kick refusal stays
// fully silent - deliberately no ErrorPayload slot: the 0x1F refusal
// code is pinned only at a client-LOCAL call site (00703ead), kick has
// no dedicated ack opcode, and no S->C carrier frame for a kick refusal
// is pinned, so a code with no evidenced carrier is not implementable
// (errors.go). PushPayload is the 0x3B29
// subOp-3 frame for every ONLINE member named in MemberNames - the
// PRE-REMOVAL member list, so the kicked player and the acting kicker
// are both included (the client branches its me-vs-not-me handling by
// looking the frame's jid up in its own member map, so ONE frame serves
// everyone). An offline kicked member gets no frame: their next login
// self-heals - the cleared FK means no 0x32C4 seed.
type KickOutcome struct {
	// ErrorPayload is 0xB4B1 [2][code] for the one refusal the client
	// names: a voter or candidate of the running vote (0x39).
	ErrorPayload []byte
	PushPayload  []byte
	MemberNames  []string
	KickedName   string
	Refusal      string
}

func refusedKick(reason string) KickOutcome {
	return KickOutcome{Refusal: reason}
}

// HandleKick applies one decoded 0x74B1 request through the ATOMIC
// RemoveGuildMember door (member row + FK clear, one commit). The
// permission check is mask-only (PermMaskKick, the same DECISION as the
// notice edit). The target resolves by EXACT name match among the
// guild's members (DECISION: no EqualFold fallback - the client's own
// me-check is exact wide-string equality and retail compares exact);
// kicking yourself or the grade-0 leader refuses. The pushed jid is the
// kicked member's STORED JID from their member record.
func HandleKick(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) KickOutcome {
	if actor == nil {
		return refusedKick("characterNotFound")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedKick("characterNotFound")
	}
	if actor.DeletePending {
		return refusedKick("deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedKick("no guild store wired")
	}
	request, err := DecodeKickRequest(payload)
	if err != nil {
		return refusedKick(err.Error())
	}
	removal, refusal := deps.GuildAuthority().KickGuildMember(
		divisionID,
		actor.ID,
		request.MemberName,
		PermMaskKick,
	)
	if refusal.Refused() {
		switch refusal {
		case enterworld.GuildRefusalPermissionDenied:
			return refusedKick("permMask lacks the kick bit")
		case enterworld.GuildRefusalTargetNotFound:
			return refusedKick("no member named " + request.MemberName)
		case enterworld.GuildRefusalSelfTarget:
			return refusedKick("cannot kick yourself")
		case enterworld.GuildRefusalTargetLeader:
			return refusedKick("cannot kick the leader")
		case enterworld.GuildRefusalVoteInProgress:
			return KickOutcome{ErrorPayload: EncodeGuildErrorResult(GuildErrVoteNoExpel), Refusal: guildRefusalReason(refusal)}
		default:
			return refusedKick(guildRefusalReason(refusal))
		}
	}

	names := make([]string, 0, len(removal.MembersBefore))
	for _, member := range removal.MembersBefore {
		names = append(names, member.Name)
	}
	return KickOutcome{
		PushPayload: EncodeMemberKick3B29(removal.RemovedMember.JID),
		MemberNames: names,
		KickedName:  removal.RemovedMember.Name,
	}
}

// LeaveOutcome is one handled 0x756E request. AckPayload is the 0xB56E
// {u8 1} success answer for the LEAVING ACTOR ONLY; PushPayload is the
// 0x3B29 subOp-3 kind-1 frame for every ONLINE member named in
// MemberNames - the PRE-REMOVAL member list, so the leaver is included
// (their client's jid==me arm performs the full guild reset; the ack
// alone does not wipe the guild block). EVERY refusal stays fully
// silent - deliberately no ErrorPayload slot: the leader-refuse 0x36
// and no-guild 0x1E codes are pinned only on the classic v1.188 leave
// job (PROBABLE for Legend), so emitting either would invent a
// contract.
type LeaveOutcome struct {
	AckPayload        []byte
	PushPayload       []byte
	MemberNames       []string
	SelectedTargetGid uint32
	Refusal           string
}

func refusedLeave(reason string) LeaveOutcome {
	return LeaveOutcome{Refusal: reason}
}

// HandleLeave applies one decoded 0x756E request through the ATOMIC
// RemoveGuildMember door (member row + FK clear, one commit - the kick
// door, self-targeted). The leaver is ALWAYS the requesting session's
// bound character: the body's u32 is decoded and logged, never used
// for identity (the create npcGid posture; the classic server parses
// and discards its leave body the same way). A grade-0 LEADER cannot
// leave - refused SILENTLY: the classic 0x4c36 refuse code is only
// PROBABLE for Legend - dissolve is the break 0x766E job (HandleBreak,
// the DissolveGuild door + subOp-1 fan-out), never this handler's.
func HandleLeave(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) LeaveOutcome {
	if actor == nil {
		return refusedLeave("characterNotFound")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedLeave("characterNotFound")
	}
	if actor.DeletePending {
		return refusedLeave("deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedLeave("no guild store wired")
	}
	request, err := DecodeLeaveRequest(payload)
	if err != nil {
		return refusedLeave(err.Error())
	}
	removal, refusal := deps.GuildAuthority().LeaveGuild(divisionID, actor.ID)
	if refusal == enterworld.GuildRefusalVoteInProgress {
		return LeaveOutcome{AckPayload: EncodeGuildErrorResult(GuildErrVoteNoLeave), Refusal: guildRefusalReason(refusal)}
	}
	if refusal.Refused() {
		return refusedLeave(guildRefusalReason(refusal))
	}

	names := make([]string, 0, len(removal.MembersBefore))
	for _, member := range removal.MembersBefore {
		names = append(names, member.Name)
	}
	return LeaveOutcome{
		AckPayload:        EncodeLeaveAckB56E(),
		PushPayload:       EncodeMemberLeave3B29(removal.RemovedMember.JID),
		MemberNames:       names,
		SelectedTargetGid: request.SelectedTargetGid,
	}
}

// BreakOutcome is one handled 0x766E request. AckPayload is the 0xB66E
// {u8 1} success answer for the DISSOLVING LEADER ONLY; PushPayload is
// the 0x3B29 subOp-1 dissolve announce for every ONLINE member named in
// MemberNames - the PRE-DISSOLVE member list, so the leader is included
// (the client's break arm reads the announced guild name from its OWN
// local state and drops the whole guild block - one payload-less frame
// serves everyone). EVERY refusal stays fully silent - deliberately no
// ErrorPayload slot: the sub_75c730 handler parses {u8 2}{u8 code} into
// the cat-0x10 sink, but NO break trigger->code pair is pinned for
// Legend (the leave posture; emitting one would invent a contract).
type BreakOutcome struct {
	AckPayload        []byte
	PushPayload       []byte
	MemberNames       []string
	SelectedTargetGid uint32
	Refusal           string
}

func refusedBreak(reason string) BreakOutcome {
	return BreakOutcome{Refusal: reason}
}

// HandleBreak applies one decoded 0x766E request through the ATOMIC
// DissolveGuild door (guild row + whole member set + every member's
// GuildID FK, one commit). The dissolver is ALWAYS the requesting
// session's bound character; the body's u32 is decoded and logged,
// never used for identity (the leave/create posture - sub_7006f0
// writes the same +0x620 selected-target slot). ONLY the grade-0
// LEADER may dissolve: leave's handler refuses leaders pointing here
// (register.go HandleLeave), and the client opens the break confirm
// (msgbox kind 0x13, sub_5da1b0 case 0x14 @0x005daf98's sibling) from
// leadership arms only - a non-leader break is refused silently.
func HandleBreak(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) BreakOutcome {
	if actor == nil {
		return refusedBreak("characterNotFound")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedBreak("characterNotFound")
	}
	if actor.DeletePending {
		return refusedBreak("deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedBreak("no guild store wired")
	}
	request, err := DecodeBreakRequest(payload)
	if err != nil {
		return refusedBreak(err.Error())
	}
	dissolved, refusal := deps.GuildAuthority().DissolveGuildAs(divisionID, actor.ID)
	if refusal.Refused() {
		if refusal == enterworld.GuildRefusalLeaderRequired {
			return refusedBreak("non-leader cannot dissolve the guild")
		}
		return refusedBreak(guildRefusalReason(refusal))
	}

	names := make([]string, 0, len(dissolved.Members))
	for _, member := range dissolved.Members {
		names = append(names, member.Name)
	}
	return BreakOutcome{
		AckPayload:        EncodeBreakAckB66E(),
		PushPayload:       EncodeGuildBreak3B29(),
		MemberNames:       names,
		SelectedTargetGid: request.SelectedTargetGid,
	}
}
