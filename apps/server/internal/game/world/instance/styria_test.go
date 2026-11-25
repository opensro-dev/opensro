/*
===========================================================================

styria_test.go - the Styria Clash arena world's catalogue shape

===========================================================================
*/
package instance

import (
	"testing"

	"opensro.online/server/internal/game/world/styria"
)

/*
================
TestStyriaDefinitionShape

World 107 as its later-client row authors it: instanced (type 1),
one layer, no resident limit. Registered, it resolves by id and by
the row's own identity; unregistered, the shipped catalogue has
never heard of it.
================
*/
func TestStyriaDefinitionShape(t *testing.T) {
	definitions := StyriaDefinitions()
	if len(definitions) != 1 {
		t.Fatalf("definitions = %+v, want the one arena world", definitions)
	}
	definition := definitions[0]
	if definition.ID != DefinitionID(styria.WorldID) || definition.CodeName != styria.WorldCodeName {
		t.Fatalf("identity = %d %q, want %d %q", definition.ID, definition.CodeName, styria.WorldID, styria.WorldCodeName)
	}
	if definition.NativeType != 1 || definition.LayerLimit != 1 || definition.PlayerLimit != 0 {
		t.Fatalf("shape = type %d, layers %d, limit %d; want the authored 1/1/0",
			definition.NativeType, definition.LayerLimit, definition.PlayerLimit)
	}
	if err := RegisterPortOnly(definitions); err != nil {
		t.Fatal(err)
	}
	got, ok := Lookup(DefinitionID(styria.WorldID))
	if !ok || got != definition {
		t.Fatalf("lookup = %+v, %v", got, ok)
	}
}
