/*
===========================================================================

definitions.go - the parameter definition table every keeper clamps by

Each parameter the server keeps has a minimum, a maximum, a fallback base
and an ignore sentinel. The table is the operator's, read from their own
server (DefinitionsFile in the projection's derived data); the GameWorld
installs it once at startup and refuses to start without it, since every
stat, speed and status clamp reads it. NativeDefinition answers from the
installed table.

===========================================================================
*/
package paramkeeper

import (
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"

	"opensro.online/server/internal/gamedata"
)

// DefinitionsFile is the definition table in the derived data.
const DefinitionsFile = "paramkeeper/definitions.json"

// definitionsFormat names the table's layout: [id, min, max, base, ignore]
// rows.
const definitionsFormat = "sro-paramkeeper-definitions-v1"

// installed is the table Install published; nil before any.
var installed atomic.Pointer[map[uint16]Definition]

/*
================
ParseDefinitions
================
*/
func ParseDefinitions(data []byte) (map[uint16]Definition, error) {
	var document struct {
		Format      string
		Definitions [][5]float64
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parameter definitions: %w", err)
	}
	if document.Format != definitionsFormat {
		return nil, fmt.Errorf("parameter definitions: format %q, want %q", document.Format, definitionsFormat)
	}
	table := make(map[uint16]Definition, len(document.Definitions))
	for i, row := range document.Definitions {
		id := row[0]
		if id < 0 || id > math.MaxUint16 || id != math.Trunc(id) || row[1] > row[2] {
			return nil, fmt.Errorf("parameter definitions: malformed row %d", i)
		}
		if _, exists := table[uint16(id)]; exists {
			return nil, fmt.Errorf("parameter definitions: duplicate parameter %v", id)
		}
		table[uint16(id)] = Definition{Minimum: float32(row[1]), Maximum: float32(row[2]), Base: float32(row[3]),
			Ignore: float32(row[4])}
	}
	return table, nil
}

/*
================
LoadDefinitions

The table from derivedDir. Its absence is an error: no keeper can clamp
without it.
================
*/
func LoadDefinitions(derivedDir string) (map[uint16]Definition, error) {
	data, err := gamedata.ReadDerived(derivedDir, DefinitionsFile)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("parameter definitions: no %s in the derived game data", DefinitionsFile)
	}
	return ParseDefinitions(data)
}

/*
================
Install

Publishes the table NativeDefinition answers from. Called once at startup,
before any keeper exists.
================
*/
func Install(table map[uint16]Definition) {
	installed.Store(&table)
}

/*
================
NativeDefinition

The installed definition of one parameter.
================
*/
func NativeDefinition(id uint16) (Definition, bool) {
	table := installed.Load()
	if table == nil {
		return Definition{}, false
	}
	definition, ok := (*table)[id]
	return definition, ok
}
