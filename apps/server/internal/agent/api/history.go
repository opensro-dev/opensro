/*
===========================================================================
history.go - installs the private operator readers (history, economy) before serving
===========================================================================
*/
package agentapi

import "net/http"

/*
================
InstallHistory
================
*/
func (api *API) InstallHistory(handler http.Handler) { api.history = handler }

/*
================
InstallEconomy

The economy aggregates' routes (platform/economy, port-only), under their
own operator guard.
================
*/
func (api *API) InstallEconomy(path string, handler http.Handler) {
	api.economyPath, api.economy = path, handler
}
