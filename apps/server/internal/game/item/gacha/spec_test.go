/*
===========================================================================

spec_test.go - the [BIIV] prize property spec parser

The native free-param rules: the marker, the four letters, the M triple,
the O clamp, the five-token cap and the duplicate refusals.

===========================================================================
*/

package gacha

import "testing"

/*
================
TestParseSpecReadsTheShippedForms
================
*/
func TestParseSpecReadsTheShippedForms(t *testing.T) {
	spec, err := ParseSpec("[BIIV]<M:strv,1,5><M:intv,1,5><O:0>")
	if err != nil {
		t.Fatal(err)
	}
	options := spec.MagicOptions()
	if len(options) != 2 || options[0].Tag != 0x73747276 || options[0].Level != 1 || options[0].Value != 5 {
		t.Fatalf("options %+v", options)
	}
	if plus, ok := spec.Plus(); !ok || plus != 0 {
		t.Fatalf("plus %d ok=%v", plus, ok)
	}
	if _, ok := spec.Variance(); ok {
		t.Fatal("variance override without a V token")
	}
	spec, err = ParseSpec("[BIIV]<V:1023><D:40><O:30>")
	if err != nil {
		t.Fatal(err)
	}
	variance, _ := spec.Variance()
	durability, _ := spec.Durability()
	plus, _ := spec.Plus()
	if variance != 1023 || durability != 40 || plus != specMaxPlus {
		t.Fatalf("V %d D %d O %d, want 1023 40 and O clamped to 12", variance, durability, plus)
	}
	if spec, err := ParseSpec("[BIIV]"); err != nil || len(spec.Tokens) != 0 {
		t.Fatalf("an empty spec = %+v, %v", spec, err)
	}
}

/*
================
TestParseSpecRefusesWhatTheNativeParserAsserts
================
*/
func TestParseSpecRefusesWhatTheNativeParserAsserts(t *testing.T) {
	for _, text := range []string{
		"<O:3>",
		"[BIIV]<O:3><O:3>",
		"[BIIV]<M:str,1,3><M:str,2,3>",
		"[BIIV]<M:str,1>",
		"[BIIV]<M:STR,1,3>",
		"[BIIV]<M:str,0,3>",
		"[BIIV]<M:str,1,0>",
		"[BIIV]<Q:1>",
		"[BIIV]<O:x>",
		"[BIIV]<O:1",
		"[BIIV]junk<O:1>",
		"[BIIV]<M:a,1,1><M:b,1,1><M:c,1,1><M:d,1,1><M:e,1,1><M:f,1,1>",
	} {
		if _, err := ParseSpec(text); err == nil {
			t.Fatalf("ParseSpec(%q) accepted", text)
		}
	}
	// Different M options may repeat the letter.
	if _, err := ParseSpec("[BIIV]<M:str,1,3><M:int,1,3>"); err != nil {
		t.Fatal(err)
	}
}
