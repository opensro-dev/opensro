/*
===========================================================================

command_test.go - the probe's actual flags, JSON and exit-status contract

A child copy of this reviewed test binary runs main with isolated flags.
Tickets stay in environment variables; arguments and reports never carry
them. All destinations belong to the parent test's loopback fixture.

===========================================================================
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"opensro.online/server/internal/transport"
)

const (
	probeCommandHelperEnv = "SRO_PROBE_TEST_HELPER"
	probeCommandTicketEnv = "SRO_PROBE_TEST_TICKET"
	probeCommandEnabled   = "1"
	probeExitFailure      = 1
	probeExitUsage        = 2
)

/*
================
TestProbeCommandHelper

Ordinary package discovery returns immediately. The subprocess drops the
testing flags before invoking the same main entry point as a built command.
================
*/
func TestProbeCommandHelper(t *testing.T) {
	if os.Getenv(probeCommandHelperEnv) != probeCommandEnabled {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		t.Fatal("helper needs a command argument separator")
	}
	os.Args = append([]string{os.Args[0]}, os.Args[separator+1:]...)
	flag.CommandLine = flag.NewFlagSet("sro-wtprobe", flag.ExitOnError)
	main()
	os.Exit(0)
}

/*
================
probeCommandResult
================
*/
type probeCommandResult struct {
	stdout []byte
	stderr string
	exit   int
}

/*
================
runProbeCommand
================
*/
func runProbeCommand(t *testing.T, args []string) probeCommandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), probeTestConnectTimeout)
	defer cancel()
	arguments := append([]string{"-test.run=^TestProbeCommandHelper$", "--"}, args...)
	command := exec.CommandContext(ctx, os.Args[0], arguments...)
	command.Env = append(os.Environ(), probeCommandHelperEnv+"="+probeCommandEnabled, probeCommandTicketEnv+"="+probeTestTicket)
	var stderr strings.Builder
	command.Stderr = &stderr
	stdout, err := command.Output()
	result := probeCommandResult{stdout: stdout, stderr: stderr.String()}
	if err != nil {
		var failed *exec.ExitError
		if !errors.As(err, &failed) {
			t.Fatalf("probe helper did not finish: %v", err)
		}
		result.exit = failed.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatal("probe command exceeded its fixture deadline")
	}
	if strings.Contains(string(result.stdout), probeTestTicket) || strings.Contains(result.stderr, probeTestTicket) {
		t.Fatal("command exposed its admission ticket")
	}
	return result
}

/*
================
probeCommandArgs
================
*/
func probeCommandArgs(t *testing.T, server *transport.Server) []string {
	t.Helper()
	return []string{
		"-url", "http://" + server.WSAddr(), "-wt-addr", server.WTAddr(),
		"-ca-cert", caCertFile(t, server), "-ticket-env", probeCommandTicketEnv,
	}
}

/*
================
TestProbeCommandKeepsLegacySmoke
================
*/
func TestProbeCommandKeepsLegacySmoke(t *testing.T) {
	server := startProbeServer(t)
	result := runProbeCommand(t, probeCommandArgs(t, server))
	if result.exit != 0 || !strings.HasPrefix(string(result.stdout), "OK welcome session=") || !strings.Contains(string(result.stdout), "cert-sha256=") {
		t.Fatalf("legacy command contract changed: exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
	}
}

/*
================
TestProbeCommandMeasurementJSON
================
*/
func TestProbeCommandMeasurementJSON(t *testing.T) {
	for _, kind := range []string{"ws", "wt"} {
		t.Run(kind, func(t *testing.T) {
			server := startProbeServer(t)
			args := append(probeCommandArgs(t, server), "-measure", "-transport", kind,
				"-samples", "2", "-burst", "2", "-ping-interval", "1ms", "-burst-interval", "1ms")
			result := runProbeCommand(t, args)
			var report measurementReport
			if err := json.Unmarshal(result.stdout, &report); err != nil {
				t.Fatalf("measurement stdout is not one JSON document: %v (%s)", err, result.stdout)
			}
			if result.exit != 0 || !report.Valid || report.HandshakeSamples != 1 ||
				report.PingTrain.Received != 2 || report.OrderedBurst.Received != 2 || report.Settings.Transport != kind {
				t.Fatalf("measurement command failed: exit=%d report=%+v stderr=%s", result.exit, report, result.stderr)
			}
		})
	}
}

/*
================
TestProbeCommandFailedArmStillWritesJSON
================
*/
func TestProbeCommandFailedArmStillWritesJSON(t *testing.T) {
	settings := probeTestServerConfig(t)
	settings.RateLimitPerSec, settings.RateLimitBurst = measurementLimitedRate, measurementLimitedBurst
	server := startProbeServerWithConfig(t, settings)
	args := append(probeCommandArgs(t, server), "-measure", "-transport", "ws",
		"-samples", "5", "-burst", "5", "-ping-interval", "1ms", "-burst-interval", "0", "-timeout", "50ms")
	result := runProbeCommand(t, args)
	var report measurementReport
	if err := json.Unmarshal(result.stdout, &report); err != nil {
		t.Fatal(err)
	}
	if result.exit != probeExitFailure || report.Valid || report.Error == "" || result.stderr == "" || report.Metrics == nil ||
		report.Metrics.CounterDeltas["rate_limited_frames"] == 0 {
		t.Fatalf("failed command lost its JSON or status: exit=%d report=%+v stderr=%s", result.exit, report, result.stderr)
	}
}

/*
================
TestProbeCommandRejectsBadScheduleAsUsage
================
*/
func TestProbeCommandRejectsBadScheduleAsUsage(t *testing.T) {
	server := startProbeServer(t)
	args := append(probeCommandArgs(t, server), "-measure", "-samples", "0")
	result := runProbeCommand(t, args)
	if result.exit != probeExitUsage || len(result.stdout) != 0 || result.stderr == "" {
		t.Fatalf("invalid flags started an arm: %+v", result)
	}
}
