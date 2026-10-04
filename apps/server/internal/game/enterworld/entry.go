package enterworld

import (
	"math"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// Movement modes as the wire and the persisted world state carry them
// (missionCharacterWalkMode / missionCharacterRunMode).
const (
	MovementModeWalk int64 = 2
	MovementModeRun  int64 = 3
)

// StartProfile is the spawn the bootstrap places the local player at:
// region-local coordinates plus the persisted run/walk mode.
type StartProfile struct {
	RegionID     int64   `json:"regionId"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	Z            float64 `json:"z"`
	Angle        int64   `json:"angle"`
	MovementMode int64   `json:"movementMode"`
}

// EntrySkill is one effect in the native local-player entry snapshot.
type EntrySkill struct {
	ID        uint32  `json:"id"`
	Token     *uint32 `json:"token,omitempty"`
	Remaining *uint32 `json:"remaining,omitempty"`
	Status    uint8   `json:"status"`
	// Wire gate is supplied by the same admitted skill record as the row.
	HasStatus bool `json:"-"`
}

func validEntrySkills(rows []EntrySkill) bool {
	if len(rows) > 255 {
		return false
	}
	for _, row := range rows {
		if row.ID == 0 || (row.Token == nil) != (row.Remaining == nil) || (!row.HasStatus && row.Status != 2) {
			return false
		}
	}
	return true
}

// LocalPlayerEntry is the admitted placement and presentation snapshot.
type LocalPlayerEntry struct {
	Population  instance.Lease `json:"-"`
	PVPState    uint8          `json:"pvpState"`
	ArenaTeam   uint8          `json:"arenaTeam"`
	WalkSpeed   float32        `json:"walkSpeed"`
	RunSpeed    float32        `json:"runSpeed"`
	SpawnSkills []EntrySkill   `json:"spawnSkills"`

	// Packed CICPlayer +0x18e4; nil uses the native no-war constructor sentinel.
	FortressWorld *uint32 `json:"fortressWorld,omitempty"`
	// Same authoritative byte as the native full-player entry; never infer it from level in the UI.
	VisualFlags uint8  `json:"visualFlags"`
	RaceKey     string `json:"raceKey"`
	// CountryByte9C is the native RefObjData+0x9c country selector consumed
	// through the CICharactor slot +0x90 effective-record path (0 China,
	// 1 Europe). Shipped explicitly; browser hosts must not invent a
	// male/China-style zero when the record field is absent.
	CountryByte9C int `json:"countryByte9c"`
	// SexSelector1AC is the native RefObjData+0x1ac selector: 0=female,
	// nonzero=male - the INVERSE of this API's character.gender enum.
	SexSelector1AC int `json:"sexSelector1ac"`
	// JobType/JobGrade/JobExp/JobAlias are the local-only job block
	// (CICUser_DeserializeSpawnState @0x00869f89..0x00869fc8): the joined
	// job, its grade and experience, and the alias the job window shows.
	JobType  uint8  `json:"jobType,omitempty"`
	JobGrade uint8  `json:"jobGrade,omitempty"`
	JobExp   uint32 `json:"jobExp,omitempty"`
	JobAlias string `json:"jobAlias,omitempty"`
	// BodyShape is the shape byte (+0x758; height low, volume high) the
	// skin change window starts from.
	BodyShape     *uint8        `json:"bodyShape,omitempty"`
	ModelRef      uint32        `json:"modelRef"`
	CameraHeight  float64       `json:"cameraHeight"`
	VisualLoadout VisualLoadout `json:"visualLoadout"`
	StartProfile  StartProfile  `json:"startProfile"`
	// DungeonFloorIndex is the only dungeon-map value owned by the server.
	// The browser resolves directory, prefix, labels, bounds and tiles from
	// its packed presentation catalogue.
	DungeonFloorIndex *int64 `json:"dungeonFloorIndex,omitempty"`
}

// coerceRunWalkMode ports coerceMissionRunWalkMode: only the two live modes
// pass; anything else takes the fallback.
func coerceRunWalkMode(value *int64, fallback int64) int64 {
	if mode, ok := coerceOptionalInt(value, 0, 0xff); ok {
		if mode == MovementModeWalk || mode == MovementModeRun {
			return mode
		}
	}
	return fallback
}

// WorldState is the slice of missionWorldStateForCharacter the bootstrap
// start profile reads: the SETTLED spawn plus the movement mode. The
// in-flight moveSegment plane (bug D's live interpolation) belongs to the
// movement lane and is not consumed here - bootstrap reads world.spawn
// exactly like the Node side does.
type WorldState struct {
	Spawn        StartProfile
	MovementMode int64
	SpawnSet     bool
}

// WorldStateForCharacter ports the spawn/movementMode half of
// missionWorldStateForCharacter, with the same race-profile fallbacks.
func WorldStateForCharacter(c *Character, raceKey string) WorldState {
	profile := StartProfileForRace(raceKey)
	fallback := StartProfile{
		RegionID: profile.RegionID,
		X:        profile.X,
		Y:        profile.Y,
		Z:        profile.Z,
		Angle:    profile.Angle,
	}
	var world *CharacterWorld
	if c != nil {
		world = c.World
	}
	spawn := fallback
	spawnSet := false
	movementMode := MovementModeRun
	if world != nil {
		spawnSet = world.SpawnSet
		movementMode = coerceRunWalkMode(world.MovementMode, MovementModeRun)
		if world.Spawn != nil {
			spawn = StartProfile{
				RegionID: coerceInt(world.Spawn.RegionID, 0, 0xffff, fallback.RegionID),
				X:        coerceFloat(world.Spawn.X, fallback.X),
				Y:        coerceFloat(world.Spawn.Y, fallback.Y),
				Z:        coerceFloat(world.Spawn.Z, fallback.Z),
				Angle:    coerceInt(world.Spawn.Angle, 0, 0xffff, fallback.Angle),
			}
		}
	}
	// The enter-world plane must ship the CANONICAL frame: a record persisted
	// as enter-region + multi-sector overflow (pre-normalization saves) would
	// otherwise seed the client's terrain residency around the wrong sector
	// and spawn the player in an unloaded void. simulation.NormalizeSpawnFrame
	// is a no-op for in-range spawns and dungeon regions.
	folded := simulation.NormalizeSpawnFrame(simulation.Spawn{
		RegionID: uint16(spawn.RegionID),
		X:        spawn.X,
		Y:        spawn.Y,
		Z:        spawn.Z,
		Angle:    uint16(spawn.Angle),
	})
	spawn.RegionID = int64(folded.RegionID)
	spawn.X = folded.X
	spawn.Y = folded.Y
	spawn.Z = folded.Z
	return WorldState{Spawn: spawn, MovementMode: movementMode, SpawnSet: spawnSet}
}

func coerceFloat(value *float64, fallback float64) float64 {
	if value == nil || *value != *value {
		return fallback
	}
	return *value
}

// CharacterAppearanceIdentity is the one resolved body identity shared by
// gameplay, account-roster presentation, and model-ref consumers. Persisted
// creation fields are provenance; a verified roster row is the render/runtime
// authority.
type CharacterAppearanceIdentity struct {
	ModelRef      uint32
	ModelCodename string
	RaceKey       string
	Gender        int64
}

// ResolveCharacterAppearanceIdentity chooses one roster row atomically. A
// valid explicit model ref wins, then a valid codename, then the race/gender
// default. Invalid stale fields never get spliced together across rows.
func ResolveCharacterAppearanceIdentity(character *Character, roster *Roster) CharacterAppearanceIdentity {
	fallbackRace := ResolveCharacterRaceKey(character)
	fallbackGender := ResolveCharacterGenderIndex(character)

	var model *RosterModel
	if character != nil {
		if explicitRef, ok := coerceOptionalInt(character.ModelRef, 1, 0xffffffff); ok {
			model = roster.ModelByRefObjID(uint32(explicitRef))
		}
		if model == nil {
			model = roster.ModelByCodename(character.ModelCodename)
		}
	}
	if model == nil {
		model = roster.ModelByRefObjID(DefaultModelRefForRaceGender(fallbackRace, fallbackGender))
	}

	modelRef := DefaultModelRefForRaceGender(fallbackRace, fallbackGender)
	modelCodename := ""
	if model != nil {
		modelRef = model.RefObjID
		modelCodename = model.Codename
	} else if character != nil {
		modelCodename = character.ModelCodename
	}

	canonicalCharacter := characterWithModelCodename(character, modelCodename)
	return CharacterAppearanceIdentity{
		ModelRef:      modelRef,
		ModelCodename: modelCodename,
		RaceKey:       ResolveCharacterRaceKey(canonicalCharacter),
		Gender:        ResolveCharacterGenderIndex(canonicalCharacter),
	}
}

func characterWithModelCodename(character *Character, modelCodename string) *Character {
	if character == nil {
		return &Character{ModelCodename: modelCodename}
	}
	canonical := *character
	canonical.ModelCodename = modelCodename
	return &canonical
}

// ResolveLocalPlayerEntry ports resolveMissionLocalPlayerEntry.
func ResolveLocalPlayerEntry(character *Character, roster *Roster) LocalPlayerEntry {
	identity := ResolveCharacterAppearanceIdentity(character, roster)
	canonicalCharacter := characterWithModelCodename(character, identity.ModelCodename)
	world := WorldStateForCharacter(canonicalCharacter, identity.RaceKey)
	visualLoadout := ResolveVisualLoadout(canonicalCharacter, roster, identity.ModelRef)

	countryByte9c := NativeCountryByte9C(canonicalCharacter)
	sexSelector1ac := NativeSexSelector1AC(canonicalCharacter)
	startProfile := world.Spawn
	startProfile.MovementMode = world.MovementMode
	var dungeonFloorIndex *int64
	if character.World != nil {
		dungeonFloorIndex = character.World.DungeonFloorIndex
	}

	packedWorld := domain.CharacterWorldInstance(character)
	return LocalPlayerEntry{
		FortressWorld:     &packedWorld,
		PVPState:          character.PVPState(),
		ArenaTeam:         character.EventTeam(),
		RaceKey:           identity.RaceKey,
		VisualFlags:       ResolveVisualFlags(character),
		CountryByte9C:     countryByte9c,
		SexSelector1AC:    sexSelector1ac,
		JobType:           character.Job.Type,
		JobGrade:          character.Job.Grade,
		JobExp:            character.Job.Exp,
		JobAlias:          character.Job.Alias,
		BodyShape:         entryBodyShape(character),
		ModelRef:          identity.ModelRef,
		CameraHeight:      ResolveCharacterCameraHeight(character),
		VisualLoadout:     visualLoadout,
		StartProfile:      startProfile,
		DungeonFloorIndex: dungeonFloorIndex,
	}
}

// entryBodyShape is the persisted shape byte, or nil when none is set.
func entryBodyShape(c *Character) *uint8 {
	if c == nil || c.BodyShapeByte == nil || *c.BodyShapeByte < 0 || *c.BodyShapeByte > 0xff {
		return nil
	}
	shape := uint8(*c.BodyShapeByte)
	return &shape
}

// spawnUndergroundToleranceUnits absorbs the difference between the server's
// bilinear height sample and the client's triangle-split sampler (sub-unit
// on the 20u grid) plus ordinary float drift. Only a spawn CLEARLY below the
// surface lifts; a legit ground-stander is untouched.
const spawnUndergroundToleranceUnits = 2.0

// LiftSpawnAboveTerrain raises an enter-world spawn onto the terrain surface
// when the persisted height sits below it. Heights are client-pick authored
// and never recomputed server-side, so a record saved before goal-frame
// normalization can carry the height of a region sectors away - re-entering
// placed the player INSIDE the mountain at the (now correctly folded) x/z.
//
// Only lifts when no nearer object surface explains the saved stand. A deck
// can lie below the heightmap. Never lowers; dungeon regions are exempt.
// Returns whether the spawn was lifted.
func LiftSpawnAboveTerrain(entry *LocalPlayerEntry, heightAt func(regionID uint16, x, z float64) (float64, bool), surfaceAt func(regionID uint16, x, authoredY, z float64) (float64, bool)) bool {
	if entry == nil || heightAt == nil {
		return false
	}
	profile := &entry.StartProfile
	regionID := uint16(profile.RegionID)
	if simulation.IsDungeonRegion(regionID) {
		return false
	}
	terrainY, ok := heightAt(regionID, profile.X, profile.Z)
	if !ok {
		return false
	}
	if profile.Y >= terrainY-spawnUndergroundToleranceUnits {
		return false
	}
	// 403D20 arbitrates terrain and object cells against the incoming Y.
	// A stair can be BELOW the heightmap. Terrain-only rescue corrupts a
	// valid saved deck stand and loses its layer before movement even starts.
	if surfaceAt != nil {
		if surfaceY, covered := surfaceAt(regionID, profile.X, profile.Y, profile.Z); covered &&
			math.Abs(surfaceY-profile.Y) < math.Abs(terrainY-profile.Y) {
			return false
		}
	}
	profile.Y = terrainY
	return true
}

// RescueStrandedSpawn is the enter-world unstuck: a spawn on a blocked
// navmesh tile, or on a walkable ISLAND too small to be the real playfield
// (the frame-bug incident's mountain plateau - movement worked but every
// path out clipped at the island edge), strands the player. When the
// relocator reports the point stranded, the entry takes the nearest
// mainland rescue point, or falls back to the race start profile when no
// rescue exists within the search radius. Dungeon regions are exempt (no
// outdoor walkability plane). Returns whether the spawn was changed.
func RescueStrandedSpawn(entry *LocalPlayerEntry, relocate func(simulation.Spawn) (simulation.Spawn, bool, bool)) bool {
	if entry == nil || relocate == nil {
		return false
	}
	profile := &entry.StartProfile
	regionID := uint16(profile.RegionID)
	if simulation.IsDungeonRegion(regionID) {
		return false
	}
	rescued, stranded, rescueFound := relocate(simulation.Spawn{
		RegionID: regionID,
		X:        profile.X,
		Y:        profile.Y,
		Z:        profile.Z,
		Angle:    uint16(profile.Angle),
	})
	if !stranded {
		return false
	}
	if !rescueFound {
		// Nothing walkable nearby: the race start profile is the one
		// placement guaranteed legal for every character.
		start := StartProfileForRace(entry.RaceKey)
		profile.RegionID = start.RegionID
		profile.X = start.X
		profile.Y = start.Y
		profile.Z = start.Z
		profile.Angle = start.Angle
		return true
	}
	profile.RegionID = int64(rescued.RegionID)
	profile.X = rescued.X
	profile.Y = rescued.Y
	profile.Z = rescued.Z
	return true
}

// CharacterModelRef resolves a character's model refObjId exactly like
// the local-player entry does (explicit ModelRef, then the roster
// codename row, then the race/gender start-profile fallback). The letter
// lane stamps it on outgoing mail as senderModelRefId - the client's
// CIFLetterSlot race mark and CIFLetterRead portrait resolve from it
// (sub_822d10 record +0x1c).
func CharacterModelRef(character *Character, roster *Roster) uint32 {
	return ResolveCharacterAppearanceIdentity(character, roster).ModelRef
}

// One value feeds both the web projection and native entry tail. Zero is a
// valid explicit packed value; it is not confused with an absent assignment.
func entryFortressWorld(entry *LocalPlayerEntry) uint32 {
	if entry != nil && entry.FortressWorld != nil {
		return *entry.FortressWorld
	}
	return 0x10001
}
