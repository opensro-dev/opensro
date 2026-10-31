/*
===========================================================================

target.go - where the load may point, and the address each bot sends from

The staircase runs against an isolated stack: a local dev cluster or a
staging host built for the purpose. It never runs against the live
shard. That is enforced here, before any socket opens: every target URL
must be loopback unless -allow-remote names the exception, and a host
under opensro.online is refused outright, flag or not - the production
service is not a load target anyone opts into by accident.

Each bot also dials the Agent from its own loopback source address. The
Agent bounds login work per client address (login_limiter.go: burst 10,
one token per 6 s) because in production the TLS edge presents one
address per player; five hundred bots behind one loopback address would
be refused as one abusive client, and the step window would measure the
ramp, not the load. A distinct 127/8 source per bot is the same signal
the limiter keys on in production - a real network identity per player,
not a forged header.

===========================================================================
*/

package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// prodHost is the live service's domain. It and every subdomain are
// refused as targets unconditionally.
const prodHost = "opensro.online"

/*
================
checkTargetURL

Refuse a target the staircase must not touch. Empty is allowed: the
optional endpoints (metrics, pprof, provisioning) may be unset.
================
*/
func checkTargetURL(raw string, allowRemote bool) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("target %q does not parse: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("target %q: scheme must be http or https", raw)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("target %q: no host", raw)
	}
	if host == prodHost || strings.HasSuffix(host, "."+prodHost) {
		return fmt.Errorf("target %q names the live service (%s); the load tool never targets it", raw, prodHost)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if !allowRemote {
		return fmt.Errorf("target %q is not loopback; pass -allow-remote only for an isolated staging stack", raw)
	}
	return nil
}

/*
================
isLoopbackHost
================
*/
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

/*
================
botSourceIP

The loopback source address one bot dials the Agent from: 127.1.x.y by
bot index, unique for the first 65,536 bots. The game connection does
not need it (admission there is the one-use ticket, not the address);
the Agent's per-address login limiter is the gate it answers.
================
*/
func botSourceIP(index int) net.IP {
	return net.IPv4(127, 1, byte(index>>8), byte(index))
}
