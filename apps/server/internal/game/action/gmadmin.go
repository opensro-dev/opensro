/*
===========================================================================

gmadmin.go - the GM's world status and guild recall

/WORLDSTATUS (sub 4), /RECALLGUILD (sub 0x12) and /SETTIME (sub 0xA), arms of the v1.188
per-player GM special-command handler. The
gmcommand dispatcher decodes the request and answers on the wire; this
file reads the world and moves characters.

===========================================================================
*/
package action

import (
	"strings"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
WorldStatusGM

The special-command case 3 ("ShowMeStatistics: [PC: %d, NPC: %d, ItemOnGrd: %d"): the
players in the world, every other world object, and the items on the
ground. INFERENCE: native counts objects in its one object manager; the
port's division holds admitted players, resident monsters across every
population, the NPC roster and the ground registry.
================
*/
func (rt *Runtime) WorldStatusGM(division string) (players, others, items uint32) {
	if rt == nil {
		return 0, 0, 0
	}
	rt.characterAdmissions.Range(func(_, value any) bool {
		if value.(populationAdmission).division == division {
			players++
		}
		return true
	})
	if rt.Monsters != nil {
		others = uint32(rt.Monsters.DormancyStats(division)["resident"])
	}
	others += uint32(len(rt.NpcRoster))
	if rt.Ground != nil {
		items = uint32(rt.Ground.Count(division))
	}
	return players, others, items
}

/*
================
RecallGuildGM

The special-command case 0xE: the guild manager finds the guild by name and relays
0x7010 to the shard manager, which brings its members to where the GM
stands. The port has one GameWorld per division, so it recalls them here,
each through RecallUserGM: a member who is offline, dead or riding stays
where they are. Reports whether the guild exists; an empty recall still
succeeds, as native acks the relay, not the arrivals.
================
*/
func (rt *Runtime) RecallGuildGM(division, name, guildName string) bool {
	if rt == nil || rt.deps == nil || guildName == "" {
		return false
	}
	members, ok := rt.guildMembersByName(division, guildName)
	if !ok {
		return false
	}
	for _, member := range members {
		if !strings.EqualFold(member, name) {
			rt.RecallUserGM(division, name, member)
		}
	}
	return true
}

/*
================
guildMembersByName

The member names of the guild called name (case-insensitive, as guild
names are unique without case). Guilds are found through their members'
records, the way the war lane resolves a guild by name.
================
*/
func (rt *Runtime) guildMembersByName(division, name string) ([]string, bool) {
	guilds := rt.deps.GuildAuthority()
	if guilds == nil {
		return nil, false
	}
	seen := make(map[int64]bool)
	for _, c := range rt.deps.CharactersForDivision(division) {
		snapshot := rt.characterSnapshot(division, c)
		if snapshot == nil || snapshot.GuildID == nil || seen[*snapshot.GuildID] {
			continue
		}
		seen[*snapshot.GuildID] = true
		guild, members, found := guilds.Guild(division, *snapshot.GuildID)
		if !found || !strings.EqualFold(guild.Name, name) {
			continue
		}
		names := make([]string, 0, len(members))
		for _, member := range members {
			names = append(names, member.Name)
		}
		return names, true
	}
	return nil, false
}

/*
================
SetGameTimeGM

/SETTIME hour (special-command case 8, "SetGameTime to %d"): the world clock moves
to that hour, and every player in the world gets the 0x31AD clock push,
since a client keeps its own clock after entry. The clock is one per
process, as the reference server's is, so every division's players hear it.

The push holds every division's publication gate (lockTickPublication, the
tick's own order). An entry binds its session and sends its EnterWorld
result under that gate: a push inside the span would reach a client that
has no world yet ("Native packet before EnterWorld") and drop it. Under the
gate the push lands before the bind (the entry's bootstrap reads the moved
clock) or after the result.
================
*/
func (rt *Runtime) SetGameTimeGM(hour uint8) {
	if rt == nil || rt.SetWorldHour == nil {
		return
	}
	payload := rt.SetWorldHour(hour).Payload()
	if rt.PushCharacterFrames == nil {
		return
	}
	unlock := rt.lockTickPublication()
	defer unlock()
	rt.characterAdmissions.Range(func(_, value any) bool {
		admission := value.(populationAdmission)
		rt.PushCharacterFrames(admission.division, admission.name,
			[]wire.Frame{{Opcode: simulation.OpGameTime, Payload: payload}})
		return true
	})
}
