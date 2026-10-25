/*
===========================================================================

admission.go - the hub's admission verifiers: who may attach and enter

The transport authenticates twice. HELLO carries an Agent-minted admission
ticket (account and shard); the hub verifies it before a session exists,
and with the two-phase verifier consumes it only once WELCOME is written.
OpEnterWorld then binds a character and is vetted again before any game
handler runs. The verifiers are installed by the GameWorld; the hub only
calls them. Split from hub.go, which keeps session ownership and routing.

===========================================================================
*/
package transport

// AdmissionIdentity is the authenticated account/shard identity carried by a
// HELLO admission ticket. It is intentionally smaller than gameplay identity: the
// character still requires a separate one-use EnterWorld authorization.
/*
================
AdmissionIdentity
================
*/
type AdmissionIdentity struct {
	AccountID string
	ShardID   string
}

// HelloAuthFunc verifies and consumes one HELLO admission ticket.
type HelloAuthFunc func(token []byte) (AdmissionIdentity, error)

// HelloCommitAuthFunc verifies one HELLO admission ticket WITHOUT
// consuming it and returns the commit that consumes it. The hub runs
// the commit only once the session's WELCOME frame has been written
// (Session.writeLoop): a connection that dies before WELCOME never
// burns its ticket, so the client can present the same ticket on its
// fallback transport. A commit reporting the ticket already consumed
// (a concurrent second admission won the race) closes the session.
type HelloCommitAuthFunc func(token []byte) (AdmissionIdentity, func() error, error)

// SetHelloAuth installs the required HELLO admission verifier.
/*
================
SetHelloAuth
================
*/
func (h *Hub) SetHelloAuth(fn HelloAuthFunc) {
	h.admission.set(fn)
}

// SetHelloCommitAuth installs the two-phase HELLO admission verifier:
// authentication at HELLO, consumption deferred to WELCOME.
/*
================
SetHelloCommitAuth
================
*/
func (h *Hub) SetHelloCommitAuth(fn HelloCommitAuthFunc) {
	h.admission.setCommit(fn)
}

/*
================
helloAuthFn
================
*/
func (h *Hub) helloAuthFn() HelloAuthFunc {
	return h.admission.current()
}

/*
================
verifyHelloAdmission

Runs the installed HELLO verification. The two-phase form wins when
both are installed. installed=false means no verifier at all: the
caller refuses the connection as before.
================
*/
func (h *Hub) verifyHelloAdmission(token []byte) (AdmissionIdentity, func() error, bool, error) {
	if fn := h.admission.commitCurrent(); fn != nil {
		identity, commit, err := fn(token)
		return identity, commit, true, err
	}
	if fn := h.admission.current(); fn != nil {
		identity, err := fn(token)
		return identity, nil, true, err
	}
	return AdmissionIdentity{}, nil, false, nil
}

// EnterWorldAuthFunc vets an OpEnterWorld bind BEFORE the game handler
// runs. Return ok=false with a native error code to refuse: the transport
// answers OpEnterWorldResult{OK:false, NativeErrorCode:denyCode} itself and
// the handler never sees the frame. Installed via SetEnterWorldAuth;
// production wires launcher-token verification here.
type EnterWorldAuthFunc func(s *Session, ew EnterWorld) (ok bool, denyCode uint32)

// SetEnterWorldAuth installs the required post-WELCOME identity gate.
/*
================
SetEnterWorldAuth
================
*/
func (h *Hub) SetEnterWorldAuth(fn EnterWorldAuthFunc) {
	h.auth.set(fn)
}

/*
================
enterWorldAuthFn
================
*/
func (h *Hub) enterWorldAuthFn() EnterWorldAuthFunc {
	return h.auth.current()
}
