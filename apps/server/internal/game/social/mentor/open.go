/*
===========================================================================

open.go - opening an Academy at the storage NPC (0x72EC -> 0xB2EC)

The v1.150 client lists "Academy Establishment" (action 0x2D) under an NPC
with capability bit 4 - the storage keepers - when the player is level 60
or more and holds no camp (CIFNPCTalk_BuildMenuFromCapabilities).
The row raises a confirm box naming the 100,000 gold fee
(CIFNpcTalk_ShowJobGuildMsgBox case 6); its Yes checks the gold
itself and sends 0x72EC (CIFNpcTalk_OnSimpleMsgBoxResult case 6 ->
CGInterface_SendTrainingCampOpen72EC). The answer 0xB2EC
(CPSMission_OnTrainingCampOpenResponse0xB2EC) is [1] and the new
camp, read by the same CTrainingCampData_ReadCampRecord record
reader the 0x3AC5 status-10 seed uses, or [2] and one
category-0x1D code.

The server rule is the retail server's, whose academy opcodes are
renumbered:
it refuses a dead master (0x6807), a level under 60 (0x6809), gold
under 100,000 (0x680A) and a master already in a camp (0x6812); its database
success burns the fee (-0x186A0) and answers [1] with the camp. The store
door applies the checks, the debit and the new rows in one commit.

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

// OpTCOpenRequest is the C->S camp opening
// (CGInterface_SendTrainingCampOpen72EC). Its one u32 is the confirm
// box's own UI id: CIFNpcTalk_OnSimpleMsgBoxResult pushes its
// wParam, which the client assert pins to m_dwSimpleMsgBoxID (+0x7CC,
// set from the box's +0x908 in CIFNpcTalk_ShowJobGuildMsgBox). It
// names no NPC or camp, so the server reads and ignores it.
const OpTCOpenRequest uint16 = 0x72EC

// OpTCOpenAnswer is the S->C 0xB2EC
// (CPSMission_OnTrainingCampOpenResponse0xB2EC).
const OpTCOpenAnswer uint16 = 0xB2EC

// CampOpenFee is the gold an opening burns: the confirm text's 100,000,
// the client's own check in CIFNpcTalk_OnSimpleMsgBoxResult
// (0x186A0) and the server's debit.
const CampOpenFee = 100000

// The category-0x1D codes an opening answers with (the server's
// answer table, base 3): the 0x68xx returns keep their low byte.
const (
	OpenErrDead       uint8 = 0x07 // UIIT_MSG_TC_ERROR_DIE_PASSING
	OpenErrLevel      uint8 = 0x09 // UIIT_MSG_TC_ERROR_OPEN_LEVEL
	OpenErrGold       uint8 = 0x0A // UIIT_MSG_TC_ERROR_OPEN_GOLD
	OpenErrInCamp     uint8 = 0x12 // UIIT_MSG_TC_ERROR_OTHER_TRAININGCAMP_ENTRY
	OpenErrReopenWait uint8 = 0x1C // UIIT_MSG_TC_ERROR_NOT_OPEN_7D
)

// openRefusalCodes maps the store's refusals onto the wire codes. A refusal
// missing here (an unavailable character) stays silent.
var openRefusalCodes = map[domain.TrainingCampOpenRefusal]uint8{
	domain.TrainingCampOpenDead:   OpenErrDead,
	domain.TrainingCampOpenLevel:  OpenErrLevel,
	domain.TrainingCampOpenGold:   OpenErrGold,
	domain.TrainingCampOpenInCamp: OpenErrInCamp,
}

/*
================
EncodeOpenAckB2EC

The success answer: {u8 1} and the camp record
CTrainingCampData_ReadCampRecord reads - the same
body as the 0x3AC5 status-10 seed after its two status bytes.
================
*/
func EncodeOpenAckB2EC(localMemberID uint32, subject, contents string, rows []MemberWireRow) []byte {
	writer := wire.NewWriter(128)
	writer.U8(1)
	writeCampRecord(writer, localMemberID, subject, contents, rows)
	return writer.Payload()
}

/*
================
EncodeOpenErrorB2EC

The refusal answer: {u8 2}{u8 code}.
================
*/
func EncodeOpenErrorB2EC(code uint8) []byte {
	return wire.NewWriter(2).U8(2).U8(code).Payload()
}

/*
================
EncodeOpenReopenWaitB2EC

The 7-day reopen cooldown refusal
(CPSMission_OnTrainingCampOpenResponse0xB2EC): {u8 2}{u8 0x1C}
{u32 remainingSeconds}.
================
*/
func EncodeOpenReopenWaitB2EC(remainingSeconds uint32) []byte {
	return wire.NewWriter(6).U8(2).U8(OpenErrReopenWait).U32(remainingSeconds).Payload()
}

/*
================
handleOpen

One 0x72EC. The NPC is not checked: the server keeps no NPC-interaction
plane (the guild create lane's posture) and the request names none.
================
*/
func (r *InviteRuntime) handleOpen(s *transport.Session, opcode uint16, payload []byte) {
	actor, divisionID, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		log.Debugf("mentor: 0x%04X (open) from unbound session %d discarded", opcode, s.ID)
		return
	}
	actor = characterSnapshot(r.deps, divisionID, actor)
	if actor == nil || actor.DeletePending {
		return
	}
	reader := wire.NewReader(payload)
	if _, err := reader.U32(); err != nil || reader.Done() != nil {
		log.Debugf("mentor: 0x72EC (open) malformed from %s", actor.Name)
		return
	}
	if r.deps.TrainingCampAuthority() == nil {
		return
	}
	opened, refusal := r.deps.TrainingCampAuthority().OpenCamp(divisionID, domain.TrainingCampOpening{
		MasterCharID: actor.ID,
		MinLevel:     MasterMinLevel,
		Fee:          CampOpenFee,
		NowMs:        r.Now().UnixMilli(),
	})
	if refusal != domain.TrainingCampOpenAccepted {
		log.Debugf("mentor: 0x72EC (open) refused for %s: refusal %d", actor.Name, refusal)
		if refusal == domain.TrainingCampOpenReopenWait {
			_ = s.Send(OpTCOpenAnswer, EncodeOpenReopenWaitB2EC(opened.RemainingSeconds))
			return
		}
		if code, answered := openRefusalCodes[refusal]; answered {
			_ = s.Send(OpTCOpenAnswer, EncodeOpenErrorB2EC(code))
		}
		return
	}
	rows := r.campWireRows(divisionID, opened.Members)
	_ = s.Send(OpTCOpenAnswer, EncodeOpenAckB2EC(enterworld.ObjectIDForCharacter(actor), opened.Camp.Subject,
		opened.Camp.Contents, rows))
	_ = s.Send(wire.OpPointsUpdate, wire.GoldRefresh{Balance: uint64(max(opened.Gold, 0))}.Encode())
	log.Debugf("mentor: %s opened an academy (camp %d) for %d gold", actor.Name, opened.Camp.ID, CampOpenFee)
}
