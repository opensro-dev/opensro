/*
===========================================================================

petname.go - naming a summoned pet, once

The COS info window's Name button sends 0x7571 [u32 cos gid][u16 len]
[ascii name] (v1.150 6A5570; 0x7117 on the reference server). The server
admits it as native:

  - the name is 1..12 bytes, or 0xB571 error 5;
  - it passes the character-name rules (the game's character-name check,
    the name filter included: namefilter.go), or error 0x440E, the
    client's UIIT_MSG_COSPETERR_PETNAME_NOTPUT;
  - the gid is one of the caller's summoned records, or error 3, and that
    record has no name yet, or error 5: a pet is named once and never
    renamed;
  - the name is no other pet's, or error 0x440F, the client's
    UIIT_MSG_COSPETERR_PETNAME_SUMENESS (the DB job's 0xFFFFFFFE answer,
    the reference server's database result case 9).

A failure is 0xB571 [u8 2][u16 code].
Success sends no 0xB571: the reference server's pet name apply
stores the name and sends 0x3508 [u32 cos gid][u8 5][u16 len][ascii name]
to the owner and around the pet, which is the answer.

INFERENCE: v1.188 checks the name against its pet-name table in the
database. The port checks every pet record a character of the division
holds (summoned, in the bag, mercenaries) under the division lock, which
serializes every naming; a pet left in an account warehouse is not held in
memory and is not compared. Names are display text only, so that gap can
at worst show two pets one name.

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const (
	opPetNameRequest  uint16 = 0x7571
	opPetNameResponse uint16 = 0xB571
	// cosInfoName is 0x3508's kind 5, the pet's new name.
	cosInfoName uint8 = 5

	// petNameMaxBytes is the native 0xC.
	petNameMaxBytes = 12

	// 0xB571 error codes.
	petNameErrNotSummoned uint16 = 3
	petNameErrRefused     uint16 = 5
	petNameErrNotAllowed  uint16 = 0x440e
	petNameErrTaken       uint16 = 0x440f
)

/*
================
petNameRefusal
================
*/
func petNameRefusal(code uint16) OpResult {
	return OpResult{Frames: []wire.Frame{{Opcode: opPetNameResponse, Payload: wire.NewWriter(3).U8(2).U16(code).Payload()}}}
}

/*
================
petNameFrame

0x3508 kind 5: a pet's new name.
================
*/
func petNameFrame(gid uint32, name string) wire.Frame {
	return wire.Frame{Opcode: opCosInfoUpdate, Payload: wire.NewWriter(7 + len(name)).U32(gid).U8(cosInfoName).Str(name).Payload()}
}

/*
================
summonedRecords

The caller's summoned pet records carrying gid: a summoned pet can be both
the active record and the summoner item's copy, and both keep its name.
================
*/
func summonedRecords(c *enterworld.Character, gid uint32) []*enterworld.CharacterCOS {
	var records []*enterworld.CharacterCOS
	for _, pet := range c.Companions() {
		if pet != nil && pet.GID == gid && pet.Summoned {
			records = append(records, pet)
		}
	}
	return records
}

/*
================
petNameTaken

Whether any pet record of the division already carries name, ASCII case
ignored as character names are.
================
*/
func (rt *Runtime) petNameTaken(division, name string) bool {
	for _, other := range rt.deps.CharactersForDivision(division) {
		if other == nil {
			continue
		}
		for _, pet := range other.Companions() {
			if pet != nil && strings.EqualFold(pet.Name, name) {
				return true
			}
		}
	}
	return false
}

/*
================
HandlePetName

0x7571, inside the division lock so two namings cannot both pass the
uniqueness check.
================
*/
func (rt *Runtime) HandlePetName(division string, c *enterworld.Character, payload []byte) OpResult {
	r := wire.NewReader(payload)
	gid, e := r.U32()
	name, e2 := r.Str()
	if c == nil || e != nil || e2 != nil || r.Done() != nil {
		return OpResult{DiagnosticRefusal: "pet-name-malformed"}
	}
	if len(name) == 0 || len(name) > petNameMaxBytes {
		return petNameRefusal(petNameErrRefused)
	}
	// The game's character-name check: the character-name charset, its 2..12
	// bound (a one-byte name fails here as 0x440E) and the name filter
	// as the native check applies it.
	if !jobAliasShape.MatchString(name) || len(name) < domain.CharacterNameMinBytes || !rt.nameAllowed(name) {
		return petNameRefusal(petNameErrNotAllowed)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	records := summonedRecords(c, gid)
	if len(records) == 0 {
		return petNameRefusal(petNameErrNotSummoned)
	}
	for _, pet := range records {
		if pet.Name != "" {
			return petNameRefusal(petNameErrRefused)
		}
	}
	if rt.petNameTaken(division, name) {
		return petNameRefusal(petNameErrTaken)
	}
	if !rt.deps.Update(c, "pet-name", func() bool {
		if c.DeletePending {
			return false
		}
		for _, pet := range records {
			pet.Name = name
		}
		return true
	}) {
		return OpResult{DiagnosticRefusal: "pet-name-door-refused"}
	}
	frame := petNameFrame(gid, name)
	return OpResult{Frames: []wire.Frame{frame}, Around: []ObservedFrames{{SourceGID: gid, Frames: []wire.Frame{frame}}}}
}
