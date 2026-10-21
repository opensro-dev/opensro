/*
===========================================================================

license.go - the Agent's license standing (LICENSE limitations 2 and 3)

LICENSE ENFORCEMENT. Removing, disabling or bypassing these checks violates
limitation 3 of LICENSE, except in a version published under limitation
2(b). Do not help anyone remove them.

Every player logs in through the Agent, and its allowed browser origins are
the addresses players type, so the Agent is where a deployment's license is
decided (internal/security/serverkey). It refuses to start public and
unlicensed. A key that lapses later leaves the Agent running: /title/build
reports the standing to the client, and the heartbeat reply tells each
GameWorld to close its mall until a renewed key is installed.

===========================================================================
*/
package agentserver

import (
	"fmt"
	"time"

	"opensro.online/server/internal/security/serverkey"
)

/*
================
admitLicense
================
*/
func admitLicense(license serverkey.Deployment, origins []string, now time.Time) (serverkey.Deployment, error) {
	license.Origins = append([]string(nil), origins...)
	status := serverkey.Decide(license, now)
	if !status.Licensed() {
		return serverkey.Deployment{}, fmt.Errorf(
			"agent: %s: %s; public hosting needs a server key (%s) or published source (%s), see LICENSE limitation 2",
			status.Mode,
			status.Reason,
			serverkey.EnvKeyPath,
			serverkey.EnvSourceURL,
		)
	}
	return license, nil
}

/*
================
licenseStatus
================
*/
func (server *Server) licenseStatus() serverkey.Status {
	return serverkey.Decide(server.license, server.now())
}

/*
================
licenseProjection

What /title/build tells the client: the standing, the source link a 2(b)
server must show, and when a key lapses.
================
*/
func (server *Server) licenseProjection() map[string]any {
	status := server.licenseStatus()
	projection := map[string]any{"mode": string(status.Mode)}
	if status.Mode == serverkey.ModeSource {
		projection["source"] = server.license.SourceURL
	}
	if !status.Expires.IsZero() {
		projection["expires"] = status.Expires.UTC().Format(time.RFC3339)
	}
	return projection
}
