package mentor

import (
	"bytes"
	"opensro.online/server/internal/domain/charactervitals"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/restriction"
	"opensro.online/server/internal/game/social/textguard"
	"opensro.online/server/internal/transport"
)

const OpTCNoticeEditRequest uint16 = 0x7220
const OpTCNoticeEditAck uint16 = 0xB220

func noticeFields(payload []byte) (subject, contents []byte, err error) {
	r := wire.NewReader(payload)
	n, err := r.U16()
	if err != nil {
		return nil, nil, err
	}
	subject, err = r.Bytes(int(n))
	if err != nil {
		return nil, nil, err
	}
	n, err = r.U16()
	if err != nil {
		return nil, nil, err
	}
	contents, err = r.Bytes(int(n))
	if err != nil {
		return nil, nil, err
	}
	return subject, contents, r.Done()
}

func noticeText(raw []byte) string {
	// DB operation 7 receives c_str pointers after validating std::string length.
	if end := bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}
	return wire.DecodeWindows1252(raw)
}

// Reference server: session restriction, master, text filter, life and byte
// limits precede DB operation 7. Client v1.150 uses B220 with a one-byte error;
// research B477/68xx values are never copied directly onto this wire.
func (r *InviteRuntime) handleNoticeEdit(s *transport.Session, _ uint16, payload []byte) {
	actor, division, bound := enterworld.SessionCharacter(r.deps, s)
	if !bound {
		return
	}
	actor = characterSnapshot(r.deps, division, actor)
	if actor == nil || actor.DeletePending {
		return
	}
	if restriction.Report(s, transport.CommandRestrictionChat) {
		return
	}
	fail := func(code byte) { _ = s.Send(OpTCNoticeEditAck, []byte{2, code}) }
	authority := r.deps.TrainingCampAuthority()
	if authority == nil {
		fail(2)
		return
	}
	campID, joined := authority.CampOfCharacter(division, actor.ID)
	camp, members, found := authority.Camp(division, campID)
	master := false
	for _, member := range members {
		if member.CharID == actor.ID && member.Kind == 0 {
			master = true
		}
	}
	if !joined || !found || camp.MasterCharID != actor.ID || !master {
		fail(0x16)
		return
	}
	subject, contents, err := noticeFields(payload)
	if err != nil {
		return
	}
	if textguard.Rejected(subject) || textguard.Rejected(contents) {
		return
	}
	if charactervitals.CurrentHP(actor) == 0 {
		fail(7)
		return
	}
	if len(subject) == 0 || len(subject) > 128 || len(contents) == 0 || len(contents) > 2048 {
		fail(0x17)
		return
	}
	camp, members, ok := authority.UpdateNotice(division, actor.ID, campID, noticeText(subject), noticeText(contents))
	if !ok {
		fail(2)
		return
	}
	// Native acknowledgment and replication have separate callbacks. This
	// single-authority port queues the author's ack before member publication.
	_ = s.Send(OpTCNoticeEditAck, []byte{1})
	w := wire.NewWriter(5 + len(subject) + len(contents))
	w.U8(7)
	writeCampString(w, camp.Subject)
	writeCampString(w, camp.Contents)
	for _, member := range members {
		character := r.findDivisionCharacterByID(division, member.CharID)
		if character == nil {
			continue
		}
		if peer, online := r.presence.SessionByName(division, character.Name); online {
			_ = peer.Send(OpTCStatus, w.Payload())
		}
	}
}
