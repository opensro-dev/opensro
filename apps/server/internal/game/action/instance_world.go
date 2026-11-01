/*
===========================================================================

instance_world.go - instanced worlds: layers on demand, transfers, returns

One owner for the lifetime of an instanced (type-1) world layer above the
population registry: OpenInstanceWorld allocates a layer and tracks it,
TransferCharacterToInstance records a return anchor and relocates one
character into the layer, ReturnCharacterFromInstance brings a character
back to its anchor, CloseInstanceWorld evacuates a layer and releases it,
and tickInstanceWorlds closes tracked layers whose deadline passed and
releases retiring populations once they are empty of characters.

The population registry (world/simulation) owns layers, membership and
monster state; this lane owns which layers a programme opened and when
they end. Character movement is relocation.go's: a transfer is a
relocation whose destination world is the instance layer, so poses, pets
and re-entry frames behave exactly like a GM warp, and the anchor rides
the same character record the relocation already rewrites.

The return anchor (domain.InstanceReturnAnchor, schema 29) is written in
its own committed update before the relocation runs and cleared again if
the move fails, so a stored anchor always names a character that actually
entered. A present anchor is also the refusal for a second transfer:
programmes do not nest, and the original return point is never
overwritten.

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
InstanceOpenRequest

One demand for an instanced world layer. DeadlineMs is absolute on the
runtime clock (the millisecond domain TickHook receives); zero means the
layer ends only when its owner closes it.
================
*/
type InstanceOpenRequest struct {
	Division   string
	Definition instance.DefinitionID
	DeadlineMs int64
}

/*
================
InstanceTransferResult

The outcome of one character's party transfer. A party transfer has no
single status: every member is answered individually, so a member who
stays behind never hides inside a party-wide success.
================
*/
type InstanceTransferResult struct {
	Gid    uint32
	Name   string // empty when the member is not online to be named
	Status instance.Status
}

/*
================
trackedInstanceWorld

A layer OpenInstanceWorld opened and this lane still owns. The lease pins
the layer generation: a released and reallocated layer with the same ID
is a different world, and the stale record can neither close nor release
it. Tracking is process-local, like the leases themselves.
================
*/
type trackedInstanceWorld struct {
	lease      instance.Lease
	deadlineMs int64
}

/*
================
instanceWorldKey
================
*/
type instanceWorldKey struct {
	division string
	id       instance.ID
}

/*
================
OpenInstanceWorld

Opens one layer of an instanced definition on demand and tracks it until
it is released. Permanent (type-0) definitions are opened at boot, never
here. Layers are a finite authored resource: when every layer is in use
the answer is AllocationFull and the caller decides what its programme
does with a refused opening.
================
*/
func (rt *Runtime) OpenInstanceWorld(req InstanceOpenRequest) (instance.Lease, instance.Status) {
	if rt.Monsters == nil {
		return instance.Lease{}, instance.MissingWorld
	}
	definition, ok := instance.Lookup(req.Definition)
	if !ok {
		return instance.Lease{}, instance.MissingWorld
	}
	if definition.NativeType == 0 {
		return instance.Lease{}, instance.InvalidLayer
	}
	lease, opened := rt.Monsters.OpenInstanceLayer(req.Division, req.Definition)
	if !opened {
		return instance.Lease{}, instance.AllocationFull
	}
	rt.instanceWorlds.Store(instanceWorldKey{division: req.Division, id: lease.ID},
		trackedInstanceWorld{lease: lease, deadlineMs: req.DeadlineMs})
	rt.instanceEvent(InstanceEventSetup, req.Division, req.Definition, lease.ID, nil, false)
	return lease, instance.Success
}

/*
================
TransferCharacterToInstance

Moves one admitted character into an opened instance layer at entry: the
character's current world and live pose are recorded as the return anchor
first, then the relocation runs. Capacity is honoured exactly (no
bypass): a full layer answers PlayerLimitReached and the character does
not move. A character already inside a programme (anchor present, or
already in the destination) is refused AlreadyMember, so the recorded
return point always names where the character entered from.
================
*/
func (rt *Runtime) TransferCharacterToInstance(division string, c *enterworld.Character, lease instance.Lease, entry simulation.Spawn) instance.Status {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.transferCharacterToInstance(division, c, lease, entry)
}

/*
================
transferCharacterToInstance

The caller holds the division lock.
================
*/
func (rt *Runtime) transferCharacterToInstance(division string, c *enterworld.Character, lease instance.Lease, entry simulation.Spawn) instance.Status {
	if rt.Monsters == nil || c == nil {
		return instance.NotMember
	}
	if _, admitted := rt.EntryPopulationLease(division, c.Name); !admitted {
		return instance.NotMember
	}
	current := instance.ID(domain.CharacterWorldInstance(c))
	if current == lease.ID {
		return instance.AlreadyMember
	}
	if c.World != nil && c.World.InstanceReturn != nil {
		return instance.AlreadyMember
	}
	live, open := rt.Monsters.PopulationLease(division, lease.ID)
	if !open || live != lease {
		return instance.MissingLayer
	}
	if status := rt.Monsters.CheckPopulationTransfer(division, lease.ID, false); status != instance.Success {
		return status
	}
	nowMs := rt.Now().UnixMilli()
	pose := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, nowMs)
	anchor := domain.InstanceReturnAnchor{
		PackedInstance: uint32(current),
		RegionID:       pose.RegionID,
		X:              float32(pose.X),
		Y:              float32(pose.Y),
		Z:              float32(pose.Z),
		Angle:          pose.Angle,
	}
	if !rt.deps.Update(c, "instance-return-anchor", func() bool {
		if c.DeletePending {
			return false
		}
		if c.World == nil {
			c.World = &domain.CharacterWorld{}
		}
		c.World.InstanceReturn = &anchor
		return true
	}) {
		return instance.NotMember
	}
	if !rt.relocateCharacter(division, c, "instance-transfer", func() (travelPoint, bool) {
		return travelPoint{spawn: entry, world: lease.ID}, true
	}) {
		// The relocation rolled its own writes back; the anchor was an
		// earlier commit, so clear exactly the anchor written above.
		rt.deps.Update(c, "instance-return-anchor-clear", func() bool {
			if c.World != nil && c.World.InstanceReturn != nil && *c.World.InstanceReturn == anchor {
				c.World.InstanceReturn = nil
			}
			return true
		})
		return instance.NotMember
	}
	rt.instanceEvent(InstanceEventEnter, division, lease.ID.Definition(), lease.ID, c, false)
	// The entered character is now fully in - equipment, companions
	// and quest state loaded - so the post-entry checkpoint follows
	// the enter at once: a programme validates the entry conditions
	// there, before any update tick reaches it.
	rt.instanceEvent(InstanceEventActivated, division, lease.ID.Definition(), lease.ID, c, false)
	return instance.Success
}

/*
================
ReturnCharacterFromInstance

Brings one character back to the world and pose its transfer anchor
recorded, and clears the anchor. A character with no anchor was never
transferred by this lane and is answered NotMember. The return itself is
a relocation, so it inherits the permanent-return capacity posture: a
return never queues behind a layer's player limit.
================
*/
func (rt *Runtime) ReturnCharacterFromInstance(division string, c *enterworld.Character) instance.Status {
	unlock := rt.lockDivision(division)
	defer unlock()
	return rt.returnCharacterFromInstance(division, c)
}

/*
================
returnCharacterFromInstance

The caller holds the division lock.
================
*/
func (rt *Runtime) returnCharacterFromInstance(division string, c *enterworld.Character) instance.Status {
	if c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return instance.NotMember
	}
	anchor := *c.World.InstanceReturn
	world := instance.ID(anchor.PackedInstance)
	if world == 0 {
		world = instance.ID(domain.DefaultWorldInstance)
	}
	entry := simulation.Spawn{
		RegionID: anchor.RegionID,
		X:        float64(anchor.X),
		Y:        float64(anchor.Y),
		Z:        float64(anchor.Z),
		Angle:    anchor.Angle,
	}
	return rt.returnCharacterFromInstanceTo(division, c, travelPoint{spawn: entry, world: world}, "instance-return")
}

/*
================
returnCharacterFromInstanceTo

Brings one transferred character out of its instance to a chosen point
(its anchor, or a quest exit gate's destination), clears the anchor and
reports the leave. The caller holds the division lock.
================
*/
func (rt *Runtime) returnCharacterFromInstanceTo(division string, c *enterworld.Character, point travelPoint, label string) instance.Status {
	if c == nil || c.World == nil || c.World.InstanceReturn == nil {
		return instance.NotMember
	}
	leaving := instance.ID(domain.CharacterWorldInstance(c))
	if !rt.relocateCharacter(division, c, label, func() (travelPoint, bool) {
		return point, true
	}) {
		return instance.NotMember
	}
	rt.deps.Update(c, "instance-return-anchor-clear", func() bool {
		if c.World != nil {
			c.World.InstanceReturn = nil
		}
		return true
	})
	rt.instanceEvent(InstanceEventLeave, division, leaving.Definition(), leaving, c, false)
	return instance.Success
}

/*
================
TransferPartyToInstance

Moves the leader's party into the layer at entry: the leader first, then
every online member standing in the leader's world, each under their own
recorded anchor (a party returns scattered to where its members stood,
which is where each of them entered from). Members who are offline or in
another world stay behind and are answered NotMember in their own result;
nothing about their state changes.
================
*/
func (rt *Runtime) TransferPartyToInstance(division string, leader *enterworld.Character, lease instance.Lease, entry simulation.Spawn) []InstanceTransferResult {
	if leader == nil {
		return nil
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	leaderGid := enterworld.ObjectIDForCharacter(leader)
	leaderWorld := instance.ID(domain.CharacterWorldInstance(leader))
	results := []InstanceTransferResult{{
		Gid:    leaderGid,
		Name:   leader.Name,
		Status: rt.transferCharacterToInstance(division, leader, lease, entry),
	}}
	party := rt.rewardPartyOf(division, leaderGid)
	if party == nil {
		return results
	}
	members := append([]uint32(nil), party.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i] < members[j] })
	for _, gid := range members {
		if gid == leaderGid {
			continue
		}
		result := InstanceTransferResult{Gid: gid, Status: instance.NotMember}
		if member := rt.findCharacterByGid(division, gid); member != nil {
			result.Name = member.Name
			if instance.ID(domain.CharacterWorldInstance(member)) == leaderWorld {
				result.Status = rt.transferCharacterToInstance(division, member, lease, entry)
			}
		}
		results = append(results, result)
	}
	return results
}

/*
================
CloseInstanceWorld

Ends a tracked layer: retirement begins, every resident this lane
transferred in is relocated back to their anchor, and the population is
released once no character remains inside. A resident without an anchor
(entered outside this lane) is never stranded by destroying the world
under them: the layer stays tracked and retiring, and the tick keeps
trying until they leave.
================
*/
func (rt *Runtime) CloseInstanceWorld(division string, lease instance.Lease) {
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.closeInstanceWorld(division, lease)
}

/*
================
closeInstanceWorld

The caller holds the division lock.
================
*/
func (rt *Runtime) closeInstanceWorld(division string, lease instance.Lease) {
	if rt.Monsters == nil {
		return
	}
	if _, ok := rt.Monsters.BeginPopulationRetirement(division, lease); !ok {
		rt.untrackInstanceWorld(division, lease.ID)
		return
	}
	for _, name := range rt.instanceResidentNames(division, lease) {
		if c := rt.findCharacter(division, name); c != nil {
			rt.returnCharacterFromInstance(division, c)
		}
	}
	rt.releaseInstanceWorldIfEmpty(division, lease)
}

/*
================
TickInstanceWorlds

The instance lane's tick: closes tracked layers whose deadline passed,
retries the evacuation of retiring tracked layers, and drops tracked
records whose layer was released or reallocated underneath them. Runs as
a TickHook step, so each layer is handled under its division lock like
every other character operation.
================
*/
func (rt *Runtime) tickInstanceWorlds(nowMs int64) {
	if rt.Monsters == nil {
		return
	}
	rt.expireInstanceInvites(nowMs)
	type trackedRef struct {
		key     instanceWorldKey
		tracked trackedInstanceWorld
	}
	var tracked []trackedRef
	rt.instanceWorlds.Range(func(key, value any) bool {
		tracked = append(tracked, trackedRef{key: key.(instanceWorldKey), tracked: value.(trackedInstanceWorld)})
		return true
	})
	sort.Slice(tracked, func(i, j int) bool {
		if tracked[i].key.division != tracked[j].key.division {
			return tracked[i].key.division < tracked[j].key.division
		}
		return tracked[i].key.id < tracked[j].key.id
	})
	for _, ref := range tracked {
		unlock := rt.lockDivision(ref.key.division)
		live, open := rt.Monsters.PopulationLease(ref.key.division, ref.key.id)
		if !open || live != ref.tracked.lease {
			rt.untrackInstanceWorld(ref.key.division, ref.key.id)
			unlock()
			continue
		}
		rt.instanceEvent(InstanceEventUpdate, ref.key.division, ref.key.id.Definition(), ref.key.id, nil, false)
		deadlinePassed := ref.tracked.deadlineMs != 0 && nowMs >= ref.tracked.deadlineMs
		if deadlinePassed || rt.instanceWorldRetiring(ref.key.division, ref.tracked.lease) {
			rt.closeInstanceWorld(ref.key.division, ref.tracked.lease)
		}
		unlock()
	}
}

/*
================
instanceWorldRetiring

Whether the population's retirement has begun (BeginPopulationRetirement
ran, by this lane or by the programme that owns the layer). This read does
not start retirement or change a programme's deadline: it only lets the
cleanup tick retry an evacuation that is already underway, including while
residents remain. The final-release inbox becomes pending later, at empty.
================
*/
func (rt *Runtime) instanceWorldRetiring(division string, lease instance.Lease) bool {
	retiring, _ := rt.Monsters.PopulationRetiring(division, lease)
	return retiring
}

/*
================
instanceResidentNames

The admitted characters inside one layer, by name, sorted: evacuation
order is deterministic so a close answers the same way every time.
================
*/
func (rt *Runtime) instanceResidentNames(division string, lease instance.Lease) []string {
	var names []string
	rt.characterAdmissions.Range(func(_, value any) bool {
		admission := value.(populationAdmission)
		if admission.division == division && admission.lease == lease {
			names = append(names, admission.name)
		}
		return true
	})
	sort.Strings(names)
	return names
}

/*
================
releaseInstanceWorldIfEmpty

Releases the layer's population and untracks it once no admitted
character remains inside; monsters and world state die with the
population, which is what ending an instance means.
================
*/
func (rt *Runtime) releaseInstanceWorldIfEmpty(division string, lease instance.Lease) bool {
	empty := true
	rt.characterAdmissions.Range(func(_, value any) bool {
		admission := value.(populationAdmission)
		if admission.division == division && admission.lease.ID == lease.ID {
			empty = false
			return false
		}
		return true
	})
	if !empty {
		return false
	}
	if !rt.Monsters.ReleasePopulation(division, lease) {
		return false
	}
	rt.untrackInstanceWorld(division, lease.ID)
	return true
}

/*
================
untrackInstanceWorld
================
*/
func (rt *Runtime) untrackInstanceWorld(division string, id instance.ID) {
	rt.instanceWorlds.Delete(instanceWorldKey{division: division, id: id})
}
