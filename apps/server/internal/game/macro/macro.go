/*
===========================================================================

macro.go - the SRO Macro settings lane: 0x7554 requests, 0xB554 answers and
the 0x3555 configuration replay (#3907)

Port-only, not native: the macro is a v1.696 feature and the v1.188
reference server has no handler for it, so the server half is inferred from
the v1.696 client's contract (sro_client.exe 8af7afc2..., research note
sro-macro.md). The client runs the potion, skill and hunt automation
itself; the server only stores the three sections and their ON states and
acknowledges each request, the way the client's handlers expect:

	0x7554 C->S  u8 0 (switch) u8 kind u8 state           (client 006947f0, 00694ab0)
	             u8 1 (save)   u8 kind section            (client 00694170)
	0xB554 S->C  u8 action u8 kind u16 result [u8 state]  (client 00cfca30)
	0x3555 S->C  u8 0 u8 available u8 sections, sections  (client 00cfcd80)

The package is pure: it decodes, applies and encodes against a
domain.MacroSettings value and leaves persistence to its caller. Off
(SRO_AUTO_MACRO unset) is native: nothing is sent and requests are dropped.

===========================================================================
*/
package macro

import (
	"encoding/binary"
	"errors"
	"strings"

	"opensro.online/server/internal/domain"
)

const (
	// Env switches the lane on; anything but on/1/true is the native off.
	Env = "SRO_AUTO_MACRO"

	OpRequest uint16 = 0x7554
	OpAnswer  uint16 = 0xB554
	OpConfig  uint16 = 0x3555

	KindPotion uint8 = 0
	KindSkill  uint8 = 1
	KindHunt   uint8 = 2
	kindCount        = 3

	actionSwitch uint8 = 0
	actionSave   uint8 = 1

	// resultOK is the only result the client's 0xB554 handler commits on.
	resultOK uint16 = 1
	// resultSwitchFailed prints the client's "ON/OFF Error" (00cfcbb3).
	resultSwitchFailed uint16 = 0x9806
	// resultSaveFailed is any other word: the client prints "Save Error".
	resultSaveFailed uint16 = 2

	configFull uint8 = 0
	// availableAll sets bit 0 potion, bit 1 skill, bit 2 hunt.
	availableAll uint8 = 0x07

	potionVersion uint16 = 3
	skillVersion  uint16 = 2
	huntVersion   uint16 = 2

	// skillRecordBytes is one list entry on the wire: u8 slot, u32 skill.
	skillRecordBytes = 5
	// emptyListBytes is the client's empty list: length 1, one zero byte.
	emptyListBytes = 1
)

// The save's skill record tags (client 00694170): four flag bytes and two lists.
var skillFlagTags = [domain.MacroSkillFlags]uint8{0, 1, 3, 4}
var skillListTags = [domain.MacroSkillLists]uint8{2, 5}

var errMalformed = errors.New("macro: malformed request")

/*
================
Enabled
================
*/
func Enabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(Env))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Answer

The outcome of one request: the settings to store (Changed false keeps
the old ones) and the 0xB554 payload, nil when the request is too short
to name an action and kind and so has no conversation to answer.
================
*/
type Answer struct {
	Settings domain.MacroSettings
	Changed  bool
	Payload  []byte
}

/*
================
Apply

Applies one 0x7554 payload to current. A refused request still answers,
with the failure word the client prints, and leaves the settings alone.
================
*/
func Apply(current domain.MacroSettings, payload []byte) Answer {
	if len(payload) < 2 {
		return Answer{Settings: current}
	}
	action, kind := payload[0], payload[1]
	switch action {
	case actionSwitch:
		next, err := applySwitch(current, kind, payload[2:])
		if err != nil {
			return Answer{Settings: current, Payload: answer(action, kind, resultSwitchFailed)}
		}
		state := payload[2]
		return Answer{Settings: next, Changed: next != current, Payload: append(answer(action, kind, resultOK), state)}
	case actionSave:
		next, err := applySave(current, kind, payload[2:])
		if err != nil {
			return Answer{Settings: current, Payload: answer(action, kind, resultSaveFailed)}
		}
		return Answer{Settings: next, Changed: next != current, Payload: answer(action, kind, resultOK)}
	}
	return Answer{Settings: current}
}

/*
================
RefusalPayload

The failure answer for a request whose change could not be stored, nil
when the request names no action and kind.
================
*/
func RefusalPayload(payload []byte) []byte {
	if len(payload) < 2 {
		return nil
	}
	if payload[0] == actionSwitch {
		return answer(actionSwitch, payload[1], resultSwitchFailed)
	}
	return answer(payload[0], payload[1], resultSaveFailed)
}

/*
================
answer
================
*/
func answer(action, kind uint8, result uint16) []byte {
	out := []byte{action, kind, 0, 0}
	binary.LittleEndian.PutUint16(out[2:], result)
	return out
}

/*
================
applySwitch

body is u8 state, 0 or 1.
================
*/
func applySwitch(s domain.MacroSettings, kind uint8, body []byte) (domain.MacroSettings, error) {
	if len(body) != 1 || body[0] > 1 {
		return s, errMalformed
	}
	on := body[0] == 1
	switch kind {
	case KindPotion:
		s.PotionOn = on
	case KindSkill:
		s.SkillOn = on
	case KindHunt:
		s.HuntOn = on
	default:
		return s, errMalformed
	}
	return s, nil
}

/*
================
applySave

The potion and hunt saves carry u16 version, one byte the client sends
ahead of the block (pending block byte 0x44 and byte 1; its meaning is not
read, so it is accepted and not stored), then the whole block. The skill
save carries u16 version and only the records that changed.
================
*/
func applySave(s domain.MacroSettings, kind uint8, body []byte) (domain.MacroSettings, error) {
	if len(body) < 2 {
		return s, errMalformed
	}
	version, rest := binary.LittleEndian.Uint16(body), body[2:]
	switch kind {
	case KindPotion:
		if version != potionVersion || len(rest) != 1+domain.MacroPotionBytes {
			return s, errMalformed
		}
		copy(s.Potion[:], rest[1:])
		s.HasPotion = true
	case KindHunt:
		if version != huntVersion || len(rest) != 1+domain.MacroHuntBytes {
			return s, errMalformed
		}
		copy(s.Hunt[:], rest[1:])
		s.HasHunt = true
	case KindSkill:
		if version != skillVersion {
			return s, errMalformed
		}
		return applySkillRecords(s, rest)
	default:
		return s, errMalformed
	}
	return s, nil
}

/*
================
applySkillRecords

Merges tagged records onto s; a malformed record refuses the whole save.
================
*/
func applySkillRecords(s domain.MacroSettings, body []byte) (domain.MacroSettings, error) {
	next := s
	for len(body) > 0 {
		tag := body[0]
		body = body[1:]
		if i, ok := tagIndex(skillFlagTags[:], tag); ok {
			if len(body) < 1 {
				return s, errMalformed
			}
			next.SkillFlags[i] = body[0]
			body = body[1:]
			continue
		}
		i, ok := tagIndex(skillListTags[:], tag)
		if !ok {
			return s, errMalformed
		}
		list, used, err := readSkillList(body)
		if err != nil {
			return s, err
		}
		next.SkillLists[i] = list
		body = body[used:]
	}
	return next, nil
}

/*
================
tagIndex
================
*/
func tagIndex(tags []uint8, tag uint8) (int, bool) {
	for i, t := range tags {
		if t == tag {
			return i, true
		}
	}
	return 0, false
}

/*
================
readSkillList

u16 bytes, then bytes/5 entries; bytes 1 is the empty list and carries
one zero byte. Returns the list and the bytes consumed.
================
*/
func readSkillList(body []byte) (domain.MacroSkillList, int, error) {
	var list domain.MacroSkillList
	if len(body) < 2 {
		return list, 0, errMalformed
	}
	size := int(binary.LittleEndian.Uint16(body))
	body = body[2:]
	if size == emptyListBytes {
		if len(body) < 1 || body[0] != 0 {
			return list, 0, errMalformed
		}
		return list, 2 + emptyListBytes, nil
	}
	count := size / skillRecordBytes
	if size%skillRecordBytes != 0 || count == 0 || count > domain.MacroSkillsPerList || len(body) < size {
		return list, 0, errMalformed
	}
	for i := 0; i < count; i++ {
		record := body[i*skillRecordBytes:]
		list.Skills[i] = domain.MacroSkill{Slot: record[0], Skill: binary.LittleEndian.Uint32(record[1:])}
	}
	list.Count = uint8(count)
	return list, 2 + size, nil
}

//============================================================================

/*
================
ConfigPayload

The 0x3555 full replay sent once at login: every section available and
all three present. An unsaved potion or hunt section goes with version 0
and no bytes, which the client skips and answers with its own defaults
(client 00694010); the skill section is always version 2.
================
*/
func ConfigPayload(s domain.MacroSettings) []byte {
	out := []byte{configFull, availableAll, kindCount}

	out = append(out, KindPotion, flag(s.PotionOn))
	if s.HasPotion {
		out = appendU16(out, potionVersion)
		out = appendU16(out, domain.MacroPotionBytes)
		out = append(out, s.Potion[:]...)
	} else {
		out = appendU16(out, 0)
		out = appendU16(out, 0)
	}

	out = append(out, KindSkill, flag(s.SkillOn))
	out = appendU16(out, skillVersion)
	out = append(out, s.SkillFlags[0], s.SkillFlags[1])
	out = appendSkillList(out, s.SkillLists[0])
	out = append(out, s.SkillFlags[2], s.SkillFlags[3])
	out = appendSkillList(out, s.SkillLists[1])

	out = append(out, KindHunt, flag(s.HuntOn))
	if s.HasHunt {
		out = appendU16(out, huntVersion)
		out = appendU16(out, domain.MacroHuntBytes)
		out = append(out, s.Hunt[:]...)
	} else {
		out = appendU16(out, 0)
		out = appendU16(out, 0)
	}
	return out
}

/*
================
appendSkillList
================
*/
func appendSkillList(out []byte, list domain.MacroSkillList) []byte {
	count := min(int(list.Count), domain.MacroSkillsPerList)
	if count == 0 {
		out = appendU16(out, emptyListBytes)
		return append(out, 0)
	}
	out = appendU16(out, uint16(count*skillRecordBytes))
	for _, skill := range list.Skills[:count] {
		out = append(out, skill.Slot)
		out = binary.LittleEndian.AppendUint32(out, skill.Skill)
	}
	return out
}

/*
================
appendU16
================
*/
func appendU16(out []byte, v uint16) []byte {
	return binary.LittleEndian.AppendUint16(out, v)
}

/*
================
flag
================
*/
func flag(on bool) byte {
	if on {
		return 1
	}
	return 0
}
