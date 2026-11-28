/*
===========================================================================

login_source.go - where a login came from, without the address

Every login event (refused or succeeded) carries the keyed hash of the
client's address and of its network, plus its browser, so "who logged
into my account" has an answer (#4815). The hashes are the connection
journal's own (connjournal.AddressKeys): no raw address is stored, and
two logins from one address or one /24 (/48) compare equal.

The key is the Agent's, minted once in its state directory and read on
every boot (connjournal.LoadOrCreateKey), so hashes stay comparable
across restarts. It is not the shard's journal key: an Agent and its
GameWorlds may run on different hosts. A login and the session it
opens are matched by account and time instead.

This is a server rule native does not have (port-only, not native): it
runs only while the census does (SRO_CONNECTION_JOURNAL, default off);
off, the key is nil and the events carry nothing new.

===========================================================================
*/
package agentserver

import (
	"net/http"

	"opensro.online/server/internal/platform/connjournal"
)

/*
================
loginSourceFields

The login event's fields with the source added: ip_key, net_key and
user_agent. A nil key (the census off) or an address that does not
parse adds nothing, never a guess.
================
*/
func (server *Server) loginSourceFields(r *http.Request, fields map[string]string) map[string]string {
	if server.loginSourceKey == nil {
		return fields
	}
	if fields == nil {
		fields = map[string]string{}
	}
	ipKey, netKey := connjournal.AddressKeys(server.loginSourceKey, clientIP(r))
	if ipKey != "" {
		fields["ip_key"], fields["net_key"] = ipKey, netKey
	}
	if agent := r.UserAgent(); agent != "" {
		// The journal's own bound, so a login and a session keep the same text.
		if len(agent) > connjournal.MaxUserAgentBytes {
			agent = agent[:connjournal.MaxUserAgentBytes]
		}
		fields["user_agent"] = agent
	}
	return fields
}
