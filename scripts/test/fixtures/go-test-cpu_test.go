/*
===========================================================================

go-test-cpu_test.go - a real test process for the CPU budget timer

The fixture performs bounded CPU work or fails on request. It is invoked
by filename, so it needs no module and never joins the server's tests.

===========================================================================
*/
package testcpu

import (
	"crypto/sha256"
	"os"
	"testing"
)

const hashIterations = 200_000

/*
================
TestCPUTimer
================
*/
func TestCPUTimer(t *testing.T) {
	if os.Getenv("SRO_CPU_TEST_MODE") == "fail" {
		t.Fatal("intentional CPU timer fixture failure")
	}
	digest := sha256.Sum256([]byte("CPU timer fixture"))
	for range hashIterations {
		digest = sha256.Sum256(digest[:])
	}
	if digest == [sha256.Size]byte{} {
		t.Fatal("empty digest")
	}
}
