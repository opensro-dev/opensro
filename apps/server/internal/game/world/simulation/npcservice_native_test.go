/*
===========================================================================

npcservice_native_test.go - native NPC codename service registration corpus

The fixture executes the original 4C64E6..4C78B3 classifier instructions.
It covers every codename literal, embedded substring matches, lower-case
variants and overlapping chains. Refdata inheritance and the Magic POP
binding are separate inputs, outside this codename classifier's contract.

===========================================================================
*/

package simulation

import (
	"encoding/json"
	"os"
	"testing"
)

/*
================
TestNpcServiceNativeCodenameCorpus
================
*/
func TestNpcServiceNativeCodenameCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/npc-services-native.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		BinarySHA256 string `json:"binary_sha256"`
		Cases        []struct {
			Codename string
			Options  []uint8
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.BinarySHA256 != "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290" || len(corpus.Cases) == 0 {
		t.Fatal("native service corpus has no cases or the wrong binary identity")
	}
	for _, fixture := range corpus.Cases {
		t.Run(fixture.Codename, func(t *testing.T) {
			want := NpcServices(0).With(fixture.Options...)
			if got := NpcServicesForCodename(fixture.Codename); got != want {
				t.Fatalf("services %#x, native %#x", uint64(got), uint64(want))
			}
		})
	}
}
