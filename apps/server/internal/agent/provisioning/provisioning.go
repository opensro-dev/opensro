/*
===========================================================================

provisioning.go - the Agent's private account-provisioning API

Trusted companion services (the website) create and manage game accounts
through this API; the Agent stays the only holder of passwords. It is served
on its own loopback-only listener, never on the Agent API the edge proxies,
and every request carries a shared bearer token. Both guards are required:
the listener keeps it off the network, the token keeps other local
processes out.

	POST /v1/accounts                     {"id","password"}  -> 201 account
	GET  /v1/accounts/{id}                                    -> 200 account
	POST /v1/accounts/{id}/verify         {"password"}        -> 200 {"valid"}
	PUT  /v1/accounts/{id}/password       {"password"}        -> 204
	PUT  /v1/accounts/{id}/disabled       {"disabled"}        -> 204

Errors are JSON {"code","message"}; codes are stable API, messages are not.

===========================================================================
*/
package provisioning

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"opensro.online/server/internal/security/auth"
)

// DefaultPort is the provisioning API's loopback port when nothing else is
// configured. The web site's provisioning client calls it, and the Agent's
// Nomad job and deployer default to it; this is the one place it is set.
const DefaultPort = 8789

const (
	maxRequestBytes = 4 << 10
	requestTimeout  = 10 * time.Second
)

// Accounts is what the API needs from the account authority (accounts.go).
type Accounts interface {
	Create(ctx context.Context, accountID, password string) (auth.AccountInfo, error)
	Lookup(accountID string) (auth.AccountInfo, error)
	Verify(ctx context.Context, accountID, password string) (bool, error)
	SetPassword(ctx context.Context, accountID, password string) error
	SetDisabled(ctx context.Context, accountID string, disabled bool) error
}

// Server is one provisioning API instance.
type Server struct {
	accounts  Accounts
	tokenHash [sha256.Size]byte
}

/*
================
New
================
*/
func New(accounts Accounts, token string) (*Server, error) {
	if accounts == nil {
		return nil, errors.New("provisioning: account authority is required")
	}
	token = strings.TrimSpace(token)
	if len(token) < auth.MinProvisioningTokenBytes {
		return nil, fmt.Errorf("provisioning: token must be at least %d bytes", auth.MinProvisioningTokenBytes)
	}
	return &Server{accounts: accounts, tokenHash: sha256.Sum256([]byte(token))}, nil
}

/*
================
Handler
================
*/
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/accounts", server.handleCreate)
	mux.HandleFunc("GET /v1/accounts/{id}", server.handleLookup)
	mux.HandleFunc("POST /v1/accounts/{id}/verify", server.handleVerify)
	mux.HandleFunc("PUT /v1/accounts/{id}/password", server.handlePassword)
	mux.HandleFunc("PUT /v1/accounts/{id}/disabled", server.handleDisabled)
	return server.authorize(mux)
}

/*
================
authorize

Compares SHA-256 digests so the comparison time is independent of the
presented token's length.
================
*/
func (server *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		digest := sha256.Sum256([]byte(presented))
		if !found || subtle.ConstantTimeCompare(digest[:], server.tokenHash[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or wrong provisioning token")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

//============================================================================

type accountDocument struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Disabled  bool      `json:"disabled"`
}

/*
================
handleCreate
================
*/
func (server *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	if !decode(w, r, &request) {
		return
	}
	info, err := server.accounts.Create(r.Context(), request.ID, request.Password)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, accountDocument{ID: info.ID, CreatedAt: info.CreatedAt, Disabled: info.Disabled})
}

/*
================
handleLookup
================
*/
func (server *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	info, err := server.accounts.Lookup(r.PathValue("id"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, accountDocument{ID: info.ID, CreatedAt: info.CreatedAt, Disabled: info.Disabled})
}

/*
================
handleVerify

A wrong password and a missing or disabled account all answer valid=false:
the caller learns nothing it could use to enumerate accounts.
================
*/
func (server *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &request) {
		return
	}
	valid, err := server.accounts.Verify(r.Context(), r.PathValue("id"), request.Password)
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"valid": valid})
}

/*
================
handlePassword
================
*/
func (server *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &request) {
		return
	}
	if err := server.accounts.SetPassword(r.Context(), r.PathValue("id"), request.Password); err != nil {
		writeAccountError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
================
handleDisabled
================
*/
func (server *Server) handleDisabled(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Disabled *bool `json:"disabled"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.Disabled == nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "disabled is required")
		return
	}
	if err := server.accounts.SetDisabled(r.Context(), r.PathValue("id"), *request.Disabled); err != nil {
		writeAccountError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

//============================================================================

/*
================
decode

Strict JSON: one object, known fields only, bounded size.
================
*/
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be one JSON object with known fields")
		return false
	}
	if decoder.More() {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be one JSON object")
		return false
	}
	return true
}

/*
================
writeAccountError
================
*/
func writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrAccountExists):
		writeError(w, http.StatusConflict, "ACCOUNT_EXISTS", "an account with that id already exists")
	case errors.Is(err, auth.ErrAccountNotFound):
		writeError(w, http.StatusNotFound, "ACCOUNT_NOT_FOUND", "no such account")
	case errors.Is(err, auth.ErrAccountIDInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_ID", "account id is invalid")
	case errors.Is(err, auth.ErrPasswordInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_PASSWORD", auth.ErrPasswordInvalid.Error())
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, http.StatusServiceUnavailable, "BUSY", "the account authority is busy")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "account operation failed")
	}
}

/*
================
writeError
================
*/
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
