package guild

import (
	"fmt"

	"opensro.online/server/internal/game/enterworld"
)

// CreateOutcome is one handled 0x7663 request. AckPayload is the 0xB663
// result-1 body for the ACTOR ONLY; a refusal leaves it nil. On the
// EVIDENCED name-length refusal arms ErrorPayload carries the 0xB663
// {u8 2}{u8 code} answer (errors.go pins the evidence); every other
// refusal leaves both payloads nil and the wire silent - the unpinned
// trigger/code pairs stay unimplemented rather than invented.
type CreateOutcome struct {
	AckPayload        []byte
	ErrorPayload      []byte
	GuildID           int64
	SelectedTargetGid uint32
	Refusal           string
}

func refusedCreate(reason string) CreateOutcome {
	return CreateOutcome{Refusal: reason}
}

// refusedCreateCode is a refusal that ANSWERS: 0xB663 {u8 2}{u8 code}.
// Only arms whose trigger/code pair is pinned in errors.go take it.
func refusedCreateCode(reason string, code byte) CreateOutcome {
	return CreateOutcome{Refusal: reason, ErrorPayload: EncodeGuildErrorResult(code)}
}

// HandleCreate applies one decoded 0x7663 request through the ATOMIC
// CreateGuild door (guild row + leader member row + GuildID FK +
// watermark, one commit). Refusals: already in a guild, empty name, a
// name over GuildNameMaxBytes, and the door's case-insensitive name
// conflict (DECISION - the CreateCharacter EqualFold precedent; no
// native pin on guild-name collation exists). The two NAME-LENGTH
// refusals answer 0xB663 {2, GuildErrInvalidGuildNameLen} - the pinned
// v1.188 0x4C18 emit covers both the empty and the over-cap name
// (errors.go); every other refusal stays silent. The selected NPC gid is
// decoded and surfaced for the log only: no NPC-interaction plane
// exists server-side, so there is nothing honest to validate it
// against.
//
// The initial guild and leader-member values are NOT EVIDENCED - no
// retail create answer was ever captured - so they are documented
// DECISIONS: guild level 1, GP 0, empty notice subject and contents,
// crestParam 0, byte10 0; leader grade 0 (grade 0 = leader-name publish
// per the sub_826610 fold), level = the character's persisted level,
// donatedGP 0, permMask LeaderPermMask (full permissions for the
// founder), dwords 0, empty grantName, refObjID = the character's model
// ref (the same CharacterModelRef chain the letter lane stamps on
// outgoing mail), JID = GuildJID(character.ID) - consistent with the
// friend lane's FriendJID projection.
//
// The leader's fortressRole is the commander's (1), evidenced: the retail
// _Guild_FnAddMember sets SiegeAuthority 1 for MemberClass 0, and the
// master's handover (v1.188 5C46E0, _Guild_Delegate_Master) moves it with
// the grade. The client's fortress windows gate on that role (827DB0)
// while the server gates on the grade (CGuild_IsMemberMaster 5D0F50); the
// two agree only while the leader holds it.
func HandleCreate(deps Dependencies, divisionID string, actor *enterworld.Character, payload []byte, online func(name string) bool) CreateOutcome {
	if actor == nil {
		return refusedCreate("characterNotFound")
	}
	liveActor := actor
	actor = characterSnapshot(deps, divisionID, actor)
	if actor == nil {
		return refusedCreate("characterNotFound")
	}
	if actor.DeletePending {
		return refusedCreate("deletePending")
	}
	if deps.GuildAuthority() == nil {
		return refusedCreate("no guild store wired")
	}
	request, err := DecodeCreateRequest(payload)
	if err != nil {
		return refusedCreate(err.Error())
	}
	if actor.GuildID != nil {
		return refusedCreate(fmt.Sprintf("already in guild %d", *actor.GuildID))
	}
	if request.Name == "" {
		return refusedCreateCode("guild name empty", GuildErrInvalidGuildNameLen)
	}
	if len(request.Name) > GuildNameMaxBytes {
		return refusedCreateCode(fmt.Sprintf("guild name %d bytes exceeds the %d cap", len(request.Name), GuildNameMaxBytes), GuildErrInvalidGuildNameLen)
	}

	record := enterworld.GuildRecord{
		Name:           request.Name,
		Level:          1,
		GP:             0,
		NoticeSubject:  "",
		NoticeContents: "",
		CrestParam:     0,
		Byte10:         0,
	}
	leader := enterworld.GuildMemberRecord{
		CharID:       actor.ID,
		JID:          GuildJID(actor.ID),
		Name:         actor.Name,
		Grade:        LeaderGrade,
		Level:        memberLevel(actor),
		DonatedGP:    0,
		PermMask:     LeaderPermMask,
		GrantName:    "",
		RefObjID:     deps.CharacterModelRef(actor),
		FortressRole: FortressRoleCommander,
	}
	id, err := deps.GuildAuthority().CreateGuild(divisionID, record, leader, liveActor)
	if err != nil {
		return refusedCreate(err.Error())
	}
	record.ID = id
	return CreateOutcome{
		AckPayload:        EncodeCreateAckB663(record, []enterworld.GuildMemberRecord{leader}, online),
		GuildID:           id,
		SelectedTargetGid: request.SelectedTargetGid,
	}
}
