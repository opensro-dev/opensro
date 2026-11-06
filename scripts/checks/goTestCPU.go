/*
===========================================================================

goTestCPU.go - CPU accounting for the server gate's uncached budget recheck

Built as a standalone Go file and passed to go test -exec. The operating
system measures the test process's user and system CPU time, excluding
time waiting for another process to release a core. This does not time
compilation or change the test binary's arguments, environment or streams.

===========================================================================
*/
package main

import (
	"fmt"
	"os"
	"os/exec"
)

const cpuMarker = "SRO_TEST_CPU_SECONDS="

/*
================
main

Report after Wait, including on a test failure; the child's exit status
still determines success. A failed start has no measurement and fails.
================
*/
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "test CPU timer: missing executable")
		os.Exit(1)
	}
	child := exec.Command(os.Args[1], os.Args[2:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	err := child.Run()
	if child.ProcessState != nil {
		cpu := child.ProcessState.UserTime() + child.ProcessState.SystemTime()
		fmt.Printf("\n%s%.6f\n", cpuMarker, cpu.Seconds())
	}
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "test CPU timer:", err)
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() > 0 {
		os.Exit(exit.ExitCode())
	}
	os.Exit(1)
}
