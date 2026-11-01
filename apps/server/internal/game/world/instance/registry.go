package instance

import (
	"sort"
	"sync"
)

// ID is the wire identity the native manager assembles. Layer zero belongs
// to the world's internal controller and is never a resident PC layer.
type ID uint32

func Pack(definition DefinitionID, layer uint16) ID {
	return ID(uint32(definition) | uint32(layer)<<16)
}
func (id ID) Definition() DefinitionID { return DefinitionID(id) }
func (id ID) Layer() uint16            { return uint16(id >> 16) }

// Lease distinguishes successive populations occupying the same wire ID.
// Generation is process-local and must not be persisted or sent on the wire.
type Lease struct {
	ID         ID
	Generation uint64
}

type Status uint8

const (
	Success            Status = 1
	MissingLayer       Status = 2  // leave
	MissingWorld       Status = 6  // transfer request
	AllocationFull     Status = 7  // allocation
	InvalidLayer       Status = 8  // allocation/admission
	AlreadyMember      Status = 10 // admission
	NotMember          Status = 11 // leave
	PlayerLimitReached Status = 14 // admission
)

type layer struct {
	lease   Lease
	members map[uint32]struct{}
	// Native +6C is zero at initialization, then set to one by the
	// manager's retirement event. It is not the definition type.
	retirementMode uint32
}

// CheckTransfer is the native non-mutating request gate.
// It does not reserve capacity or admit a PC. In particular, an unlimited
// definition passes before looking up the destination layer; the subsequent
// transfer/allocation owner must still obtain a live lease before admission.
func (r *Registry) CheckTransfer(destination ID, capacityBypass bool) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	if destination.Definition() == 0 || destination.Layer() == 0 {
		return InvalidLayer
	}
	w := r.worlds[destination.Definition()]
	if w == nil {
		return MissingWorld
	}
	if w.definition.PlayerLimit == 0 {
		return Success
	}
	if int(destination.Layer()) >= len(w.slots) || w.slots[destination.Layer()] == nil {
		return MissingLayer
	}
	if capacityBypass || len(w.slots[destination.Layer()].members) < int(w.definition.PlayerLimit) {
		return Success
	}
	return PlayerLimitReached
}

type world struct {
	definition Definition
	slots      []*layer
	free       []*layer
}

// Registry owns allocation and PC membership for one server division.
// It deliberately does not infer admission from a persisted packed ID.
type Registry struct {
	mu         sync.Mutex
	worlds     map[DefinitionID]*world
	generation uint64
}

func NewRegistry(definitions []Definition) *Registry {
	r := &Registry{worlds: make(map[DefinitionID]*world, len(definitions))}
	for _, definition := range definitions {
		if definition.ID == 0 || definition.ID == 0xffff || r.worlds[definition.ID] != nil {
			panic("invalid world definition identity")
		}
		// The native allocation makes limit+1 slots, including the internal
		// zero slot.
		r.worlds[definition.ID] = &world{definition: definition, slots: make([]*layer, int(definition.LayerLimit)+1)}
	}
	return r
}

// Allocate implements the bounded world allocation door. The caller
// supplies the layer selected by the world manager; the free object pool is
// not a substitute for that layer ID. Invalid/occupied slots are programmer
// errors in native, checked here before consuming a pooled object.
func (r *Registry) Allocate(id ID) (Lease, Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.worlds[id.Definition()]
	if w == nil || id.Layer() == 0 {
		return Lease{}, InvalidLayer
	}
	if int(id.Layer()) >= len(w.slots) {
		panic("world layer outside authored slot vector")
	}
	if len(w.free) == 0 {
		available := false
		for index := 1; index < len(w.slots); index++ {
			if w.slots[index] == nil {
				available = true
				break
			}
		}
		if !available {
			return Lease{}, AllocationFull
		}
		w.free = append(w.free, &layer{})
	}
	if w.slots[id.Layer()] != nil {
		panic("world layer already allocated")
	}
	allocated := w.free[0]
	w.free[0] = nil
	w.free = w.free[1:]
	if len(allocated.members) != 0 {
		panic("pooled world layer still has residents")
	}
	if r.generation == ^uint64(0) {
		panic("world generation exhausted")
	}
	r.generation++
	allocated.lease = Lease{ID: id, Generation: r.generation}
	allocated.members = make(map[uint32]struct{})
	allocated.retirementMode = 0
	w.slots[id.Layer()] = allocated
	return allocated.lease, Success
}

func (r *Registry) resolve(lease Lease) (*world, *layer) {
	w := r.worlds[lease.ID.Definition()]
	if w == nil || lease.ID.Layer() == 0 || int(lease.ID.Layer()) >= len(w.slots) {
		return nil, nil
	}
	l := w.slots[lease.ID.Layer()]
	if l == nil || l.lease != lease {
		return w, nil
	}
	return w, l
}

func (r *Registry) Lookup(id ID) (Lease, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.worlds[id.Definition()]
	if w == nil || id.Layer() == 0 || int(id.Layer()) >= len(w.slots) || w.slots[id.Layer()] == nil {
		return Lease{}, false
	}
	return w.slots[id.Layer()].lease, true
}

// AdmitPC checks capacity before duplicate membership, as the native
// admission does.
// CapacityBypass is the result of the authority's native +53C predicate;
// callers must not derive it from a displayed name such as "[GM]".
func (r *Registry) AdmitPC(lease Lease, gid uint32, capacityBypass bool) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, l := r.resolve(lease)
	if l == nil {
		return InvalidLayer
	}
	if gid == 0 {
		panic("zero PC identity")
	}
	if w.definition.PlayerLimit != 0 && len(l.members) >= int(w.definition.PlayerLimit) && !capacityBypass {
		return PlayerLimitReached
	}
	if _, exists := l.members[gid]; exists {
		return AlreadyMember
	}
	l.members[gid] = struct{}{}
	return Success
}

// LeavePC returns the native empty-layer retirement request. Release is a
// separate manager transition after population/session cleanup has completed.
func (r *Registry) LeavePC(lease Lease, gid uint32) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, l := r.resolve(lease)
	if l == nil {
		return MissingLayer, false
	}
	if _, exists := l.members[gid]; !exists {
		return NotMember, false
	}
	delete(l.members, gid)
	return Success, l.retirementMode == 1 && len(l.members) == 0
}

// BeginRetirement is the membership part of the manager's retirement event.
// Nonempty layers return the GID-ordered evacuation roster; empty ones request
// release immediately. Evacuation/transfer belongs to the world coordinator.
// Allocation and a routine last leave never manufacture this event.
func (r *Registry) BeginRetirement(lease Lease) ([]uint32, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, l := r.resolve(lease)
	if l == nil {
		return nil, false
	}
	l.retirementMode = 1
	residents := make([]uint32, 0, len(l.members))
	for gid := range l.members {
		residents = append(residents, gid)
	}
	sort.Slice(residents, func(i, j int) bool { return residents[i] < residents[j] })
	return residents, true
}

// Release retires the current lease and returns its object to the FIFO pool.
// Forced native destruction clears residual membership as well.
// A stale manager callback cannot release a replacement population.
func (r *Registry) Release(lease Lease) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, l := r.resolve(lease)
	if l == nil {
		return false
	}
	w.slots[lease.ID.Layer()] = nil
	clear(l.members)
	l.lease = Lease{}
	w.free = append(w.free, l)
	return true
}
