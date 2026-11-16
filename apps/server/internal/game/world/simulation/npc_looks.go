/*
===========================================================================

npc_looks.go - a seasonal look: one NPC worn as another reference

Port-only, not native. The later client dresses an event NPC by its
event_npc_look.txt rows (v1.188/v1.696: NPC_CH_EVENT_KISAENG1 drawn with
npc\npc\event_santa_kisaeng1.bsr under the name
SN_NPC_CH_EVENT_SANTA_KISAENG1); the v1.150 client has no such table,
but it ships the dressed NPC as its own reference
(NPC_CH_EVENT_SANTA_KISAENG1, the same NPC class). So the server dresses
an NPC by spawning it under that reference: same object id, place and
talk, another model and name. Only the spawn changes; every handler
still resolves the NPC by its object id to the undressed row, so its
dialogues and services stay its own.

A look stands for a window. A spawn already on a client cannot change
its reference, so for lookChangeover after each window edge the NPC is
hidden: every scope pass in that span despawns it (NpcScopeFramesInto),
and the next pass after it spawns it under the new look. A viewer whose
legs did not run in that span (a loading screen) keeps the old look
until the NPC next enters its view. The rosters are built once, so a
tick only picks one of three slices.

===========================================================================
*/
package simulation

import "time"

// lookChangeover is how long a dressed NPC stays hidden at a window
// edge: several ticks, so every viewer's scope pass drops the old look.
const lookChangeover = 3 * time.Second

/*
================
NpcLook

The NPC to dress (by codename, every placement), the reference it wears,
and the window it wears it in.
================
*/
type NpcLook struct {
	Codename string
	Look     NpcDef
	StartsAt time.Time
	EndsAt   time.Time
}

/*
================
NpcLookRosters

The base roster and its two seasonal variants: dressed (the window) and
hidden (the changeover after each edge). The zero value has no look: At
answers the base roster.
================
*/
type NpcLookRosters struct {
	base, dressed, hidden []NpcDef
	look                  NpcLook
}

/*
================
NewNpcLookRosters

Dresses every placement of look.Codename. A look with no reference or
no window, or a roster without the NPC, dresses nothing.
================
*/
func NewNpcLookRosters(base []NpcDef, look NpcLook) NpcLookRosters {
	out := NpcLookRosters{base: base}
	if look.Look.RefObjID == 0 || !look.EndsAt.After(look.StartsAt) {
		return out
	}
	dressed := make([]NpcDef, len(base))
	hidden := make([]NpcDef, len(base))
	copy(dressed, base)
	copy(hidden, base)
	found := false
	for i := range base {
		if base[i].Codename != look.Codename {
			continue
		}
		found = true
		dressed[i].RefObjID = look.Look.RefObjID
		dressed[i].TidWord = look.Look.TidWord
		dressed[i].NameStrID = look.Look.NameStrID
		dressed[i].Name = look.Look.Name
		dressed[i].ModelPath = look.Look.ModelPath
		hidden[i].Hidden = true
	}
	if !found {
		return out
	}
	out.dressed, out.hidden, out.look = dressed, hidden, look
	return out
}

/*
================
NpcLookRosters.At

The roster to spawn from at now: hidden in the changeover after either
window edge, dressed inside the window, the base roster otherwise.
================
*/
func (r NpcLookRosters) At(now time.Time) []NpcDef {
	if r.dressed == nil {
		return r.base
	}
	for _, edge := range [2]time.Time{r.look.StartsAt, r.look.EndsAt} {
		if !now.Before(edge) && now.Before(edge.Add(lookChangeover)) {
			return r.hidden
		}
	}
	if !now.Before(r.look.StartsAt) && now.Before(r.look.EndsAt) {
		return r.dressed
	}
	return r.base
}

/*
================
NpcLookRosters.Look

The reference the look wears, and whether any NPC wears it: the row the
client's reference snapshot must carry before the dressed NPC spawns.
================
*/
func (r NpcLookRosters) Look() (NpcDef, bool) {
	return r.look.Look, r.dressed != nil
}

/*
================
LoadNpcLookRef

The NPC reference codename names in the shipped data (the same NPC class
gate the world roster passes), as an NpcDef carrying its identity, name
and model; false when the data has no such enabled NPC.
================
*/
func LoadNpcLookRef(textdataDir, codename string) (NpcDef, bool) {
	for _, ref := range loadNpcWorldRefs(textdataDir) {
		if ref.codename == codename {
			return NpcDef{
				RefObjID: ref.refObjID, TidWord: ref.tidWord, Codename: ref.codename,
				NameStrID: ref.nameID, Name: ref.name, ModelPath: ref.model,
				Level: ref.level, MaxHP: ref.maxHP,
			}, true
		}
	}
	return NpcDef{}, false
}

/*
================
Ticker.sessionRoster

The NPCs one session's scope pass spawns from at a tick instant: the
static roster, or its seasonal look variant when RosterAt is set
(port-only; nil means Roster), then the runtime objects standing in the
session's world instance (WorldNpcs: a quest instance's exit gate).
================
*/
func (t *Ticker) sessionRoster(world uint32, nowMs int64) []NpcDef {
	roster := t.Roster
	if t.RosterAt != nil {
		roster = t.RosterAt(nowMs)
	}
	if t.WorldNpcs != nil {
		if extra := t.WorldNpcs(world); len(extra) > 0 {
			roster = append(append(make([]NpcDef, 0, len(roster)+len(extra)), roster...), extra...)
		}
	}
	return roster
}
