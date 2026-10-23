/*
===========================================================================

paramtest.go - the parameter definitions for tests

The definition table is the operator's source-data, never in the
repository. InstallForTests runs from a package's TestMain: it installs the
operator's table when source-data has it, else a test-authored table built
from the parameter classes the keepers rely on (the level range, the
percentage factors whose base is 100, the sentinel parameters whose ignore
value is -1, and wide clamps for the rest). Tests that pin a real bound call
RequireOperatorTable and skip without it.

===========================================================================
*/
package paramtest

import (
	"os"
	"testing"

	"opensro.online/server/internal/game/paramkeeper"
	"opensro.online/server/internal/testsupport/licensed"
)

// operatorTable records that InstallForTests installed the operator's table.
var operatorTable bool

// Parameter classes of the test-authored table, by parameter index.
var (
	// authoredRanges are the defined parameter indices, inclusive.
	authoredRanges = [][2]uint16{{0, 60}, {128, 141}, {143, 150}, {169, 200}, {202, 202}, {256, 259}}
	// authoredFactors start from a base of 100 (percent factors).
	authoredFactors = []uint16{34, 35, 36, 37, 50, 51, 140}
	// authoredSentinels ignore an insertion of -1.
	authoredSentinels = []uint16{23, 24, 50, 51, 56, 57, 58, 59, 169, 256, 257, 258}
)

// levelParameter is parameter 0, the character level.
const levelParameter = 0

// authoredMaximum is the wide clamp of every authored parameter but level.
const authoredMaximum = 2100000000

/*
================
Authored

The test-authored table.
================
*/
func Authored() map[uint16]paramkeeper.Definition {
	table := map[uint16]paramkeeper.Definition{}
	for _, span := range authoredRanges {
		for id := uint32(span[0]); id <= uint32(span[1]); id++ {
			table[uint16(id)] = paramkeeper.Definition{Maximum: authoredMaximum}
		}
	}
	table[levelParameter] = paramkeeper.Definition{Minimum: 1, Maximum: 140}
	for _, id := range authoredFactors {
		definition := table[id]
		definition.Base = 100
		table[id] = definition
	}
	for _, id := range authoredSentinels {
		definition := table[id]
		definition.Ignore = -1
		table[id] = definition
	}
	return table
}

/*
================
InstallForTests

Call from TestMain. An unreadable or invalid operator table is fatal there.
================
*/
func InstallForTests() {
	data, ok, err := licensed.ReadSourceData("server/" + paramkeeper.DefinitionsFile)
	if err != nil {
		panic(err)
	}
	if !ok {
		paramkeeper.Install(Authored())
		return
	}
	table, err := paramkeeper.ParseDefinitions(data)
	if err != nil {
		panic(err)
	}
	paramkeeper.Install(table)
	operatorTable = true
}

/*
================
RequireOperatorTable

Skips the test (or fails it under SRO_REQUIRE_GAME_DATA=1) unless the
operator's table is installed.
================
*/
func RequireOperatorTable(t testing.TB) {
	t.Helper()
	if operatorTable {
		return
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("parameter definitions are not in source-data (%s=1 requires them)", licensed.RequireEnv)
	}
	t.Skip("parameter definitions are not in source-data; the test-authored table has no real bounds")
}
