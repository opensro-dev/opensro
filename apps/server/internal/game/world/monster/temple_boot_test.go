/*
===========================================================================

temple_boot_test.go - the temple switch's boot and refusal legs

Port-only, not native. AppendTemple is the boot composition's one
temple call: off must be the native state (the population file is
never read), and on must refuse before startup - first when the
cap-110 expansion it requires is off, before any temple data is
read, then when the installed population is absent or malformed.
Which rows the population admits is temple_test.go's data level;
this file pins the switch contract the boot sees.

===========================================================================
*/
package monster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestTempleOffNeverReads

Off returns the template untouched: an empty folder would fail any
read, and a malformed marker on disk is never opened either,
whatever the expansion setting - off short-circuits both.
================
*/
func TestTempleOffNeverReads(t *testing.T) {
	template := tombTemplate("MOB_GOD_SOLDIER", 70, 0)
	t.Setenv(EnvCap110Monsters, "off")
	got, err := AppendTemple(template, t.TempDir(), false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("off touched the population: %v (%d nests)", err, len(got.Nests))
	}
	t.Setenv(EnvCap110Monsters, "on")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templePopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = AppendTemple(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("off read the temple file: %v (%d nests)", err, len(got.Nests))
	}
}

/*
================
TestTempleRequiresCap110BeforeAnyRead

On without the expansion refuses with the requires error even when
a valid population is installed (any other outcome proves the file
was read first), and a malformed marker meets the same refusal
rather than a parse error: the requirement is checked before the
read, so the refusal names the two settings in every posture.
================
*/
func TestTempleRequiresCap110BeforeAnyRead(t *testing.T) {
	template := tombTemplate("MOB_GOD_SOLDIER", 70, 0)
	t.Setenv(EnvCap110Monsters, "off")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templePopulationFile), templeSource(t, "MOB_GOD_SOLDIER", 70, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendTemple(template, dir, true); !templeRefusalNamesThePair(err) {
		t.Fatalf("valid marker without the expansion: %v", err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templePopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendTemple(template, dir, true); !templeRefusalNamesThePair(err) {
		t.Fatalf("malformed marker without the expansion: %v", err)
	}
}

/*
================
templeRefusalNamesThePair

The boot refusal an operator reads: it must name both settings, so
a mistyped shard is corrected from the message alone.
================
*/
func templeRefusalNamesThePair(err error) bool {
	return err != nil && strings.Contains(err.Error(), "requires") &&
		strings.Contains(err.Error(), EnvCap110Temple) && strings.Contains(err.Error(), EnvCap110Monsters)
}

/*
================
TestTempleOnLoadsInstalledPopulation

On with the expansion: absent or malformed population data refuses
the start, and the installed population joins its band world.
================
*/
func TestTempleOnLoadsInstalledPopulation(t *testing.T) {
	template := tombTemplate("MOB_GOD_SOLDIER", 70, 0)
	t.Setenv(EnvCap110Monsters, "on")
	if _, err := AppendTemple(template, t.TempDir(), true); err == nil {
		t.Fatal("on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templePopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendTemple(template, dir, true); err == nil {
		t.Fatal("on admitted malformed input")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, templePopulationFile), templeSource(t, "MOB_GOD_SOLDIER", 70, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendTemple(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed population refused: %v", err)
	}
	if got.Nests[1].WorldCode != "INS_TEMPLE_70" {
		t.Fatalf("installed nest joined %q, want INS_TEMPLE_70", got.Nests[1].WorldCode)
	}
}

/*
================
TestTempleFlagWords
================
*/
func TestTempleFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvCap110Temple, word)
		if got := TempleEnabled(); got != want {
			t.Fatalf("%q = %t, want %t", word, got, want)
		}
	}
}
