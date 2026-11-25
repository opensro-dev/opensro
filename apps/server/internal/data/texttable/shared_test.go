/*
===========================================================================

shared_test.go - the shared-parse fingerprint names fresh files by bytes

A loader that rewrites a table in place (the Alexandria portal test
staging a link condition) must never read the previous parse back: while
a file is fresh, the fingerprint changes with its contents even when
path, size and modification time all stand still. A settled file is
named by path, size and stamp alone (git's racy-clean rule), which is
what keeps the fingerprint from hashing the shipped tables per loader.

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

A fresh file, same path, same size, same modification time, different
bytes (one digit of a link condition): the fingerprints must differ, or
a shared parse serves the old table. Fails on the size+mtime
fingerprint, because the file's stamp stands still across the rewrite.
================
*/
func TestFingerprintNamesTheBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleportlink.txt")
	stamp := time.Now()
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t7\t0\t0\n", stamp)
	first := Fingerprint(path)
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t4\t1\t0\n", stamp)
	if second := Fingerprint(path); second == first {
		t.Fatalf("fingerprint collides for different bytes:\n%s", first)
	}
}

/*
================
TestFingerprintSharesSettledFile

A settled file is named by path, size and stamp: the same file read
twice keeps one fingerprint (one shared parse), and even a same-size
rewrite backdated to the same old stamp keeps it - the accepted
racy-clean limit, pinned so widening the rule is a deliberate change.
Freshness is what names bytes (TestFingerprintNamesTheBytes); nothing
stages a table with a backdated stamp.
================
*/
func TestFingerprintSharesSettledFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleportlink.txt")
	stamp := time.Unix(1700000000, 0)
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t7\t0\t0\n", stamp)
	first := Fingerprint(path)
	if second := Fingerprint(path); second != first {
		t.Fatalf("settled file fingerprinted apart:\n%s\n%s", first, second)
	}
	writeStamped(t, path, "1\t1\t175\t0\t0\t0\t4\t1\t0\n", stamp)
	if second := Fingerprint(path); second != first {
		t.Fatalf("backdated rewrite should keep the settled fingerprint:\n%s\n%s", first, second)
	}
}

/*
================
TestFingerprintNamesNewerStamp

The same bytes under a newer settled stamp fingerprint apart: the
stamp is part of the key, so a re-extracted projection reparses once
instead of trusting a stale parse under a recycled path.
================
*/
func TestFingerprintNamesNewerStamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleportlink.txt")
	body := "1\t1\t175\t0\t0\t0\t7\t0\t0\n"
	writeStamped(t, path, body, time.Unix(1700000000, 0))
	first := Fingerprint(path)
	writeStamped(t, path, body, time.Unix(1800000000, 0))
	if second := Fingerprint(path); second == first {
		t.Fatalf("newer stamp kept the fingerprint:\n%s", first)
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
	first, second := Fingerprint(absent), Fingerprint(absent)
	if first != second {
		t.Fatal("absent fingerprint is unstable")
	}
	present := filepath.Join(dir, "present.txt")
	writeStamped(t, present, "x", time.Unix(1700000000, 0))
	if Fingerprint(present) == Fingerprint(absent) {
		t.Fatal("present and absent fingerprints collide")
	}
}
