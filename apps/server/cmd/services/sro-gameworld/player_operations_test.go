/*
===========================================================================

player_operations_test.go - recovery lease and persistence failure boundaries

The real transport hub and store exercise the production control wrapper.
No listener is started; certificates and database files stay in test temp dirs.

===========================================================================
*/
package main

import (
	"errors"
	"strings"
	"testing"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/transport"
)

/*
================
TestPlayerOperationControlLeaseAndPersistence
================
*/
func TestPlayerOperationControlLeaseAndPersistence(t *testing.T) {
	server, err := transport.NewServer(transport.Config{CertDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	control := playerOperationControl{hub: server.Hub, authority: authority, shard: "test"}
	for _, mode := range []string{"success", "refused", "persistence"} {
		t.Run(mode, func(t *testing.T) {
			called := false
			err := control.run("Probe", "PK clear", func() error {
				called = true
				if lease, acquired := server.Hub.AcquireBindingControl("test:probe"); acquired {
					lease.Release()
					t.Fatal("mutation ran without exclusive binding control")
				}
				if mode == "refused" {
					return errors.New("character PK clear refused")
				}
				if mode == "persistence" {
					authority.FailCommits(errors.New("test disk failure"))
				}
				authority.Mutate("operator-clear-pk", func() {})
				return nil
			})
			if !called || (err != nil) != (mode != "success") {
				t.Fatalf("called=%v error=%v", called, err)
			}
			if mode == "persistence" && !strings.Contains(err.Error(), "persistence failed") {
				t.Fatal(err)
			}
			lease, acquired := server.Hub.AcquireBindingControl("test:probe")
			if !acquired {
				t.Fatal("operation leaked its binding lease")
			}
			lease.Release()
		})
	}
	if err := control.run("Probe", "PK clear", func() error {
		t.Fatal("unhealthy storage admitted mutation")
		return nil
	}); err == nil || !strings.Contains(err.Error(), "storage is unhealthy") {
		t.Fatal("storage refusal missing", err)
	}
	authority.FailCommits(nil)
}
