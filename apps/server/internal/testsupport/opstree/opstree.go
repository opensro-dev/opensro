/*
===========================================================================

opstree.go - tests that check the code against the private ops tree

The deployment tooling (Nomad job templates, release controls) lives in a
private repository. The release build lays that tree into the module as
ops/, beside the code it deploys, and runs the tests that compare the two
there (the ops repository's release-tests step). A public checkout has no
ops/, so those tests skip with a reason instead of failing.

===========================================================================
*/
package opstree

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// moduleLevels is how far this file sits below the module root
// (internal/testsupport/opstree).
const moduleLevels = 3

/*
================
Require

The ops tree's directory inside the module, or a skip when this checkout
has none. Every caller finds its files below the returned path.
================
*/
func Require(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("opstree: cannot locate the module")
	}
	root := filepath.Dir(file)
	for i := 0; i < moduleLevels; i++ {
		root = filepath.Dir(root)
	}
	dir := filepath.Join(root, "ops")
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		t.Skip("needs the private ops tree beside the code (the release build lays it in as ops/)")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("opstree: %s is not a directory", dir)
	}
	return dir
}
