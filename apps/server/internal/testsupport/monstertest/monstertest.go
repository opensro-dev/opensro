/*
===========================================================================

monstertest.go - the monster population evidence for tests

The evidence is the operator's source-data (source-data/server/monster),
never in the repository. Evidence parses it once per test binary and hands
every caller the same read-only tables; a test that needs it skips without
it (or fails under SRO_REQUIRE_GAME_DATA=1). Tests of a single rule build
their own small rows instead.

===========================================================================
*/
package monstertest

import (
	"os"
	"sync"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/licensed"
)

var loaded struct {
	once     sync.Once
	evidence *monster.Evidence
	err      error
}

/*
================
load

Every table of the set, or nil when one is absent.
================
*/
func load() (*monster.Evidence, error) {
	data := map[string][]byte{}
	for _, name := range monster.EvidenceFiles() {
		bytes, ok, err := licensed.ReadSourceData("server/" + name)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		data[name] = bytes
	}
	return monster.EvidenceFromTables(data)
}

/*
================
Evidence

The operator's evidence. Skips the test (or fails it under
SRO_REQUIRE_GAME_DATA=1) when it is not in source-data; an unreadable or
invalid set fails it.
================
*/
func Evidence(t testing.TB) *monster.Evidence {
	t.Helper()
	loaded.once.Do(func() { loaded.evidence, loaded.err = load() })
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	if loaded.evidence != nil {
		return loaded.evidence
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("monster evidence is not in source-data/server/monster (%s=1 requires it)", licensed.RequireEnv)
	}
	t.Skip("monster evidence is not in source-data/server/monster")
	return nil
}

/*
================
Template

The shipped template over textdataDir with the operator's evidence.
================
*/
func Template(t testing.TB, textdataDir string) monster.Template {
	t.Helper()
	template, err := monster.LoadTemplate(textdataDir, Evidence(t))
	if err != nil {
		t.Fatal(err)
	}
	return template
}
