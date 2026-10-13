package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

func TestNomadAutoRevertAndGracefulStopIntegration(t *testing.T) {
	if os.Getenv("SRO_NOMAD_INTEGRATION") != "1" {
		t.Skip("set SRO_NOMAD_INTEGRATION=1 with a development Nomad agent")
	}
	if runtime.GOOS != "windows" {
		t.Skip("this integration jobspec exercises the Windows raw_exec path")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := newNomadClient(defaultNomadNamespace, devStack{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()

	jobID := fmt.Sprintf("sro-nomad-integration-%d", time.Now().UnixNano())
	cleaned := false
	defer func() {
		if cleaned {
			return
		}
		_, _, _ = client.api.Jobs().DeregisterOpts(
			jobID,
			&nomad.DeregisterOptions{
				Purge:           true,
				NoShutdownDelay: true,
			},
			(&nomad.WriteOptions{}).WithContext(context.Background()),
		)
	}()

	stable, err := client.parseJob(
		ctx,
		integrationJobHCL(jobID, false),
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.validateAndPlan(ctx, stable); err != nil {
		t.Fatal(err)
	}
	if err := client.registerJob(ctx, stable); err != nil {
		t.Fatalf("establish stable job: %v", err)
	}
	stable, _, err = client.api.Jobs().Info(
		jobID,
		(&nomad.QueryOptions{}).WithContext(ctx),
	)
	if err != nil || stable.Version == nil {
		t.Fatalf("read stable job: version=%v err=%v", stable.Version, err)
	}
	failedVersion := *stable.Version + 1

	broken, err := client.parseJob(
		ctx,
		integrationJobHCL(jobID, true),
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = client.registerJob(ctx, broken)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf(
			"broken deployment error = %v, want exact failed deployment",
			err,
		)
	}

	reverted := waitForNewerJobVersion(t, ctx, client, jobID, failedVersion)
	allocationContext, cancelAllocations := context.WithTimeout(
		ctx,
		30*time.Second,
	)
	defer cancelAllocations()
	if err := client.waitForJobAllocations(
		allocationContext,
		reverted,
		*reverted.Version,
	); err != nil {
		t.Fatalf("auto-reverted allocation: %v", err)
	}
	if err := client.stopJob(ctx, jobID); err != nil {
		t.Fatalf("graceful stop: %v", err)
	}
	cleaned = true
}

func waitForNewerJobVersion(
	t *testing.T,
	ctx context.Context,
	client *nomadClient,
	jobID string,
	failedVersion uint64,
) *nomad.Job {
	t.Helper()
	var waitIndex uint64
	for {
		job, metadata, err := client.api.Jobs().Info(
			jobID,
			(&nomad.QueryOptions{
				WaitIndex: waitIndex,
				WaitTime:  5 * time.Second,
			}).WithContext(ctx),
		)
		if err != nil {
			t.Fatal(err)
		}
		if metadata != nil && metadata.LastIndex > waitIndex {
			waitIndex = metadata.LastIndex
		}
		if job.Version != nil && *job.Version > failedVersion {
			return job
		}
	}
}

func integrationJobHCL(jobID string, broken bool) []byte {
	command := "C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	arguments := `
        args = [
          "-NoProfile",
          "-NonInteractive",
          "-Command",
          "Start-Sleep -Seconds 120",
        ]`
	if broken {
		command = "Z:/sro-nomad-deliberately-missing.exe"
		arguments = ""
	}
	return []byte(fmt.Sprintf(`
job %q {
  datacenters = ["dc1"]
  type        = "service"

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "windows"
  }

  update {
    max_parallel      = 1
    min_healthy_time  = "1s"
    healthy_deadline  = "8s"
    progress_deadline = "12s"
    auto_revert       = true
  }

  group "probe" {
    count = 1

    restart {
      attempts = 0
      interval = "1m"
      delay    = "1s"
      mode     = "fail"
    }

    task "probe" {
      driver = "raw_exec"

      config {
        command = %q
%s
      }

      resources {
        cpu    = 50
        memory = 64
      }
    }
  }
}
`, jobID, command, arguments))
}
