/*
===========================================================================

browser_session.go - the browser's HttpOnly copy of the Agent session

The signed Agent session lives in an HttpOnly cookie so a page reload can
resume it, plus a character hint cookie that remembers the last world
entry. Neither is an admission credential on its own: world entry still
needs the one-use transport and EnterWorld tickets.

===========================================================================
*/
package agentserver

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/security/auth"
)

// The cookie only restores the existing signed Agent credential. World admission
// still requires the normal one-use transport and EnterWorld tickets.
const browserSessionCookie = "__Host-SROSession"
const loopbackSessionCookie = "SROLoopbackSession"

/*
================
browserCookieName
================
*/
func browserCookieName(r *http.Request) (string, bool) {
	host := r.URL.Hostname()
	if host == "" {
		host = r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	// Plain HTTP is supported only for the local development endpoint. A TLS
	// edge keeps the Secure cookie even when it forwards over loopback HTTP.
	ip := net.ParseIP(strings.Trim(host, "[]"))
	local := host == "localhost" || ip != nil && ip.IsLoopback()
	if local && r.TLS == nil && r.URL.Scheme != "https" && r.Header.Get("X-Forwarded-Proto") != "https" {
		return loopbackSessionCookie, false
	}
	return browserSessionCookie, true
}

/*
================
setBrowserSession
================
*/
func (server *Server) setBrowserSession(w http.ResponseWriter, r *http.Request, token string, clear bool) {
	name, secure := browserCookieName(r)
	cookie := &http.Cookie{Name: name, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
		MaxAge: int(auth.AgentSessionLifetime / time.Second), Expires: server.now().Add(auth.AgentSessionLifetime)}
	if clear {
		cookie.Value = ""
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, cookie)
	// A fresh login or logout must not inherit another account's character hint.
	server.setBrowserCharacter(w, r, "", time.Unix(1, 0))
}

/*
================
rememberBrowserCharacter

This is a navigation hint, never an admission credential. Only remember a
selection accepted by GameWorld, made with this browser's authenticated token.
================
*/
func (server *Server) rememberBrowserCharacter(w http.ResponseWriter, r *http.Request, body, response []byte, status int) {
	if r.Method != http.MethodPost || r.URL.Path != "/auth/enterworld-token" || status != http.StatusOK {
		return
	}
	name, _ := browserCookieName(r)
	cookie, err := r.Cookie(name)
	if err != nil || r.Header.Get("Authorization") != "Bearer "+cookie.Value {
		return
	}
	claims, err := server.sessionSigner.Verify(cookie.Value, server.now())
	if err != nil {
		return
	}
	var input struct {
		CharacterName string `json:"characterName"`
	}
	var result struct {
		OK    bool   `json:"ok"`
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &input) != nil || json.Unmarshal(response, &result) != nil || !result.OK || result.Token == "" || input.CharacterName == "" || len(input.CharacterName) > 64 {
		return
	}
	server.setBrowserCharacter(w, r, input.CharacterName, claims.ExpiresAt)
}

/*
================
setBrowserCharacter
================
*/
func (server *Server) setBrowserCharacter(w http.ResponseWriter, r *http.Request, character string, expires time.Time) {
	name, secure := browserCookieName(r)
	maxAge := int(expires.Sub(server.now()) / time.Second)
	if character == "" || maxAge <= 0 {
		maxAge = -1
		character = ""
	}
	http.SetCookie(w, &http.Cookie{Name: name + "Character", Value: base64.RawURLEncoding.EncodeToString([]byte(character)), Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: maxAge, Expires: expires})
}

/*
================
browserIdentity

The account behind this browser's session cookie. present is false when
there is no cookie at all; ok is false when there is one but it no longer
names a live account on an enabled division.
================
*/
type browserIdentity struct {
	token      string
	accountID  string
	definition shard.Definition
	character  string
}

/*
================
resolveBrowserIdentity
================
*/
func (server *Server) resolveBrowserIdentity(r *http.Request) (identity browserIdentity, present bool, ok bool) {
	name, _ := browserCookieName(r)
	cookie, err := r.Cookie(name)
	if err != nil {
		return browserIdentity{}, false, false
	}
	claims, err := server.sessionSigner.Verify(cookie.Value, server.now())
	if err != nil {
		return browserIdentity{}, true, false
	}
	definition, live := server.liveSession(claims)
	if !live {
		return browserIdentity{}, true, false
	}
	character := ""
	if hint, err := r.Cookie(name + "Character"); err == nil && len(hint.Value) <= 128 {
		if decoded, err := base64.RawURLEncoding.DecodeString(hint.Value); err == nil && len(decoded) <= 64 {
			character = string(decoded)
		}
	}
	return browserIdentity{token: cookie.Value, accountID: claims.AccountID, definition: definition, character: character}, true, true
}

/*
================
handleBrowserSession
================
*/
func (server *Server) handleBrowserSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Non-simple JSON requests plus the exact Origin allowlist prevent other
	// sites from reading or changing browser authentication.
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return
	}
	identity, present, ok := server.resolveBrowserIdentity(r)
	if !present {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	if !ok {
		server.setBrowserSession(w, r, "", true)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false})
		return
	}
	// Do not renew expiry on reload: the original login's twelve-hour bound is
	// authoritative, including across Agent restarts and signing-key rotation.
	definition := identity.definition
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sessionToken": identity.token, "divisionId": definition.ID,
		"nativeServerId": definition.NativeServerID, "nativeServerName": definition.Name, "transportUrl": definition.AdvertisedTransportURL(), "nextScene": "character-select", "resumeCharacter": identity.character})
}

/*
================
handleBrowserLogout
================
*/
func (server *Server) handleBrowserLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return
	}
	server.setBrowserSession(w, r, "", true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/*
================
handleBrowserCharacterSelect

Restart retains account authentication but retires the remembered world entry.
================
*/
func (server *Server) handleBrowserCharacterSelect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return
	}
	server.setBrowserCharacter(w, r, "", time.Unix(1, 0))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/*
================
liveSession

A verified session still names a stored account and an enabled shard. A
signature outlives the account it was minted for, so every path that acts on
a session's identity checks both, not the signature alone.
================
*/
func (server *Server) liveSession(claims auth.AgentSessionClaims) (shard.Definition, bool) {
	if _, exists := server.accounts.PasswordHash(claims.AccountID); !exists {
		return shard.Definition{}, false
	}
	definition, known := server.catalog.Resolve(claims.ShardID)
	return definition, known && definition.Enabled
}
