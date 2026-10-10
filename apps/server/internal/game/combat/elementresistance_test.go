/*
===========================================================================

elementresistance_test.go - Fire Shield's bgra writes the element resistances

===========================================================================
*/
package combat

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/paramkeeper"
)

/*
================
TestElementResistanceWritesFollowTheMask

595698..5957EE: one flat write of the value to 0x1B+i per mask bit i.
================
*/
func TestElementResistanceWritesFollowTheMask(t *testing.T) {
	all := ElementResistanceWrites(enterworld.SkillPassiveReat{Mask: 63, Value: 18})
	if len(all) != 6 {
		t.Fatalf("mask 63 wrote %d parameters", len(all))
	}
	for i, w := range all {
		want := paramkeeper.Write{Parameter: 0x1b + uint16(i), Channel: paramkeeper.Flat, Value: 18}
		if !reflect.DeepEqual(w, want) {
			t.Fatalf("write %d = %+v, want %+v", i, w, want)
		}
	}
	some := ElementResistanceWrites(enterworld.SkillPassiveReat{Mask: 0b100001, Value: 7})
	if len(some) != 2 || some[0].Parameter != 0x1b || some[1].Parameter != 0x20 {
		t.Fatalf("mask 0x21 wrote %+v", some)
	}
	if len(ElementResistanceWrites(enterworld.SkillPassiveReat{})) != 0 {
		t.Fatal("an empty mask wrote parameters")
	}
}
