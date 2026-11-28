/*
===========================================================================

staff.go - staff grants on the live character, and what clients are told

Port-only, not native; the default by owner decision 2026-11-23. The
staff book (package staff) decides who is staff; this file projects a
grant onto the character through its mutation door and tells the
clients, so a grant or revoke applies with no restart and no relog:

	0x3FCC staff state, to the character itself:
	       u8 gm (0 or 1), u32 staff.Capability bits
	0x3FCD staff roster, to every player of the division:
	       u16 count, then per entry: u16 len + UTF-8 character name,
	       u16 len + UTF-8 tag, u32 name ARGB, u32 chat ARGB
	0x3FD3 staff nameplates off, to every player of the division:
	       u16 count, then per entry: u16 len + UTF-8 character name

The browser client flips its GM console and default chat channel on
0x3FCC (INFERENCE: the original learns GM only with world entry, as this
server sent it until now),
and draws each roster name's tag and colours from 0x3FCD. Both are sent
at world entry too. A shard with no staff sends neither, which is the
native behaviour. Both opcodes are free in the v1.150 client and this
server's port range (the next after 0x3FCB).

0x3FD3 names the staff whose nameplate glow is off (/NAMEPLATE OFF, owner
2026-11-28): every client draws the glow on each roster name except these,
so every player sees what the staff member chose. It is a separate frame
because 0x3FCD's decoder refuses trailing bytes: a tab that predates it
drops the unknown opcode and keeps its roster (no release-protocol bump).
It goes with every roster push and, when anyone is off, at world entry.
0x3FD3 is the next free port-only opcode (0x3FCE..0x3FD2 are taken).

===========================================================================
*/
package action

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
)

const (
	// OpStaffState and OpStaffRoster are the port-only S->C frames above.
	OpStaffState      uint16 = 0x3FCC
	OpStaffRoster     uint16 = 0x3FCD
	OpStaffNameplates uint16 = 0x3FD3
)

/*
================
StaffStateFrame
================
*/
func StaffStateFrame(c *enterworld.Character) wire.Frame {
	gm := uint8(0)
	capabilities := uint32(0)
	if c.GMPrivilege {
		gm, capabilities = 1, c.GMCapabilities
	}
	return wire.Frame{Opcode: OpStaffState, Payload: wire.NewWriter(5).U8(gm).U32(capabilities).Payload()}
}

/*
================
StaffRosterFrame
================
*/
func StaffRosterFrame(roster []staff.RosterEntry) wire.Frame {
	w := wire.NewWriter(2 + len(roster)*24).U16(uint16(len(roster)))
	for _, entry := range roster {
		w.Str(entry.Character).Str(entry.Tag).U32(entry.NameColor).U32(entry.ChatColor)
	}
	return wire.Frame{Opcode: OpStaffRoster, Payload: w.Payload()}
}

/*
================
StaffNameplatesFrame

The roster's staff whose nameplate glow is off.
================
*/
func StaffNameplatesFrame(roster []staff.RosterEntry) wire.Frame {
	var off []string
	for _, entry := range roster {
		if entry.NameplateOff {
			off = append(off, entry.Character)
		}
	}
	w := wire.NewWriter(2 + len(off)*16).U16(uint16(len(off)))
	for _, name := range off {
		w.Str(name)
	}
	return wire.Frame{Opcode: OpStaffNameplates, Payload: w.Payload()}
}

/*
================
anyNameplateOff
================
*/
func anyNameplateOff(roster []staff.RosterEntry) bool {
	for _, entry := range roster {
		if entry.NameplateOff {
			return true
		}
	}
	return false
}

/*
================
StaffEntryFrames

What a world entry owes the entering player: the roster when the division
has staff, and its own state when it is staff. Nothing on a shard without.
================
*/
func (rt *Runtime) StaffEntryFrames(division string, c *enterworld.Character) []wire.Frame {
	var frames []wire.Frame
	if rt.StaffRoster != nil {
		if roster := rt.StaffRoster(division); len(roster) > 0 {
			frames = append(frames, StaffRosterFrame(roster))
			if anyNameplateOff(roster) {
				frames = append(frames, StaffNameplatesFrame(roster))
			}
		}
	}
	if c.GMPrivilege {
		frames = append(frames, StaffStateFrame(c))
	}
	return frames
}

/*
================
OperatorStaffTarget

The canonical name of the character a staff change names, refused when
it does not exist or is pending deletion, so the book never records a
grant the world cannot project.
================
*/
func (rt *Runtime) OperatorStaffTarget(division, name string) (string, error) {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return "", fmt.Errorf("character not found")
	}
	snapshot := rt.characterSnapshot(division, c)
	if snapshot.DeletePending {
		return "", fmt.Errorf("character is pending deletion")
	}
	return snapshot.Name, nil
}

/*
================
OperatorSetStaff

Projects one staff change onto the named character (live or offline)
through its mutation door, then tells its session its new state and every
player the new roster. gm false clears the capability bits too.
================
*/
func (rt *Runtime) OperatorSetStaff(division, name string, gm bool, capabilities staff.Capability) error {
	if !gm {
		capabilities = 0
	}
	unlock := rt.lockDivision(division)
	c := rt.findCharacter(division, name)
	if c == nil {
		unlock()
		return fmt.Errorf("character not found")
	}
	var refused error
	applied := rt.deps.Update(c, "operator-staff", func() bool {
		if c.DeletePending {
			refused = fmt.Errorf("character is pending deletion")
			return false
		}
		c.GMPrivilege, c.GMCapabilities = gm, uint32(capabilities)
		return true
	})
	snapshot := rt.characterSnapshot(division, c)
	unlock()
	if !applied {
		if refused != nil {
			return refused
		}
		return fmt.Errorf("staff change refused")
	}
	log.WithFields(log.Fields{"character": snapshot.Name, "gm": gm, "capabilities": staff.IDs(capabilities)}).Info("staff: operator change")
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, snapshot.Name, []wire.Frame{StaffStateFrame(snapshot)})
	}
	rt.PushStaffRoster(division)
	return nil
}

/*
================
PushStaffRoster

Sends the division's current roster and nameplate switches to every
player in it: after a grant, a revoke, a role edit or a /NAMEPLATE. An
empty roster is sent too, so a last revoke clears every client's tags.
================
*/
func (rt *Runtime) PushStaffRoster(division string) {
	if rt.StaffRoster == nil || rt.PushShardPeerFrames == nil {
		return
	}
	roster := rt.StaffRoster(division)
	rt.PushShardPeerFrames(division, "", []wire.Frame{StaffRosterFrame(roster), StaffNameplatesFrame(roster)})
}

/*
================
SetGMNameplate

The gmcommand port: a GM's /NAMEPLATE ON or OFF, the glow every client
draws around the GM's own name. Port-only, not native (owner 2026-11-28).
The sender must still be staff when the command runs; the switch is the
staff book's (StaffSetNameplate), and every player is told at once.
================
*/
func (rt *Runtime) SetGMNameplate(division, sender string, on bool) bool {
	if rt == nil || rt.StaffSetNameplate == nil {
		return false
	}
	unlock := rt.lockDivision(division)
	gm := rt.findCharacter(division, sender)
	name, staffNow := "", gm != nil && gm.GMPrivilege
	if staffNow {
		name = gm.Name
	}
	unlock()
	if !staffNow {
		return false
	}
	changed, err := rt.StaffSetNameplate(division, name, on)
	if err != nil {
		log.WithError(err).WithField("character", name).Warn("staff: nameplate switch not saved")
		return false
	}
	if changed {
		log.WithFields(log.Fields{"character": name, "on": on}).Info("staff: nameplate switched")
		rt.PushStaffRoster(division)
	}
	return true
}
