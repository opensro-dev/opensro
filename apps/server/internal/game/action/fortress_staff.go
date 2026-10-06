/*
===========================================================================

fortress_staff.go - fortress employment replies and holder synchronization

632020 queries flags; 632100 hires. 620090 sends holder flags only to the
holder guild, not its allies. The holder may be temporary during a war.

===========================================================================
*/
package action

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
fortressHolderFlags
================
*/
func fortressHolderFlags(id uint32, flags uint8) wire.Frame {
	return wire.Frame{Opcode: opFortressWarState, Payload: wire.NewWriter(6).U8(0x12).U32(id).U8(flags).Payload()}
}

/*
================
FortressHolderFrames

4DF9E0 sends the holder flags on siege-world entry even outside war.
================
*/
func (rt *Runtime) FortressHolderFrames(division string, c *enterworld.Character) []wire.Frame {
	unlock := rt.lockDivision(division)
	defer unlock()
	c = rt.characterSnapshot(division, c)
	if c == nil || rt.Fortresses == nil || rt.Guilds == nil || c.GuildID == nil || *c.GuildID == 0 {
		return nil
	}
	world, ok := instance.Lookup(instance.ID(domain.CharacterWorldInstance(c)).Definition())
	if !ok {
		return nil
	}
	id, ok := rt.Fortresses.ForWorld(world)
	if !ok {
		return nil
	}
	record, ok := rt.Fortresses.Get(division, id)
	if !ok || record.Holder() != *c.GuildID {
		return nil
	}
	if _, _, ok := rt.Guilds.Guild(division, record.Holder()); !ok {
		return nil
	}
	return []wire.Frame{fortressHolderFlags(id, record.StaffFlags)}
}

/*
================
fortressStaffService

NPC admission precedes both operations. Queries have no guild restriction.
The durable hire door rechecks master, overlap, gold and GP under its lock.
================
*/
func (rt *Runtime) fortressStaffService(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	if rt.Fortresses == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	record, ok := rt.Fortresses.Get(division, request.Fortress)
	if request.Action == siege.ActionStaffQuery {
		if !ok {
			return fortressRefusal(request.Action, fortressErrInvalid)
		}
		return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: []byte{request.Action, 1, record.StaffFlags}}}}
	}
	code, err := rt.Fortresses.HireStaff(division, request.Fortress, c.ID, request.Value8)
	if err != nil {
		log.WithError(err).Error("fortress staff hire")
		code = fortressErrUnknown
	}
	if code != 0 {
		return fortressRefusal(request.Action, code)
	}
	record, _ = rt.Fortresses.Get(division, request.Fortress)
	state := fortressHolderFlags(record.ID, record.StaffFlags)
	var updates []wire.Frame
	if rt.Guilds != nil {
		if row, members, exists := rt.Guilds.Guild(division, record.Holder()); exists {
			updates = []wire.Frame{{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildGp3B29(row.GP)}}
			for _, member := range members {
				if member.CharID != c.ID && rt.PushCharacterFrames != nil {
					rt.PushCharacterFrames(division, member.Name, append(append([]wire.Frame{}, updates...), state))
				}
			}
		}
	}
	return OpResult{Frames: append(updates, goldFrame(c), wire.Frame{Opcode: opFortressInteractionResult, Payload: []byte{request.Action, 1, record.StaffFlags}}, state)}
}
