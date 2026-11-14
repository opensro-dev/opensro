/*
===========================================================================

portonly.go - port-only world definitions, registered behind their switches

The shipped catalogue (catalog.go) is the v1.150 table extract and never
grows: optional port-only content (the Pharaoh Tombs, #1750; the temple
bands, #1800 step 3; the Jupiter worlds, #3267; the Muhan wave worlds,
#3273) registers its own world definitions here while its
bands, #1800 step 3; the Jupiter worlds, #3267) registers its own world definitions here while its
switch is on, so Lookup
and the registry builders see one catalogue. Registration is idempotent
for identical rows and refuses anything that collides with a shipped row
or with a different row already registered under the same id.

===========================================================================
*/
package instance

import (
	"fmt"
	"sync"

	"opensro.online/server/internal/game/world/arabia"
	"opensro.online/server/internal/game/world/jupiter"
	"opensro.online/server/internal/game/world/muhan"
	"opensro.online/server/internal/game/world/pharaohtomb"
	"opensro.online/server/internal/game/world/temple"
)

// The Pharaoh Tombs' world identities: the source database's per-tomb
// world ids (57-59), unused by the shipped 41 rows, carried as the wire
// identity so the gates' authored destination worlds name them directly.
const (
	PharaohTomb1ID DefinitionID = pharaohtomb.World1ID
	PharaohTomb2ID DefinitionID = pharaohtomb.World2ID
	PharaohTomb3ID DefinitionID = pharaohtomb.World3ID
)

// The temple bands' layer shape (port-only, #1800 step 3): the source
// world table rows for 63-84 are native type 1 with fifty layers; a
// layer's resident limit rides each world row (the leaf's PlayerLimit:
// four on the LEVEL_1/_2 worlds of a band, eight on LEVEL_3/_4).
const templeLayerLimit = 50

var portOnly struct {
	sync.RWMutex
	rows []Definition
	byID map[DefinitionID]Definition
}

/*
================
PharaohTombDefinitions

The three port-only permanent tomb worlds, in tomb order. Type zero,
one resident layer, no player limit: the default world's own shape.
================
*/
func PharaohTombDefinitions() []Definition {
	worlds := pharaohtomb.Worlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:         DefinitionID(world.ID),
			CodeName:   world.CodeName,
			NativeType: 0,
			LayerLimit: 1,
		}
	}
	return definitions
}

/*
================
TempleDefinitions

The 22 port-only temple band worlds, in band order. Native type 1
(instanced, never opened at boot), layered per party, each layer
holding its world row's resident limit.
================
*/
func TempleDefinitions() []Definition {
	worlds := temple.Worlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:          DefinitionID(world.ID),
			CodeName:    world.CodeName,
			NativeType:  1,
			LayerLimit:  templeLayerLimit,
			PlayerLimit: uint16(world.PlayerLimit),
		}
	}
	return definitions
}

/*
================
ArabiaDefinitions

The one port-only Arabia dungeon world. Type zero, one resident
layer, no player limit: the default world's own shape.
================
*/
func ArabiaDefinitions() []Definition {
	worlds := arabia.Worlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:         DefinitionID(world.ID),
			CodeName:   world.CodeName,
			NativeType: 0,
			LayerLimit: 1,
		}
	}
	return definitions
}

/*
================
JupiterFieldDefinitions

The port-only Jupiter field world (port-only, #3267). Type zero, one
resident layer, no player limit: the default world's own shape, like
the Pharaoh Tombs' permanent worlds.
================
*/
func JupiterFieldDefinitions() []Definition {
	worlds := jupiter.FieldWorlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:         DefinitionID(world.ID),
			CodeName:   world.CodeName,
			NativeType: 0,
			LayerLimit: 1,
		}
	}
	return definitions
}

/*
================
JupiterTempleDefinitions

The six port-only Jupiter temple worlds of the two Mirror Dimension
sets (#3267), in id order. The later client's rows are ordinary
outdoor worlds, so the shape is the field's: type zero, one resident
layer, no player limit. Which temple copy a gate admits is the
admission slice's; these register beside the field while
SRO_JUPITER_TEMPLE is on.
================
*/
func JupiterTempleDefinitions() []Definition {
	worlds := jupiter.TempleWorlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:         DefinitionID(world.ID),
			CodeName:   world.CodeName,
			NativeType: 0,
			LayerLimit: 1,
		}
	}
	return definitions
}

/*
================
MuhanDefinitions

The port-only Muhan wave worlds (#3273), in id order. The later
server's rows are ordinary permanent worlds, so the shape is the
Jupiter field's: type zero, one resident layer, no player limit.
The unbanded shells (99-102) bind no region and author no waves, so
they stay unregistered. Which arena a player enters is the entry
slice's; these register while SRO_MUHAN is on.
*/
func MuhanDefinitions() []Definition {
	worlds := muhan.Worlds()
	definitions := make([]Definition, len(worlds))
	for index, world := range worlds {
		definitions[index] = Definition{
			ID:         DefinitionID(world.ID),
			CodeName:   world.CodeName,
			NativeType: 0,
			LayerLimit: 1,
		}
	}
	return definitions
}

/*
================
RegisterPortOnly

Adds port-only definitions to the catalogue Lookup and Definitions see.
A row whose id is already shipped, or already registered with different
content, is refused; re-registering identical rows is a no-op, so every
enabling caller may register without coordinating.
================
*/
func RegisterPortOnly(definitions []Definition) error {
	portOnly.Lock()
	defer portOnly.Unlock()
	if portOnly.byID == nil {
		portOnly.byID = make(map[DefinitionID]Definition)
	}
	for _, row := range definitions {
		if row.ID == 0 || row.CodeName == "" {
			return fmt.Errorf("port-only world has no identity: %+v", row)
		}
		if _, shippedRow := lookupShipped(row.ID); shippedRow {
			return fmt.Errorf("port-only world %d collides with a shipped world", row.ID)
		}
		if existing, ok := portOnly.byID[row.ID]; ok {
			if existing != row {
				return fmt.Errorf("port-only world %d re-registered with different content", row.ID)
			}
			continue
		}
		portOnly.byID[row.ID] = row
		portOnly.rows = append(portOnly.rows, row)
	}
	return nil
}

/*
================
Definitions

The whole catalogue: the shipped rows in authored order, then the
registered port-only rows in registration order.
================
*/
func Definitions() []Definition {
	portOnly.RLock()
	defer portOnly.RUnlock()
	out := Shipped()
	return append(out, portOnly.rows...)
}

/*
================
lookupPortOnly
================
*/
func lookupPortOnly(id DefinitionID) (Definition, bool) {
	portOnly.RLock()
	defer portOnly.RUnlock()
	row, ok := portOnly.byID[id]
	return row, ok
}
