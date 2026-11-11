package transport_test

/*
===========================================================================

boot_health_test.go - healthz answers from process start (#3186)

The liveness endpoint must exist before the game does: StartHealth
binds the TCP listener while the boot is still loading, healthz names
the phase it is in, and everything a player could use stays refused
until Start and readiness. The boot phases and their durations ride
the metrics document.
===========================================================================
*/

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

/*
================
TestHealthAnswersDuringBoot

StartHealth alone: healthz 200 with the phase, readyz refused, the
WebSocket path refused; a phase change shows at once; Start then
serves as before and readiness is the listeners-up answer until a
check is injected.
================
*/
func TestHealthAnswersDuringBoot(t *testing.T) {
	srv, err := transport.NewServer(transport.Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	if err := srv.StartHealth(); err != nil {
		t.Fatal(err)
	}
	base := "http://" + srv.WSAddr()
	if code, body := httpGet(t, base+transport.PathHealth); code != 200 || body != "ok "+transport.BootPhaseLoadingData {
		t.Fatalf("healthz during data load = %d %q, want 200 with the phase", code, body)
	}
	if code, _ := httpGet(t, base+transport.PathReady); code != 503 {
		t.Fatalf("readyz during boot = %d, want 503", code)
	}
	if code, _ := httpGet(t, base+transport.PathWS); code != 503 {
		t.Fatalf("websocket during boot = %d, want 503", code)
	}

	srv.SetBootPhase(transport.BootPhaseOpeningStore)
	if code, body := httpGet(t, base+transport.PathHealth); code != 200 || body != "ok "+transport.BootPhaseOpeningStore {
		t.Fatalf("healthz while the store opens = %d %q, want 200 with the new phase", code, body)
	}
	if code, body := httpGet(t, base+transport.PathMetrics); code != 200 {
		t.Fatalf("metrics during boot = %d", code)
	} else {
		var document struct {
			Boot transport.BootStatus `json:"boot"`
		}
		if err := json.Unmarshal([]byte(body), &document); err != nil {
			t.Fatal(err)
		}
		if document.Boot.Phase != transport.BootPhaseOpeningStore ||
			len(document.Boot.Phases) != 1 || document.Boot.Phases[0].Name != transport.BootPhaseLoadingData {
			t.Fatalf("metrics boot = %+v, want the loading phase closed and the store opening", document.Boot)
		}
	}

	installSmokeAdmissionVerifier(srv)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	if code, body := httpGet(t, base+transport.PathReady); code != 200 || body != "ready" {
		t.Fatalf("readyz after Start = %d %q, want the listeners-up answer", code, body)
	}
	srv.SetBootPhase(transport.BootPhaseReady)
	if code, body := httpGet(t, base+transport.PathHealth); code != 200 || body != "ok "+transport.BootPhaseReady {
		t.Fatalf("healthz when ready = %d %q, want 200 ok ready", code, body)
	}
	status := srv.BootStatus()
	if status.Phase != transport.BootPhaseReady || len(status.Phases) != 2 {
		t.Fatalf("boot status = %+v, want two closed phases and ready running", status)
	}
}
