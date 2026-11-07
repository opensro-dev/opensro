/*
===========================================================================

crestregister.go - the crest register items: stand a guild or union crest

#2282 (register half): ITEM_ETC_GUILD_CREST_USER and
ITEM_ETC_UNION_CREST_USER (type 3/3/12/2; param1 1 guild / 2 union,
param2 1 register) stand a crest on their kind. Natively the image is
a local bitmap the client reads and quantizes itself (4B3900) and
uploads out of band; the browser port has no install folder, so the
owner's 2026-11-07 contract supplies the source instead: the master
picks a BMP, PNG or JPG in the browser and the bytes ride this
runtime's control lane (opCrestRegister) to the crest store, which
alone decides what they are (crest/image.go). The plain item use
still refuses - a bitmap cannot ride the native packet - so this
lane is the register flow, and the item is found in the user's bag
by slot and judged exactly as a use would judge it.

The server's judgement order mirrors the delete half with the
conditions inverted: the guild item wants a guild of level 4 or more
and its master for a user, the union item a guild of level 5 or more
and its master in a guild that leads the union, and each wants NO
crest standing (the native OVERLAP line). Then the owner's rules: one
change per guild per crest.ChangeCooldown, and the image must
re-encode - every refusal before the commit writes nothing and
spends nothing. The commit stands the new revision through the guild
or union door, then the item is spent and the ordinary 0xB5BD
success answers with the 0x34F3 crest update behind it, as the
delete half answers. Every committed change, upload or staff
removal, is audited with the old and new payload hashes
(RecordCrestChange; the operations journal in cmd/services).

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/crest"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/transport"
)

// opCrestRegister is the crest upload control frame (client
// commerce-controls.ts CREST_UPLOAD_CONTROL). The control numbers
// below 256 are allocated one per lane: 18 is the Item Mall silk
// history's (#2758), so the upload lane takes 19.
const opCrestRegister = 19

// The closed refusal reasons a crest upload answer names. The client
// maps each to its native line (crest-register.ts): a crest already
// standing, an image that is not a usable crest, and everything else
// - which a gate's native notice code, when one rides along, names
// more precisely instead.
const (
	crestRefusedOverlap = "overlap"
	crestRefusedInvalid = "invalid"
	crestRefusedGeneric = "refused"
)

// maxCrestEncodedBytes bounds the base64 body before it is decoded:
// the owner's 256 KB image cap, encoded, with the padding's slack.
const maxCrestEncodedBytes = (crest.MaxCrestImageBytes+2)/3*4 + 4

/*
================
crestRegisterRequest

One upload: the crest kind, the bag slot holding the register item,
and the picked image's bytes, base64. The character is the session's
own (hubHandler), never the payload's business.
================
*/
type crestRegisterRequest struct {
	Kind  string `json:"kind"`
	Slot  int    `json:"slot"`
	Image string `json:"image"`
}

/*
================
crestRegisterAnswer

The upload lane's one answer: the new standing revision on success,
or the closed refusal - a reason, and for the guild and union gates
the native item-use notice byte (0xBB level, 0xBC master, 0xBD no
union) the client renders as it renders a refused use.
================
*/
type crestRegisterAnswer struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
	Code   uint8  `json:"code,omitempty"`
	Crest  uint32 `json:"crest,omitempty"`
}

/*
================
CrestChange

One committed crest change, for the audit journal: who stood or
cleared which crest, the revisions and payload hashes it moved
between. Source is "upload" (the register lane, Operator empty) or
"operator" (a staff removal, Operator the credential's name).
================
*/
type CrestChange struct {
	Division, Character, Kind string
	GuildID, AllianceID       int64
	OldRevision, NewRevision  uint32
	OldHash, NewHash          string
	Source, Operator          string
}

/*
================
registerCrest
================
*/
func (rt *Runtime) registerCrest(hub *transport.Hub) {
	hub.Handle(opCrestRegister, rt.hubHandler(hub, rt.HandleCrestRegister))
}

/*
================
crestRegisterRefusal

A refused upload's whole answer: the refusal frame alone, with the
diagnostic the hub logs.
================
*/
func crestRegisterRefusal(diagnostic, reason string, code uint8) OpResult {
	payload, err := json.Marshal(crestRegisterAnswer{Reason: reason, Code: code})
	if err != nil {
		return OpResult{DiagnosticRefusal: diagnostic}
	}
	return OpResult{
		Frames:            []wire.Frame{{Opcode: opCrestRegister, Payload: payload}},
		DiagnosticRefusal: diagnostic,
	}
}

/*
================
HandleCrestRegister

The session character's crest upload, judged end to end (see the
file banner for the order). Every refusal answers on the upload lane
and writes nothing; a committed change answers with the success
burst a delete answers with, plus the lane's own ok.
================
*/
func (rt *Runtime) HandleCrestRegister(division string, character *enterworld.Character, payload []byte) OpResult {
	if character == nil || rt.Crests == nil {
		return crestRegisterRefusal("crest register: no crest store wired", crestRefusedGeneric, 0)
	}
	var request crestRegisterRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.More() {
		return crestRegisterRefusal("crest register: malformed upload", crestRefusedGeneric, 0)
	}
	kind := request.Kind
	if kind != crest.KindGuild && kind != crest.KindUnion {
		return crestRegisterRefusal("crest register: unknown crest kind", crestRefusedGeneric, 0)
	}
	if request.Slot < 0 || request.Slot > 255 {
		return crestRegisterRefusal("crest register: slot out of range", crestRefusedGeneric, 0)
	}

	unlock := rt.lockDivision(division)
	defer unlock()

	// The item is judged as a use judges it: the session character's
	// own stored row at the slot, the reference the catalog makes of
	// it, alive, in a guild. Nothing here trusts the payload past the
	// slot it names.
	var snapshot *domain.Character
	alive := false
	rt.deps.Read(division, func() {
		snapshot = character.Snapshot()
		alive = enterworld.CharacterAlive(character)
	})
	if snapshot == nil || snapshot.DeletePending || !alive || snapshot.GuildID == nil {
		return crestRegisterRefusal("crest register: user cannot register a crest", crestRefusedGeneric, 0)
	}
	var row enterworld.InventoryRow
	rowFound := false
	for _, candidate := range snapshot.MissionInventory {
		if candidate.Slot == int64(request.Slot) {
			row, rowFound = candidate, true
			break
		}
	}
	if !rowFound || row.StackCount < 1 {
		return crestRegisterRefusal("crest register: no register item at the slot", crestRefusedGeneric, 0)
	}
	ref, ok := rt.deps.ItemReferences().ItemRefByCodename(row.Codename)
	if !ok || ref == nil || ref.RefObjID != row.RefObjID || ref.TypeFlags() != row.TypeFlags ||
		ref.TypeIDs != [4]int64{3, 3, 12, 2} {
		return crestRegisterRefusal("crest register: the slot holds no crest item", crestRefusedGeneric, 0)
	}
	param1, _ := ref.NativeFields.Lookup("itemParam1_29c")
	param2, _ := ref.NativeFields.Lookup("itemParam2_2a0")
	wantKind := ""
	switch param1 {
	case 1:
		wantKind = crest.KindGuild
	case 2:
		wantKind = crest.KindUnion
	}
	if param2 != 1 || wantKind == "" || wantKind != kind {
		return crestRegisterRefusal("crest register: the item registers another crest", crestRefusedGeneric, 0)
	}

	// The gates, in the delete half's order with the standing-crest
	// judgement inverted, probed read-only before the image is read:
	// level, master, then the crest the lane names must be standing
	// free - the native OVERLAP condition.
	minLevel := uint8(crestDeleteGuildMinLevel)
	if kind == crest.KindUnion {
		minLevel = crestDeleteUnionMinLevel
	}
	code, probedRevision, guildID := rt.probeCrestRegisterGate(division, character, minLevel)
	if code != 0 {
		return crestRegisterRefusal("crest register: refused at the guild gate", crestRefusedGeneric, code)
	}
	crestID := guildID
	var allianceID int64
	if kind == crest.KindUnion {
		if rt.Unions == nil {
			return crestRegisterRefusal("crest register: no union authority wired", crestRefusedGeneric, 0)
		}
		record, refusal := rt.Unions.CrestState(division, guildID)
		switch refusal {
		case union.CrestRefusalNone:
		case union.CrestRefusalNoUnion:
			return crestRegisterRefusal("crest register: no union to crest", crestRefusedGeneric, wire.ErrCodeCrestNoUnion)
		case union.CrestRefusalNotMasterGuild:
			return crestRegisterRefusal("crest register: the guild does not lead its union", crestRefusedGeneric, wire.ErrCodeCrestNotMaster)
		default:
			return crestRegisterRefusal("crest register: refused at the union gate", crestRefusedGeneric, 0)
		}
		probedRevision = record.Crest
		allianceID = record.AllianceID
		crestID = allianceID
	}
	if probedRevision != 0 {
		return crestRegisterRefusal("crest register: a crest already stands", crestRefusedOverlap, 0)
	}

	// The owner's rate limit, judged before the image is decoded:
	// one change per guild (per union) per cooldown.
	now := rt.Now()
	if !rt.Crests.AllowChange(kind, crestID, now) {
		return crestRegisterRefusal("crest register: changed too recently", crestRefusedGeneric, 0)
	}

	// The image: the size cap rides before the decode, and the
	// re-encode judges everything else about the bytes. Past here
	// the only artifact is 256 canonical bytes and their hash.
	if len(request.Image) > maxCrestEncodedBytes {
		return crestRegisterRefusal("crest register: image over the size cap", crestRefusedInvalid, 0)
	}
	raw, err := base64.StdEncoding.DecodeString(request.Image)
	if err != nil || len(raw) > crest.MaxCrestImageBytes {
		return crestRegisterRefusal("crest register: image does not decode", crestRefusedInvalid, 0)
	}
	payload256, err := crest.Reencode(raw)
	if err != nil {
		return crestRegisterRefusal("crest register: not a usable crest image", crestRefusedInvalid, 0)
	}
	hash, err := rt.Crests.Save(payload256)
	if err != nil {
		log.WithError(err).Errorf("crest register: storing the crest of %q failed", character.Name)
		return crestRegisterRefusal("crest register: storing the crest failed", crestRefusedGeneric, 0)
	}
	oldHash := ""
	if old, ok := rt.Crests.Current(kind, crestID); ok {
		oldHash = old.Hash
	}
	newRevision := rt.Crests.NextRevision(kind, crestID, probedRevision)

	// The commit: the crest stands through its own door, re-judged
	// against the probed revision, before anything is spent.
	var update wire.Frame
	var audience []domain.GuildMemberRecord
	committed := false
	switch kind {
	case crest.KindGuild:
		update, audience, committed = rt.commitGuildCrest(division, character, probedRevision, newRevision)
	case crest.KindUnion:
		update, audience, committed = rt.commitUnionCrest(division, character, guildID, probedRevision, newRevision)
	}
	if !committed {
		return crestRegisterRefusal("crest register: refused at the commit", crestRefusedGeneric, 0)
	}
	// The index is written last: only a committed crest ever
	// resolves by name, so a failed write here leaves the revision
	// artless (an honest 404) rather than lending it another's art.
	if err := rt.Crests.SetCurrent(kind, crestID, crest.Ref{Revision: newRevision, Hash: hash}); err != nil {
		log.WithError(err).Errorf("crest register: indexing the crest of %q failed", character.Name)
		return crestRegisterRefusal("crest register: indexing the crest failed", crestRefusedGeneric, 0)
	}

	result := OpResult{}
	spent := rt.deps.Update(character, "crest-register", func() bool {
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(request.Slot) &&
				character.MissionInventory[index].Codename == ref.Codename {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		remaining := rt.consumeItemUseRow(character, rowIndex)
		answer, err := json.Marshal(crestRegisterAnswer{OK: true, Crest: newRevision})
		if err != nil {
			return false
		}
		result = OpResult{Frames: []wire.Frame{
			{Opcode: opCrestRegister, Payload: answer},
			{Opcode: wire.OpItemUseResponse,
				Payload: wire.EncodeItemUseSuccess(uint8(request.Slot), remaining, ref.TypeFlags())},
			update,
		}}
		result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
		return true
	})
	if !spent {
		// The delete half's loud shape: the crest stands (its door
		// committed) but the item that bought it is gone, so the
		// answer refuses rather than pretend a cleaner story.
		log.Warnf("action: crest register for %q in %s stood but its item row is gone; left unspent", character.Name, division)
		return crestRegisterRefusal("crest register: the register item row vanished before it could be spent", crestRefusedGeneric, 0)
	}
	rt.Crests.MarkChange(kind, crestID, now)
	rt.publishCrestUpdate(division, character, audience, update, &result)
	rt.publishItemUseVisual(character, ref, 0, &result)
	rt.auditCrestChange(CrestChange{Division: division, Character: character.Name, Kind: kind,
		GuildID: guildID, AllianceID: allianceID, NewRevision: newRevision,
		OldHash: oldHash, NewHash: hash, Source: "upload"})
	return result
}

/*
================
probeCrestRegisterGate

The register lane's guild gates, judged read-only inside the guild
door (the union arm's pre-commit and the guild arm's probe alike):
the first gate's wire code or 0, the crest revision the record
currently stands, and the guild's id. A guild the door will not
open for this user reads as the generic code - the no-guild refusal.
================
*/
func (rt *Runtime) probeCrestRegisterGate(divisionID string, character *enterworld.Character, minLevel uint8) (uint8, uint32, int64) {
	authority := rt.deps.GuildAuthority()
	if authority == nil {
		return wire.ErrCodeInvalidRequest, 0, 0
	}
	code := wire.ErrCodeInvalidRequest
	probed := false
	var revision uint32
	var guildID int64
	authority.UpdateGuildAs(divisionID, character.ID, "crest-register",
		domain.GuildAuthorization{},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			probed = true
			code = crestGateCode(record, members, character.ID, minLevel)
			revision = record.CrestParam
			guildID = record.ID
			return record, members, false
		})
	if !probed {
		return wire.ErrCodeInvalidRequest, 0, 0
	}
	return code, revision, guildID
}

/*
================
commitGuildCrest

Stand the guild's new crest revision through its door: the gates
re-judged, and the write landing only while the record still shows
the probed revision (expected) - a change that landed meanwhile
refuses rather than trading arts. On success the 0x34F3 guild row
and the committed member list come back.
================
*/
func (rt *Runtime) commitGuildCrest(divisionID string, character *enterworld.Character, expected, revision uint32) (wire.Frame, []domain.GuildMemberRecord, bool) {
	authority := rt.deps.GuildAuthority()
	if authority == nil {
		return wire.Frame{}, nil, false
	}
	committed := false
	snapshot, refusal := authority.UpdateGuildAs(divisionID, character.ID, "crest-register",
		domain.GuildAuthorization{},
		func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			if crestGateCode(record, members, character.ID, crestDeleteGuildMinLevel) != 0 {
				return record, members, false
			}
			if record.CrestParam != expected {
				return record, members, false
			}
			record.CrestParam = revision
			committed = true
			return record, members, true
		})
	if refusal.Refused() || !committed {
		return wire.Frame{}, nil, false
	}
	update := wire.Frame{Opcode: guild.OpCrestUpdate,
		Payload: guild.EncodeGuildCrestUpdate(uint32(snapshot.Guild.ID), snapshot.Guild.Name, revision)}
	return update, snapshot.Members, true
}

/*
================
commitUnionCrest

Stand the union's new crest revision through its door (SetCrest
re-judges leadership and the probed revision under its own lock).
On success the 0x34F3 union row and the member list of every guild
in the union come back.
================
*/
func (rt *Runtime) commitUnionCrest(divisionID string, character *enterworld.Character, guildID int64, expected, revision uint32) (wire.Frame, []domain.GuildMemberRecord, bool) {
	record, refusal, err := rt.Unions.SetCrest(divisionID, guildID, expected, revision)
	if err != nil || refusal != union.CrestRefusalNone {
		return wire.Frame{}, nil, false
	}
	names := []string{}
	members := []domain.GuildMemberRecord{}
	if authority := rt.deps.GuildAuthority(); authority != nil {
		for _, memberGuildID := range record.Guilds {
			if memberGuildID == 0 {
				continue
			}
			if guildRecord, guildMembers, ok := authority.Guild(divisionID, memberGuildID); ok {
				names = append(names, guildRecord.Name)
				members = append(members, guildMembers...)
			}
		}
	}
	update := wire.Frame{Opcode: guild.OpCrestUpdate,
		Payload: guild.EncodeAllianceCrestUpdate(uint32(record.AllianceID), names, revision)}
	return update, members, true
}

/*
================
auditCrestChange

Journal one committed crest change and note it in the server log:
character, guild, old hash, new hash (the owner's audit row).
================
*/
func (rt *Runtime) auditCrestChange(change CrestChange) {
	log.WithFields(log.Fields{
		"character": change.Character, "kind": change.Kind, "guild": change.GuildID,
		"alliance": change.AllianceID, "oldHash": change.OldHash, "newHash": change.NewHash,
		"revision": change.NewRevision, "source": change.Source, "operator": change.Operator,
	}).Info("crest: changed")
	if rt.RecordCrestChange != nil {
		rt.RecordCrestChange(change)
	}
}

/*
================
OperatorRemoveCrest

The staff crest removal (#2282, owner contract): clear the named
character's guild crest - or the crest of the union that guild
stands in - reverting it to none, from the console. The operator
credential is the authority: no mastership is judged, only that the
crest stands. The clear commits through the guild or union door,
the store's standing pointer drops, the 0x34F3 update goes to the
division, and the removal is audited like an upload.
================
*/
func (rt *Runtime) OperatorRemoveCrest(division, operator, name, kind string) error {
	if rt.Crests == nil {
		return fmt.Errorf("crest removal unavailable")
	}
	if kind != crest.KindGuild && kind != crest.KindUnion {
		return fmt.Errorf("crest removal names a guild or union crest")
	}
	var found *domain.Character
	rt.deps.Read(division, func() {
		if candidate := rt.findCharacter(division, name); candidate != nil {
			found = candidate.Snapshot()
		}
	})
	if found == nil || found.DeletePending {
		return fmt.Errorf("character not found")
	}
	if found.GuildID == nil {
		return fmt.Errorf("character stands in no guild")
	}
	guildID := *found.GuildID

	var update wire.Frame
	var audience []domain.GuildMemberRecord
	crestID := guildID
	var allianceID int64
	var oldRevision uint32
	switch kind {
	case crest.KindGuild:
		authority := rt.deps.GuildAuthority()
		if authority == nil {
			return fmt.Errorf("crest removal unavailable")
		}
		record, members, ok := authority.Guild(division, guildID)
		if !ok {
			return fmt.Errorf("guild not found")
		}
		if record.CrestParam == 0 {
			return fmt.Errorf("no guild crest stands")
		}
		masterID := int64(0)
		for _, member := range members {
			if member.Grade == domain.GuildLeaderGrade {
				masterID = member.CharID
				break
			}
		}
		if masterID == 0 {
			return fmt.Errorf("the guild has no master to clear through")
		}
		oldRevision = record.CrestParam
		cleared := false
		_, refusal := authority.UpdateGuildAs(division, masterID, "crest-remove",
			domain.GuildAuthorization{},
			func(record domain.GuildRecord, members []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
				if record.CrestParam == 0 {
					return record, members, false
				}
				record.CrestParam = 0
				cleared = true
				return record, members, true
			})
		if refusal.Refused() || !cleared {
			return fmt.Errorf("guild crest removal refused")
		}
		update = wire.Frame{Opcode: guild.OpCrestUpdate,
			Payload: guild.EncodeGuildCrestUpdate(uint32(guildID), record.Name, 0)}
		audience = members
	case crest.KindUnion:
		if rt.Unions == nil {
			return fmt.Errorf("crest removal unavailable")
		}
		state, stateRefusal := rt.Unions.CrestState(division, guildID)
		if stateRefusal == union.CrestRefusalNoUnion {
			return fmt.Errorf("the guild stands in no union")
		}
		allianceID = state.AllianceID
		crestID = allianceID
		record, refusal, err := rt.Unions.RemoveCrest(division, guildID)
		if err != nil {
			return err
		}
		switch refusal {
		case union.CrestRefusalNone:
		case union.CrestRefusalNoUnion:
			return fmt.Errorf("the guild stands in no union")
		default:
			return fmt.Errorf("no union crest stands")
		}
		oldRevision = state.Crest
		names := []string{}
		if authority := rt.deps.GuildAuthority(); authority != nil {
			for _, memberGuildID := range record.Guilds {
				if memberGuildID == 0 {
					continue
				}
				if guildRecord, guildMembers, ok := authority.Guild(division, memberGuildID); ok {
					names = append(names, guildRecord.Name)
					audience = append(audience, guildMembers...)
				}
			}
		}
		update = wire.Frame{Opcode: guild.OpCrestUpdate,
			Payload: guild.EncodeAllianceCrestUpdate(uint32(record.AllianceID), names, 0)}
	}

	oldHash := ""
	if old, ok := rt.Crests.Current(kind, crestID); ok {
		oldHash = old.Hash
		if oldRevision == 0 {
			oldRevision = old.Revision
		}
	}
	if err := rt.Crests.RemoveCurrent(kind, crestID); err != nil {
		return err
	}
	rt.auditCrestChange(CrestChange{Division: division, Character: found.Name, Kind: kind,
		GuildID: guildID, AllianceID: allianceID, OldRevision: oldRevision,
		OldHash: oldHash, Source: "operator", Operator: operator})
	if rt.PushShardPeerFrames != nil {
		rt.PushShardPeerFrames(division, "", []wire.Frame{update})
	} else if rt.PushCharacterFrames != nil {
		for _, member := range audience {
			if member.Name != "" {
				rt.PushCharacterFrames(division, member.Name, []wire.Frame{update})
			}
		}
	}
	return nil
}
