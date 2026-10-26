/*
===========================================================================

registration.go - public username and password signup at the account authority

Port-only, not native. Signup uses the existing account store and its bcrypt
policy. The username is the login ID, so ownership and login share one identity.
Confirmation is reserved as a policy option and fails closed until a mail
delivery and token verification flow exists.

===========================================================================
*/
package agentserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/security/auth"
)

const registrationMinPasswordCharacters = 8

/*
================
RegistrationOptions
================
*/
type RegistrationOptions struct {
	Enabled                  bool
	RequireEmailConfirmation bool
}

/*
================
accountCreator
================
*/
type accountCreator interface {
	Create(context.Context, string, string) (auth.AccountInfo, error)
}

/*
================
handleRegister

No browser session is created: the player explicitly logs in after signup.
The login budget and password slots also bound unauthenticated signup work.
================
*/
func (server *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creator, ok := server.accounts.(accountCreator)
	if !server.registration.Enabled || !ok {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "message": "Registration is disabled."})
		return
	}
	if server.registration.RequireEmailConfirmation {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "message": "Registration is unavailable while email confirmation is being configured."})
		return
	}
	if !server.loginAttempts.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", "6")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "message": "Too many attempts. Please try again shortly."})
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "Invalid registration request."})
		return
	}
	username := strings.TrimSpace(request.Username)
	if !domain.AccountIDValid(username) || domain.FoldAccountID(username) == domain.ReservedAccountID {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "Enter a valid username of at most 64 bytes."})
		return
	}
	if utf8.RuneCountInString(request.Password) < registrationMinPasswordCharacters || len(request.Password) > auth.MaxPasswordBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "Password must have at least 8 characters and at most 72 UTF-8 bytes."})
		return
	}
	select {
	case server.passwordSlots <- struct{}{}:
		defer func() { <-server.passwordSlots }()
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "message": "Registration is busy. Please try again shortly."})
		return
	}
	_, err := creator.Create(r.Context(), username, request.Password)
	if errors.Is(err, auth.ErrAccountExists) {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "message": "This username is already taken. Please choose another or log in."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "Could not create the account. Please try again."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "username": username, "emailConfirmationRequired": false})
}
