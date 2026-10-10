/*
===========================================================================

privacy.go - the website's privacy write (port-only)

	PUT /v1/accounts/{id}/characters/{name}/hidden  {"hidden": bool} -> 204

The /account page calls it for the signed-in player's own characters. It is
served on the same loopback listener as the reads but only when a write
token is configured, and every request carries that bearer token: the
listener keeps it off the network, the token keeps other local processes
out (the provisioning API's two guards). The character must belong to the
account in the path; any other name answers 404, exactly like an unknown
one, so the write cannot be used to probe who owns what.

A successful write empties the answer cache: a player who hides is gone
from every read at once, not after the cache period.

===========================================================================
*/
package publicstats

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// EnvTokenPath names the file holding the write token (the provisioning
// token the website already holds). Unset or empty disables the write.
const EnvTokenPath = "SRO_PUBLIC_API_TOKEN_PATH"

// minWriteTokenBytes refuses a guessable token.
const minWriteTokenBytes = 32

// maxPrivacyBody bounds the request body.
const maxPrivacyBody = 256

// ErrNotOwned answers 404: no such character on that account.
var ErrNotOwned = errors.New("character not found on this account")

/*
================
installPrivacy

Adds the write route when the token and the setter are both present.
================
*/
func (s *Service) installPrivacy(mux *http.ServeMux) {
	token := strings.TrimSpace(s.src.WriteToken)
	if len(token) < minWriteTokenBytes || s.src.SetHidden == nil {
		return
	}
	digest := sha256.Sum256([]byte(token))
	mux.HandleFunc("PUT /v1/accounts/{id}/characters/{name}/hidden", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		presentedDigest := sha256.Sum256([]byte(presented))
		if !found || subtle.ConstantTimeCompare(presentedDigest[:], digest[:]) != 1 {
			writeStatus(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var body struct {
			Hidden *bool `json:"hidden"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrivacyBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.Hidden == nil {
			writeStatus(w, http.StatusBadRequest, "body must be {\"hidden\": true|false}")
			return
		}
		account, name := r.PathValue("id"), r.PathValue("name")
		if account == "" || name == "" || len(name) > maxNameLength {
			writeStatus(w, http.StatusNotFound, "not found")
			return
		}
		if err := s.src.SetHidden(account, name, *body.Hidden); err != nil {
			if errors.Is(err, ErrNotOwned) {
				writeStatus(w, http.StatusNotFound, "not found")
				return
			}
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		s.mu.Lock()
		s.cache = map[string]cacheEntry{}
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
}

/*
================
writeStatus
================
*/
func writeStatus(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	body, _ := json.Marshal(map[string]string{"error": message})
	_, _ = w.Write(body)
}
