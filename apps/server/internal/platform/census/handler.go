/*
===========================================================================

handler.go - /internal/operations/census on the operator listener

GET only, for the staff console on the same host (operatorauth: the
shard's operator-token, loopback, no browser origin, no forwarding).
With no token file the routes answer nobody, as every operator read
does. Every allowed read is access-logged (route and, for a person
read, the character), so the history journal keeps who looked at
whom; the log carries no hash and no address, like the answers.

Answers never carry a raw address or a hash value (census.go), and
errors are the operator contract's {"error":{"code","message"}}.

===========================================================================
*/
package census

import (
	"encoding/json"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/operatorauth"
)

// onlineWindows maps the online route's window names to its span and
// series bucket, both milliseconds.
var onlineWindows = map[string][2]int64{
	"24h": {windowDayMs, 15 * 60 * 1000},
	"7d":  {7 * windowDayMs, 60 * 60 * 1000},
}

/*
================
Handler

The census routes over service, guarded by tokenPath.
================
*/
func Handler(service *Service, tokenPath string) (http.Handler, error) {
	guard, found, err := operatorauth.Load(tokenPath, "census")
	if err != nil {
		return nil, err
	}
	if !found {
		return http.NotFoundHandler(), nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guard.Allows(r) {
			writeError(w, http.StatusForbidden, "unauthorized", "operator authentication required")
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		route := strings.Trim(strings.TrimPrefix(r.URL.Path, Path), "/")
		log.WithField("route", route).Info("census: read")
		switch route {
		case "online":
			serveOnline(w, r, service)
		case "uniques":
			serve(w, func() (any, error) { return service.Uniques() })
		case "clusters":
			serve(w, func() (any, error) { return service.Clusters() })
		case "person":
			servePerson(w, r, service)
		case "signals":
			serve(w, func() (any, error) { return service.Signals() })
		default:
			http.NotFound(w, r)
		}
	}), nil
}

// serveOnline answers the online route with its window checked.
func serveOnline(w http.ResponseWriter, r *http.Request, service *Service) {
	name := r.URL.Query().Get("window")
	if name == "" {
		name = "24h"
	}
	window, known := onlineWindows[name]
	if !known {
		writeError(w, http.StatusBadRequest, "bad_window", "window must be 24h or 7d")
		return
	}
	serve(w, func() (any, error) { return service.OnlineNow(window[0], window[1]) })
}

// servePerson answers the person route; the character read is the
// one access the audit must name, so it logs the name itself.
func servePerson(w http.ResponseWriter, r *http.Request, service *Service) {
	character := r.URL.Query().Get("character")
	if character == "" || len(character) > 256 {
		writeError(w, http.StatusBadRequest, "bad_character", "a character name is required")
		return
	}
	log.WithField("character", character).Info("census: person read")
	person, found, err := service.Person(character)
	if err != nil {
		fail(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no_such_character", "that character has not entered")
		return
	}
	writeJSON(w, person)
}

// serve answers one computed read, mapping its failure to the
// contract's internal error (logged, naming no internals).
func serve(w http.ResponseWriter, read func() (any, error)) {
	answer, err := read()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, answer)
}

// fail logs a read failure and answers the contract's internal error.
func fail(w http.ResponseWriter, err error) {
	log.WithError(err).Warn("census: read failed")
	writeError(w, http.StatusInternalServerError, "internal", "the census could not be read")
}

/*
================
writeError
================
*/
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
