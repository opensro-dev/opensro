/*
===========================================================================

companion_refusal_test.go - a refused companion restore names its record

The player sees S2 for every refusal of this kind; the log line is the
only place that says which companion failed and which check (#4019).

===========================================================================
*/
package enterworld

import (
	"errors"
	"strings"
	"testing"
)

/*
================
TestCompanionRefusalNamesTheMovedID

A fox stored under an id the catalogue moved (50853, now 51295) is
refused with both ids named; an unknown codename and a GID held twice
name their own check, and FailureCause keeps the detail off nothing but
the log.
================
*/
func TestCompanionRefusalNamesTheMovedID(t *testing.T) {
	t.Parallel()

	fox := &CharacterCOS{RefObjID: 50853, Codename: "COS_P_FOX_001", GID: 7}
	moved := &CharacterRef{RefObjID: 51295, Codename: "COS_P_FOX_001", TidWord: 3 << 11}
	cases := []struct {
		name   string
		ref    *CharacterRef
		cos    *CharacterCOS
		unique bool
		want   []string
	}{
		{"moved id", moved, fox, true, []string{"stored id 50853", "catalogue id 51295"}},
		{"unknown codename", nil, fox, true, []string{"not in the catalogue"}},
		{"gid held twice", moved, &CharacterCOS{RefObjID: 51295, Codename: "COS_P_FOX_001", GID: 7}, false, []string{"held by another"}},
	}
	for _, c := range cases {
		err := companionRefusal(c.cos, c.ref, 7, true, c.unique)
		for _, want := range c.want {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %v does not name %q", c.name, err, want)
			}
		}
	}

	result := FailureCause(nativeErrorInvalidRequest, "invalidCOSRestoration", errors.New("detail"))
	if result.cause != "detail" || result.Reason != "invalidCOSRestoration" || result.NativeErrorCode != 0x02 {
		t.Fatalf("FailureCause = %+v", result)
	}
	if blob, err := EnterWorldBlob(result); err != nil || strings.Contains(string(blob), "detail") {
		t.Fatalf("the cause reached the client blob: %s (%v)", blob, err)
	}
}
