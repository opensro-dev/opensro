package eventdispatch

import "encoding/binary"

// Arguments projects the server's fixed event-argument buffer.
// Actor references are explicit 32-bit host handles, never truncated Go pointers.
// Invalid transitions panic instead of continuing after a native assertion.
// This is a portable behavior projection, not the native in-memory layout.
type Arguments struct {
	data             [128]byte
	cursor, written  uint32
	present, reading bool
}

// ResetCursor is the sole +88 write performed before each native callback.
// In particular it does not clear the read-mode flag or the payload.
func (a *Arguments) ResetCursor() { a.cursor = 0 }
func (a *Arguments) AppendWord(value uint32) {
	if a.cursor > 124 || a.reading {
		panic("invalid native event argument append")
	}
	a.present = true
	binary.LittleEndian.PutUint32(a.data[a.cursor:a.cursor+4], value)
	a.cursor += 4
	a.written += 4
}
func (a *Arguments) Read(dst []byte) {
	n := uint32(len(dst))
	if len(dst) > len(a.data) || a.cursor > a.written || n > a.written-a.cursor || !a.present {
		panic("invalid native event argument read")
	}
	// The native reader checks written bounds BEFORE this first-read reset.
	if !a.reading {
		a.cursor = 0
		a.reading = true
	}
	if a.cursor > 128 || n > 128-a.cursor {
		panic("native event argument capacity exceeded")
	}
	copy(dst, a.data[a.cursor:a.cursor+n])
	a.cursor += n
}
func (a *Arguments) ReadWord() uint32 {
	var word [4]byte
	a.Read(word[:])
	return binary.LittleEndian.Uint32(word[:])
}

// DispatchArguments binds the verified cursor reset to receiver ownership.
// Callback implementations and the actor-handle resolver remain supplied by
// the gameplay owner; no default callback can silently swallow an event.
func (r *Receiver) DispatchArguments(phase uint32, key int32, arguments *Arguments, now func() uint32, invoke func(*Handler, *Arguments) uint32) uint32 {
	if arguments == nil || now == nil || invoke == nil {
		panic("missing event dispatch dependency")
	}
	return r.Dispatch(phase, key, argumentHost{arguments, now, invoke})
}

type argumentHost struct {
	arguments *Arguments
	now       func() uint32
	invoke    func(*Handler, *Arguments) uint32
}

func (h argumentHost) ResetArguments()                { h.arguments.ResetCursor() }
func (h argumentHost) NowMillis() uint32              { return h.now() }
func (h argumentHost) Invoke(handler *Handler) uint32 { return h.invoke(handler, h.arguments) }
