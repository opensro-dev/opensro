/*
===========================================================================

admission_phase_test.go - the title follows GameWorld admission, not the lease

A GameWorld holds its lease ~50 s before its worlds load. Players who
logged in then saw an empty character dock (the roster answered 503
STARTING). A starting lease is not operating; admission latches per
process; a heartbeat without a phase is judged by its lease alone.

===========================================================================
*/
package shard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

/*
================
phaseDirectory
================
*/
func phaseDirectory(t *testing.T, path string) *Directory {
	t.Helper()
	catalog, err := NewCatalog([]Definition{testDefinition("alpha", 1, true)})
	if err != nil {
		t.Fatal(err)
	}
	var directory *Directory
	if path == "" {
		directory, err = NewDirectory(catalog, 10*time.Second)
	} else {
		directory, err = NewPersistentDirectory(catalog, 10*time.Second, path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

/*
================
TestStartingLeaseIsNotOperating
================
*/
func TestStartingLeaseIsNotOperating(t *testing.T) {
	directory := phaseDirectory(t, "")
	now := time.Unix(1000, 0)
	publish := func(sequence uint64, phase string) {
		t.Helper()
		if err := directory.Publish(Heartbeat{
			ShardID: "alpha", InstanceID: "boot-a", Sequence: sequence, OnlinePlayers: 1, Phase: phase,
		}, now); err != nil {
			t.Fatal(err)
		}
	}
	publish(1, PhaseStarting)
	if got := directory.Snapshot(now)[0]; got.Operating || got.OnlinePlayers != 0 {
		t.Fatalf("a starting owner shows %+v; want not operating", got)
	}
	publish(2, PhaseAdmitting)
	if got := directory.Snapshot(now)[0]; !got.Operating || got.OnlinePlayers != 1 {
		t.Fatalf("an admitting owner shows %+v; want operating", got)
	}

	// A GameWorld from before the phase field is judged by its lease alone,
	// so a mixed-version restart never takes the whole fleet offline.
	legacy := phaseDirectory(t, "")
	if err := legacy.Publish(Heartbeat{ShardID: "alpha", InstanceID: "old", Sequence: 1}, now); err != nil {
		t.Fatal(err)
	}
	if !legacy.Snapshot(now)[0].Operating {
		t.Fatal("a phase-less heartbeat lost its lease-only operating status")
	}
	var decoded Heartbeat
	if err := json.Unmarshal([]byte(`{"shardId":"alpha","instanceId":"old","sequence":2,"onlinePlayers":0}`), &decoded); err != nil || decoded.Phase != "" {
		t.Fatalf("a missing phase decodes as %q (%v); want empty, not starting", decoded.Phase, err)
	}

	if err := directory.Publish(Heartbeat{ShardID: "alpha", InstanceID: "boot-a", Sequence: 3, Phase: "ready"}, now); err == nil {
		t.Fatal("an unknown phase was accepted")
	}
}

/*
================
TestStartingPhaseSurvivesAgentRestart
================
*/
func TestStartingPhaseSurvivesAgentRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shard-leases.json")
	now := time.Unix(1000, 0)
	if err := phaseDirectory(t, path).Publish(Heartbeat{
		ShardID: "alpha", InstanceID: "boot-a", Sequence: 1, Phase: PhaseStarting,
	}, now); err != nil {
		t.Fatal(err)
	}
	restarted := phaseDirectory(t, path)
	if restarted.Snapshot(now.Add(time.Second))[0].Operating {
		t.Fatal("an Agent restart turned a starting owner online")
	}
	if err := restarted.Publish(Heartbeat{
		ShardID: "alpha", InstanceID: "boot-a", Sequence: 2, Phase: PhaseAdmitting,
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !restarted.Snapshot(now.Add(time.Second))[0].Operating {
		t.Fatal("the next admitting heartbeat after an Agent restart stayed offline")
	}
}

/*
================
TestReporterAnnouncesAdmissionAtOnce

Before MarkAdmitting every heartbeat says starting; MarkAdmitting makes Run
publish an admitting heartbeat without waiting for its interval, from its
own loop, and the phase stays latched.
================
*/
func TestReporterAnnouncesAdmissionAtOnce(t *testing.T) {
	var mu sync.Mutex
	var phases []string
	arrived := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var heartbeat Heartbeat
		if err := json.Unmarshal(body, &heartbeat); err != nil {
			t.Error(err)
		}
		mu.Lock()
		phases = append(phases, heartbeat.Phase)
		mu.Unlock()
		arrived <- struct{}{}
	}))
	defer server.Close()
	identity := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identity, []byte("identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(server.URL, identity, "alpha", func() int { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	reporter.Interval = time.Hour
	if err := reporter.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-arrived

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- reporter.Run(ctx) }()
	reporter.MarkAdmitting()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("MarkAdmitting did not publish before the hour-long interval")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := reporter.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-arrived
	mu.Lock()
	defer mu.Unlock()
	if len(phases) != 3 || phases[0] != PhaseStarting || phases[1] != PhaseAdmitting || phases[2] != PhaseAdmitting {
		t.Fatalf("heartbeat phases %v; want starting, admitting, admitting", phases)
	}
}
