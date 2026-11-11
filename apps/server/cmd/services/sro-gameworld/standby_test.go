package main

/*
===========================================================================

standby_test.go - the canary standby handover's seams (opensro-ops#281)

No full-process boot harness exists in this package (boot_phases_test.go);
what can be proven here is: the switch reads its environment strictly
(unset is the native boot), and the standby-health bind retries past a
transient holder of the address - a scanner copy of the allocation -
while an interrupt during the wait still ends it at once.
===========================================================================
*/

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"opensro.online/server/internal/transport"
)

/*
================
TestStandbyHandoverEnabledReadsEnv
================
*/
func TestStandbyHandoverEnabledReadsEnv(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "off": false, "0": false, "junk": false,
		"on": true, "1": true, "true": true, "ON": true, " On ": true,
	} {
		t.Setenv(EnvStandbyHandover, value)
		if got := standbyHandoverEnabled(); got != want {
			t.Fatalf("%s=%q: got %v, want %v", EnvStandbyHandover, value, got, want)
		}
	}
}

/*
================
TestStandbyHealthAddrReadsEnv
================
*/
func TestStandbyHealthAddrReadsEnv(t *testing.T) {
	t.Setenv(EnvStandbyHealthAddr, "")
	if got := standbyHealthAddr(); got != "127.0.0.1:0" {
		t.Fatalf("unset: got %q, want a loopback dynamic port", got)
	}
	t.Setenv(EnvStandbyHealthAddr, " 127.0.0.1:9777 ")
	if got := standbyHealthAddr(); got != "127.0.0.1:9777" {
		t.Fatalf("set: got %q, want the named address", got)
	}
}

/*
================
TestStartStandbyHealthRetriesAndHonoursCancel

A copy of this allocation starts with the same environment and can win
the health bind for the seconds it lives: the bind retries past it.
An interrupt during the wait ends the call instead of the retry.
================
*/
func TestStartStandbyHealthRetriesAndHonoursCancel(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvStandbyHealthAddr, holder.Addr().String())
	newServer := func() *transport.Server {
		ts, err := transport.NewServer(transport.Config{
			WTAddr:  "127.0.0.1:0",
			WSAddr:  "127.0.0.1:0",
			CertDir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if err := startStandbyHealth(ctx, newServer()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a held address + a stopped wait: err = %v, want the context's", err)
	}

	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	ts := newServer()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = ts.Shutdown(ctx)
	})
	if err := startStandbyHealth(context.Background(), ts); err != nil {
		t.Fatal(err)
	}
	if got := ts.StandbyHealthAddr(); got != holder.Addr().String() {
		t.Fatalf("bound %q, want the released %q", got, holder.Addr().String())
	}
	resp, err := http.Get("http://" + ts.StandbyHealthAddr() + transport.PathStandby)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("standbyz during load = %d, want 503", resp.StatusCode)
	}
}
