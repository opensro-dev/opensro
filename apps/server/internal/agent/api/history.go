/*
===========================================================================
history.go - installs the private operator readers (history, economy, settings) before serving
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

/*
================
InstallSettings

The staff console's settings read (platform/settingsapi, port-only),
under its own operator guard.
================
*/
func (api *API) InstallSettings(path string, handler http.Handler) {
	api.settingsPath, api.settings = path, handler
}
