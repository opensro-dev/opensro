/*
===========================================================================
damagetomp_test.go - original-machine damage redirection corpus
===========================================================================
*/
package combat

import (
	"encoding/json"
	"os"
	"testing"
)

/*
================
TestDamageToMPNativeCorpus
================
*/
func TestDamageToMPNativeCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/native-damage-to-mp.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct{ Damage, Percent, MP, HPDamage, MPDamage uint32 }
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) != 2304 {
		t.Fatal("incomplete native capture")
	}
	for _, c := range corpus.Cases {
		hp, mp := DamageToMP(c.Damage, c.MP, c.Percent)
		if hp != c.HPDamage || mp != c.MPDamage {
			t.Fatalf("%+v: got HP %d MP %d", c, hp, mp)
		}
	}
}

/*
================
TestDamageToMPHitEligibility
================
*/
func TestDamageToMPHitEligibility(t *testing.T) {
	for _, hit := range []Result{{Damage: 100}, {Damage: 100, PhysicalDamage: 100, Blocked: true}, {MagicalDamage: 100}} {
		got, spent := RedirectDamageToMP(hit, 100, 20)
		if got != hit || spent != 0 {
			t.Fatal("ineligible hit redirected", hit, got, spent)
		}
	}
	for _, hit := range []Result{{Damage: 100, PhysicalDamage: 100}, {Damage: 100, MagicalDamage: 100}, {Damage: 100, PhysicalDamage: 50, MagicalDamage: 50}} {
		got, spent := RedirectDamageToMP(hit, 100, 20)
		if got.Damage != 80 || spent != 30 || got.PhysicalDamage != hit.PhysicalDamage || got.MagicalDamage != hit.MagicalDamage {
			t.Fatal("incorrect lane processing", hit, got, spent)
		}
	}
}
