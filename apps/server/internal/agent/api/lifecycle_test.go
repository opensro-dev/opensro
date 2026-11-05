/*
===========================================================================

lifecycle_test.go - the control listener's start, policy and shutdown

The address policy is tested without binding: a test that bound a
non-loopback interface would make Windows Firewall prompt for every
fresh test binary. Real starts bind loopback.

===========================================================================
*/
package agentapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestStartOwnsListenerBeforeReturning(t *testing.T) {
	api, _ := newTestAPI(t)
	serveError, err := api.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve error = %v, want http.ErrServerClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve goroutine did not stop")
	}
}

func TestStartReportsBindFailureSynchronously(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer blocker.Close()

	api, _ := newTestAPI(t)
	if _, err := api.Start(blocker.Addr().String()); err == nil {
		t.Fatal("Start succeeded on an occupied address")
	}
}

func TestStartRefusesPublicPlaintextListener(t *testing.T) {
	api, _ := newTestAPI(t)
	if _, err := api.Start("0.0.0.0:0"); err == nil {
		t.Fatal("agent API accepted a public plaintext listener")
	}
}

func TestStartAllowsExplicitPrivateNetworkListener(t *testing.T) {
	api, _ := newTestAPI(t)
	api.privateNetwork = true
	serveError, err := api.Start("127.0.0.1:0")
	if err != nil {
		t.Fatalf("private network Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveError; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve error = %v", err)
	}
}

/*
================
TestControlListenAddressPolicy

The pre-bind policy itself: a wildcard address needs the explicit
private-network deployment; loopback never does. Testing the function
keeps the wildcard cases off the network entirely.
================
*/
func TestControlListenAddressPolicy(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", "[::]:0"} {
		if err := checkControlListenAddress(addr, true); err != nil {
			t.Fatalf("%s with a private-network deployment: %v", addr, err)
		}
		if err := checkControlListenAddress(addr, false); err == nil {
			t.Fatalf("%s accepted without a private-network deployment", addr)
		}
	}
	if err := checkControlListenAddress("127.0.0.1:0", false); err != nil {
		t.Fatalf("loopback without a private-network deployment: %v", err)
	}
}

func TestHTTPServerCarriesResourceLimits(t *testing.T) {
	api, _ := newTestAPI(t)
	serveError, err := api.Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, _ := api.listener.current()
	if server.ReadHeaderTimeout <= 0 ||
		server.ReadTimeout <= 0 ||
		server.WriteTimeout <= 0 ||
		server.IdleTimeout <= 0 ||
		server.MaxHeaderBytes <= 0 {
		t.Fatalf("HTTP limits incomplete: %+v", server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := api.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveError; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve error = %v", err)
	}
}
