package guild

import (
	"fmt"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/social/textguard"
)

// NoticeRefusal preserves the decision independently of diagnostic wording.
// Unsupported prerequisites have no fabricated native error code.
type NoticeRefusal uint8

const (
	NoticeAccepted NoticeRefusal = iota
	NoticeActorUnavailable
	NoticeStoreUnavailable
	NoticeMalformed
	NoticeTextRejected
	NoticeSubjectClientLimit
	NoticeBodyClientLimit
	NoticeEmptySubject
	NoticeEmptyBody
	NoticePermissionDenied
	NoticeNotMember
	NoticeAuthorityRejected
)

type NoticeEditOutcome struct {
	AckPayload   []byte
	ErrorPayload []byte
	PushPayload  []byte
	MemberNames  []string
	Reason       NoticeRefusal
	Refusal      string
}

// NoticeRefusalPayload is the one refusal writer. The native guild notice step
// distinguishes missing guild (0D, silent in the v1.150 dispatcher) from
// permission failure (1E). Client 75C9A9 supplies category 10, not a wire byte.
func NoticeRefusalPayload(reason NoticeRefusal) []byte {
	switch reason {
	case NoticeEmptySubject:
		return EncodeGuildErrorResult(GuildErrInvalidMasterCommentTitle)
	case NoticeEmptyBody:
		return EncodeGuildErrorResult(GuildErrInvalidMasterComment)
	case NoticePermissionDenied:
		return EncodeGuildErrorResult(GuildErrPermissionDenied)
	case NoticeNotMember:
		return EncodeGuildErrorResult(GuildErrNotMember)
	default:
		return nil
	}
}
func refusedNoticeEdit(kind NoticeRefusal, detail string) NoticeEditOutcome {
	return NoticeEditOutcome{Reason: kind, Refusal: detail, ErrorPayload: NoticeRefusalPayload(kind)}
}

func HandleNoticeEdit(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte) NoticeEditOutcome {
	if actor == nil {
		return refusedNoticeEdit(NoticeActorUnavailable, "characterNotFound")
	}
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedNoticeEdit(NoticeActorUnavailable, "characterNotFound")
	}
	if actor.DeletePending {
		return refusedNoticeEdit(NoticeActorUnavailable, "deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedNoticeEdit(NoticeStoreUnavailable, "no guild store wired")
	}
	request, err := DecodeNoticeEditRequest(payload)
	if err != nil {
		return refusedNoticeEdit(NoticeMalformed, err.Error())
	}

	// The native notice edit rejects either encoded field before the guild permission check.
	// DecodeNoticeEditRequest preserves raw ANSI bytes in these strings.
	if textguard.Rejected([]byte(request.Subject)) || textguard.Rejected([]byte(request.Contents)) {
		return refusedNoticeEdit(NoticeTextRejected, "notice text rejected")
	}

	var rejected NoticeEditOutcome
	snapshot, refusal := deps.GuildAuthority().UpdateGuildAs(
		divisionID, actor.ID, "guild-notice-edit",
		enterworld.GuildAuthorization{RequiredPermission: PermMaskNoticeEdit},
		func(record enterworld.GuildRecord, members []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			// Natively authorization precedes text validation. Keep both decisions
			// inside the same authority transaction, so membership cannot race them.
			switch {
			case request.Subject == "":
				rejected = refusedNoticeEdit(NoticeEmptySubject, "empty notice subject")
			case request.Contents == "":
				rejected = refusedNoticeEdit(NoticeEmptyBody, "empty notice contents")
			case len(request.Subject) > NoticeSubjectMaxBytes:
				rejected = refusedNoticeEdit(NoticeSubjectClientLimit, fmt.Sprintf("subject %d bytes exceeds the client edit cap %d", len(request.Subject), NoticeSubjectMaxBytes))
			case len(request.Contents) > NoticeContentsMaxBytes:
				rejected = refusedNoticeEdit(NoticeBodyClientLimit, fmt.Sprintf("contents %d bytes exceeds the client edit cap %d", len(request.Contents), NoticeContentsMaxBytes))
			default:
				record.NoticeSubject = request.Subject
				record.NoticeContents = request.Contents
				return record, members, true
			}
			return record, members, false
		},
	)
	if refusal.Refused() {
		switch refusal {
		case enterworld.GuildRefusalPermissionDenied:
			return refusedNoticeEdit(NoticePermissionDenied, "permMask lacks the notice-edit bit")
		case enterworld.GuildRefusalNotMember:
			return refusedNoticeEdit(NoticeNotMember, guildRefusalReason(refusal))
		case enterworld.GuildRefusalUpdateRejected:
			if rejected.Reason != NoticeAccepted {
				return rejected
			}
		}
		return refusedNoticeEdit(NoticeAuthorityRejected, guildRefusalReason(refusal))
	}
	names := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		names = append(names, member.Name)
	}
	return NoticeEditOutcome{AckPayload: EncodeNoticeEditAckB77A(), PushPayload: EncodeNoticeUpdate3B29(request.Subject, request.Contents), MemberNames: names}
}
