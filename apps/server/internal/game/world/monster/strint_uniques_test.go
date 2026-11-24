/*
===========================================================================

strint_uniques_test.go - the build-locked copies' references and anchors

Pins the shape of SRO_STR_INT_UNIQUES: off changes nothing, on adds one STR
and one INT copy of each roster unique over the species' whole anchor list,
each in a hive of its own, so all three can stand at once and only the spawn
choice keeps them off the same point.

===========================================================================
*/
package monster

import (
	"reflect"
	"strings"
	"testing"
)

// strIntTestAnchors is how many spawn points each fixture species owns.
const strIntTestAnchors = 3

/*
================
strIntTestTemplate

A minimal template carrying every roster base: one reference each and one
hive of three anchors, which is what the split deals out.
================
*/
func strIntTestTemplate() Template {
	refs := make(map[uint32]MonsterRef, len(strIntUniqueBases))
	var nests []NestRow
	for index, codename := range strIntUniqueBases {
		id := uint32(1000 + index)
		refs[id] = MonsterRef{
			RefObjID: id, Codename: codename, NameStrID: "SN_" + codename, Name: "Base " + codename,
			ModelPath: "mob/test.bsr", Level: 20, MaxHP: 5000, Country: 0, MonsterType: 3,
		}
		for anchor := 0; anchor < strIntTestAnchors; anchor++ {
			nests = append(nests, NestRow{
				SpawnPoint:         SpawnPoint{RefObjID: id, RegionID: 2000, X: float64(10 * (anchor + 1)), Y: 2, Z: 3},
				PolicyPinned:       true,
				MaxCount:           1,
				Respawn:            true,
				RespawnDelayMinSec: 1200,
				RespawnDelayMaxSec: 1400,
				HiveKey:            "hive-" + codename,
				HiveMaxCount:       1,
				HiveOrder:          anchor,
			})
		}
	}
	return TemplateFromParts(refs, nests)
}

/*
================
TestStrIntUniquesOffIsNative

Off leaves the loaded population byte-identical: same references, same
nests, same hives.
================
*/
func TestStrIntUniquesOffIsNative(t *testing.T) {
	base := strIntTestTemplate()
	got, err := AppendStrIntUniques(base, false)
	if err != nil {
		t.Fatalf("AppendStrIntUniques(off): %v", err)
	}
	if !reflect.DeepEqual(got.Nests, base.Nests) || !reflect.DeepEqual(got.Refs, base.Refs) {
		t.Fatal("off changed the loaded population")
	}
}

/*
================
TestStrIntUniquesShareEveryAnchor

On adds one copy per unique and per build, and every copy keeps the whole
anchor list in a hive of its own: the copies can be up together, and the
population keeps them off the same point at spawn time rather than by
taking anchors away from anyone.
================
*/
func TestStrIntUniquesShareEveryAnchor(t *testing.T) {
	base := strIntTestTemplate()
	got, err := AppendStrIntUniques(base, true)
	if err != nil {
		t.Fatalf("AppendStrIntUniques(on): %v", err)
	}
	if want := len(base.Refs) + 2*len(strIntUniqueBases); len(got.Refs) != want {
		t.Fatalf("references = %d, want %d", len(got.Refs), want)
	}
	if want := len(base.Nests) * 3; len(got.Nests) != want {
		t.Fatalf("nests = %d, want %d (one full copy of the anchors per build)", len(got.Nests), want)
	}
	seen := make(map[string]bool, len(got.Refs))
	for _, ref := range got.Refs {
		seen[ref.Codename] = true
	}
	for _, codename := range strIntUniqueBases {
		baseID := refIDInTemplate(t, base, codename)
		baseRef := got.Refs[baseID]
		for _, variant := range strIntVariants {
			copyCode := codename + variant.suffix
			if !seen[copyCode] {
				t.Fatalf("missing copy %s", copyCode)
			}
			ref := refByCodename(t, got, copyCode)
			if ref.BaseCodename() != codename || ref.VariantOf != codename {
				t.Fatalf("%s base codename = %q / %q, want %s", copyCode, ref.BaseCodename(), ref.VariantOf, codename)
			}
			if ref.UniqueBuild != variant.build {
				t.Fatalf("%s build = %d, want %d", copyCode, ref.UniqueBuild, variant.build)
			}
			if !strings.HasSuffix(ref.Name, "("+variant.name+")") {
				t.Fatalf("%s name = %q, want a (%s) suffix", copyCode, ref.Name, variant.name)
			}
			if ref.NameStrID == baseRef.NameStrID || ref.NameStrID == "" {
				t.Fatalf("%s name key = %q, want its own non-empty key", copyCode, ref.NameStrID)
			}
			// Everything the reward path reads stays the base's.
			if ref.Level != baseRef.Level || ref.MaxHP != baseRef.MaxHP || ref.Country != baseRef.Country ||
				ref.MonsterType != baseRef.MonsterType || ref.ModelPath != baseRef.ModelPath {
				t.Fatalf("%s is not a clone of %s: %+v", copyCode, codename, ref)
			}
			if !RarityIsUnique(ref.MonsterType) {
				t.Fatalf("%s is not classified as a unique", copyCode)
			}
		}
		builds := []string{codename, codename + "_STR", codename + "_INT"}
		hives := make(map[string]bool, len(builds))
		var anchors []float64
		for _, build := range builds {
			ref := refByCodename(t, got, build)
			nests := nestsOfRef(got.Nests, ref.RefObjID)
			if len(nests) != strIntTestAnchors {
				t.Fatalf("%s owns %d anchors, want all %d", build, len(nests), strIntTestAnchors)
			}
			for _, nest := range nests {
				if nest.UniqueShare != codename {
					t.Fatalf("%s nest share = %q, want %s", build, nest.UniqueShare, codename)
				}
				if nest.HiveMaxCount != 1 || !nest.Respawn {
					t.Fatalf("%s lost its unique lifecycle: %+v", build, nest)
				}
			}
			// Every build's hive is its own, and all three hold the same
			// anchor list.
			if hive := nests[0].HiveKey; hive == "" || hives[hive] {
				t.Fatalf("%s hive = %q, want a hive of its own", build, nests[0].HiveKey)
			}
			hives[nests[0].HiveKey] = true
			if anchors == nil {
				anchors = nestAnchors(nests)
			} else if got := nestAnchors(nests); !sameFloats(got, anchors) {
				t.Fatalf("%s anchors = %v, want the species' full list %v", build, got, anchors)
			}
		}
	}
}

/*
================
nestAnchors
================
*/
func nestAnchors(nests []NestRow) []float64 {
	out := make([]float64, 0, len(nests))
	for _, nest := range nests {
		out = append(out, nest.SpawnPoint.X)
	}
	return out
}

/*
================
sameFloats
================
*/
func sameFloats(left, right []float64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

/*
================
TestStrIntUniquesRefuseTheOtherBuild

A strictly physical total (strength above intelligence) is the STR build;
everything else, a balanced total included, is the INT build. The shipped
unique refuses nobody.
================
*/
func TestStrIntUniquesRefuseTheOtherBuild(t *testing.T) {
	str := MonsterRef{UniqueBuild: UniqueBuildSTR}
	intel := MonsterRef{UniqueBuild: UniqueBuildINT}
	shipped := MonsterRef{}
	for _, tc := range []struct {
		name            string
		ref             MonsterRef
		strength, magic int64
		want            bool
	}{
		{"physical attacks STR copy", str, 300, 100, false},
		{"magical attacks STR copy", str, 100, 300, true},
		{"balanced attacks STR copy", str, 200, 200, true},
		{"physical attacks INT copy", intel, 300, 100, true},
		{"magical attacks INT copy", intel, 100, 300, false},
		{"balanced attacks INT copy", intel, 200, 200, false},
		{"physical attacks shipped unique", shipped, 300, 100, false},
		{"magical attacks shipped unique", shipped, 100, 300, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ref.AttackRefused(tc.strength, tc.magic); got != tc.want {
				t.Fatalf("AttackRefused(%d, %d) = %v, want %v", tc.strength, tc.magic, got, tc.want)
			}
		})
	}
}

/*
================
TestStrIntUniquesMissingBaseFails

A roster entry the installed data does not carry is a boot error, not a
silently smaller roster.
================
*/
func TestStrIntUniquesMissingBaseFails(t *testing.T) {
	base := strIntTestTemplate()
	delete(base.Refs, 1000)
	if _, err := AppendStrIntUniques(base, true); err == nil {
		t.Fatal("a missing base unique was accepted")
	}
}

/*
================
TestStrIntUniquesReferenceCollisionFails
================
*/
func TestStrIntUniquesReferenceCollisionFails(t *testing.T) {
	base := strIntTestTemplate()
	base.Refs[strIntRefBase] = MonsterRef{RefObjID: strIntRefBase, Codename: "MOB_TAKEN"}
	if _, err := AppendStrIntUniques(base, true); err == nil {
		t.Fatal("an occupied reference id was overwritten")
	}
}

/*
================
refIDInTemplate
================
*/
func refIDInTemplate(t *testing.T, template Template, codename string) uint32 {
	t.Helper()
	id, ok := refIDByCodename(template.Refs, codename)
	if !ok {
		t.Fatalf("template has no %s", codename)
	}
	return id
}

/*
================
refByCodename
================
*/
func refByCodename(t *testing.T, template Template, codename string) MonsterRef {
	t.Helper()
	return template.Refs[refIDInTemplate(t, template, codename)]
}
