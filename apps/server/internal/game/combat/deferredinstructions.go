package combat

// DeferredInstructions models the eight captured pointers of the server's
// deferred-effect descriptor. Inputs are immutable, validated reference
// instructions. This executor does not admit a skill or claim that its host
// callbacks have been implemented.
type DeferredInstructions struct {
	Parameters [8]*[4]uint32 // ref 338,33C,340,344,348,34C,350,364, in native order
	StartedAt  uint32
	Applied    bool // descriptor +10, shared by parameters 340 and 344
}

// DeferredVitalLatches belong to the actor, not an individual descriptor.
// Their lifetime and initialization must be supplied by the actor owner.
type DeferredVitalLatches struct{ HP, MP bool }

// NewDeferredVitalLatches is called on actor initialization/reset, never on
// per-effect allocation. The native actor initializer writes both latches
// to one.
func NewDeferredVitalLatches() DeferredVitalLatches {
	return DeferredVitalLatches{HP: true, MP: true}
}

// DeferredHost exposes the actual side effects at native call boundaries.
// Callbacks run immediately: later branches must see any state they changed.
type DeferredHost interface {
	NowMillis() uint32
	CurrentHP() uint32
	CurrentMP() uint32
	IsPlayer() bool
	ApplyHit(damage int32)                            // source nil, secondary damage 0, flags 4/0
	ConsumeResources(hp, mp int32)                    // reason 4
	WriteParameter(id, channel uint32, value float32) // source = this descriptor
	SendParameterStats()
	CancelActionsAndRetireSkills() // action cancel (false): movement/action cancellation and selected-skill retirement
	SetMotion(seconds float32)     // native action 13, mode 0
	DamageEquipment(kind, probability uint32)
}

func (d *DeferredInstructions) NeedsQueue() bool {
	for i, p := range d.Parameters {
		if p != nil && (i >= 4 || p[3] != 2) {
			return true
		}
	}
	return false
}

// Advance is the full control flow of the native descriptor tick. False
// requires the owner to remove this descriptor's parameter contributions
// before releasing it.
func (d *DeferredInstructions) Advance(h DeferredHost, l *DeferredVitalLatches) bool {
	for i := 0; i < 2; i++ {
		p := d.Parameters[i]
		if p == nil {
			continue
		}
		latch := &l.HP
		if i == 1 {
			latch = &l.MP
		}
		if h.NowMillis()-d.StartedAt >= p[0] {
			*latch = true
			return false
		}
		if !*latch {
			continue
		}
		switch p[3] {
		case 0:
			var current uint32
			if i == 0 {
				current = h.CurrentHP()
			} else {
				current = h.CurrentMP()
			}
			// Native IMUL truncates BEFORE conversion to unsigned x87 division.
			amount := int32(uint32(uint64(current*p[2])/100 + uint64(p[1])))
			if i == 0 {
				h.ApplyHit(amount)
			} else {
				h.ConsumeResources(0, amount)
			}
		case 1:
			if p[1] != 0 {
				h.WriteParameter(uint32(3+i), 0, -float32(p[1]))
			} else if p[2] != 0 {
				h.WriteParameter(uint32(3+i), 1, -float32(p[2]))
			}
			if h.IsPlayer() {
				h.SendParameterStats()
			}
		}
		*latch = false
	}
	for i := 2; i < 4; i++ {
		p := d.Parameters[i]
		if p == nil {
			continue
		}
		if h.NowMillis()-d.StartedAt >= p[0] {
			d.Applied = false
			return false
		}
		if !d.Applied {
			if p[3] == 1 {
				id, channel := uint32(0xb2), uint32(2)
				if i == 3 {
					id, channel = 5, 1
				}
				h.WriteParameter(id, channel, -float32(p[1]))
				h.WriteParameter(id+1, channel, -float32(p[2]))
			}
			d.Applied = true
		}
	}
	if d.Parameters[4] != nil {
		h.ConsumeResources(int32(h.CurrentHP()-1), 0)
		return false
	}
	if d.Parameters[5] != nil {
		h.ConsumeResources(0, int32(h.CurrentMP()-1))
		return false
	}
	if p := d.Parameters[6]; p != nil {
		h.CancelActionsAndRetireSkills() // action cancel (false): movement/action cancellation and selected-skill retirement
		h.SetMotion(float32(float64(p[0]) / 1000))
		return false
	}
	if p := d.Parameters[7]; p != nil {
		if h.IsPlayer() {
			h.DamageEquipment(p[0], p[1])
		}
		return false
	}
	return true
}
