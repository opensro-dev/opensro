/*
===========================================================================

build.go - /title/build, the commit this Agent runs and for how long

The FPS chip names what the server is running so a stale deployment is
visible from inside the game. The revision is the one the Go toolchain
stamps into every binary built inside a git checkout (vcs.revision); a
binary built without it answers an empty revision and the chip shows
nothing. The toolchain stamps no commit message, so the deployer passes the
subject in at link time (BuildSubjectSymbol, base64 so that no quote or
space in it can break -ldflags); without it the subject is empty. Uptime is
counted from the Agent's own clock, so the client can add its local
elapsed time without trusting either wall clock.

===========================================================================
*/
package agentserver

import (
	"encoding/base64"
	"net/http"
	"runtime/debug"
)

const buildPath = "/title/build"

// BuildSubjectSymbol is the -X target the deployer sets to the base64 of
// the commit subject it builds from.
const BuildSubjectSymbol = "opensro.online/server/internal/agent/server.buildSubjectBase64"

// buildSubjectBase64 is set at link time; see BuildSubjectSymbol.
var buildSubjectBase64 string

/*
================
buildSubject

The commit subject the deployer stamped, or "" when there is none.
================
*/
func buildSubject() string {
	subject, err := base64.StdEncoding.DecodeString(buildSubjectBase64)
	if err != nil {
		return ""
	}
	return string(subject)
}

/*
================
buildRevision

The vcs.revision setting of the running binary, or "" when the build
carried no version-control stamp.
================
*/
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

/*
================
handleBuild
================
*/
func (server *Server) handleBuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	uptime := server.now().Sub(server.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"build": map[string]any{
			"revision":      buildRevision(),
			"subject":       buildSubject(),
			"uptimeSeconds": int64(uptime.Seconds()),
		},
	})
}
