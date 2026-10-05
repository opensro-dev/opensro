/*
===========================================================================

onboarding.go - /title/onboarding, whether the client shows its tour

The tour itself lives in the browser client; the Agent only answers the
deployer's SRO_ONBOARDING choice (internal/agent/onboarding).

===========================================================================
*/
package agentserver

import "net/http"

const onboardingPath = "/title/onboarding"

/*
================
handleOnboarding
================
*/
func (server *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"onboarding": map[string]any{"enabled": !server.onboardingOff},
	})
}
