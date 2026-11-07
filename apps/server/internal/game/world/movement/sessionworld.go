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
	"slices"
	"sort"
	"sync"

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

	// The peer appearance last handed to a tick and the exact inputs
	// that produced it: WorldSnapshot rebuilds the appearance only
	// when an input moved, so an unchanged peer costs a fingerprint
	// compare per tick instead of a fresh build. Guarded because
	// WorldSnapshot may run concurrently (the snapshot race test
	// drives it so).
	appearanceMu       sync.Mutex
	appearancePrimed   bool
	appearanceInputs   peerAppearanceFingerprint
	appearanceSnapshot *simulation.PeerAppearance
}

/*
==================
WorldView

WorldSnapshot without the peer presentation: the world, its instance and
the character's identity, read under the same door. Tick hooks that never
spawn this player for a viewer take this (worldsession.ViewProvider), so
action speed, spawn skills, guild, stall and companions are built once per
tick, by the coordinator's full snapshot, not once per hook.
==================
*/
func (p *sessionWorld) WorldView() simulation.SessionView {
	key := simulation.WorldKey(p.divisionID, p.character.Name)
	var view simulation.SessionView
	p.rt.deps.Read(p.divisionID, func() {
		view = simulation.SessionView{
			DivisionID:    p.divisionID,
			CharacterID:   p.character.ID,
			WorldInstance: domain.CharacterWorldInstance(p.character),
			World:         p.rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(p.character) }),
		}
	})
	return view
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
	appearance := p.appearanceFor(captured)
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
sessionWorld.appearanceFor

The session's peer appearance for one snapshot: the build of the
last WorldSnapshot, handed out again while every input that feeds
it - the door capture, the guild half, the stall, the action
speed, the spawn skills - is unchanged. A peer's faces change
rarely next to the tick rate, so the unchanged case (the common
one) costs a fingerprint compare instead of a fresh build.

The kept appearance is shared across ticks, which is safe here
because no reader mutates a snapshot's appearance: the peer leg
copies the struct before stamping walk/run speeds onto its copy,
and the rest only read. Any input change rebuilds and replaces
the kept value; the replaced one is never written again.
==================
*/
func (p *sessionWorld) appearanceFor(captured peerAppearanceCapture) *simulation.PeerAppearance {
	if !captured.hasModel {
		return nil
	}
	inputs := peerAppearanceFingerprint{captured: captured}
	inputs.guild = resolvePeerGuild(p.rt.deps.GuildAuthority(), p.rt.Unions, p.divisionID, captured)
	if p.rt.Stalls != nil {
		if s, ok := p.rt.Stalls.Get(p.divisionID, captured.name); ok {
			inputs.stallTitle, inputs.stallDecor = s.Title, s.Decoration
		}
	}
	if p.rt.ActionSpeed != nil {
		inputs.actionSpeed = p.rt.ActionSpeed(p.divisionID, captured.name)
	}
	var skillRows []enterworld.EntrySkill
	if p.rt.SpawnSkills != nil {
		skillRows = p.rt.SpawnSkills(p.divisionID, captured.name)
	}
	p.appearanceMu.Lock()
	defer p.appearanceMu.Unlock()
	if p.appearancePrimed && p.appearanceInputs.equal(inputs) &&
		spawnSkillsMatch(skillRows, p.appearanceInputs.spawnSkills) {
		return p.appearanceSnapshot
	}
	inputs.spawnSkills = peerSpawnSkills(skillRows)
	appearance := buildPeerAppearance(inputs.guild, captured)
	if appearance != nil {
		appearance.StallTitle = inputs.stallTitle
		appearance.StallDecoration = inputs.stallDecor
		appearance.ActionSpeed = inputs.actionSpeed
		appearance.SpawnSkills = inputs.spawnSkills
	}
	p.appearanceInputs = inputs
	p.appearanceSnapshot = appearance
	p.appearancePrimed = true
	return appearance
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
	pvpState  uint8
	eventTeam uint8
	hasEvent  bool
	hasModel  bool
	modelRef  uint32
	name      string
	// displayName is the name other players are shown: the job alias in
	// job mode, else the character name (see capturePeerAppearance).
	displayName   string
	charID        int64
	hasGuild      bool
	guildID       int64
	bodyShapeByte uint8
	visualFlags   uint8
	jobType       uint8
	jobGrade      uint8
	skin          wire.TransformSkin
	worn          []wornEquipRow
	avatars       []wornEquipRow
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
equipment-band rows copy (the client CICUser_DeserializeAppearanceSpawnData equip loop's input); the row
filter matches the emission filter peerAppearance applied before the
capture/build split, so the wire output is unchanged.
==================
*/
func capturePeerAppearance(character *enterworld.Character, resolvedModelRef uint32) peerAppearanceCapture {
	captured := peerAppearanceCapture{
		pvpState:    character.PVPState(),
		eventTeam:   character.EventTeam(),
		hasEvent:    character.EventTeam() != 0xff,
		name:        character.Name,
		displayName: character.Name,
		charID:      character.ID,
		modelRef:    resolvedModelRef,
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
	// A worn job suit shows the job and its grade (job mode), and other
	// players see the job alias in place of the name: the server's spawn
	// tail writes the alias unless the job state is 4, and the name only
	// then. The
	// v1.150 client reads that one string as the peer's name (869E77). Its
	// own board takes the alias only at world entry (client 8675F0 calls vtable
	// +0x74 with +0x1898 when the entry's slot-8 suit sets a job class);
	// the overhead job mark comes from that suit (client 868D00), not from here.
	// Wearing a suit requires an alias (0x9F), so it is never empty here.
	if job := enterworld.DressedJob(character); job != 0 {
		captured.jobType, captured.jobGrade = job, character.Job.Grade
		if character.Job.Alias != "" {
			captured.displayName = character.Job.Alias
		}
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
	// The worn avatar dresses ride the spawn row's avatar loop, which the
	// v1.150 client reads and dresses (86B0C7). INFERENCE: the server fills
	// that loop from the avatar inventory, the only place a worn dress is
	// kept; this server sent it empty, so other players never saw a dress.
	if character.AvatarInventory != nil {
		for _, row := range character.AvatarInventory.Rows {
			if row.RefObjID == 0 {
				continue
			}
			captured.avatars = append(captured.avatars, wornEquipRow{
				slot:      row.Slot,
				refObjID:  row.RefObjID,
				typeFlags: row.TypeFlags,
			})
		}
	}
	return captured
}

/*
==================
peerAppearance

peerAppearance builds the tick-time simulation.PeerAppearance value copy from
the door-captured character fields - every field is REAL persisted state:
the model ref (the CIGIDObject_ResolveRefObj resolve target on the viewer's client), the
character name, the creation body-shape byte (-> CICUser +0x758), the
WORN inventory rows (the equipment-band slots 0..12) in slot order for the
client CICUser_DeserializeAppearanceSpawnData equip loop, and - for guild members - the guild identity the
client CICPlayer_DeserializeSpawnData non-local tail carries (name/id/grant/crestParam, resolved
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
	return buildPeerAppearance(resolvePeerGuild(guilds, unions, divisionID, captured), captured)
}

/*
==================
peerGuildHalf

The guild fields of one peer appearance (name, id, crest, the guild's
union and this member's grant and fortress role), resolved through
the guild door. The zero half is a guildless peer - a non-member or a
dangling FK, which leaves the client's BindGuild leg unreached
exactly like the retail no-guild row.
==================
*/
type peerGuildHalf struct {
	name     string
	id       uint32
	grant    string
	crest    uint32
	alliance uint32
	emblem   uint32
	fortRole uint8
}

/*
==================
resolvePeerGuild

Resolve the guild half of one peer appearance from the captured FK:
the guild identity the client CICPlayer_DeserializeSpawnData
non-local tail carries, through the SAME guild door the 0x32C4 seed
encodes from. Runs OUTSIDE the character read door: guilds.Guild
takes the store lock, which must not be taken inside the door.
==================
*/
func resolvePeerGuild(guilds enterworld.GuildStore, unions *union.Authority, divisionID string, captured peerAppearanceCapture) peerGuildHalf {
	var half peerGuildHalf
	if guilds == nil || !captured.hasGuild {
		return half
	}
	record, members, ok := guilds.Guild(divisionID, captured.guildID)
	if !ok {
		return half
	}
	half.name = record.Name
	half.id = uint32(record.ID)
	half.crest = record.CrestParam
	if alliance, ok := unions.Of(divisionID, record.ID); ok {
		half.alliance, half.emblem = uint32(alliance.AllianceID), alliance.Crest
	}
	for _, member := range members {
		if member.CharID == captured.charID {
			half.grant = member.GrantName
			// The member row's fortress-war role byte rides the
			// spawn guild sub-block's trailing team byte (client
			// CICPlayer_DeserializeSpawnData @0x0086a1b2 -> CICPlayer+0x7e0) - the
			// same persisted byte the 0x32C4 member loop encodes.
			half.fortRole = member.FortressRole
			break
		}
	}
	return half
}

/*
==================
peerAppearanceFingerprint

Everything one built PeerAppearance is a pure function of: the door
capture, the resolved guild half, the stall face, the action speed
and the spawn skills. WorldSnapshot's memo (sessionWorld.appearanceFor)
keeps the last fingerprint beside the appearance it built; the next
snapshot reuses the build while its fingerprint still matches.
==================
*/
type peerAppearanceFingerprint struct {
	captured    peerAppearanceCapture
	guild       peerGuildHalf
	stallTitle  string
	stallDecor  uint32
	actionSpeed float32
	spawnSkills []wire.SpawnSkillEntry
}

/*
==================
peerAppearanceFingerprint.equal

Whether two fingerprints describe the same appearance, spawn skills
aside (spawnSkillsMatch, which compares the unconverted rows): every
scalar compared plainly, the worn and avatar slices by value.
Nothing here allocates.
==================
*/
func (f peerAppearanceFingerprint) equal(other peerAppearanceFingerprint) bool {
	a, b := f.captured, other.captured
	return a.pvpState == b.pvpState && a.eventTeam == b.eventTeam && a.hasEvent == b.hasEvent &&
		a.hasModel == b.hasModel && a.modelRef == b.modelRef && a.name == b.name &&
		a.displayName == b.displayName && a.charID == b.charID && a.hasGuild == b.hasGuild &&
		a.guildID == b.guildID && a.bodyShapeByte == b.bodyShapeByte && a.visualFlags == b.visualFlags &&
		a.jobType == b.jobType && a.jobGrade == b.jobGrade && a.skin == b.skin &&
		slices.Equal(a.worn, b.worn) && slices.Equal(a.avatars, b.avatars) &&
		f.guild == other.guild && f.stallTitle == other.stallTitle && f.stallDecor == other.stallDecor &&
		f.actionSpeed == other.actionSpeed
}

/*
==================
spawnSkillsMatch

Whether the entry rows this snapshot's SpawnSkills hook returned
are the rows the kept fingerprint's converted entries were built
from: the compare runs on the unconverted rows so an unchanged
peer pays no conversion slice per tick. Mirrors peerSpawnSkills
field for field.
==================
*/
func spawnSkillsMatch(rows []enterworld.EntrySkill, built []wire.SpawnSkillEntry) bool {
	if len(rows) != len(built) {
		return false
	}
	for i, row := range rows {
		entry := built[i]
		if entry.SkillID != row.ID || entry.Status != row.Status || entry.HasStatus != row.HasStatus {
			return false
		}
		if row.Token == nil {
			if entry.HasToken {
				return false
			}
		} else if !entry.HasToken || entry.Token != *row.Token {
			return false
		}
	}
	return true
}

/*
==================
buildPeerAppearance

Assemble the PeerAppearance from the resolved faces. The worn and
avatar rows arrive in inventory order and the wire wants slot
order; the sort runs on a copy, because the caller's capture backs
a fingerprint the next tick still compares against.
==================
*/
func buildPeerAppearance(half peerGuildHalf, captured peerAppearanceCapture) *simulation.PeerAppearance {
	if !captured.hasModel {
		return nil
	}
	appearance := &simulation.PeerAppearance{
		PVPState:      captured.pvpState,
		RefObjID:      captured.modelRef,
		Name:          captured.displayName,
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
	appearance.GuildName = half.name
	appearance.GuildID = half.id
	appearance.GuildGrantName = half.grant
	appearance.CrestParam = half.crest
	appearance.AllianceID = half.alliance
	appearance.AllianceCrest = half.emblem
	appearance.FortSiegeAuthority = half.fortRole
	worn := make([]wornEquipRow, len(captured.worn))
	copy(worn, captured.worn)
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
	avatars := make([]wornEquipRow, len(captured.avatars))
	copy(avatars, captured.avatars)
	sort.Slice(avatars, func(i, j int) bool { return avatars[i].slot < avatars[j].slot })
	for _, row := range avatars {
		appearance.Avatars = append(appearance.Avatars, wire.PlayerEquipItem{RefObjID: row.refObjID, TypeFlags: row.typeFlags})
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
