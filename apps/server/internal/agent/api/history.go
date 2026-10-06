/*
===========================================================================
history.go - installs the private operator history reader before serving
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
