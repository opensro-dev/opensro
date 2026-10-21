package shard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestReporterAcquireWaitsThroughTransientLeaseContention(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/cluster/shards/heartbeat" {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"LEASE_REFUSED"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	identityFile := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identityFile, []byte("workload-identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(server.URL, identityFile, "test", func() int { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	if err := reporter.Acquire(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("heartbeat attempts = %d, want 3", got)
	}
}

func TestReporterReadsAuthenticatedAccountDirectory(t *testing.T) {
	const identity = "workload-identity"
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/internal/accounts" {
				http.NotFound(w, r)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+identity {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.Header.Get(ControlShardHeader) != "test" {
				http.Error(w, "wrong shard", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accountIds":["alice","bob"]}`))
		},
	))
	t.Cleanup(server.Close)
	identityFile := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identityFile, []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(
		server.URL,
		identityFile,
		"test",
		func() int { return 0 },
	)
	if err != nil {
		t.Fatal(err)
	}
	accountIDs, err := reporter.AccountIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accountIDs) != 2 ||
		accountIDs[0] != "alice" ||
		accountIDs[1] != "bob" {
		t.Fatalf("account ids = %v", accountIDs)
	}
}

func TestReporterRefusesMalformedAccountDirectory(t *testing.T) {
	for name, body := range map[string]string{
		"empty":     `{"accountIds":[]}`,
		"duplicate": `{"accountIds":["alice","alice"]}`,
		"invalid":   `{"accountIds":[" padded "]}`,
		"unknown":   `{"accountIds":["alice"],"hashes":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(body))
				},
			))
			t.Cleanup(server.Close)
			identityFile := filepath.Join(t.TempDir(), "identity.jwt")
			if err := os.WriteFile(
				identityFile,
				[]byte("workload-identity"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			reporter, err := NewReporter(
				server.URL,
				identityFile,
				"test",
				func() int { return 0 },
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reporter.AccountIDs(context.Background()); err == nil {
				t.Fatalf("body %s accepted", body)
			}
		})
	}
}

/*
================
TestReporterFollowsAgentMallAnswer

The mall follows the Agent's license answer; an answer without one (an
Agent older than the server key) leaves it as it was.
================
*/
func TestReporterFollowsAgentMallAnswer(t *testing.T) {
	var answer atomic.Value
	answer.Store(`{"ok":true}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer.Load().(string)))
	}))
	t.Cleanup(server.Close)
	identityFile := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identityFile, []byte("workload-identity"), 0o600); err != nil {
		t.Fatal(err)
	}
	reporter, err := NewReporter(server.URL, identityFile, "test", func() int { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		body string
		want bool
	}{
		{`{"ok":true}`, true},
		{`{"ok":true,"mall":false}`, false},
		{`{"ok":true}`, false},
		{`{"ok":true,"mall":true}`, true},
	}
	for index, step := range steps {
		answer.Store(step.body)
		if err := reporter.Publish(context.Background()); err != nil {
			t.Fatal(err)
		}
		if reporter.MallOpen() != step.want {
			t.Fatalf("step %d (%s): MallOpen = %t", index, step.body, !step.want)
		}
	}
}
