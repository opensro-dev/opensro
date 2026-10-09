// Package readiness owns the small process-admission boundary shared by
// HTTP handlers and the composition root.
package readiness

import (
	"net/http"
	"sync/atomic"
)

const (
	PathHealth = "/healthz"
	PathReady  = "/readyz"

	// The refusal codes a closed gate answers with. The client retries a
	// starting process after Retry-After.
	CodeStarting = "PROCESS_STARTING"
	CodeDraining = "PROCESS_DRAINING"
	// RetryAfterSeconds is the Retry-After a refusal carries.
	RetryAfterSeconds = 1
)

// Gate is closed during startup and shutdown, and open only while the
// process can accept new work. It keeps one bit of history, whether it has
// ever opened, so a refusal can tell a starting process (retry soon) from a
// draining one (#246); orchestration still owns process state.
type Gate struct {
	open   atomic.Bool
	opened atomic.Bool
}

func NewGate() *Gate {
	return &Gate{}
}

func (gate *Gate) Open() {
	gate.opened.Store(true)
	gate.open.Store(true)
}

func (gate *Gate) Close() {
	gate.open.Store(false)
}

func (gate *Gate) Ready() bool {
	return gate != nil && gate.open.Load()
}

// Starting reports a gate that is closed and has never opened: the process
// is still coming up, as opposed to draining after it ran.
func (gate *Gate) Starting() bool {
	return gate != nil && !gate.open.Load() && !gate.opened.Load()
}

// Refusal is the code and message a closed gate answers with, naming the
// process ("GameWorld", "Agent").
func (gate *Gate) Refusal(process string) (string, string) {
	if gate.Starting() {
		return CodeStarting, "The " + process + " is starting."
	}
	return CodeDraining, "The " + process + " is draining."
}

// HealthHandler is a liveness check. Reaching the handler proves that the
// process and HTTP listener can make progress; dependency health belongs in
// readiness so a shared outage does not create a restart storm.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if !allowRead(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (gate *Gate) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	if !allowRead(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !gate.Ready() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready"))
}

func allowRead(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}
