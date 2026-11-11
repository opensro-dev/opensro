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
	"net/http"
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

/*
================
TestStandbyHealthAnswersBeforeTakeover

opensro-ops#281: StartStandbyHealth binds its own address at once,
serving healthz (200, naming the phase) and standbyz (503 while the
save-free load runs, 200 from the standby phase through ready, never
flapping back); the game paths stay off that listener, and Shutdown
closes it.
================
*/
func TestStandbyHealthAnswersBeforeTakeover(t *testing.T) {
	srv, err := transport.NewServer(transport.Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.StandbyHealthAddr(); got != "" {
		t.Fatalf("standby health address before start = %q, want none", got)
	}
	if err := srv.StartStandbyHealth("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	if err := srv.StartStandbyHealth("127.0.0.1:0"); err == nil {
		t.Fatal("a second standby health listener must refuse")
	}
	base := "http://" + srv.StandbyHealthAddr()
	if code, body := httpGet(t, base+transport.PathHealth); code != 200 || body != "ok "+transport.BootPhaseLoadingData {
		t.Fatalf("healthz while loading = %d %q, want 200 with the phase", code, body)
	}
	if code, _ := httpGet(t, base+transport.PathStandby); code != 503 {
		t.Fatalf("standbyz while loading = %d, want 503", code)
	}
	for _, path := range []string{transport.PathReady, transport.PathWS, transport.PathMetrics} {
		if code, _ := httpGet(t, base+path); code != 404 {
			t.Fatalf("%s on the standby listener = %d, want 404 (nothing a player could use)", path, code)
		}
	}
	srv.SetBootPhase(transport.BootPhaseStandby)
	if code, body := httpGet(t, base+transport.PathStandby); code != 200 || body != "ok "+transport.BootPhaseStandby {
		t.Fatalf("standbyz at standby = %d %q, want 200", code, body)
	}
	// The standbyz answer must also hold on the public listener once
	// it exists: the deployment check follows the allocation, not one
	// socket.
	installSmokeAdmissionVerifier(srv)
	if err := srv.StartHealth(); err != nil {
		t.Fatal(err)
	}
	if code, _ := httpGet(t, "http://"+srv.WSAddr()+transport.PathStandby); code != 200 {
		t.Fatalf("standbyz on the public listener = %d, want 200", code)
	}
	for _, phase := range []string{transport.BootPhaseOpeningStore, transport.BootPhaseWiring,
		transport.BootPhaseFilling, transport.BootPhaseReady} {
		srv.SetBootPhase(phase)
		if code, _ := httpGet(t, base+transport.PathStandby); code != 200 {
			t.Fatalf("standbyz at %q = %d, want it to stay 200", phase, code)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	if _, err := client.Get(base + transport.PathHealth); err == nil {
		t.Fatal("the standby health listener answered after Shutdown")
	}
}
