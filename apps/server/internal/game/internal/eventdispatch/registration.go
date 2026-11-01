package eventdispatch

// Receiver owns retained handler entries. Registry owns only associations.
// A receiver must outlive external registries referencing its handlers, as in
// the native event registry. Clear during an active dispatch is not supported.
type Receiver struct {
	// Native registry keys are receiver pointer values. The host supplies their
	// ordering identity; gameplay GIDs must not silently substitute for it.
	OrderKey uint32
	phases   [3][]*Handler
	own      Registry
}
type association struct {
	receiver *Receiver
	handler  *Handler
}
type Registry struct{ entries []association }

// Register preserves the supplied enabled state. Native does not re-enable a
// disabled handler on registration. Keys are ordered; equal keys keep insertion
// order. Already-owned handlers and out-of-range phases are invalid inputs.
func (r *Receiver) Register(phase uint32, key int32, h *Handler, owner *Registry) {
	if phase >= 3 || h == nil || h.receiver != nil || r.OrderKey == 0 {
		panic("invalid event registration")
	}
	if owner == nil {
		owner = &r.own
	}
	h.Phase = phase
	h.EventKey = key
	h.receiver = r
	h.registry = owner
	rows := r.phases[phase]
	i := 0
	for i < len(rows) && rows[i].EventKey <= key {
		i++
	}
	rows = append(rows, nil)
	copy(rows[i+1:], rows[i:])
	rows[i] = h
	r.phases[phase] = rows
	i = 0
	for i < len(owner.entries) && owner.entries[i].receiver.OrderKey <= r.OrderKey {
		i++
	}
	owner.entries = append(owner.entries, association{})
	copy(owner.entries[i+1:], owner.entries[i:])
	owner.entries[i] = association{r, h}
}
func (r *Receiver) EqualRange(phase uint32, key int32) []*Handler {
	if phase >= 3 {
		panic("invalid event phase")
	}
	var out []*Handler
	for _, h := range r.phases[phase] {
		if h.EventKey == key {
			out = append(out, h)
		}
	}
	return out
}

// Presence does not test enabled: disabled entries stay in native trees.
func (r *Receiver) IsAbsent(phase uint32, key int32) bool { return len(r.EqualRange(phase, key)) == 0 }
func IsAbsentEverywhere(global, local *Receiver, phase uint32, key int32) bool {
	return global.IsAbsent(phase, key) && local.IsAbsent(phase, key)
}

// Disable optionally removes the registry association first, then scans the
// receiver by identity. It does not erase the retained handler or destroy it.
func (r *Receiver) Disable(h *Handler, detachRegistry bool) uint32 {
	if detachRegistry && h.registry != nil {
		h.registry.Detach(h, false)
	}
	for _, rows := range r.phases {
		for _, entry := range rows {
			if entry == h {
				h.Enabled = 0
				return 0
			}
		}
	}
	return 2
}
func (g *Registry) Detach(h *Handler, disableReceiver bool) {
	for i, a := range g.entries {
		if a.handler == h {
			if disableReceiver {
				a.receiver.Disable(h, false)
			}
			g.entries = append(g.entries[:i], g.entries[i+1:]...)
			return
		}
	}
}
func (h *Handler) Unregister() {
	if h.registry == nil {
		panic("unregistered event handler")
	}
	h.registry.Detach(h, true)
}

// The native removal drops just the first matching association, then returns.
func (g *Registry) RemoveFirstPhaseZeroEvent12() {
	for _, a := range g.entries {
		if a.handler.Phase == 0 && a.handler.EventKey == 12 {
			g.Detach(a.handler, true)
			return
		}
	}
}
func (g *Registry) Clear() {
	for _, a := range g.entries {
		a.receiver.Disable(a.handler, false)
	}
	g.entries = nil
}

// Destroy is the explicit native virtual-destructor boundary. Entries remain
// owned until this call, including entries disabled by registry cleanup.
func (r *Receiver) Clear(destroy func(*Handler)) {
	if destroy == nil {
		panic("missing event handler destructor")
	}
	r.own.Clear()
	for phase, rows := range r.phases {
		for _, h := range rows {
			destroy(h)
		}
		r.phases[phase] = nil
	}
}

// CallbackHost supplies native virtual execution, argument state and clock.
// Receiver owns result-4 unregister so callers cannot replace it with deletion.
type CallbackHost interface {
	ResetArguments()
	NowMillis() uint32
	Invoke(*Handler) uint32
}
type registeredHost struct{ CallbackHost }

func (registeredHost) Unregister(h *Handler) { h.Unregister() }
func (r *Receiver) Dispatch(phase uint32, key int32, host CallbackHost) uint32 {
	return Dispatch(r.EqualRange(phase, key), registeredHost{host})
}
