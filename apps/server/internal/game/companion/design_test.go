/*
===========================================================================

design_test.go - the shared per-kind skill lists stay the workbook's

PetSkills and SkillsForPet hand out the package's static per-kind
lists (#1943): identical in content and order to the filter the
accessors used to run per call, one backing shared between calls,
and nothing allocated to hand them out.

===========================================================================
*/
package companion

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
filterSkillsByKind

The per-call filter the accessors ran before the lists were indexed:
one pet kind's rows in workbook order.
================
*/
func filterSkillsByKind(rows []SkillDef, kind string) []SkillDef {
	var out []SkillDef
	for _, s := range rows {
		if s.Pet == kind {
			out = append(out, s)
		}
	}
	return out
}

/*
================
sameSkillRows
================
*/
func sameSkillRows(t *testing.T, table, kind string, got, want []SkillDef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s %q: %d rows, want %d", table, kind, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s %q row %d differs: %+v vs %+v", table, kind, i, got[i], want[i])
		}
	}
	if len(got) != 0 && cap(got) != len(got) {
		t.Fatalf("%s %q: shared list capacity %d beyond length %d", table, kind, cap(got), len(got))
	}
}

/*
================
TestPetSkillsShareTheWorkbookLists

For every kind the workbook names (and kinds it does not), the
shared list equals the old filter's output row for row, and two
calls hand out the same backing.
================
*/
func TestPetSkillsShareTheWorkbookLists(t *testing.T) {
	kinds := map[string]bool{"": true, "NoSuchPet": true}
	for _, s := range D.Skills {
		kinds[s.Pet] = true
	}
	for _, s := range D.TreeSkills {
		kinds[s.Pet] = true
	}
	for _, pet := range D.Pets {
		kinds[pet.Name] = true
	}
	for kind := range kinds {
		sameSkillRows(t, "skills", kind, PetSkills(kind), filterSkillsByKind(D.Skills, kind))
		sameSkillRows(t, "tree", kind, treeSkillsByKind[kind], filterSkillsByKind(D.TreeSkills, kind))
		if got := PetSkills(kind); len(got) != 0 && &got[0] != &PetSkills(kind)[0] {
			t.Fatalf("%q: two calls do not share one list", kind)
		}
	}
}

/*
================
TestSkillsForPetSharesTheWorkbookLists

Both tree versions read the same shared lists the old filter built,
and a companion outside the design gets none.
================
*/
func TestSkillsForPetSharesTheWorkbookLists(t *testing.T) {
	for _, pet := range D.Pets {
		for _, version := range []uint8{0, 1, 2, 3} {
			p := &domain.CharacterCOS{
				Codename: pet.CodenamePrefix + "_017",
				Training: domain.PetTraining{TreeVersion: version},
			}
			rows := D.TreeSkills
			if version < 2 {
				rows = D.Skills
			}
			sameSkillRows(t, "pet", pet.Name, SkillsForPet(p), filterSkillsByKind(rows, pet.Name))
		}
	}
	stranger := &domain.CharacterCOS{Codename: "COS_T_HORSE1", Training: domain.PetTraining{TreeVersion: 2}}
	if SkillsForPet(stranger) != nil {
		t.Fatal("a companion outside the design has design skills")
	}
}

/*
================
TestPetSkillListsAllocateNothing

Handing out the shared lists is a map lookup: no rebuild, no copy.
================
*/
func TestPetSkillListsAllocateNothing(t *testing.T) {
	kind := D.Pets[0].Name
	pet := &domain.CharacterCOS{
		Codename: D.Pets[0].CodenamePrefix + "_017",
		Training: domain.PetTraining{TreeVersion: 2},
	}
	var sink []SkillDef
	skillsAllocs := testing.AllocsPerRun(100, func() { sink = PetSkills(kind) })
	petAllocs := testing.AllocsPerRun(100, func() { sink = SkillsForPet(pet) })
	if len(sink) == 0 {
		t.Fatal("the pin measured an empty list")
	}
	t.Logf("allocs: PetSkills %v, SkillsForPet %v", skillsAllocs, petAllocs)
	if skillsAllocs != 0 || petAllocs != 0 {
		t.Fatalf("handing out the static lists allocates: PetSkills %v, SkillsForPet %v", skillsAllocs, petAllocs)
	}
}
