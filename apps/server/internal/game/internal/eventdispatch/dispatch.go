// Package eventdispatch projects the server's event dispatch control flow. It is a
// shared prerequisite, not proof that gameplay event producers are installed.
package eventdispatch

type Handler struct {
	Phase    uint32
	EventKey int32
	registry *Registry
	receiver *Receiver

	LastTick, Deadline, Interval uint32
	Enabled                      uint8
}

func NewHandler(now uint32) *Handler {
	return &Handler{LastTick: now, Enabled: 1, Phase: 3, EventKey: 32}
}
func (h *Handler) SetTiming(now, delay, repeat uint32) {
	h.LastTick = now
	h.Deadline = now + delay
	h.Interval = repeat
}
func (h *Handler) Due(now uint32) bool {
	h.LastTick = now
	if h.Deadline != 0 {
		if now < h.Deadline {
			return false
		}
		if h.Interval != 0 {
			h.Deadline += h.Interval
		}
	}
	return true
}

// Host retains handlers until dispatch exits. Unregister performs the owner
// registry operation; merely dropping the snapshot pointer is insufficient.
type Host interface {
	ResetArguments()
	NowMillis() uint32
	Invoke(*Handler) uint32
	Unregister(*Handler)
}

// Dispatch takes the ordered equal-key range, snapshots entries enabled == 1,
// then rechecks each before invocation. Registrations added during a callback
// participate in the next dispatch; disabling a captured handler is immediate.
func Dispatch(ordered []*Handler, host Host) uint32 {
	if len(ordered) == 0 {
		return 2
	}
	snapshot := make([]*Handler, 0, len(ordered))
	for _, h := range ordered {
		if h == nil {
			panic("null native event handler")
		}
		if h.Enabled == 1 {
			snapshot = append(snapshot, h)
		}
	}
	for _, h := range snapshot {
		host.ResetArguments()
		if h.Enabled == 0 {
			continue
		}
		result := uint32(2)
		if h.Due(host.NowMillis()) {
			result = host.Invoke(h)
		}
		switch result {
		case 0, 2:
		case 1:
			return 0
		case 3:
			return 3
		case 4:
			host.Unregister(h)
			return 4
		default:
			panic("invalid native event callback result")
		}
	}
	return 0
}
