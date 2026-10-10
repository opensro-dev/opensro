/*
===========================================================================

registry.go - stationary skill objects (traps, buff fields) and their scan lifetime

One registry owns identities, native scan deadlines and retirement. The
action owner supplies authoritative actor snapshots and commits quest events;
this package never mutates inventory or invents capture rewards.

===========================================================================
*/
package skillobject

import (
	"fmt"
	"math"
	"sort"
	"sync"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
Program

The descriptor compiler admits the complete native qest/lnks/dura/efr/trap
program before the action owner can create an object.
================
*/
type Program struct {
	SkillID    uint32
	DurationMs uint32
	ScanMs     uint32
	Radius     uint32
	Targets    [3]uint32
	// Combat marks a planted hostile trap: any living monster in Radius
	// triggers it. Hidden traps remain replicated; the native client owns concealment;
	// OwnerDistance retires the trap once its planter walks beyond it.
	Combat        bool
	Hidden        bool
	OwnerDistance uint32
	LinkGroup     uint32
	// Field marks a buff field (efr kind 3, object mode 2): characters
	// standing in Radius hold an instance of the skill, Select and
	// MaxTargets (0 = unlimited) choose them.
	Field      bool
	Select     uint32
	MaxTargets uint32
}

/*
================
FieldRecipient

One member of a buff field's tracked set (the std::set at object +0x61
words). GID tells a recipient from a later character of the same name.
================
*/
type FieldRecipient struct {
	Name string
	GID  uint32
}

/*
================
Object

Population generation prevents a trap from surviving a recycled instance.
GIDs are never reused during a process lifetime, including failed captures.
================
*/
type Object struct {
	Division   string
	Population instance.Lease
	OwnerGID   uint32
	OwnerName  string
	Program    Program
	Spawn      wire.SkillObjectSpawn
	CreatedMs  int64
	NextScanMs int64
	// OwnerEffect is the planter's buff-board instance that mirrors a combat
	// trap's life (lnks board word); zero for quest traps.
	OwnerEffect uint32
	// Tracked is a buff field's recipient set, in admission order. Only
	// Track replaces it; snapshots carry a copy.
	Tracked []FieldRecipient
}

/*
================
Target

A detached monster snapshot sampled at the same simulation time as its trap.
================
*/
type Target struct {
	GID      uint32
	RefID    uint32
	OwnerGID uint32
	Alive    bool
	Region   uint16
	X, Y, Z  float64
}

/*
================
Registry

Zero value is ready to use. Mutex ownership stops item admission, tick scans
and scene bootstrap from publishing partially initialized objects.
================
*/
type Registry struct {
	mu      sync.Mutex
	nextGID uint32
	objects map[uint32]Object
}

/*
================
Create

Reject incomplete descriptors before allocating identity. Item consumption
belongs after this successful admission inside the character transaction.
================
*/
func (r *Registry) Create(object Object) (Object, error) {
	if object.Division == "" || object.OwnerGID == 0 || object.OwnerName == "" ||
		object.Population.ID == 0 || object.Program.SkillID == 0 || object.Program.DurationMs == 0 ||
		object.Program.ScanMs == 0 || object.Program.Radius == 0 {
		return Object{}, fmt.Errorf("skill object: incomplete admission")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nextGID == 0 {
		r.nextGID = domain.SkillObjectGIDBase
	}
	if r.nextGID == domain.SkillObjectGIDLimit {
		return Object{}, fmt.Errorf("skill object: identity range exhausted")
	}
	r.nextGID++
	object.Spawn.GID = r.nextGID
	if object.Program.Combat {
		// 86C70F identifies the planter's decoration by this same object ID.
		object.OwnerEffect = object.Spawn.GID
	}
	object.Spawn.SkillID = object.Program.SkillID
	object.NextScanMs = object.CreatedMs + int64(object.Program.ScanMs)
	if r.objects == nil {
		r.objects = make(map[uint32]Object)
	}
	r.objects[object.Spawn.GID] = object
	return object, nil
}

/*
================
Snapshot

Stable identity order gives deterministic scan and publication ordering.
Returned values cannot mutate registry-owned lifetimes.
================
*/
func (r *Registry) Snapshot() []Object {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Object, 0, len(r.objects))
	for _, object := range r.objects {
		object.Tracked = append([]FieldRecipient(nil), object.Tracked...)
		out = append(out, object)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spawn.GID < out[j].Spawn.GID })
	return out
}

/*
================
Remove

Retirement is idempotent. The publication owner reconciles absent objects
against transport-admitted GIDs, including peers that joined after creation.
================
*/
func (r *Registry) Remove(gid uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, present := r.objects[gid]
	delete(r.objects, gid)
	return present
}

/*
================
Scan

48CEA0 expires only when elapsed time exceeds duration and scans once per
due update, without replaying missed pulses. 48D690 retires on the first
matching monster even if its owner quest subsequently refuses the event.
================
*/
func (r *Registry) Scan(gid uint32, nowMs int64, ownerPresent bool, targets []Target) (Object, uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	object, present := r.objects[gid]
	if !present {
		return Object{}, 0, false
	}
	if !ownerPresent || nowMs-object.CreatedMs > int64(object.Program.DurationMs) {
		delete(r.objects, gid)
		return object, 0, true
	}
	if nowMs < object.NextScanMs {
		return object, 0, false
	}
	object.NextScanMs = nowMs + int64(object.Program.ScanMs)
	r.objects[gid] = object
	for _, target := range targets {
		if !Matches(object, target) {
			continue
		}
		delete(r.objects, gid)
		return object, target.GID, true
	}
	return object, 0, false
}

/*
================
Matches

Target lists terminate at their first zero. Empty promotion lists remain
valid objects but cannot capture arbitrary monsters. Radius is strict and
three-dimensional, with region-local coordinates re-expressed first.
================
*/
func Matches(object Object, target Target) bool {
	// A quest trap needs the owner's own fight; a combat trap any victim.
	if !target.Alive || target.GID == 0 || !object.Program.Combat && target.OwnerGID != 0 && target.OwnerGID != object.OwnerGID ||
		!worldgeom.SamePlane(object.Spawn.Region, target.Region) {
		return false
	}
	matched := object.Program.Combat
	for _, ref := range object.Program.Targets {
		if matched {
			break
		}
		if ref == 0 {
			break
		}
		if ref == target.RefID {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	from := worldgeom.RegionXZ{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Z: float64(object.Spawn.Z)}
	to := worldgeom.RegionXZ{RegionID: target.Region, X: target.X, Z: target.Z}
	distance := math.Hypot(worldgeom.Distance(from, to), target.Y-float64(object.Spawn.Y))
	return distance < float64(object.Program.Radius)
}

/*
================
Advance

A buff field's pass of 48CEA0: the same expiry as Scan (an absent owner or
elapsed duration retires it), then a due pass every ScanMs without replaying
missed ones. The caller does the recipient work of a due pass and stores the
result with Track.
================
*/
func (r *Registry) Advance(gid uint32, nowMs int64, ownerPresent bool) (object Object, due, retired bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	object, present := r.objects[gid]
	if !present {
		return Object{}, false, false
	}
	object.Tracked = append([]FieldRecipient(nil), object.Tracked...)
	if !ownerPresent || nowMs-object.CreatedMs > int64(object.Program.DurationMs) {
		delete(r.objects, gid)
		return object, false, true
	}
	if nowMs < object.NextScanMs {
		return object, false, false
	}
	stored := r.objects[gid]
	stored.NextScanMs = nowMs + int64(object.Program.ScanMs)
	r.objects[gid] = stored
	return object, true, false
}

/*
================
Track

Replace a live field's recipient set. Reports false once the object has
retired, so the caller can retire what it admitted in the meantime.
================
*/
func (r *Registry) Track(gid uint32, tracked []FieldRecipient) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	object, present := r.objects[gid]
	if !present {
		return false
	}
	object.Tracked = append([]FieldRecipient(nil), tracked...)
	r.objects[gid] = object
	return true
}
