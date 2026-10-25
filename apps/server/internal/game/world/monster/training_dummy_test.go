/*
===========================================================================
training_dummy_test.go - optional references never mutate the native roster.
===========================================================================
*/
package monster

import "testing"

/*
================
TestTrainingDummyReferences
================
*/
func TestTrainingDummyReferences(t *testing.T) {
	base := MonsterRef{RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", Name: "Mangnyang", MaxHP: 54, TidWord: 0xc6}
	template := TemplateFromParts(map[uint32]MonsterRef{1933: base}, nil)
	off, err := AppendTrainingDummies(template, false)
	if err != nil || len(off.Refs) != 1 {
		t.Fatal("off changed native references", err)
	}
	on, err := AppendTrainingDummies(template, true)
	if err != nil || len(on.Refs) != 141 || len(template.Refs) != 1 || on.Refs[1933].MaxHP != 54 {
		t.Fatal("dummy roster changed native template", err)
	}
	for level := uint32(1); level <= 140; level++ {
		r := on.Refs[TrainingDummyRefBase+level]
		if !r.TrainingDummy || r.Level != uint8(level) || r.MaxHP != 100000000 || r.ExpToGive != 0 || r.Name != "Training Dummy" || r.NameStrID == "" {
			t.Fatal("invalid dummy", level, r)
		}
	}
	if _, err := AppendTrainingDummies(on, true); err == nil {
		t.Fatal("reference collision accepted")
	}
	if _, err := AppendTrainingDummies(Template{}, true); err == nil {
		t.Fatal("missing native model accepted")
	}
	for _, value := range []string{"", "off", "on", "true", "1"} {
		t.Setenv(EnvTrainingDummy, value)
		if TrainingDummyEnabled() != (value == "on" || value == "true" || value == "1") {
			t.Fatal("environment gate", value)
		}
	}
}
