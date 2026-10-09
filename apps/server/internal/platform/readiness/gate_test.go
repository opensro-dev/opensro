package readiness

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGateSeparatesLivenessFromReadiness(t *testing.T) {
	gate := NewGate()

	assertStatus(t, http.HandlerFunc(HealthHandler), PathHealth, http.StatusOK)
	assertStatus(t, http.HandlerFunc(gate.ReadyHandler), PathReady, http.StatusServiceUnavailable)

	gate.Open()
	assertStatus(t, http.HandlerFunc(gate.ReadyHandler), PathReady, http.StatusOK)

	gate.Close()
	assertStatus(t, http.HandlerFunc(gate.ReadyHandler), PathReady, http.StatusServiceUnavailable)
}

// TestRefusalTellsStartingFromDraining: a gate that never opened answers
// PROCESS_STARTING (the client retries); one that opened and closed again
// answers PROCESS_DRAINING (#246).
func TestRefusalTellsStartingFromDraining(t *testing.T) {
	gate := NewGate()
	if code, message := gate.Refusal("GameWorld"); code != CodeStarting || message != "The GameWorld is starting." {
		t.Fatalf("new gate refusal = %q %q", code, message)
	}
	gate.Open()
	if gate.Starting() {
		t.Fatal("an open gate reports starting")
	}
	gate.Close()
	if code, message := gate.Refusal("GameWorld"); code != CodeDraining || message != "The GameWorld is draining." {
		t.Fatalf("closed-after-open refusal = %q %q", code, message)
	}
	var missing *Gate
	if missing.Starting() || missing.Ready() {
		t.Fatal("a nil gate reports starting or ready")
	}
}

func TestHealthAndReadyAreReadOnly(t *testing.T) {
	gate := NewGate()
	for _, handler := range []http.Handler{
		http.HandlerFunc(HealthHandler),
		http.HandlerFunc(gate.ReadyHandler),
	} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
		}
	}
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s status = %d, want %d", path, response.Code, want)
	}
}
