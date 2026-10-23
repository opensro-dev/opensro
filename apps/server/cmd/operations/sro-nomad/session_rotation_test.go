//go:build opensro_ops

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nomad "github.com/hashicorp/nomad/api"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/security/auth"
)

func TestValidateSessionRotationGameWorldJobs(t *testing.T) {
	shards := []shardDeployment{
		{Definition: shard.Definition{ID: "global-official"}},
		{Definition: shard.Definition{ID: "test"}},
	}
	running := func(id string) *nomad.JobListStub {
		return &nomad.JobListStub{
			ID:     gameWorldJobPrefix + id,
			Status: "running",
		}
	}

	if err := validateSessionRotationGameWorldJobs(
		shards,
		[]*nomad.JobListStub{running("global-official"), running("test")},
	); err != nil {
		t.Fatalf("reconciled fleet: %v", err)
	}

	cases := []struct {
		name string
		jobs []*nomad.JobListStub
	}{
		{
			name: "disabled job remains registered",
			jobs: []*nomad.JobListStub{
				running("global-official"),
				running("test"),
				running("retired"),
			},
		},
		{
			name: "enabled job is missing",
			jobs: []*nomad.JobListStub{
				running("global-official"),
			},
		},
		{
			name: "enabled job is stopped",
			jobs: []*nomad.JobListStub{
				running("global-official"),
				{
					ID:     gameWorldJobPrefix + "test",
					Status: "running",
					Stop:   true,
				},
			},
		},
		{
			name: "enabled job is not running",
			jobs: []*nomad.JobListStub{
				running("global-official"),
				{
					ID:     gameWorldJobPrefix + "test",
					Status: "dead",
				},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateSessionRotationGameWorldJobs(
				shards,
				testCase.jobs,
			); err == nil {
				t.Fatal("unreconciled fleet was accepted")
			}
		})
	}
}

func TestValidateSessionRotationRecovery(t *testing.T) {
	base := auth.AgentSessionKeyRingStatus{
		ActiveKeyID:  "old",
		PublicDigest: "base",
		KeyIDs:       []string{"old"},
	}
	if err := validateSessionRotationRecovery(base, base); err != nil {
		t.Fatalf("matching rings: %v", err)
	}
	drift := base
	drift.PublicDigest = "other"
	if err := validateSessionRotationRecovery(base, drift); err == nil {
		t.Fatal("untracked drift was accepted")
	}

	pending := auth.AgentSessionKeyRingStatus{
		ActiveKeyID:  "old",
		PendingKeyID: "next",
		PublicDigest: "prepared",
		KeyIDs:       []string{"next", "old"},
	}
	if err := validateSessionRotationRecovery(pending, base); err != nil {
		t.Fatalf("unpublished prepared key: %v", err)
	}
	published := pending
	if err := validateSessionRotationRecovery(pending, published); err != nil {
		t.Fatalf("published prepared key: %v", err)
	}
	activated := pending
	activated.ActiveKeyID = "next"
	activated.PendingKeyID = ""
	if err := validateSessionRotationRecovery(pending, activated); err != nil {
		t.Fatalf("remotely activated key: %v", err)
	}
}

func TestWaitForSessionKeyAcknowledgement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(sessionKeyringHeader, "digest")
			w.Header().Set(sessionActiveKeyHeader, "active")
			_, _ = w.Write([]byte("ready"))
		},
	))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForSessionKeyAcknowledgement(
		ctx,
		server.URL,
		"digest",
		"active",
	); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRotationPreflightAcceptsRecoverableMixedGameWorldState(
	t *testing.T,
) {
	ready := func(digest string, active string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(sessionKeyringHeader, digest)
				if active != "" {
					w.Header().Set(sessionActiveKeyHeader, active)
				}
				_, _ = w.Write([]byte("ready"))
			},
		))
	}
	agent := ready("base", "old")
	defer agent.Close()
	baseGame := ready("base", "")
	defer baseGame.Close()
	preparedGame := ready("prepared", "")
	defer preparedGame.Close()

	deployment := deployment{
		AgentURL: agent.URL,
		Shards: []shardDeployment{
			{
				Definition: shard.Definition{
					ID:         "global-official",
					ControlURL: baseGame.URL,
				},
			},
			{
				Definition: shard.Definition{
					ID:         "test",
					ControlURL: preparedGame.URL,
				},
			},
		},
	}
	local := auth.AgentSessionKeyRingStatus{
		ActiveKeyID:  "old",
		PendingKeyID: "next",
		PublicDigest: "prepared",
	}
	remote := auth.AgentSessionKeyRingStatus{
		ActiveKeyID:  "old",
		PublicDigest: "base",
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := deployment.requireSessionRotationControlPlanesReady(
		ctx,
		local,
		remote,
	); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForSessionKeyStateRefusesUnknownDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(sessionKeyringHeader, "unrelated")
			_, _ = w.Write([]byte("ready"))
		},
	))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := waitForSessionKeyState(
		ctx,
		server.URL,
		[]string{"base", "prepared"},
		"",
		time.Second,
	); err == nil {
		t.Fatal("unknown digest was accepted")
	}
}
