/*
===========================================================================

dispatch.go - frame dispatch and handler-panic containment

Routes one admitted game frame to its handler: the per-session inbound
budget, the evicted-session drop, the EnterWorld identity gate, then the
handler under a recover that closes only the offending session. A panic's
site rides the close cause, so the line production keeps names it.

===========================================================================
*/
package transport

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

/*
================
recoverHookPanic
================
*/
func recoverHookPanic(s *Session, hook string) {
	if r := recover(); r != nil {
		log.WithFields(log.Fields{"session": s.ID, "hook": hook, "panic": r}).
			Error("transport: hook panicked")
	}
}

// dispatch routes one game frame. A handler panic closes the session rather
// than the server.
/*
================
dispatch
================
*/
func (h *Hub) dispatch(s *Session, f Frame) {
	// Per-session inbound budget (ratelimit.go): every dispatched frame
	// costs one token, so a flooding client is shed HERE — before the
	// evicted check's per-frame log line and before any handler burns CPU.
	// The frame is dropped, never the session.
	if !s.admitFrame(f.Opcode, f.EncodedLen()) {
		return
	}

	if s.evicted.Load() {
		// Lame duck: a replaced session must not drive the
		// character it lost while its BYE drains. Everything dispatchable
		// (game opcodes AND control extensions like a fresh EnterWorld) is
		// dropped; PING/PONG/BYE are handled in readLoop and still flow.
		log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode)}).
			Info("transport: dropping frame from evicted session")
		return
	}

	// Identity gate: every OpEnterWorld must decode and pass the installed
	// verifier before game code sees it. Server.Start prevents a live server
	// from reaching this branch without a verifier; the explicit nil refusal
	// keeps directly attached test/in-process sessions fail-closed too.
	if f.Opcode == OpEnterWorld {
		auth := h.enterWorldAuthFn()
		ew, err := DecodeEnterWorld(f.Payload)
		if auth == nil || err != nil {
			h.refuseEnterWorldAuth(s, enterWorldUnauthorizedCode, err)
			return
		}
		if ok, denyCode := auth(s, ew); !ok {
			h.refuseEnterWorldAuth(s, denyCode, nil)
			return
		}
	}

	fn := h.handlers.lookup(f.Opcode)
	if fn == nil {
		h.metrics.unhandledFrames.Add(1)
		h.metrics.timing.recordUnhandled(f.Opcode)
		log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode)}).
			Debug("transport: unhandled opcode")
		return
	}
	if hook := h.activity.Load(); hook != nil {
		if account := s.account.Load(); account != nil {
			(*hook)(*account)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			log.WithFields(log.Fields{"session": s.ID, "opcode": fmt.Sprintf("0x%04X", f.Opcode), "panic": r, "stack": stack}).
				Error("transport: handler panicked, closing session")
			// The site also rides the close cause: production once kept the
			// "session closed" line but not this one (thread 1559546270748512288).
			h.closeSession(s, fmt.Errorf("%w on 0x%04X: %v at %s", errHandlerPanic, f.Opcode, r, panicSite(stack)))
		}
	}()
	// Deferred after the recover above, so it runs first while a panic
	// unwinds: a slow handler that panics is still counted.
	started := time.Now()
	defer func() { h.metrics.timing.recordHandler(f.Opcode, time.Since(started)) }()
	fn(s, f.Opcode, f.Payload)
}

/*
================
panicSite

The frame that panicked, from a debug.Stack() taken in the recover: the
first frame after panic() that is not in the runtime (sigpanic, panicmem
and the like), as "function file:line". Empty when the stack has no such
frame.
================
*/
func panicSite(stack string) string {
	lines := strings.Split(stack, "\n")
	unwound := false
	for i := 0; i+1 < len(lines); i++ {
		name := lines[i]
		if name == "" || strings.HasPrefix(name, "\t") || strings.HasPrefix(name, "goroutine ") {
			continue
		}
		if !unwound {
			unwound = strings.HasPrefix(name, "panic(")
			continue
		}
		if strings.HasPrefix(name, "runtime.") {
			continue
		}
		where := strings.TrimSpace(lines[i+1])
		if cut := strings.LastIndex(where, " +0x"); cut >= 0 {
			where = where[:cut]
		}
		if cut := strings.LastIndex(name, "("); cut > 0 {
			name = name[:cut]
		}
		return name + " " + where
	}
	return ""
}

const enterWorldUnauthorizedCode uint32 = 0x00A1

/*
================
refuseEnterWorldAuth
================
*/
func (h *Hub) refuseEnterWorldAuth(s *Session, denyCode uint32, decodeErr error) {
	h.metrics.enterWorldAuthRefused.Add(1)
	fields := log.Fields{"session": s.ID, "denyCode": denyCode}
	if decodeErr != nil {
		fields["error"] = decodeErr
	}
	log.WithFields(fields).Warn("transport: EnterWorld refused by auth gate")
	_ = s.Send(OpEnterWorldResult, EncodeEnterWorldResult(EnterWorldResult{
		NativeErrorCode: denyCode,
	}))
}
