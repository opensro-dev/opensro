/*
===========================================================================

sessionworld.go - the session's view of the world for the simulation tick

WorldBound installs, per bound session, the snapshot the tick reads: the
live world state, the NPC anchor and the peer appearance captured from the
durable character (worn equipment, guild, skills riding the spawn row).
The move handlers in runtime.go own the world state this reads.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/domain"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/union"
	"opensro.online/server/internal/game/world/simulation"
)

//============================================================================
// Tick visibility (SnapshotProvider + division keys)

/*
==================
SessionWorld

The narrow transport seam the world binder needs.
==================
*/
type SessionWorld interface {
	SetWorldSnapshot(divisionID string, snapshot any)
}

/*
==================
WorldBound

WorldBound installs the simulation-tick session glue after a successful
enter-world bind: the division routing key and the SnapshotProvider the
worldsession SnapshotSessions leg reads. Wire it through the shared
enterworld.Deps.OnWorldBound before gameplay starts. Idempotent - a re-enter
simply replaces both values.
==================
*/
func (rt *Runtime) WorldBound(s SessionWorld, divisionID string, character *enterworld.Character) {
	if s == nil || character == nil {
		return
	}
	s.SetWorldSnapshot(divisionID, &sessionWorld{rt: rt, divisionID: divisionID, character: character})
}

/*
==================
sessionWorld

sessionWorld adapts one bound character onto the tick's SnapshotProvider
contract. The snapshot is a value copy taken under the WorldStore lock
(CloneWorldState inside Snapshot), per the SessionSnapshot concurrency
contract.
==================
*/
type sessionWorld struct {
	rt         *Runtime
	divisionID string
	character  *enterworld.Character
}

/*
==================
WorldSnapshot

WorldSnapshot yields the session's tick snapshot: the SHARED live
world plane plus the NPC anchor policy ported from the fixture env gates,
plus the character appearance the peer-visibility leg spawns on other
clients.

CONCURRENCY: the bound record is the store's LIVE *Character (ADR-1
pointer identity), and its mutable fields - MissionInventory (swapped by
action commits), GuildID (guild create/join/leave), BodyShapeByte,
World (the seed closure's first touch) - are only readable inside the
store's doors. Everything that touches them runs inside ONE Deps.Read
door here: the world snapshot (WorldStore.mu nests under store.mu per
the lock table, the same nesting HandleMove's mutation door commits
with), the NPC anchor's seed read, and the appearance field capture.
The guild resolve stays OUTSIDE the door - guilds.Guild takes the store
lock, which does not re-enter (see peerAppearance).
==================
*/
func (p *sessionWorld) WorldSnapshot() simulation.SessionSnapshot {
	key := simulation.WorldKey(p.divisionID, p.character.Name)
	var world simulation.WorldState
	var anchor simulation.Spawn
	var captured peerAppearanceCapture
	var bodyRadius simulation.BodyRadius
	var combatEligible bool
	var nativeBodyStatus uint8
	var worldInstance uint32
	p.rt.deps.Read(p.divisionID, func() {
		world = p.rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(p.character) })
		anchor = p.rt.npcAnchor(p.character)
		// Peer rows must resolve the model through the exact same chain as
		// local-player entry: explicit ref, roster codename, race/gender
		// fallback. Requiring the raw persisted ModelRef pointer made valid
		// codename-backed characters invisible to every other session.
		captured = capturePeerAppearance(p.character, p.rt.deps.CharacterModelRef(p.character))
		if resolved, ok := p.rt.deps.CharacterBodyRadius(p.character); ok {
			bodyRadius = simulation.BodyRadius(resolved)
		}
		combatEligible = enterworld.CharacterAlive(p.character) && !p.character.DeletePending
		nativeBodyStatus = p.character.NativeBodyStatus
		worldInstance = domain.CharacterWorldInstance(p.character)
	})
	var companions []*simulation.PeerCOS
	if p.rt.CompanionPresentations != nil {
		companions = p.rt.CompanionPresentations(p.divisionID, captured.name)
	}
	var cos *simulation.PeerCOS
	if p.rt.CompanionPresentations == nil && p.rt.PetPresentation != nil {
		cos = p.rt.PetPresentation(p.divisionID, captured.name)
	}
	appearance := peerAppearance(p.rt.deps.GuildAuthority(), p.rt.Unions, p.divisionID, captured)
	if appearance != nil && p.rt.Stalls != nil {
		if s, ok := p.rt.Stalls.Get(p.divisionID, captured.name); ok {
			appearance.StallTitle = s.Title
			appearance.StallDecoration = s.Decoration
		}
	}
	if appearance != nil && p.rt.ActionSpeed != nil {
		appearance.ActionSpeed = p.rt.ActionSpeed(p.divisionID, captured.name)
	}
	if appearance != nil && p.rt.SpawnSkills != nil {
		appearance.SpawnSkills = peerSpawnSkills(p.rt.SpawnSkills(p.divisionID, captured.name))
	}
	return simulation.SessionSnapshot{
		DivisionID:       p.divisionID,
		WorldInstance:    worldInstance,
		CharacterID:      captured.charID,
		CombatEligible:   combatEligible,
		NativeBodyStatus: nativeBodyStatus,
		World:            world,
		MovementCurrent:  func() bool { return p.rt.Worlds.MovementCurrent(key, world) },
		BodyRadius:       bodyRadius,
		NpcAnchor:        anchor,
		NpcsEnabled:      p.rt.npcsEnabled,
		Appearance:       appearance,
		COS:              cos,
		Companions:       companions,
	}
}

/*
==================
peerAppearanceCapture

peerAppearanceCapture is the door-side value copy of everything the tick
appearance needs from the live character record: plain values only, no
pointers or slice headers aliasing the record, so it stays valid after
the read door returns (the store contract: no field read may be trusted
outside the door).
==================
*/
type peerAppearanceCapture struct {
	pvpState      uint8
	eventTeam     uint8
	hasEvent      bool
	hasModel      bool
	modelRef      uint32
	name          string
	charID        int64
	hasGuild      bool
	guildID       int64
	bodyShapeByte uint8
	visualFlags   uint8
	jobType       uint8
	jobGrade      uint8
	skin          wire.TransformSkin
	worn          []wornEquipRow
}

/*
==================
wornEquipRow

One equipment-band inventory row narrowed to the fields the spawn
appearance emits (slot / refObjId / typeFlags / plus).
==================
*/
type wornEquipRow struct {
	slot      int64
	refObjID  uint32
	typeFlags uint16
	plus      int64
}

/*
==================
capturePeerAppearance

capturePeerAppearance copies the appearance inputs off the live record.
MUST run inside the store's read door (Deps.Read) - it dereferences the
ModelRef/GuildID/BodyShapeByte pointers and walks the MissionInventory
slice, all of which MutateCharacter closures swap concurrently. Only the
equipment-band rows copy (the sub_86afb0 equip loop's input); the row
filter matches the emission filter peerAppearance applied before the
capture/build split, so the wire output is unchanged.
==================
*/
func capturePeerAppearance(character *enterworld.Character, resolvedModelRef uint32) peerAppearanceCapture {
	captured := peerAppearanceCapture{
		pvpState:  character.PVPState(),
		eventTeam: character.EventTeam(),
		hasEvent:  character.EventTeam() != 0xff,
		name:      character.Name,
		charID:    character.ID,
		modelRef:  resolvedModelRef,
	}
	if resolvedModelRef == 0 {
		return captured
	}
	captured.hasModel = true
	if character.GuildID != nil {
		captured.hasGuild = true
		captured.guildID = *character.GuildID
	}
	if character.BodyShapeByte != nil {
		captured.bodyShapeByte = uint8(*character.BodyShapeByte & 0xff)
	}
	captured.visualFlags = enterworld.ResolveVisualFlags(character)
	// A worn job suit shows the job and its grade (job mode).
	if job := enterworld.DressedJob(character); job != 0 {
		captured.jobType, captured.jobGrade = job, character.Job.Grade
	}
	captured.skin = enterworld.CharacterTransformSkin(character)
	captured.worn = make([]wornEquipRow, 0, len(character.MissionInventory))
	for _, row := range character.MissionInventory {
		if row.RefObjID == 0 || row.Slot < 0 || row.Slot > 0xff {
			continue
		}
		if !inventory.IsEquipmentSlot(uint8(row.Slot)) {
			continue
		}
		captured.worn = append(captured.worn, wornEquipRow{
			slot:      row.Slot,
			refObjID:  row.RefObjID,
			typeFlags: row.TypeFlags,
			plus:      row.Plus,
		})
	}
	return captured
}

/*
==================
peerAppearance

peerAppearance builds the tick-time simulation.PeerAppearance value copy from
the door-captured character fields - every field is REAL persisted state:
the model ref (the sub_850c60 resolve target on the viewer's client), the
character name, the creation body-shape byte (-> CICUser +0x758), the
WORN inventory rows (the equipment-band slots 0..12) in slot order for the
sub_86afb0 equip loop, and - for guild members - the guild identity the
sub_869df0 non-local tail carries (name/id/grant/crestParam, resolved
through the SAME guild door the 0x32C4 seed encodes from; a non-member or
a dangling FK honestly leaves the guild fields zero, so the client's
BindGuild leg stays unreached exactly like the retail no-guild row). nil
when the character has no model record - such a row could never resolve on
the receiving client.

Runs OUTSIDE the read door, on the captured values: guilds.Guild takes
the store lock, and the store's doors must not call lock-taking store
accessors (Go mutexes do not re-enter - inside the door this lookup
would deadlock, which is why the FK copies out and resolves here).
==================
*/
func peerAppearance(guilds enterworld.GuildStore, unions *union.Authority, divisionID string, captured peerAppearanceCapture) *simulation.PeerAppearance {
	if !captured.hasModel {
		return nil
	}
	appearance := &simulation.PeerAppearance{
		PVPState:      captured.pvpState,
		RefObjID:      captured.modelRef,
		Name:          captured.name,
		BodyShapeByte: captured.bodyShapeByte,
		VisualFlags:   captured.visualFlags,
		JobType:       captured.jobType,
		JobGrade:      captured.jobGrade,
		Skin:          captured.skin,
	}
	if captured.hasEvent {
		team := captured.eventTeam
		appearance.EventTeam = &team
	}
	if guilds != nil && captured.hasGuild {
		if record, members, ok := guilds.Guild(divisionID, captured.guildID); ok {
			appearance.GuildName = record.Name
			appearance.GuildID = uint32(record.ID)
			appearance.CrestParam = record.CrestParam
			if alliance, ok := unions.Of(divisionID, record.ID); ok {
				appearance.AllianceID, appearance.AllianceCrest = uint32(alliance.AllianceID), alliance.Crest
			}
			for _, member := range members {
				if member.CharID == captured.charID {
					appearance.GuildGrantName = member.GrantName
					// The member row's fortress-war role byte rides the
					// spawn guild sub-block's trailing team byte (client
					// sub_869df0 @0x0086a1b2 -> CICPlayer+0x7e0) - the
					// same persisted byte the 0x32C4 member loop encodes.
					appearance.FortSiegeAuthority = member.FortressRole
					break
				}
			}
		}
	}
	worn := captured.worn
	sort.Slice(worn, func(i, j int) bool { return worn[i].slot < worn[j].slot })
	for _, row := range worn {
		plus := row.plus
		if plus < 0 {
			plus = 0
		}
		if plus > 0xff {
			plus = 0xff
		}
		appearance.Equipment = append(appearance.Equipment, wire.PlayerEquipItem{
			RefObjID:  row.refObjID,
			TypeFlags: row.typeFlags,
			OptLevel:  uint8(plus),
		})
	}
	return appearance
}

/*
==================
npcAnchor

npcAnchor resolves the roster anchor (resolveMissionNpcSpawnAnchor): the
player's start placement with AT_PLAYER, else the fixed Constantinople
shop anchor.
==================
*/
func (rt *Runtime) npcAnchor(character *enterworld.Character) simulation.Spawn {
	if rt.npcsAtPlayer {
		return simulation.SeedWorldState(character).Spawn
	}
	return simulation.NpcShopSpawn()
}

/*
==================
peerSpawnSkills

The entry projection as spawn-row entries: a token rides when the skill
has one (its remaining time is local-only and dropped), a status byte when
the skill has one.
==================
*/
func peerSpawnSkills(rows []enterworld.EntrySkill) []wire.SpawnSkillEntry {
	out := make([]wire.SpawnSkillEntry, 0, len(rows))
	for _, row := range rows {
		entry := wire.SpawnSkillEntry{SkillID: row.ID, Status: row.Status, HasStatus: row.HasStatus}
		if row.Token != nil {
			entry.Token, entry.HasToken = *row.Token, true
		}
		out = append(out, entry)
	}
	return out
}
