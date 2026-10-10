/*
===========================================================================

operator.go - operator diagnostics and recovery through character authority

Recovery runs under an exclusive transport control lease supplied by the
composition root. It uses authored town destinations and ordinary persistence;
it never grants GM privileges, spends items, revives a corpse, or edits SQL.
The stat reset is the one that changes a character's build: it keeps every
earned point and only returns the spent ones to the free pool.

===========================================================================
*/
package action

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
OperatorTown
================
*/
type OperatorTown struct {
	ID       uint32           `json:"id"`
	Code     string           `json:"code"`
	Position simulation.Spawn `json:"position"`
}

/*
================
OperatorTowns

Only authored outdoor recall gates qualify, matching appointedRebirthPoint.
The portal building flag includes city gates; it does not mean an indoor region.
Client coordinates are never input.
================
*/
func (rt *Runtime) OperatorTowns() []OperatorTown {
	rows := []OperatorTown{}
	if rt.portals == nil {
		return rows
	}
	for _, destination := range rt.portals.destinations {
		if destination.recall && destination.ref != 0 && destination.spawn.RegionID != 0 && !simulation.IsDungeonRegion(destination.spawn.RegionID) {
			rows = append(rows, OperatorTown{destination.id, destination.code, destination.spawn})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

/*
================
OperatorCharacter

Explicit projection excludes account IDs, authentication material and chat.
================
*/
func (rt *Runtime) OperatorCharacter(division, name string) (map[string]any, error) {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.characterSnapshot(division, rt.findCharacter(division, name))
	if c == nil {
		return nil, fmt.Errorf("character not found")
	}
	world := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
	return map[string]any{"id": c.ID, "name": c.Name, "level": c.Level, "hp": c.CurrentHP, "mp": c.CurrentMP,
		"savedWorld": c.World, "liveWorld": world, "teleportMode": c.NativeTeleportMode,
		"bodyStatus": c.NativeBodyStatus, "companions": c.Companions(), "inventory": c.MissionInventory,
		"pk": c.PK, "pvpState": c.PVPState(), "aggressions": c.Aggressions,
		"strength": domain.CharacterStrength(c), "intellect": domain.CharacterIntellect(c), "statPoints": c.StatPoints}, nil
}

/*
================
OperatorClearPK

Operator recovery is port-only, not native, and disabled by default unless
the dedicated operator credential is configured. The caller holds the
character's transport control lease.
Daily and total history remain intact, including the daily attack cap. Repair
and relief share one authority update so no intermediate red state is visible.
The normal penalty owner supplies the keeper deadline; no live frames are sent
to the evicted session. Entry publishes the resulting state on reconnect.
================
*/
func (rt *Runtime) OperatorClearPK(division, name string) error {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if !rt.deps.Update(c, "operator-clear-pk", func() bool {
		if c.DeletePending {
			return false
		}
		if c.PK != nil {
			now := rt.Now()
			pk.RepairKeeper(c, now)
			pk.AddPenalty(c, -pk.MaxPenalty, now)
		}
		c.Aggressions = nil
		return true
	}) {
		return fmt.Errorf("character PK clear refused")
	}
	rt.aggressionActors.Delete(simulation.WorldKey(division, c.Name))
	rt.notePKRecord(division, c)
	return nil
}

/*
================
OperatorResetStats

Port-only, not native: the operator's stat reset, under the same disabled-by-
default credential and transport control lease as the other recoveries.
STR and INT return to their value at the character's level with nothing spent
(progression.BaseStatAtLevel); every point above that comes back as a free
stat point, so the total is kept and nothing earned is lost, including points
kept through a level-down. A lower maximum trims the stored current HP/MP as
any other maximum drop does (4E3294); entry publishes the result on reconnect.
================
*/
func (rt *Runtime) OperatorResetStats(division, name string) error {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if !rt.deps.Update(c, "operator-reset-stats", func() bool {
		if c.DeletePending || c.Level == nil {
			return false
		}
		base := progression.BaseStatAtLevel(*c.Level)
		points := int64(0)
		if c.StatPoints != nil && *c.StatPoints > 0 {
			points = *c.StatPoints
		}
		free := domain.CharacterStrength(c) + domain.CharacterIntellect(c) + points - 2*base
		if free < 0 {
			// Below the level's own base: an inconsistent record, not one to "reset".
			return false
		}
		strength, intellect := base, base
		c.Strength, c.Intellect, c.StatPoints = &strength, &intellect, &free
		rt.clampStoredGaugeToKeeper(division, c)
		return true
	}) {
		return fmt.Errorf("character stat reset refused")
	}
	return nil
}

/*
================
OperatorRescue

The caller must hold the character's transport control lease. Session teardown
retires combat, trade, movement and companion runtime through their owners.
The next login rebuilds companions at the rescued position, including mounts.
================
*/
func (rt *Runtime) OperatorRescue(division, name string, townID uint32) error {
	unlock := rt.lockDivision(division)
	defer unlock()
	var destination simulation.Spawn
	found := false
	for _, town := range rt.OperatorTowns() {
		if town.ID == townID {
			destination, found = town.Position, true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown rescue town")
	}
	c := rt.findCharacter(division, name)
	if c == nil {
		return fmt.Errorf("character not found")
	}
	if rt.characterSnapshot(division, c).DeletePending {
		return fmt.Errorf("character is pending deletion")
	}
	// Retire the same owners even for an offline character with stale runtime.
	rt.forgetCharacterLocked(division, c.Name)
	if !rt.deps.Update(c, "operator-rescue", func() bool {
		if c.DeletePending {
			return false
		}
		world := simulation.SeedWorldState(c)
		world.Spawn = destination
		world.MoveSegment = nil
		world.Sitting = false
		world.PostureTransitionUntilMs = 0
		world.SpawnSet = true
		world.MovementSourceSeeded = true
		writeBackWorld(c, world)
		c.World.MoveSegment = nil
		c.World.AuthoredAreaReturn = nil
		c.World.DungeonFloorIndex = nil
		c.World.PackedInstance = nil
		c.World.SavedReturn = &domain.SavedReturnLocation{Definition: 1, RegionID: destination.RegionID,
			X: float32(destination.X), Y: float32(destination.Y), Z: float32(destination.Z)}
		c.NativeTeleportMode = 0
		return true
	}) {
		return fmt.Errorf("character rescue refused")
	}
	return nil
}
