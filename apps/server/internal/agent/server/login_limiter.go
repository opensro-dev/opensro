/*
===========================================================================

login_limiter.go - the Agent's per-client pacing allowance

Login, client incidents and refusal lines are paced per key (a client
address, an account) with one shared allowance: a burst of ten, then
one every six seconds. The buckets themselves are platform/pacer's.

===========================================================================
*/

package agentserver

import (
	"net"
	"time"

	"opensro.online/server/internal/platform/pacer"
)

const (
	loginAttemptBurst     = 10
	loginAttemptRefill    = 6 * time.Second
	maxLoginClientBuckets = 4096
)

/*
================
newLoginLimiter

The pacer behind loginAttempts and the incident and refusal pacers:
expensive bcrypt work and log lines bounded per client. Agent is a
loopback service behind the deployment's TLS edge, so its caller
supplies the already-resolved client address.
================
*/
func newLoginLimiter(now func() time.Time) *pacer.Keyed {
	return pacer.New(pacer.Config{
		Burst:   loginAttemptBurst,
		Refill:  loginAttemptRefill,
		MaxKeys: maxLoginClientBuckets,
		Now:     now,
	})
}

/*
================
loginClientKey

The host of an address, or the address itself when it has no port.
================
*/
func loginClientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return "unknown"
	}
	return remoteAddr
}
