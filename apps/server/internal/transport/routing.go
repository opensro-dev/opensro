package transport

import (
	"slices"
	"sync"
)

type helloAdmissionGate struct {
	mu     sync.RWMutex
	verify HelloAuthFunc
	commit HelloCommitAuthFunc
}

func (gate *helloAdmissionGate) set(verify HelloAuthFunc) {
	gate.mu.Lock()
	gate.verify = verify
	gate.mu.Unlock()
}

func (gate *helloAdmissionGate) setCommit(verify HelloCommitAuthFunc) {
	gate.mu.Lock()
	gate.commit = verify
	gate.mu.Unlock()
}

func (gate *helloAdmissionGate) current() HelloAuthFunc {
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.verify
}

func (gate *helloAdmissionGate) commitCurrent() HelloCommitAuthFunc {
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.commit
}

// enterWorldGate owns the replaceable pre-dispatch identity verifier.
type enterWorldGate struct {
	mu     sync.RWMutex
	verify EnterWorldAuthFunc
}

func (gate *enterWorldGate) set(verify EnterWorldAuthFunc) {
	gate.mu.Lock()
	gate.verify = verify
	gate.mu.Unlock()
}

func (gate *enterWorldGate) current() EnterWorldAuthFunc {
	gate.mu.RLock()
	defer gate.mu.RUnlock()
	return gate.verify
}

// handlerRegistry owns opcode routing independently of live sessions.
type handlerRegistry struct {
	mu             sync.RWMutex
	byOpcode       map[uint16]HandlerFunc
	defaultHandler HandlerFunc
}

func newHandlerRegistry() handlerRegistry {
	return handlerRegistry{
		byOpcode: make(map[uint16]HandlerFunc),
	}
}

func (registry *handlerRegistry) register(opcode uint16, handler HandlerFunc) {
	registry.mu.Lock()
	registry.byOpcode[opcode] = handler
	registry.mu.Unlock()
}

func (registry *handlerRegistry) setDefault(handler HandlerFunc) {
	registry.mu.Lock()
	registry.defaultHandler = handler
	registry.mu.Unlock()
}

func (registry *handlerRegistry) lookup(opcode uint16) HandlerFunc {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	if handler := registry.byOpcode[opcode]; handler != nil {
		return handler
	}
	return registry.defaultHandler
}

// sessionHooks owns registration and immutable dispatch snapshots for the
// three session lifecycle phases.
type sessionHooks struct {
	mu      sync.RWMutex
	open    []func(*Session)
	resumed []func(*Session)
	close   []func(*Session, error)
}

func (hooks *sessionHooks) addOpen(hook func(*Session)) {
	hooks.mu.Lock()
	hooks.open = append(hooks.open, hook)
	hooks.mu.Unlock()
}

func (hooks *sessionHooks) addResumed(hook func(*Session)) {
	hooks.mu.Lock()
	hooks.resumed = append(hooks.resumed, hook)
	hooks.mu.Unlock()
}

func (hooks *sessionHooks) addClose(hook func(*Session, error)) {
	hooks.mu.Lock()
	hooks.close = append(hooks.close, hook)
	hooks.mu.Unlock()
}

func (hooks *sessionHooks) openSnapshot() []func(*Session) {
	hooks.mu.RLock()
	defer hooks.mu.RUnlock()
	return slices.Clone(hooks.open)
}

func (hooks *sessionHooks) resumedSnapshot() []func(*Session) {
	hooks.mu.RLock()
	defer hooks.mu.RUnlock()
	return slices.Clone(hooks.resumed)
}

func (hooks *sessionHooks) closeSnapshot() []func(*Session, error) {
	hooks.mu.RLock()
	defer hooks.mu.RUnlock()
	return slices.Clone(hooks.close)
}
