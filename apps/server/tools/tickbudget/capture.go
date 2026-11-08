/*
===========================================================================

capture.go - pull one reconciled capture from a live server

The sequence runs on the host over ssh, in a private /tmp directory
that a trap deletes when the script ends: metrics, a 30 s CPU
profile, metrics; metrics, a 5 s execution trace, metrics; a heap
pull, 60 s, a second heap pull and metrics. The files name players
(stack samples carry session work), so they are tarred straight back
over the ssh connection into --out and never rest on the host.

Both service addresses must be loopback: the pprof listener exposes
the whole process, and the metrics listener rides the same transport,
so a non-loopback address is refused before anything is fetched. No
host name or address is ever written into this repository - the ssh
target exists only on the command line.

===========================================================================
*/

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
)

/*
================
captureOptions
================
*/
type captureOptions struct {
	sshTarget   string
	pprofAddr   string
	metricsAddr string
	outDir      string
}

/*
================
requireLoopback

The one guard on capture: the address must name this host only.
"localhost", 127.0.0.0/8 and ::1 pass; anything else refuses.
================
*/
func requireLoopback(addr, flag string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--%s %q: %w", flag, addr, err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--%s %q refused: host %q is not loopback (the pprof and metrics listeners must never be pulled through a routable address)", flag, addr, host)
	}
	return nil
}

/*
================
remoteScript

The on-host capture sequence. The metrics endpoint path and the
pprof paths are the ones the GameWorld serves; the working directory
is private and deleted by the trap on any exit.
================
*/
func remoteScript(pprofAddr, metricsAddr string) string {
	metrics := "http://" + metricsAddr + "/transport/metrics"
	pprof := "http://" + pprofAddr + "/debug/pprof"
	var b strings.Builder
	b.WriteString("set -eu\n")
	b.WriteString("d=$(mktemp -d /tmp/sro-tickbudget.XXXXXX)\n")
	b.WriteString("trap 'rm -rf \"$d\"' EXIT\n")
	b.WriteString("cd \"$d\"\n")
	fmt.Fprintf(&b, "curl -fsS %q -o metrics-1.json\n", metrics)
	fmt.Fprintf(&b, "curl -fsS %q -o cpu.pb.gz\n", pprof+"/profile?seconds=30")
	fmt.Fprintf(&b, "curl -fsS %q -o metrics-2.json\n", metrics)
	fmt.Fprintf(&b, "curl -fsS %q -o metrics-3.json\n", metrics)
	fmt.Fprintf(&b, "curl -fsS %q -o trace.out\n", pprof+"/trace?seconds=5")
	fmt.Fprintf(&b, "curl -fsS %q -o metrics-4.json\n", metrics)
	fmt.Fprintf(&b, "curl -fsS %q -o heap-1.pb.gz\n", pprof+"/heap")
	b.WriteString("sleep 60\n")
	fmt.Fprintf(&b, "curl -fsS %q -o heap-2.pb.gz\n", pprof+"/heap")
	fmt.Fprintf(&b, "curl -fsS %q -o metrics-5.json\n", metrics)
	b.WriteString("tar czf - .\n")
	return b.String()
}

/*
================
extractTarGz

Unpack the returned stream into the output directory. Members are
created through an os.Root opened on that directory, so a member
name cannot escape it by construction: a ".." climb or an absolute
name fails the create and refuses the whole capture. The remote
script writes plain "./name" members, and "./name" is exactly the
local form the root accepts.
================
*/
func extractTarGz(r io.Reader, outDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return err
	}
	defer root.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		out, err := root.Create(header.Name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}

/*
================
runCapture

ssh the sequence to the host and unpack what comes back. The capture
takes about 95 seconds by construction (30 s profile, 5 s trace,
60 s heap gap).
================
*/
func runCapture(opts captureOptions) error {
	if opts.sshTarget == "" {
		return fmt.Errorf("capture needs --ssh USER@HOST")
	}
	if opts.outDir == "" {
		return fmt.Errorf("capture needs --out DIR")
	}
	if err := requireLoopback(opts.pprofAddr, "pprof"); err != nil {
		return err
	}
	if err := requireLoopback(opts.metricsAddr, "metrics"); err != nil {
		return err
	}
	if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
		return err
	}
	script := remoteScript(opts.pprofAddr, opts.metricsAddr)
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", opts.sshTarget, "sh -s")
	cmd.Stdin = strings.NewReader(script)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	extractErr := extractTarGz(stdout, opts.outDir)
	waitErr := cmd.Wait()
	if extractErr != nil {
		return fmt.Errorf("unpack capture: %w", extractErr)
	}
	if waitErr != nil {
		return fmt.Errorf("remote capture: %w", waitErr)
	}
	fmt.Printf("capture written to %s (run: sro-tickbudget analyze %s)\n", opts.outDir, opts.outDir)
	return nil
}
