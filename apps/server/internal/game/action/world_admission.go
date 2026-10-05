package action

import (
	"fmt"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

// The authenticated transport owns a membership in one allocated lifetime.
// A persisted packed ID is only an admission request, never a live lease.
type populationAdmission struct {
	session        uint64
	gid            uint32
	lease          instance.Lease
	division, name string
	region         uint16
	regionBound    bool
}

func (rt *Runtime) admitPopulationSession(division, name string, session uint64) error {
	c := rt.characterSnapshot(division, rt.findCharacter(division, name))
	if c == nil || session == 0 || rt.Monsters == nil {
		return fmt.Errorf("missing actor, session or population authority")
	}
	rt.Monsters.StartDivision(division)
	id := instance.ID(domain.CharacterWorldInstance(c))
	lease, exists := rt.Monsters.PopulationLease(division, id)
	if !exists {
		return fmt.Errorf("world %08x is not allocated", uint32(id))
	}
	key := simulation.WorldKey(division, name)
	gid := enterworld.ObjectIDForCharacter(c)
	if previous, exists := rt.characterAdmissions.Load(key); exists {
		owner := previous.(populationAdmission)
		if owner.lease != lease || owner.gid != gid {
			return fmt.Errorf("world transfer requires retiring the previous membership")
		}
		// Replacing the socket does not insert a second PC or recheck capacity.
		owner.session = session
		rt.characterAdmissions.Store(key, owner)
		return nil
	}
	if status := rt.Monsters.AdmitPopulationPC(division, lease, gid, c.GMPrivilege); status != instance.Success {
		return fmt.Errorf("world %08x refused membership (native status %d)", uint32(id), status)
	}
	rt.characterAdmissions.Store(key, populationAdmission{session: session, gid: gid, lease: lease, division: division, name: name})
	rt.bindResidentRegion(key, rt.Now().UnixMilli())
	return nil
}

/*
================
transferPopulationSession

Moves an admitted session's membership into another world's live layer:
the population half of the PC world teleport (CGObjPC vtable +0x378, called
by 4F2B50). The new membership is taken first so a full layer refuses
without leaving the old one. Returns the previous admission for rollback.
The caller holds the division lock.
================
*/
func (rt *Runtime) transferPopulationSession(division, name string, destination instance.ID, capacityBypass bool) (populationAdmission, instance.Status) {
	key := simulation.WorldKey(division, name)
	value, exists := rt.characterAdmissions.Load(key)
	if !exists || rt.Monsters == nil {
		return populationAdmission{}, instance.NotMember
	}
	owner := value.(populationAdmission)
	if owner.lease.ID == destination {
		return owner, instance.Success
	}
	lease, open := rt.Monsters.PopulationLease(division, destination)
	if !open {
		return owner, instance.MissingLayer
	}
	if status := rt.Monsters.AdmitPopulationPC(division, lease, owner.gid, capacityBypass); status != instance.Success {
		return owner, status
	}
	rt.Monsters.LeavePopulationPC(division, owner.lease, owner.gid)
	moved := owner
	moved.lease, moved.regionBound = lease, false
	rt.characterAdmissions.Store(key, moved)
	return owner, instance.Success
}

/*
================
restorePopulationSession

Undoes transferPopulationSession when the re-entry that follows it fails.
================
*/
func (rt *Runtime) restorePopulationSession(previous populationAdmission) {
	key := simulation.WorldKey(previous.division, previous.name)
	value, exists := rt.characterAdmissions.Load(key)
	if !exists {
		return
	}
	current := value.(populationAdmission)
	if current.lease == previous.lease {
		return
	}
	// The old layer is permanent or still holds the departing PC's slot
	// request; readmission bypasses capacity because the PC never left it
	// from the player's point of view.
	if rt.Monsters.AdmitPopulationPC(previous.division, previous.lease, previous.gid, true) != instance.Success {
		return
	}
	rt.Monsters.LeavePopulationPC(current.division, current.lease, current.gid)
	rt.characterAdmissions.Store(key, previous)
}

func (rt *Runtime) leavePopulationSession(division, name string) {
	previous, exists := rt.characterAdmissions.LoadAndDelete(simulation.WorldKey(division, name))
	if !exists || rt.Monsters == nil {
		return
	}
	owner := previous.(populationAdmission)
	rt.Monsters.LeavePopulationPC(division, owner.lease, owner.gid)
}

func (rt *Runtime) CharacterPopulationLease(division, name string, session uint64) (instance.Lease, bool) {
	value, exists := rt.characterAdmissions.Load(simulation.WorldKey(division, name))
	if !exists {
		return instance.Lease{}, false
	}
	owner := value.(populationAdmission)
	if owner.session != session || rt.Monsters == nil {
		return instance.Lease{}, false
	}
	current, exists := rt.Monsters.PopulationLease(division, owner.lease.ID)
	return owner.lease, exists && current == owner.lease
}

func (rt *Runtime) EntryPopulationLease(division, name string) (instance.Lease, bool) {
	value, exists := rt.characterAdmissions.Load(simulation.WorldKey(division, name))
	if !exists {
		return instance.Lease{}, false
	}
	owner := value.(populationAdmission)
	return rt.CharacterPopulationLease(division, name, owner.session)
}
