/*
===========================================================================

jupiter_boot_test.go - the Jupiter switches' boot and refusal legs

Port-only, not native. AppendJupiter and AppendJupiterTemple are the
boot composition's two Jupiter calls: off must be the native state
(neither population file is ever read), and on must refuse before
startup - the temple first when the field it requires is off, before
any temple data is read, then when the installed population is absent
or malformed. Which rows the populations admit is jupiter_test.go's
data level; this file pins the switch contract the boot sees.

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
TestJupiterOffNeverReads

Off returns the template untouched: an empty folder would fail any
read, and malformed files on disk are never opened either - both
switches short-circuit before the read.
================
*/
func TestJupiterOffNeverReads(t *testing.T) {
	template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
	dir := t.TempDir()
	for _, name := range []string{jupiterFieldPopulationFile, jupiterTemplePopulationFile} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a population"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := AppendJupiter(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("field off read the population: %v (%d nests)", err, len(got.Nests))
	}
	got, err = AppendJupiterTemple(template, dir, false)
	if err != nil || len(got.Nests) != len(template.Nests) {
		t.Fatalf("temple off read the population: %v (%d nests)", err, len(got.Nests))
	}
}

/*
================
TestJupiterTempleRequiresFieldBeforeAnyRead

Temple on without the field refuses with the requires error even when
a valid population is installed (any other outcome proves the file
was read first), and a malformed file meets the same refusal rather
than a parse error: the requirement is checked before the read, so
the refusal names the two settings in every posture.
================
*/
func TestJupiterTempleRequiresFieldBeforeAnyRead(t *testing.T) {
	template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
	t.Setenv(EnvJupiter, "off")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, jupiterTemplePopulationFile), jupiterSource(t, "MOB_JUPITER_SOLDIER", 86, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendJupiterTemple(template, dir, true); !jupiterRefusalNamesThePair(err) {
		t.Fatalf("valid temple population without the field: %v", err)
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, jupiterTemplePopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendJupiterTemple(template, dir, true); !jupiterRefusalNamesThePair(err) {
		t.Fatalf("malformed temple population without the field: %v", err)
	}
}

/*
================
jupiterRefusalNamesThePair

The boot refusal an operator reads: it must name both settings, so
a mistyped shard is corrected from the message alone.
================
*/
func jupiterRefusalNamesThePair(err error) bool {
	return err != nil && strings.Contains(err.Error(), "requires") &&
		strings.Contains(err.Error(), EnvJupiterTemple) && strings.Contains(err.Error(), EnvJupiter)
}

/*
================
TestJupiterOnLoadsInstalledPopulations

On: absent or malformed population data refuses the start, and the
installed populations join their own worlds.
================
*/
func TestJupiterOnLoadsInstalledPopulations(t *testing.T) {
	template := tombTemplate("MOB_JUPITER_SOLDIER", 115, 0)
	t.Setenv(EnvJupiter, "on")
	if _, err := AppendJupiter(template, t.TempDir(), true); err == nil {
		t.Fatal("field on admitted missing input")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, jupiterFieldPopulationFile), []byte("not a population"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendJupiter(template, dir, true); err == nil {
		t.Fatal("field on admitted malformed input")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, jupiterFieldPopulationFile), jupiterSource(t, "MOB_JUPITER_SOLDIER", 90, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := AppendJupiter(template, dir, true)
	if err != nil || len(got.Nests) != 2 {
		t.Fatalf("installed field population refused: %v", err)
	}
	if got.Nests[1].WorldCode != "GROUP_JUPITER_FIELD" {
		t.Fatalf("field nest joined %q, want GROUP_JUPITER_FIELD", got.Nests[1].WorldCode)
	}
	if _, err := AppendJupiterTemple(got, dir, true); err == nil {
		t.Fatal("temple on admitted missing input")
	}
	if err := os.WriteFile(filepath.Join(dir, jupiterTemplePopulationFile), jupiterSource(t, "MOB_JUPITER_SOLDIER", 93, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = AppendJupiterTemple(got, dir, true)
	if err != nil || len(got.Nests) != 3 {
		t.Fatalf("installed temple population refused: %v", err)
	}
	if got.Nests[2].WorldCode != "GROUP_JUPITER_A3" {
		t.Fatalf("temple nest joined %q, want GROUP_JUPITER_A3", got.Nests[2].WorldCode)
	}
}

/*
================
TestJupiterFlagWords
================
*/
func TestJupiterFlagWords(t *testing.T) {
	for word, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvJupiter, word)
		if got := JupiterEnabled(); got != want {
			t.Fatalf("field %q = %t, want %t", word, got, want)
		}
		t.Setenv(EnvJupiterTemple, word)
		if got := JupiterTempleEnabled(); got != want {
			t.Fatalf("temple %q = %t, want %t", word, got, want)
		}
	}
}
