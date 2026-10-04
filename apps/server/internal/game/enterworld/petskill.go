package enterworld

import (
	"time"

	wire "opensro.online/server/internal/game/item/wire"
)

// PetSkillItemType classifies the ITEM_MALL_PET_SKILL_COLD/FIRE/LIGHTNING and
// ITEM_MALL_PET_GROWTH_POTION family: tid 3/3/13/15, Param1 1800 under the
// label 사용시간(초). It is the COS-scoped family whose Param1 is authored in
// SECONDS, which is what sub_6E6E00 kind 3 scales by 1000, so it is the one
// v1.150 producer of the magic-state board's two-bar slot. A COS summoner is
// not: its Param1 is minutes (ITEM_COS_P_EXTENSION_1D carries 1440).
func PetSkillItemType(typeIDs [4]int64) bool {
	return typeIDs == [4]int64{3, 3, 13, 15}
}

// PetSkillWindowRemaining reports the whole seconds left, rounded up so a
// window only reads spent once it truly is. sub_6E6E00 seeds the row's
// elapsed accumulator to limit minus this value, so it is what the wire
// carries - what is LEFT, never what has run.
func PetSkillWindowRemaining(endUnixMs, nowMs int64) uint32 {
	if endUnixMs <= nowMs {
		return 0
	}
	seconds := (endUnixMs - nowMs + 999) / 1000
	if seconds > int64(^uint32(0)) {
		return 0
	}
	return uint32(seconds)
}

// clock is the single time source for a bootstrap. The package keeps time an
// explicit input (HandleEventGuideAck takes now as a parameter) and the server
// wiring points this at the action runtime's own clock, so the bootstrap and
// the tick sweep agree on every deadline.
func (d *Deps) clock() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// petSkillWindowPackets re-raises each live window after world entry. The
// reset that opens every bootstrap destroys it: 0x3369 (sub_74B880) runs the
// 0x366A handler, whose sub_685400 -> sub_6E6270(board, 0) empties every
// board row, kind 3 included. The client cannot rebuild one itself: its
// load-end restore walk (sub_683B40) repopulates the board only through
// sub_6E5D40, which stamps kind 1, and of sub_67A470's fourteen callers only
// sub_775F20 passes kind 3 (the rest pass 4-8). A kind-3 row therefore exists
// after login or a ReentryPackets re-entry only if 0x3691 is sent again after
// the reset.
// Sending it during loading is safe: CPSMission::OnCreate (sub_729A90)
// installs the 0x3691 handler and creates the CGInterface in the same call,
// and sub_683B40 never clears a row.
//
// Each window must still resolve to the same pet-skill item it was raised
// from, so a changed or unpublished reference fails closed rather than
// announcing a window the browser has no record for. A spent window is not
// announced; the tick sweep owns its retirement.
func petSkillWindowPackets(deps *Deps, character *Character, nowMs int64) []Packet {
	if character == nil || len(character.PetSkillWindows) == 0 || deps == nil || deps.Items == nil {
		return nil
	}
	var packets []Packet
	for _, window := range character.PetSkillWindows {
		remaining := PetSkillWindowRemaining(window.EndUnixMs, nowMs)
		if remaining == 0 {
			continue
		}
		ref, ok := deps.Items.ItemRefByCodename(window.Codename)
		if !ok || ref == nil || ref.RefObjID != window.ItemRefObjID || !PetSkillItemType(ref.TypeIDs) {
			continue
		}
		packets = append(packets, NewPacket(wire.OpCosStateRefresh,
			wire.EncodeCosSummonTimer3691(window.ItemRefObjID, remaining, 0)))
	}
	return packets
}

/*
================
paramJobPackets

0x32AF (76F750) re-raises every live param job after the board reset, the
same way the pet-skill windows above are re-raised. A job whose internal
item no longer resolves is not announced; the action sweep retires it.
================
*/
func paramJobPackets(deps *Deps, character *Character, nowMs int64) []Packet {
	if character == nil || len(character.ParamJobs) == 0 || deps == nil || deps.Items == nil {
		return nil
	}
	owner := ObjectIDForCharacter(character)
	var packets []Packet
	// One row per item: a premium ticket raises two keepers under one row.
	shown := map[uint32]bool{}
	for _, job := range character.ParamJobs {
		remaining := PetSkillWindowRemaining(job.EndUnixMs, nowMs)
		if remaining == 0 || shown[job.ItemRefObjID] {
			continue
		}
		shown[job.ItemRefObjID] = true
		ref, ok := deps.Items.ItemRefByCodename(job.Codename)
		if !ok || ref == nil || ref.RefObjID != job.ItemRefObjID {
			continue
		}
		packets = append(packets, NewPacket(wire.OpParamJobResume, wire.EncodeParamJobRow(owner, remaining, job.ItemRefObjID)))
	}
	return packets
}
