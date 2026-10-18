package agentapi

import (
	"net/http"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/security/auth"
)

// handleTransportAdmissionToken mints the short-lived one-use ticket carried
// by the transport HELLO. The authenticated Agent session supplies account and shard;
// callers cannot select either claim in the request body.
func (api *API) handleTransportAdmissionToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct{}
	if err := decodeJSONRequest(r.Body, &request); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "BAD_REQUEST"})
		return
	}
	identity := requestIdentity(r)
	expiresAt := api.now().Add(transportAdmissionTokenTTL)
	token, err := auth.MintTransportAdmission(
		api.enterWorldAuthSecret,
		identity.accountID,
		identity.shardID,
		expiresAt,
	)
	if err != nil {
		log.WithError(err).Error("agentapi: transport admission token mint failed")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "code": "INTERNAL"})
		return
	}
	api.recordIssuedTicket(identity.accountID, token, expiresAt)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"token":         token,
		"expiresAtUnix": expiresAt.Unix(),
	})
}
