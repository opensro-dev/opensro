/*
===========================================================================

fortress_query.go - fortress manager dates and aide entry admission

The existing siege lane owns dates and the fortress authority owns the
applications. 754A40 requires the v1.150 dates and guild list, not the
v1.188 6322A0 compact dates and 61D980 attack-present flag.

===========================================================================
*/
package action

import (
	"sort"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
)

const fortressMaxApplicants = 255

/*
================
fortressServiceQuery

519E60 checks the selected NPC's service. Case 9 deliberately returns
without a response after admission (51A0FF..51A11C).
================
*/
func (rt *Runtime) fortressServiceQuery(division string, c *enterworld.Character, request siege.Interaction) OpResult {
	selected, ok := rt.Selected.Get(division, c.Name)
	if !ok || selected != request.Target {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	npc, ok := rt.npcForCurrentViewer(division, c, request.Target)
	if !ok || !npc.Services.Has(siege.InteractionService(request.Action)) || !rt.npcWithinHitRange(division, c, npc) {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	if request.Action == siege.ActionAide {
		return OpResult{}
	}
	if rt.Fortresses == nil || rt.FortressWarDates == nil || rt.Guilds == nil {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	record, ok := rt.Fortresses.Get(division, request.Fortress)
	if !ok {
		return fortressRefusal(request.Action, fortressErrInvalid)
	}
	ids := make([]int64, 0, len(record.Applicants))
	for id := range record.Applicants {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	guilds := make([]domain.GuildRecord, 0, len(ids))
	for _, id := range ids {
		if guild, _, exists := rt.Guilds.Guild(division, id); exists {
			guilds = append(guilds, guild)
		}
	}
	if len(guilds) > fortressMaxApplicants {
		return fortressRefusal(request.Action, fortressErrUnknown)
	}
	previous, next := rt.FortressWarDates(rt.Now().UnixMilli())
	w := wire.NewWriter(64).U8(request.Action).U8(1)
	writeFortressDate(w, previous)
	writeFortressDate(w, next)
	w.U8(uint8(len(guilds)))
	for _, guild := range guilds {
		w.U16(uint16(len(guild.Name))).Bytes([]byte(guild.Name)).U8(guild.Level).U8(uint8(record.Applicants[guild.ID]))
	}
	return OpResult{Frames: []wire.Frame{{Opcode: opFortressInteractionResult, Payload: w.Payload()}}}
}

/*
================
writeFortressDate

An absent previous/next occurrence is the native zero SYSTEMTIME.
================
*/
func writeFortressDate(w *wire.Writer, at time.Time) {
	if at.IsZero() {
		for range 8 {
			w.U16(0)
		}
		return
	}
	writeSystemTime(w, at)
}
