package monster

// Bounded v1.188 timer model used by the Go simulation cadence adapter.
// Native-vector tests establish only their declared execution bounds; this
// is not a proof-accepted C++ replacement or proof of v1.150 AI parity.
type AITimerID int32

const TimerIDAcquisition AITimerID = 1

// AITimerEntry is a detached snapshot, not a Go ABI overlay of the 12-byte
// native slot. Both arithmetic fields must retain unsigned 32-bit semantics.
type AITimerEntry struct {
	ID             AITimerID
	ArmedImmediate bool
	LastCheckMs    uint32
	IntervalMs     uint32
}

// AITimeManager owns both banks and selected-slot identity. The reference
// server's timer-bank setup allocates nine first-bank entries and three
// second-bank entries, with split=9.
// ID 9 fails native bounds checking; 10/11 address second-bank indices 1/2.
// A map accepting arbitrary IDs would erase that exceptional branch.
type AITimeManager struct {
	first    [9]AITimerEntry
	second   [3]AITimerEntry
	selected struct {
		id     AITimerID
		active bool
	}
}

func NewAITimeManager() *AITimeManager {
	m := &AITimeManager{}
	for i := range m.first {
		m.first[i].ID = AITimerID(i)
	}
	for i := 1; i < len(m.second); i++ {
		m.second[i].ID = AITimerID(9 + i)
	}
	return m
}

func (m *AITimeManager) slot(id AITimerID) *AITimerEntry {
	if id <= 9 {
		if uint32(id) < uint32(len(m.first)) {
			return &m.first[id]
		}
	} else if index := uint32(id - 9); index < uint32(len(m.second)) {
		return &m.second[index]
	}
	// The reference server's timer accessors reach a non-returning invalid-
	// subscript throw here. This panic preserves fail-closed ownership, not
	// native exception/ABI equivalence.
	panic("AI timer ID outside native timer banks")
}

// The native CRT draw yields 0..32767. Intn(modulus) has a different
// distribution from
// rand()%modulus and hides discarded random calls. Never silently inject zero.
func nextAITimerRandom(next func() uint32) uint32 {
	if next == nil {
		panic("AI timer requires an explicit random source")
	}
	value := next()
	if value > 0x7fff {
		panic("AI timer random sample outside native CRT domain")
	}
	return value
}

// InitAcquisitionTimer retains both flag branches, both modulo operations
// of the reference server's tactics reset, and SetTimer's otherwise
// discarded third random draw.
// Gameplay meanings of these flag bits remain unverified.
func (m *AITimeManager) InitAcquisitionTimer(flags uint8, next func() uint32) {
	base, modulo := uint32(1000), uint32(491)
	if flags&4 != 0 || int8(flags) < 0 {
		base, modulo = 200, 41
	}
	jitter := nextAITimerRandom(next)%modulo + 10
	m.SetTimer(TimerIDAcquisition, base, jitter, 0, false, next)
}

// SetTimer models the reference server's bank-specific timer writes. Its
// EAX result is the phase
// sample in the first bank, the quotient in the second. The second bank
// preserves its armed flag. Arithmetic wraps BEFORE the minimum-one clamp.
func (m *AITimeManager) SetTimer(id AITimerID, base, jitter, now uint32, selectTimer bool, next func() uint32) uint32 {
	entry := m.slot(id)
	interval := base
	if jitter != 0 {
		interval += nextAITimerRandom(next)%jitter + 1
	}
	if interval <= 1 {
		interval = 1
	}
	phase := nextAITimerRandom(next)
	result := phase
	if id <= 9 {
		entry.LastCheckMs, entry.ArmedImmediate = 0, true
	} else {
		entry.LastCheckMs = now + phase%interval
		result = phase / interval
	}
	entry.IntervalMs = interval
	if selectTimer {
		m.selected.id, m.selected.active = id, true
	}
	return result
}

// GetTimer returns a detached copy; the private slot pointer never escapes.
func (m *AITimeManager) GetTimer(id AITimerID) AITimerEntry { return *m.slot(id) }

// SUB(now, interval); unsigned CMP(result, last). Do not commute subtraction:
// now=1, interval=225, last=0 fires because uint32(1-225) wraps, whereas
// (now-last)>=interval is false. Native startup/rollover quirks are retained.
func fireAITimer(entry *AITimerEntry, now uint32) bool {
	if !entry.ArmedImmediate && now-entry.IntervalMs < entry.LastCheckMs {
		return false
	}
	entry.ArmedImmediate, entry.LastCheckMs = false, now
	return true
}

// CheckTimer gates the selected slot BEFORE looking up the requested slot.
// Firing selection clears it, then continues; it does not return success.
// When selected=requested, the same slot is checked twice.
// Selection aliases the bank entry, including replacements via SetTimer.
func (m *AITimeManager) CheckTimer(id AITimerID, now uint32) bool {
	if !m.CheckSelectedTimer(now) {
		return false
	}
	return fireAITimer(m.slot(id), now)
}

// CheckSelectedTimer is the shared prefix of the reference server's timer
// check. The battle
// adapter uses this before its existing action-interval deadline; it must
// neither consume another timer nor reset the selection on a state change.
func (m *AITimeManager) CheckSelectedTimer(now uint32) bool {
	if m.selected.active {
		if !fireAITimer(m.slot(m.selected.id), now) {
			return false
		}
		m.selected.active = false
	}
	return true
}
