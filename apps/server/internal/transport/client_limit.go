/*
===========================================================================

client_limit.go - the per-client-address live session cap (#2671)

The conduct cap (Config.MaxSessionsPerClient, port-only) counts the
live sessions one client address holds across every account and
refuses the newcomer past it. It lives apart from hub.go's admission
flow so the hub owner stays its size; the admission call site and the
counting rule are one decision and move together.

===========================================================================
*/
package transport

import (
	"net"
)

/*
================
clientHost

The host part of a reported client address ("203.0.113.9" from
"203.0.113.9:41000"); a portless or unparseable address names itself.
Sessions count against a client limit by this, so the tabs and
accounts behind one address share one budget.
================
*/
func clientHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

/*
================
clientSlotOpen

Whether one more live session may attach from this client address
(Config.MaxSessionsPerClient, port-only #2671; 0 refuses nothing, the
native posture). Live sessions count by the host their connection
reports (Conn.RemoteAddr: behind the local edge, the client the edge
recorded), across every account - the per-account cap cannot see an
alt farm's several accounts, and this one can. A resume reattaches to
its own session and never reaches this check; a detached session in
resume grace reports no address and holds no slot. The count is taken
at admission, so HELLOs already in flight may overshoot the cap by
their number: a conduct speed bump accepts that, where a hard boundary
would have to queue.
================
*/
func (h *Hub) clientSlotOpen(address string) bool {
	limit := h.cfg.MaxSessionsPerClient
	host := clientHost(address)
	if limit <= 0 || host == "" {
		return true
	}
	h.mu.RLock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.RUnlock()
	live := 0
	for _, s := range sessions {
		if s.Evicted() || clientHost(s.RemoteAddr()) != host {
			continue
		}
		live++
		if live >= limit {
			return false
		}
	}
	return true
}
