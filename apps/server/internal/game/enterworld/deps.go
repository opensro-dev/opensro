/*
===========================================================================

deps.go - bootstrap dependencies and authority composition

===========================================================================
*/
package enterworld

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"strings"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// StaticCharacterSource is an in-memory CharacterSource.
/*
================
StaticCharacterSource
================
*/
type StaticCharacterSource map[string][]*Character

// CharactersForDivision returns the division's characters.
/*
================
CharactersForDivision
================
*/
func (s StaticCharacterSource) CharactersForDivision(divisionID string) []*Character {
	return s[divisionID]
}

// Deps owns bootstrap composition. Other gameplay packages consume narrow,
// package-owned ports rather than retaining this concrete type.
/*
================
Deps
================
*/
type Deps struct {
	BrowserReferences *BrowserReferences
	Roster            *Roster
	Characters        CharacterSource

	Items             ItemRefSource
	Levels            LevelDataSource
	Skills            SkillDataSource
	MagicOptions      MagicOptionSource
	EquipItemsEnabled bool
	// StarterKit is the beta starter kit (starterkit.go); empty when disabled.
	StarterKit []WireItem
	// MasteryTotalOverride is shared with progression; zero means native.
	MasteryTotalOverride int64
	// StarterRefills are the beta's HP/MP potion refills (starterrefill.go);
	// empty when the kit is disabled.
	StarterRefills []StarterRefill
	// NpcSpawns is the shared static-world NPC policy consumed by bootstrap,
	// selection and the simulation ticker. One composition-owned value prevents
	// the old env/default roster split from creating objects no action lane
	// could resolve.
	NpcSpawns NpcSpawnConfig

	RestoreEntryEffects  func(divisionID, characterName string)
	PrepareEntry         func(divisionID, characterName string) error
	EntryPopulationLease func(divisionID, characterName string) (instance.Lease, bool)
	// Pure quest projection on the detached entry snapshot; never grants items.
	NormalizeEntryQuests         func(character *Character) error
	AdmitCharacterSession        func(divisionID, characterName string, session uint64) error
	RetireCharacterSession       func(divisionID, characterName string, session uint64)
	EntryActionSpeed             func(divisionID, characterName string) float32
	EntryCompanionActionSpeed    func(division string, character *Character, pet *CharacterCOS) float32
	EntryCompanionMovementSpeeds func(division string, character *Character, pet *CharacterCOS) (float32, float32)
	EntryMovementSpeeds          func(divisionID, characterName string) (float32, float32)
	EntryCompanionSpawn          func(string, *Character, *CharacterCOS) simulation.Spawn
	EntrySkills                  func(divisionID, characterName string) []EntrySkill
	ObjectListRows               func(divisionID string, character *Character, entry *LocalPlayerEntry) []Packet
	MonsterState                 *simulation.MonsterState
	RefObjSnapshot               func() []RefObjRow
	// ExtraRefItemCodenames names the division-dependent item references a
	// login carries (the ground). StaticRefItemCodenames names the fixed set
	// every viewer needs; it is published once in BrowserReferences.
	ExtraRefItemCodenames  func(divisionID string) []string
	StaticRefItemCodenames func() []string
	// Now is the bootstrap's clock. The server wiring points it at the action
	// runtime's clock so both agree on every pet-skill deadline; nil falls
	// back to the wall clock.
	Now func() time.Time
	// TrackTimedWindows hands a character with pet-skill windows to the action
	// runtime's tick sweep, which alone retires spent rows with the native
	// zero pair. Without it a window re-raised at world entry would sit at
	// zero on the client: CIFMagicStateBoard_OnUpdate (6E6AA0) does not retire
	// a kind-3 pet-skill window solely because its timer reached zero.
	TrackTimedWindows func(divisionID, characterName string)
	// ExtraMagicOptionIDs names options live item producers can create after
	// bootstrap. Their definitions must precede the first result body.
	ExtraMagicOptionIDs func() []uint32
	// AvatarMagicOptions are the options the smith may grant each avatar
	// part (magicoptionassign.txt), which the grant window lists.
	AvatarMagicOptions    func() []AvatarMagicOptionRow
	SpawnTerrainHeight    func(regionID uint16, x, z float64) (float64, bool)
	SpawnSurfaceHeight    func(regionID uint16, x, authoredY, z float64) (float64, bool)
	RelocateStrandedSpawn func(spawn simulation.Spawn) (
		result simulation.Spawn,
		stranded bool,
		rescueFound bool,
	)
	CanEnterWorldRegion func(character *Character, regionID uint16) bool
	PlayerBaseStats     func(character *Character) (wire.BaseStats, error)

	MutateCharacter  func(character *Character, label string, mutate func())
	MutateCharacters func(characters []*Character, label string, mutate func())
	UpdateCharacter  func(character *Character, label string, update func() bool) bool
	UpdateCharacters func(characters []*Character, label string, update func() bool) bool
	UpdateTrade      func(characters []*Character, label string, update func(*domain.TradeRewardPool) bool) bool
	ReadCharacter    func(divisionID string, read func())

	SystemMessages    func(character *Character) interface{}
	ResolveDivisionID func(requestDivisionID string) string
	OnWorldBound      func(
		session *transport.Session,
		divisionID string,
		character *Character,
	)
	// Immutable references consumed by scene-local client owners must be
	// republished after every bootstrap, including same-session travel.
	SceneReferenceFrames   func() []wire.Frame
	CommunitySeedFramesFor func(
		divisionID string,
		character *Character,
	) []Packet

	Letters       LetterStore
	Guilds        GuildStore
	GuildWars     domain.GuildWarStore
	TrainingCamps TrainingCampStore
	// Fortresses keeps fortress occupation and the war's requests.
	Fortresses FortressStore
	// Alliances keeps the guild unions.
	Alliances AllianceStore
}

// NpcSpawnPolicy exposes the composition-owned static NPC world through a
// narrow optional port. Itemops type-asserts this method so detached test
// dependencies do not need to manufacture world data.
/*
================
NpcSpawnConfig
================
*/
func (d *Deps) NpcSpawnPolicy() NpcSpawnConfig {
	if d == nil {
		return NpcSpawnConfig{}
	}
	return d.NpcSpawns
}

// Mutate routes an accepted single-character mutation through its commit
// door. Detached unit compositions execute in memory.
/*
================
Mutate
================
*/
func (d *Deps) Mutate(character *Character, label string, mutate func()) {
	if d.MutateCharacter != nil {
		d.MutateCharacter(character, label, mutate)
		return
	}
	mutate()
}

// Update routes a validate-and-change operation through the conditional
// character commit door. A false callback result is a refusal: no dirty state
// and no database transaction are produced.
/*
================
bool
================
*/
func (d *Deps) Update(character *Character, label string, update func() bool) bool {
	if d.UpdateCharacter != nil {
		return d.UpdateCharacter(character, label, update)
	}
	if update == nil {
		return false
	}
	// A detached composition may provide only the unconditional commit seam.
	if d.MutateCharacter != nil {
		changed := false
		d.MutateCharacter(character, label, func() {
			changed = update()
		})
		return changed
	}
	return update()
}

// MutateMany routes a multi-character invariant through one atomic door.
/*
================
MutateMany
================
*/
func (d *Deps) MutateMany(characters []*Character, label string, mutate func()) {
	if d.MutateCharacters != nil {
		d.MutateCharacters(characters, label, mutate)
		return
	}
	mutate()
}

// UpdateMany routes a conditional multi-character invariant through one
// authority transaction.
/*
================
bool
================
*/
func (d *Deps) UpdateMany(characters []*Character, label string, update func() bool) bool {
	if d.UpdateCharacters != nil {
		return d.UpdateCharacters(characters, label, update)
	}
	if update == nil {
		return false
	}
	if d.MutateCharacters != nil {
		changed := false
		d.MutateCharacters(characters, label, func() {
			changed = update()
		})
		return changed
	}
	return update()
}

/*
================
SettleTrade

Production supplies the shard pool transaction. The in-memory fallback is
for isolated gameplay fixtures, which do not run a durable authority.
================
*/
func (d *Deps) SettleTrade(characters []*Character, label string, update func(*domain.TradeRewardPool) bool) bool {
	if d.UpdateTrade != nil {
		return d.UpdateTrade(characters, label, update)
	}
	return d.UpdateMany(characters, label, func() bool {
		pool := domain.TradeRewardPool{}
		return update(&pool)
	})
}

// Read routes mutable character reads through the authority read door.
/*
================
Read
================
*/
func (d *Deps) Read(divisionID string, read func()) {
	if d.ReadCharacter != nil {
		d.ReadCharacter(divisionID, read)
		return
	}
	read()
}

// ReentryPackets rebuilds the native reset/character/object-list
// sequence for an already-bound mission session. Teleport-style operations
// use this after their authoritative world mutation so the client enters the
// same loading transition as a retail region reset instead of receiving only
// an in-world position correction.
/*
================
ReentryPackets
================
*/
func (d *Deps) ReentryPackets(divisionID, characterName string) ([]Packet, bool) {
	projection := *d
	projection.RestoreEntryEffects = nil // Caller already owns the live actor transaction.
	projection.PrepareEntry = nil
	result := Build(&projection, BootstrapRequest{
		DivisionID:    divisionID,
		CharacterName: characterName,
	})
	return d.encodeReentry(result)
}

// PreparedReentry couples the packets to the resolved placement they encode.
// The action owner commits this placement only after preparation succeeds.
/*
================
PreparedReentry
================
*/
type PreparedReentry struct {
	Packets []Packet
	Spawn   simulation.Spawn
}

/*
================
PrepareReentry
================
*/
func (d *Deps) PrepareReentry(divisionID string, character *Character) (PreparedReentry, bool) {
	if character == nil || character.DeletePending || character.MissionInventory == nil {
		return PreparedReentry{}, false
	}
	snapshot := character.Snapshot()
	if d.NormalizeEntryQuests != nil {
		if err := d.NormalizeEntryQuests(snapshot); err != nil {
			return PreparedReentry{}, false
		}
	}
	result := buildCharacterProjection(d, divisionID, snapshot)
	packets, ok := d.encodeReentry(result)
	if !ok {
		return PreparedReentry{}, false
	}
	spawn := result.LocalPlayerEntry.StartProfile
	return PreparedReentry{Packets: packets, Spawn: simulation.Spawn{
		RegionID: uint16(spawn.RegionID), X: spawn.X, Y: spawn.Y, Z: spawn.Z, Angle: uint16(spawn.Angle),
	}}, true
}

/*
================
encodeReentry
================
*/
func (d *Deps) encodeReentry(result *BootstrapResult) ([]Packet, bool) {
	if result == nil || result.NativeResult != nativeResultSuccess {
		return nil, false
	}
	if len(result.Packets) == 0 || result.Packets[0].NativeOpcode != OpcodeResetClient {
		return nil, false
	}
	payload, err := browserEntryPayload(result, d.BrowserReferences)
	if err != nil {
		return nil, false
	}
	// Reset opens the existing session's re-entry admission. Replace the browser
	// projection before its GID latch: retaining the login DTO here resurrects
	// stale HP and coordinates even though the native stream is already fresh.
	packets := make([]Packet, 0, len(result.Packets)+1)
	packets = append(packets, result.Packets[0], NewPacket(transport.OpEnterWorldResult, payload))
	packets = append(packets, result.Packets[1:]...)
	return packets, true
}

// Validate rejects incomplete production composition before gameplay starts.
/*
================
error
================
*/
func (d *Deps) Validate() error {
	var missing []string
	require := func(name string, absent bool) {
		if absent {
			missing = append(missing, name)
		}
	}

	require("Roster", d.Roster == nil)
	require("Characters", d.Characters == nil)
	require("ResolveDivisionID", d.ResolveDivisionID == nil)
	require("MutateCharacter", d.MutateCharacter == nil)
	require("MutateCharacters", d.MutateCharacters == nil)
	require("UpdateCharacter", d.UpdateCharacter == nil)
	require("UpdateCharacters", d.UpdateCharacters == nil)
	require("UpdateTrade", d.UpdateTrade == nil)
	require("ReadCharacter", d.ReadCharacter == nil)
	require("Letters", d.Letters == nil)
	require("Guilds", d.Guilds == nil)
	require("TrainingCamps", d.TrainingCamps == nil)
	require("ObjectListRows", d.ObjectListRows == nil)
	require("ExtraRefItemCodenames", d.ExtraRefItemCodenames == nil)
	require("StaticRefItemCodenames", d.StaticRefItemCodenames == nil)
	require("TrackTimedWindows", d.TrackTimedWindows == nil)
	require("SpawnTerrainHeight", d.SpawnTerrainHeight == nil)
	require("SpawnSurfaceHeight", d.SpawnSurfaceHeight == nil)
	require("RelocateStrandedSpawn", d.RelocateStrandedSpawn == nil)
	require("CanEnterWorldRegion", d.CanEnterWorldRegion == nil)
	require("PlayerBaseStats", d.PlayerBaseStats == nil)
	require("CommunitySeedFramesFor", d.CommunitySeedFramesFor == nil)
	require("OnWorldBound", d.OnWorldBound == nil)
	require("EntryCompanionSpawn", d.EntryCompanionSpawn == nil)
	require("SceneReferenceFrames", d.SceneReferenceFrames == nil)

	if len(missing) > 0 {
		return fmt.Errorf(
			"required field(s) nil before gameplay start: %s",
			strings.Join(missing, ", "),
		)
	}
	return nil
}
