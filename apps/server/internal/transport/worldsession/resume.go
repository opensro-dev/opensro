/*
===========================================================================

resume.go - the bridge's resumed-session registry

The Hub's OnSessionResumed hooks fire synchronously on the HELLO goroutine,
right after the session reattaches and the ordered ring replays what the
client last applied. A hook may only record: any game work it did inline
would run on the handshake path and could overtake the replay. So the
bridge records the resumed session's scene identity here, and the
simulation ticker drains the registry at the start of its next tick and
re-pushes current non-player mover state to that viewer alone
(internal/game/world/simulation/resume.go, #1818), landing behind the
replayed stream on the ordered lane.

A mark is one-shot: DrainResumedSessions empties the registry. A mark whose
scene identity the tick no longer snapshots (the character re-entered under
a new revision, or has not entered yet) is dropped by the ticker - the
fresh entry bootstraps that session's state itself.

===========================================================================
*/

package worldsession

import (
	"sort"

	"opensro.online/server/internal/transport"
)

/*
================
noteSessionResumed

noteSessionResumed is the bridge's OnSessionResumed hook: record the scene
identity, nothing more (see file banner). Safe on the handshake goroutine.
================
*/
func (b *Bridge) noteSessionResumed(s *transport.Session) {
	id := SessionSceneID(s)
	b.resumeMu.Lock()
	if b.resumed == nil {
		b.resumed = make(map[string]struct{})
	}
	b.resumed[id] = struct{}{}
	b.resumeMu.Unlock()
}

/*
================
DrainResumedSessions

DrainResumedSessions returns the recorded scene identities and clears the
registry (simulation.ResumeSource). Identity strings sort so a tick serving
several resumes drains them deterministically.
================
*/
func (b *Bridge) DrainResumedSessions() []string {
	b.resumeMu.Lock()
	defer b.resumeMu.Unlock()
	if len(b.resumed) == 0 {
		return nil
	}
	out := make([]string, 0, len(b.resumed))
	for id := range b.resumed {
		out = append(out, id)
	}
	clear(b.resumed)
	sort.Strings(out)
	return out
}
