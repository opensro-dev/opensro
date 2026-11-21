/*
===========================================================================

shared_test.go - the shared-parse fingerprint names the bytes

A loader that rewrites a table in place (the Alexandria portal test
staging a link condition) must never read the previous parse back: the
fingerprint has to change with the contents even when path, size and
modification time all stand still.

===========================================================================
*/
package texttable

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
================
writeStamped

Writes body and stamps the file with stamp, so a test controls the
modification time a coarse filesystem would report.
================
*/
func writeStamped(t *testing.T, path, body string, stamp time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestFingerprintNamesTheBytes

Same path, same size, same modification time, different bytes (one
digit of a link condition): the fingerprints must differ, or a shared
parse serves the old table. Fails on the size+mtime fingerprint.
================
*/
func TestFingerprintNamesTheBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleportlink.txt")
	stamp := time.Unix(1700000000, 0)
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t7\t0\t0\n", stamp)
	first := Fingerprint(path)
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t4\t1\t0\n", stamp)
	if second := Fingerprint(path); second == first {
		t.Fatalf("fingerprint collides for different bytes:\n%s", first)
	}
}

/*
================
TestFingerprintSharesIdenticalBytes

The same bytes written again under a newer timestamp are the same
parse: the fingerprint must not change, or every re-extracted
projection would be parsed afresh for nothing.
================
*/
func TestFingerprintSharesIdenticalBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleportlink.txt")
	body := "1\t1\t175\t0\t0\t0\t7\t0\t0\n"
	writeStamped(t, path, body, time.Unix(1700000000, 0))
	first := Fingerprint(path)
	writeStamped(t, path, body, time.Unix(1800000000, 0))
	if second := Fingerprint(path); second != first {
		t.Fatalf("identical bytes fingerprinted apart:\n%s\n%s", first, second)
	}
}

/*
================
TestFingerprintAbsentFile

An absent file fingerprints stably, and differs from a present one.
================
*/
func TestFingerprintAbsentFile(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.txt")
	if Fingerprint(absent) != Fingerprint(absent) {
		t.Fatal("absent fingerprint is unstable")
	}
	present := filepath.Join(dir, "present.txt")
	writeStamped(t, present, "x", time.Unix(1700000000, 0))
	if Fingerprint(present) == Fingerprint(absent) {
		t.Fatal("present and absent fingerprints collide")
	}
}
