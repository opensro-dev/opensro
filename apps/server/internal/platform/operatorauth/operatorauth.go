/*
===========================================================================

operatorauth.go - the local operator check every private read endpoint uses

The GameWorld's operator reads (the history journal, the economy
aggregates) are answered only to the staff console on the same host: a
loopback peer addressing a loopback host, marked as a local diagnostics
request, with no browser Origin, no forwarding header, and the bearer
token from the shard's token file. One owner keeps those rules, so the
readers cannot drift apart.

===========================================================================
*/
package operatorauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// minTokenLength is the shortest token file the guard accepts.
const minTokenLength = 32

/*
================
Guard

The expected Authorization header, hashed so the comparison takes the
same time whatever the input's length.
================
*/
type Guard struct {
	want [sha256.Size]byte
}

/*
================
Load

The guard of tokenPath. found is false, with no error, when the file does
not exist: the endpoint then answers nobody.
================
*/
func Load(tokenPath, owner string) (guard Guard, found bool, err error) {
	bytes, err := os.ReadFile(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return Guard{}, false, nil
	}
	if err != nil {
		return Guard{}, false, err
	}
	token := strings.TrimSpace(string(bytes))
	if len(token) < minTokenLength {
		return Guard{}, false, fmt.Errorf("%s: operator token must have at least %d characters", owner, minTokenLength)
	}
	return Guard{want: sha256.Sum256([]byte("Bearer " + token))}, true, nil
}

/*
================
Guard.Allows
================
*/
func (g Guard) Allows(r *http.Request) bool {
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	host := r.Host
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	a, b := net.ParseIP(remote), net.ParseIP(strings.Trim(host, "[]"))
	got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if a == nil || !a.IsLoopback() || b == nil || !b.IsLoopback() || r.Header.Get("X-SRO-Local-Diagnostics") != "1" ||
		r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare(g.want[:], got[:]) != 1 {
		return false
	}
	for key := range r.Header {
		if strings.EqualFold(key, "Forwarded") || strings.HasPrefix(strings.ToLower(key), "x-forwarded-") {
			return false
		}
	}
	return true
}
