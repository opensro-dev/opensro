/*
===========================================================================

main.go - sro-tickbudget: capture and reconcile a live tick budget

Two halves (#2824): capture pulls one metrics/profile/trace set from
a live GameWorld over ssh (capture.go); analyze reads a capture
directory against the server's exact tick_zones ledger and reconciles
the three sources, printing BLIND SPOT lines wherever they disagree
(analyze.go). The tool lives in its own module so the server module
gains no dependencies from it.

===========================================================================
*/

package main

import (
	"flag"
	"fmt"
	"os"
)

/*
================
usage
================
*/
func usage() {
	fmt.Fprintf(os.Stderr, `sro-tickbudget - capture and reconcile a live tick budget (#2824)

  sro-tickbudget capture --ssh USER@HOST [--ssh-identity KEY] [--pprof 127.0.0.1:8792] [--metrics 127.0.0.1:8788] --out DIR
      Pull metrics, a 30s CPU profile, a 5s execution trace and two heap
      pulls (60s apart) from a live GameWorld, bracketed by metrics
      snapshots. Runs in a private /tmp directory on the host, deleted
      when done. Non-loopback service addresses are refused.

  sro-tickbudget analyze [--targets FILE] DIR
      Print the nested zone budget for a capture directory, reconcile
      zones x CPU profile x trace (BLIND SPOT lines where the sources
      disagree), and write DIR/budget.json. --targets is a JSON object
      of zone key -> ms/tick budget, printed as now / target / gap.
`)
}

/*
================
main
================
*/
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "capture":
		err = captureCommand(os.Args[2:])
	case "analyze":
		err = analyzeCommand(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sro-tickbudget:", err)
		os.Exit(1)
	}
}

/*
================
captureCommand
================
*/
func captureCommand(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	var opts captureOptions
	fs.StringVar(&opts.sshTarget, "ssh", "", "ssh target, USER@HOST")
	fs.StringVar(&opts.sshIdentity, "ssh-identity", "", "ssh private key file (default: ssh's own choice)")
	fs.StringVar(&opts.pprofAddr, "pprof", "127.0.0.1:8792", "host-local pprof address (loopback only)")
	fs.StringVar(&opts.metricsAddr, "metrics", "127.0.0.1:8788", "host-local /transport/metrics address (loopback only)")
	fs.StringVar(&opts.outDir, "out", "", "directory to write the capture into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runCapture(opts)
}

/*
================
analyzeCommand
================
*/
func analyzeCommand(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	targets := fs.String("targets", "", "JSON file of zone key -> ms/tick budget")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("analyze needs exactly one capture directory")
	}
	_, err := analyzeDir(fs.Arg(0), *targets, os.Stdout)
	return err
}
