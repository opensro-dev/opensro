/*
===========================================================================

census_install_test.go - the census routes land on the operator mux

===========================================================================
*/
package agentapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
================
TestInstallCensusRoutes

An installed census handler answers its exact path and its
subroutes through the API mux; without the install the path is the
mux's own 404.
================
*/
func TestInstallCensusRoutes(t *testing.T) {
	api, _ := newTestAPI(t)
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	api.InstallCensus("/internal/operations/census", stub)
	handler := api.Handler()
	for _, path := range []string{"/internal/operations/census", "/internal/operations/census/online"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
		if recorder.Code != http.StatusTeapot {
			t.Fatalf("GET %s: got %d, want the installed handler", path, recorder.Code)
		}
	}

	bare, _ := newTestAPI(t)
	recorder := httptest.NewRecorder()
	bare.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/internal/operations/census/online", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("without the install: got %d, want 404", recorder.Code)
	}
}
