/*
===========================================================================

testcpu - the go test supervisor the server gate measures CPU time with

The server gate (scripts/checks/check_go_server.mjs) runs its everyday
tests through `go test -exec <this binary>`: go test hands over each
package's compiled test binary, this supervisor runs it with the same
arguments and stdio, and records the child's CPU time (user + system)
as one JSON file per invocation under SRO_GO_TEST_CPU_DIR. The gate
budgets that CPU time; a busy box inflates a test's wall clock, not
the CPU it burns, so contention alone can no longer fail the budget
(opensro#2582).

Without SRO_GO_TEST_CPU_DIR the supervisor is a plain pass-through,
so running it by hand changes nothing.

===========================================================================
*/
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

/*
================
main
================
*/
func main() {
	os.Exit(run(os.Args[1:]))
}

/*
================
run

Run the test binary go test named, with its arguments and this
process's stdio, then record what it cost. The exit status is the
binary's own, so go test judges the package exactly as if it had run
the binary itself.
================
*/
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "testcpu: no test binary given")
		return 2
	}
	command := exec.Command(args[0], args[1:]...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	started := time.Now()
	runErr := command.Run()
	record(command, time.Since(started))
	if runErr == nil {
		return 0
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		if code := exitErr.ExitCode(); code > 0 {
			return code
		}
	}
	return 1
}

/*
================
record

Write this invocation's times as one JSON file in SRO_GO_TEST_CPU_DIR,
named by the hash of the working directory (go test runs each test
binary in its package's directory, so the directory names the
package). Concurrent packages never share a file. Best effort: when
no record lands, the gate falls back to the wall-clock verdict, so a
lost measurement can never wave a slow package through.
================
*/
func record(command *exec.Cmd, wall time.Duration) {
	dir := os.Getenv("SRO_GO_TEST_CPU_DIR")
	if dir == "" || command.ProcessState == nil {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	entry := struct {
		Dir    string `json:"dir"`
		UserMs int64  `json:"userMs"`
		SysMs  int64  `json:"sysMs"`
		WallMs int64  `json:"wallMs"`
	}{
		Dir:    cwd,
		UserMs: command.ProcessState.UserTime().Milliseconds(),
		SysMs:  command.ProcessState.SystemTime().Milliseconds(),
		WallMs: wall.Milliseconds(),
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	sum := sha256.Sum256([]byte(cwd))
	_ = os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), encoded, 0o644)
}
