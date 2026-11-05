/*
===========================================================================

jsonscratch.go - JSON encoding into a buffer the caller keeps

encoding/json.Marshal allocates the encoded bytes fresh for every
value it encodes: bytes.Clone inside the runtime's Marshal was
796 MB of a 60-second production heap profile (6% of all allocation),
paid by callers that consume the bytes synchronously - a store commit
writing them into a SQL statement, an envelope copying them into its
own frame - and never keep them.

A Scratch keeps one encode buffer (and its json.Encoder) across
calls, so a repeat encode reuses it and pays only the encoder's own
working allocations. The bytes it returns are byte-identical to
json.Marshal's: they come from the same encoding/json encoder with
the same defaults, minus the newline Encoder.Encode appends. They
are borrowed, not owned: the next Marshal on the same Scratch
overwrites them, so a caller that must keep the bytes copies them -
once, at final size, instead of Marshal's encode-plus-clone.

Why not the encoding/json/v2 API, which can also write into a
caller buffer: v2 changed struct-tag semantics (its omitempty keeps
zero numbers and booleans that v1 omits - the persisted character
record is dense with omitempty), so v2 output is a different byte
stream for this codebase's records. The corpus test pins the
identity against Marshal itself, including those cases.

A Scratch is state, and like every other piece of state it has one
owner at a time: the store's commit lock, a sync.Pool checkout. It is
not safe for concurrent use.

===========================================================================
*/

package jsonscratch

import (
	"bytes"
	"encoding/json"
)

/*
================
Scratch

One reusable JSON encode buffer. The zero value is ready to use; the
buffer grows to the largest value encoded and keeps that capacity for
the next call.
================
*/
type Scratch struct {
	buf bytes.Buffer
	enc *json.Encoder
}

/*
================
Scratch.Marshal

Encodes v exactly as json.Marshal does and returns the encoded bytes.
The bytes belong to the Scratch: they stay valid until the next Marshal
on it, and the caller must copy anything it keeps past that.
================
*/
func (s *Scratch) Marshal(v any) ([]byte, error) {
	if s.enc == nil {
		s.enc = json.NewEncoder(&s.buf)
	}
	s.buf.Reset()
	if err := s.enc.Encode(v); err != nil {
		return nil, err
	}
	// Encode terminates the value with a newline Marshal never emits;
	// the encoded value is never empty, so the last byte is that
	// newline.
	return s.buf.Bytes()[:s.buf.Len()-1], nil
}

/*
================
Scratch.Cap

The retained buffer capacity, so a pool can drop a Scratch that grew
far past its usual size instead of holding the memory forever.
================
*/
func (s *Scratch) Cap() int {
	return s.buf.Cap()
}
